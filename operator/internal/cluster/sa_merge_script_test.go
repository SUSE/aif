/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// stubKubectl simulates the kubectl calls the SA-merge script makes against a
// state directory:
//
//	helm-sas   space-separated names of Helm-labelled ServiceAccounts
//	sa/<name>  one imagePullSecrets name per line (absent file = none)
//	pods       pre-rendered output of the script's pod listing
//	log        appended "patch <sa>" / "delete <pod>" lines
const stubKubectl = `#!/bin/sh
shift 2
verb=$1; shift
case "$verb" in
get)
  kind=$1; shift
  if [ "$kind" = sa ]; then
    if [ "$1" = "-l" ]; then cat "$STATE/helm-sas"; exit 0; fi
    if [ -f "$STATE/sa/$1" ]; then cat "$STATE/sa/$1"; fi
    exit 0
  fi
  if [ "$kind" = pods ]; then cat "$STATE/pods"; exit 0; fi
  ;;
patch)
  echo "patch $2" >> "$STATE/log"
  printf '%s\n' "$5" | tr '{},[]' '\n\n\n\n\n' | sed -n 's/^"name":"\(.*\)"$/\1/p' > "$STATE/sa/$2"
  ;;
delete)
  echo "delete $2" >> "$STATE/log"
  ;;
esac
`

// renderedMergeScript extracts the merge script from the rendered Job.
func renderedMergeScript(t *testing.T, secretNames []string) string {
	t.Helper()
	manifests, err := buildSAMergeResources("demo-ns", secretNames, "kubectl:test")
	if err != nil {
		t.Fatalf("buildSAMergeResources: %v", err)
	}
	for _, doc := range strings.Split(manifests, "\n---\n") {
		var out map[string]any
		if err := yaml.Unmarshal([]byte(doc), &out); err != nil || out["kind"] != "Job" {
			continue
		}
		spec, _ := out["spec"].(map[string]any)
		tmpl, _ := spec["template"].(map[string]any)
		podSpec, _ := tmpl["spec"].(map[string]any)
		containers, _ := podSpec["containers"].([]any)
		if len(containers) != 1 {
			t.Fatalf("Job: want 1 container, got %d", len(containers))
		}
		args, _ := containers[0].(map[string]any)["args"].([]any)
		script, _ := args[0].(string)
		return script
	}
	t.Fatal("no Job document in rendered SA-merge resources")
	return ""
}

type mergeScriptState struct {
	helmSAs string
	sas     map[string][]string
	pods    []string // name|serviceAccount|managedBy|podSecrets,|waitingReasons,
}

// runMergeScript runs the rendered script against the stub and returns the
// logged patch/delete actions.
func runMergeScript(t *testing.T, secretNames []string, st mergeScriptState) []string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	stateDir := filepath.Join(dir, "state")
	for _, d := range []string{binDir, filepath.Join(stateDir, "sa")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(binDir, "kubectl"), stubKubectl, 0o755)
	write(filepath.Join(stateDir, "helm-sas"), st.helmSAs, 0o644)
	write(filepath.Join(stateDir, "pods"), strings.Join(st.pods, "\n")+"\n", 0o644)
	write(filepath.Join(stateDir, "log"), "", 0o644)
	for name, secrets := range st.sas {
		write(filepath.Join(stateDir, "sa", name), strings.Join(secrets, "\n")+"\n", 0o644)
	}

	cmd := exec.Command(sh, "-c", renderedMergeScript(t, secretNames))
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "STATE="+stateDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("merge script failed: %v\n%s", err, out)
	}
	logged, err := os.ReadFile(filepath.Join(stateDir, "log"))
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, line := range strings.Split(string(logged), "\n") {
		if line != "" {
			actions = append(actions, line)
		}
	}
	return actions
}

func containsAction(actions []string, verb, name string) bool {
	for _, a := range actions {
		if a == verb+" "+name {
			return true
		}
	}
	return false
}

const scriptTestSecret = "aif-custom-pull-private-charts"

// A chart ServiceAccount created after the one-shot Job ran is patched by a
// later run; its pod (not Helm-labelled, as many charts leave pods unlabelled)
// started without the secret and must be recreated.
func TestMergeScript_RecreatesUnlabelledPodAfterPatchingItsSA(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		helmSAs: "qdrant",
		sas:     map[string][]string{"default": {scriptTestSecret}},
		pods:    []string{"qdrant-0|qdrant|||ImagePullBackOff,"},
	})
	if !containsAction(actions, "patch", "qdrant") {
		t.Errorf("expected SA qdrant to be patched, actions: %v", actions)
	}
	if !containsAction(actions, "delete", "qdrant-0") {
		t.Errorf("expected pod qdrant-0 to be recreated, actions: %v", actions)
	}
}

// The SA was patched on an earlier run but the pod was not recreated then:
// a later run with nothing left to patch must still recreate it.
func TestMergeScript_RecreatesPodMissingSecretWhenNothingToPatch(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		helmSAs: "qdrant",
		sas:     map[string][]string{"default": {scriptTestSecret}, "qdrant": {scriptTestSecret}},
		pods:    []string{"qdrant-0|qdrant|||ErrImagePull,"},
	})
	if len(actions) != 1 || !containsAction(actions, "delete", "qdrant-0") {
		t.Errorf("actions = %v, want only the qdrant-0 recreation", actions)
	}
}

// A recreated pod that already carries the secret but still cannot pull
// (wrong credentials, missing image) must not be deleted on every run.
func TestMergeScript_LeavesPodThatAlreadyHasSecret(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		helmSAs: "qdrant",
		sas:     map[string][]string{"default": {scriptTestSecret}, "qdrant": {scriptTestSecret}},
		pods: []string{
			"qdrant-0|qdrant||" + scriptTestSecret + ",|ImagePullBackOff,",
			"web-1|qdrant|Helm|" + scriptTestSecret + ",|ImagePullBackOff,",
		},
	})
	if len(actions) != 0 {
		t.Errorf("actions = %v, want none in a stable namespace", actions)
	}
}

// An empty serviceAccountName means the namespace "default" ServiceAccount.
func TestMergeScript_EmptyServiceAccountMeansDefault(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		sas:  map[string][]string{"default": {scriptTestSecret}},
		pods: []string{"worker-0||||ImagePullBackOff,"},
	})
	if !containsAction(actions, "delete", "worker-0") {
		t.Errorf("expected pod worker-0 to be recreated, actions: %v", actions)
	}
}

// Running pods and pods whose ServiceAccount lacks the secret are untouched.
func TestMergeScript_LeavesHealthyAndUnrelatedPods(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		sas: map[string][]string{"default": {scriptTestSecret}},
		pods: []string{
			"running-0|default|||,",
			"other-0|other|||ImagePullBackOff,",
		},
	})
	if len(actions) != 0 {
		t.Errorf("actions = %v, want none", actions)
	}
}

// Existing behaviour: when this run patched an SA, Helm-labelled pods stuck
// pulling are recreated even if their spec already lists the secret.
func TestMergeScript_RecreatesHelmPodWhenAnSAWasPatched(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		helmSAs: "web",
		sas:     map[string][]string{"default": {scriptTestSecret}},
		pods:    []string{"web-1|web|Helm|" + scriptTestSecret + ",|ImagePullBackOff,"},
	})
	if !containsAction(actions, "patch", "web") || !containsAction(actions, "delete", "web-1") {
		t.Errorf("actions = %v, want SA web patched and pod web-1 recreated", actions)
	}
}
