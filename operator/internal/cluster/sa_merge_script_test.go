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
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/yaml"
)

// stubKubectl simulates the kubectl calls the SA-merge script makes against a
// state directory:
//
//	helm-sas   space-separated names of Helm-labelled ServiceAccounts
//	sa/<name>  one imagePullSecrets name per line (absent file = none)
//	pods       pre-rendered output of the script's pod listing
//	owner/<Kind>-<name>  pull secret names in that controller's pod template
//	           (absent file = the controller lookup fails)
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
  if [ -f "$STATE/owner/$kind-$1" ]; then cat "$STATE/owner/$kind-$1"; exit 0; fi
  echo "not found" >&2; exit 1
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
	pods    []string          // name|serviceAccount|managedBy|ownerKind|ownerName|podSecrets,|waitingReasons,
	owners  map[string]string // "<Kind>-<name>" -> template pull secret names
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
	for _, d := range []string{binDir, filepath.Join(stateDir, "sa"), filepath.Join(stateDir, "owner")} {
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
	for owner, secrets := range st.owners {
		write(filepath.Join(stateDir, "owner", owner), secrets, 0o644)
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
		pods:    []string{"qdrant-0|qdrant||StatefulSet|qdrant||ImagePullBackOff,"},
		owners:  map[string]string{"StatefulSet-qdrant": ""},
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
		pods:    []string{"qdrant-0|qdrant||StatefulSet|qdrant||ErrImagePull,"},
		owners:  map[string]string{"StatefulSet-qdrant": ""},
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
			"qdrant-0|qdrant||StatefulSet|qdrant|" + scriptTestSecret + ",|ImagePullBackOff,",
			"web-1|qdrant|Helm|ReplicaSet|web-rs|" + scriptTestSecret + ",|ImagePullBackOff,",
		},
		owners: map[string]string{"StatefulSet-qdrant": "", "ReplicaSet-web-rs": ""},
	})
	if len(actions) != 0 {
		t.Errorf("actions = %v, want none in a stable namespace", actions)
	}
}

// An empty serviceAccountName means the namespace "default" ServiceAccount.
func TestMergeScript_EmptyServiceAccountMeansDefault(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		sas:    map[string][]string{"default": {scriptTestSecret}},
		pods:   []string{"worker-0|||ReplicaSet|worker-rs||ImagePullBackOff,"},
		owners: map[string]string{"ReplicaSet-worker-rs": ""},
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
			"running-0|default||ReplicaSet|running-rs||,",
			"other-0|other||ReplicaSet|other-rs||ImagePullBackOff,",
		},
		owners: map[string]string{"ReplicaSet-running-rs": "", "ReplicaSet-other-rs": ""},
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
		pods:    []string{"web-1|web|Helm|ReplicaSet|web-rs|" + scriptTestSecret + ",|ImagePullBackOff,"},
		owners:  map[string]string{"ReplicaSet-web-rs": ""},
	})
	if !containsAction(actions, "patch", "web") || !containsAction(actions, "delete", "web-1") {
		t.Errorf("actions = %v, want SA web patched and pod web-1 recreated", actions)
	}
}

// A pod template that lists its own imagePullSecrets never gets the SA's
// merged in: recreating its pods cannot add the missing secret, so they must
// not be deleted on every run.
func TestMergeScript_LeavesPodWhoseTemplateSetsPullSecrets(t *testing.T) {
	actions := runMergeScript(t, []string{"ngc-api", "ngc-secret"}, mergeScriptState{
		sas:    map[string][]string{"default": {"ngc-api", "ngc-secret"}},
		pods:   []string{"nim-0|default||StatefulSet|nim|ngc-secret,|ImagePullBackOff,"},
		owners: map[string]string{"StatefulSet-nim": "ngc-secret"},
	})
	if len(actions) != 0 {
		t.Errorf("actions = %v, want none", actions)
	}
}

// Pods nothing would recreate (no controller), Job pods (deletions count
// against the Job's backoff limit), and pods whose controller cannot be read
// are left alone.
func TestMergeScript_LeavesPodsWithoutRecreatingController(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		sas: map[string][]string{"default": {scriptTestSecret}},
		pods: []string{
			"debug|default|||||ImagePullBackOff,",
			"migrate-x|default||Job|migrate||ImagePullBackOff,",
			"gone-0|default||ReplicaSet|gone-rs||ImagePullBackOff,",
		},
	})
	if len(actions) != 0 {
		t.Errorf("actions = %v, want none", actions)
	}
}

// The stub above serves a pre-rendered pod listing; this pins the script's
// real jsonpath (escaped label key, controller filter, empty fields) against
// kubectl's evaluator so the listing the script parses matches the stub's
// format.
func TestMergeScript_PodListingJSONPathMatchesStubFormat(t *testing.T) {
	script := renderedMergeScript(t, []string{scriptTestSecret})
	start := strings.Index(script, `get pods -o jsonpath='`)
	if start < 0 {
		t.Fatal("pod listing not found in script")
	}
	expr := script[start+len(`get pods -o jsonpath='`):]
	expr = expr[:strings.Index(expr, "'")]

	tru := true
	pods := corev1.PodList{Items: []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "qdrant-0",
				Labels: map[string]string{"app.kubernetes.io/name": "qdrant"},
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "ConfigMap", Name: "not-a-controller"},
					{Kind: "StatefulSet", Name: "qdrant", Controller: &tru},
				},
			},
			Spec: corev1.PodSpec{ServiceAccountName: "qdrant"},
			Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
			}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "web-1",
				Labels: map[string]string{"app.kubernetes.io/managed-by": "Helm"},
			},
			Spec: corev1.PodSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "a"}, {Name: "b"}}},
		},
	}}
	raw, err := json.Marshal(pods)
	if err != nil {
		t.Fatal(err)
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	jp := jsonpath.New("pods").AllowMissingKeys(true)
	if err := jp.Parse(expr); err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	var out bytes.Buffer
	if err := jp.Execute(&out, data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := "qdrant-0|qdrant||StatefulSet|qdrant||ImagePullBackOff,\n" +
		"web-1||Helm|||a,b,|\n"
	if out.String() != want {
		t.Errorf("pod listing:\n got %q\nwant %q", out.String(), want)
	}
}
