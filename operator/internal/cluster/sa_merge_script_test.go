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
	"time"

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
//	release/<kind>-<name>  that controller's Helm release name
//	rs-deployment/<name>   the Deployment controlling that ReplicaSet
//	log        appended "patch <sa>" / "delete <pod>" lines
//	pod-lists  one line appended per pod listing (counts script runs)
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
  if [ "$kind" = pods ]; then echo list >> "$STATE/pod-lists"; cat "$STATE/pods"; exit 0; fi
  case "$3" in
    *release-name*) file="$STATE/release/$kind-$1" ;;
    *ownerReferences*) file="$STATE/rs-deployment/$1" ;;
    *) file="$STATE/owner/$kind-$1" ;;
  esac
  if [ -f "$file" ]; then cat "$file"; exit 0; fi
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
	manifests, err := buildSAMergeResources("default/wl", "demo-ns", secretNames, []string{scriptTestRelease}, "kubectl:test")
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
	// releases overrides the Helm release of a controller ("<kind>-<name>");
	// every controller in owners defaults to scriptTestRelease.
	releases map[string]string
	// rsDeployments maps a ReplicaSet name to its controlling Deployment.
	rsDeployments map[string]string
}

// runMergeScript runs the rendered script against the stub and returns the
// logged patch/delete actions.
func runMergeScript(t *testing.T, secretNames []string, st mergeScriptState) []string {
	actions, _ := runMergeScriptCounting(t, secretNames, st)
	return actions
}

// runMergeScriptCounting also returns how many merge passes the script made
// (one pod listing per pass).
func runMergeScriptCounting(t *testing.T, secretNames []string, st mergeScriptState, env ...string) ([]string, int) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	stateDir := filepath.Join(dir, "state")
	for _, d := range []string{binDir, filepath.Join(stateDir, "sa"), filepath.Join(stateDir, "owner"),
		filepath.Join(stateDir, "release"), filepath.Join(stateDir, "rs-deployment")} {
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
		write(filepath.Join(stateDir, "release", owner), scriptTestRelease, 0o644)
	}
	// Every ReplicaSet in owners is controlled by a Deployment in the
	// workload's release unless rsDeployments says otherwise.
	rsDeployments := map[string]string{}
	for owner := range st.owners {
		if rs, ok := strings.CutPrefix(owner, "ReplicaSet-"); ok {
			rsDeployments[rs] = rs + "-dep"
		}
	}
	for rs, dep := range st.rsDeployments {
		rsDeployments[rs] = dep
	}
	for rs, dep := range rsDeployments {
		write(filepath.Join(stateDir, "rs-deployment", rs), dep, 0o644)
		write(filepath.Join(stateDir, "release", "deployment-"+dep), scriptTestRelease, 0o644)
	}
	for owner, release := range st.releases {
		write(filepath.Join(stateDir, "release", owner), release, 0o644)
	}

	cmd := exec.Command(sh, "-c", renderedMergeScript(t, secretNames))
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "STATE="+stateDir)
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("merge script failed: %v\n%s", err, out)
	}
	passes := 0
	if lists, err := os.ReadFile(filepath.Join(stateDir, "pod-lists")); err == nil {
		passes = strings.Count(string(lists), "list")
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
	return actions, passes
}

func containsAction(actions []string, verb, name string) bool {
	for _, a := range actions {
		if a == verb+" "+name {
			return true
		}
	}
	return false
}

const (
	scriptTestSecret  = "aif-custom-pull-private-charts"
	scriptTestRelease = "demo-release"
)

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
		rsDeployments: map[string]string{"gone-rs": "gone"},
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
			Status: corev1.PodStatus{Phase: corev1.PodPending, InitContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
			}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "web-1",
				Labels: map[string]string{"app.kubernetes.io/managed-by": "Helm"},
			},
			Spec: corev1.PodSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "a"}, {Name: "b"}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.NewTime(time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)),
				}},
			}}},
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
	want := "qdrant-0|qdrant||StatefulSet|qdrant||ImagePullBackOff,|Pending|\n" +
		"web-1||Helm|||a,b,|,|Running|2026-10-06T10:00:00Z\n"
	if out.String() != want {
		t.Errorf("pod listing:\n got %q\nwant %q", out.String(), want)
	}
}

// A pod admitted while its ServiceAccount lacked the secret, still creating
// its containers, is recreated right away instead of waiting for its first
// failed pull and the next scheduled run.
func TestMergeScript_RecreatesNotStartedPendingPod(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		helmSAs: "qdrant",
		sas:     map[string][]string{"default": {scriptTestSecret}},
		pods:    []string{"qdrant-0|qdrant||StatefulSet|qdrant||ContainerCreating,|Pending|"},
		owners:  map[string]string{"StatefulSet-qdrant": ""},
	})
	if !containsAction(actions, "delete", "qdrant-0") {
		t.Errorf("expected pod qdrant-0 to be recreated, actions: %v", actions)
	}
}

// A Pending pod with a container already started (e.g. a running init
// container) is left to fail its pull first, so its progress is not lost.
func TestMergeScript_LeavesStartedPendingPod(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		sas:    map[string][]string{"default": {scriptTestSecret}, "qdrant": {scriptTestSecret}},
		pods:   []string{"qdrant-0|qdrant||StatefulSet|qdrant||PodInitializing,|Pending|2026-10-06T10:00:00Z"},
		owners: map[string]string{"StatefulSet-qdrant": ""},
	})
	if len(actions) != 0 {
		t.Errorf("actions = %v, want none", actions)
	}
}

// The install-time Job repeats the merge so pods and ServiceAccounts that the
// chart creates after the Job started are still handled within the install.
func TestMergeScript_RepeatsMergeWhenAskedTo(t *testing.T) {
	_, passes := runMergeScriptCounting(t, []string{scriptTestSecret}, mergeScriptState{
		sas: map[string][]string{"default": {scriptTestSecret}},
	}, "MERGE_RUNS=3", "MERGE_INTERVAL=0")
	if passes != 3 {
		t.Errorf("merge passes = %d, want 3", passes)
	}
	_, passes = runMergeScriptCounting(t, []string{scriptTestSecret}, mergeScriptState{
		sas: map[string][]string{"default": {scriptTestSecret}},
	})
	if passes != 1 {
		t.Errorf("merge passes without MERGE_RUNS = %d, want 1", passes)
	}
}

// Only the install-time Job repeats the merge over the install window; the
// CronJob safety net runs it once per tick.
func TestMergeResources_OnlyTheJobRepeatsTheMerge(t *testing.T) {
	manifests, err := buildSAMergeResources("default/wl", "demo-ns", []string{scriptTestSecret}, []string{scriptTestRelease}, "kubectl:test")
	if err != nil {
		t.Fatalf("buildSAMergeResources: %v", err)
	}
	for _, doc := range strings.Split(manifests, "\n---\n") {
		var out map[string]any
		if err := yaml.Unmarshal([]byte(doc), &out); err != nil {
			continue
		}
		switch out["kind"] {
		case "Job":
			for _, want := range []string{"name: MERGE_RUNS\n              value: \"20\"", "name: MERGE_INTERVAL\n              value: \"15\""} {
				if !strings.Contains(doc, want) {
					t.Errorf("Job env missing %q:\n%s", want, doc)
				}
			}
		case "CronJob":
			if strings.Contains(doc, "name: MERGE_RUNS") {
				t.Errorf("CronJob must run the merge once per tick:\n%s", doc)
			}
		}
	}
}

// Not-started Pending pods are recreated only for a missing delivered secret.
// The Helm-labelled path stays limited to pods failing to pull, so passes that
// patch an SA do not delete healthy Pending pods.
func TestMergeScript_LeavesNotStartedPodsThatAlreadyHaveTheSecret(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		helmSAs: "web",
		sas:     map[string][]string{"default": {scriptTestSecret}},
		pods: []string{
			"web-1|web|Helm|ReplicaSet|web-rs|" + scriptTestSecret + ",|ContainerCreating,|Pending|",
			"api-0|default||ReplicaSet|api-rs|" + scriptTestSecret + ",|ContainerCreating,|Pending|",
		},
		owners: map[string]string{"ReplicaSet-web-rs": "", "ReplicaSet-api-rs": ""},
	})
	if !containsAction(actions, "patch", "web") {
		t.Errorf("expected SA web to be patched, actions: %v", actions)
	}
	for _, pod := range []string{"web-1", "api-0"} {
		if containsAction(actions, "delete", pod) {
			t.Errorf("pod %s already carries the secret and must not be deleted, actions: %v", pod, actions)
		}
	}
}

// The Job's name must change whenever its pod template does (Job templates
// are immutable, so Fleet could not re-apply a changed Job under the same
// name), including the repeat settings that live outside the script.
func TestSAMergeJobName_CoversRepeatSettings(t *testing.T) {
	base := saMergeJobName("default/wl", "demo-ns", []string{scriptTestSecret}, "kubectl:test", "script", 20, 15)
	if base != saMergeJobName("default/wl", "demo-ns", []string{scriptTestSecret}, "kubectl:test", "script", 20, 15) {
		t.Fatal("job name is not deterministic")
	}
	if base == saMergeJobName("default/wl", "demo-ns", []string{scriptTestSecret}, "kubectl:test", "script", 30, 15) {
		t.Error("job name ignores the number of passes")
	}
	if base == saMergeJobName("default/wl", "demo-ns", []string{scriptTestSecret}, "kubectl:test", "script", 20, 10) {
		t.Error("job name ignores the pass interval")
	}
}

// Only pods of the workload's own Helm releases are recreated; another app's
// pods sharing the namespace (and its default ServiceAccount) are left alone.
func TestMergeScript_OnlyRecreatesPodsOfTheWorkloadReleases(t *testing.T) {
	actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
		helmSAs: "web",
		sas:     map[string][]string{"default": {scriptTestSecret}},
		pods: []string{
			"mine-0|default||StatefulSet|mine||ImagePullBackOff,",
			"theirs-0|default||StatefulSet|theirs||ImagePullBackOff,",
			"theirs-web-1|web|Helm|StatefulSet|theirs-web|" + scriptTestSecret + ",|ImagePullBackOff,",
		},
		owners:   map[string]string{"StatefulSet-mine": "", "StatefulSet-theirs": "", "StatefulSet-theirs-web": ""},
		releases: map[string]string{"StatefulSet-theirs": "someone-elses-app", "StatefulSet-theirs-web": "someone-elses-app"},
	})
	if !containsAction(actions, "delete", "mine-0") {
		t.Errorf("expected the workload's pod to be recreated, actions: %v", actions)
	}
	for _, pod := range []string{"theirs-0", "theirs-web-1"} {
		if containsAction(actions, "delete", pod) {
			t.Errorf("pod %s of another release must be left alone, actions: %v", pod, actions)
		}
	}
}

// A ReplicaSet carries no Helm release annotation; the release is read from
// the Deployment that controls it.
func TestMergeScript_ResolvesReleaseThroughTheDeployment(t *testing.T) {
	for _, tc := range []struct {
		release string
		want    bool
	}{{scriptTestRelease, true}, {"someone-elses-app", false}} {
		actions := runMergeScript(t, []string{scriptTestSecret}, mergeScriptState{
			sas:           map[string][]string{"default": {scriptTestSecret}},
			pods:          []string{"web-abc|default||ReplicaSet|web-rs||ImagePullBackOff,"},
			owners:        map[string]string{"ReplicaSet-web-rs": ""},
			releases:      map[string]string{"ReplicaSet-web-rs": "", "deployment-web": tc.release},
			rsDeployments: map[string]string{"web-rs": "web"},
		})
		if got := containsAction(actions, "delete", "web-abc"); got != tc.want {
			t.Errorf("Deployment release %q: recreated = %v, want %v (actions %v)", tc.release, got, tc.want, actions)
		}
	}
}

// The stub answers release lookups without evaluating them; this pins the
// script's two lookup jsonpaths against kubectl's evaluator.
func TestMergeScript_ReleaseLookupJSONPaths(t *testing.T) {
	script := renderedMergeScript(t, []string{scriptTestSecret})
	extract := func(marker string) string {
		i := strings.Index(script, marker)
		if i < 0 {
			t.Fatalf("%q not found in script", marker)
		}
		rest := script[i+len(marker):]
		return rest[:strings.Index(rest, "'")]
	}
	eval := func(expr string, obj any) string {
		raw, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		var data any
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		jp := jsonpath.New("lookup").AllowMissingKeys(true)
		if err := jp.Parse(expr); err != nil {
			t.Fatalf("parse %q: %v", expr, err)
		}
		var out bytes.Buffer
		if err := jp.Execute(&out, data); err != nil {
			t.Fatalf("execute %q: %v", expr, err)
		}
		return out.String()
	}

	tru := true
	rs := metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{
		{Kind: "ConfigMap", Name: "unrelated"},
		{Kind: "Deployment", Name: "web", Controller: &tru},
	}}
	if got := eval("{.metadata.ownerReferences"+extract(`get "$1" "$2" -o jsonpath='{.metadata.ownerReferences`), map[string]any{"metadata": rs}); got != "web" {
		t.Errorf("ReplicaSet owner lookup = %q, want web", got)
	}
	dep := metav1.ObjectMeta{Annotations: map[string]string{"meta.helm.sh/release-name": scriptTestRelease}}
	if got := eval(extract(`get deployment "$dep" -o jsonpath='`), map[string]any{"metadata": dep}); got != scriptTestRelease {
		t.Errorf("release lookup = %q, want %s", got, scriptTestRelease)
	}
}

// manifestNames returns kind -> metadata.name for the rendered SA-merge docs.
func manifestNames(t *testing.T, manifests string) map[string]string {
	t.Helper()
	names := map[string]string{}
	for _, doc := range strings.Split(manifests, "\n---\n") {
		var out map[string]any
		if err := yaml.Unmarshal([]byte(doc), &out); err != nil || out == nil {
			continue
		}
		meta, _ := out["metadata"].(map[string]any)
		kind, _ := out["kind"].(string)
		name, _ := meta["name"].(string)
		names[kind] = name
	}
	return names
}

// Each workload ships its own merge runners into the namespace. Shared names
// would make two workloads' bundles overwrite each other's CronJob (each with
// its own releases), so only one workload's pods would keep being recovered.
func TestMergeResources_NamesAreOwnedPerWorkload(t *testing.T) {
	render := func(owner string) map[string]string {
		m, err := buildSAMergeResources(owner, "shared-ns", []string{scriptTestSecret}, []string{scriptTestRelease}, "kubectl:test")
		if err != nil {
			t.Fatalf("buildSAMergeResources: %v", err)
		}
		return manifestNames(t, m)
	}
	a, again, b := render("default/app-a"), render("default/app-a"), render("default/app-b")
	for _, kind := range []string{"ServiceAccount", "Role", "RoleBinding", "Job", "CronJob"} {
		if a[kind] == "" {
			t.Fatalf("no %s rendered: %v", kind, a)
		}
		if a[kind] != again[kind] {
			t.Errorf("%s name is not stable for one workload: %q vs %q", kind, a[kind], again[kind])
		}
		if a[kind] == b[kind] {
			t.Errorf("%s name %q is shared between workloads", kind, a[kind])
		}
	}
	if len(a["CronJob"]) > 52 {
		t.Errorf("CronJob name %q exceeds the 52-character limit", a["CronJob"])
	}
}

// Release names reach a single-quoted shell string; anything that is not a
// valid Helm release name is dropped rather than rendered.
func TestMergeResources_DropsInvalidReleaseNames(t *testing.T) {
	m, err := buildSAMergeResources("default/app-a", "demo-ns", []string{scriptTestSecret},
		[]string{"vdb", "bad'; touch /tmp/x; '", strings.Repeat("a", 54), "Upper"}, "kubectl:test")
	if err != nil {
		t.Fatalf("buildSAMergeResources: %v", err)
	}
	if !strings.Contains(m, "RELEASES='vdb'") {
		t.Errorf("want only the valid release rendered, got:\n%s", m)
	}
}
