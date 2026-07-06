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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The SA-merge script bounces chart-managed Pods stuck in ImagePullBackOff so
// they re-read their ServiceAccount's imagePullSecrets (which are only merged
// into a Pod at admission). These tests execute the rendered script against a
// stub `kubectl` to lock in the recovery behavior — in particular the race a
// pure string-assertion test cannot catch: an SA that was patched on an EARLIER
// tick (so this run finds nothing to patch) with a Pod that was admitted before
// that patch and is therefore permanently stuck.

// renderSAMergeScript renders just the shell script the operator ships in the
// Job/CronJob, for the given namespace and desired secret names.
func renderSAMergeScript(t *testing.T, namespace string, desired []string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := saMergeScriptTemplate.Execute(&buf, struct {
		Namespace    string
		DesiredNames string
	}{Namespace: namespace, DesiredNames: strings.Join(desired, " ")}); err != nil {
		t.Fatalf("render script: %v", err)
	}
	return buf.String()
}

// saMergeStubKubectl is a POSIX-sh stand-in for kubectl. It serves canned
// responses out of $KUBECTL_STATE and records mutating calls (patch sa, delete
// pod) to files there so the test can assert on them. Positional args mirror the
// real invocations the merge script makes:
//
//	kubectl -n NS get  sa   -l <selector> -o jsonpath=...   -> $STATE/sa_list
//	kubectl -n NS get  sa   <name>        -o jsonpath=...   -> $STATE/sa_<name>_ips
//	kubectl -n NS get  pods -l <selector> -o jsonpath=...   -> $STATE/pod_reasons
//	kubectl -n NS get  pod  <name>        -o jsonpath=...   -> $STATE/pod_<name>_ips
//	kubectl -n NS patch sa  <name> ...                      -> append <name> to $STATE/patched
//	kubectl -n NS delete pod <name> --ignore-not-found      -> append <name> to $STATE/deleted
const saMergeStubKubectl = `#!/bin/sh
S="$KUBECTL_STATE"
verb="$3"
obj="$4"
name="$5"
case "$verb" in
  get)
    case "$obj" in
      sa)
        case "$name" in
          -l) cat "$S/sa_list" 2>/dev/null || true ;;
          *)  cat "$S/sa_${name}_ips" 2>/dev/null || true ;;
        esac
        ;;
      pods) cat "$S/pod_reasons" 2>/dev/null || true ;;
      pod)  cat "$S/pod_${name}_ips" 2>/dev/null || true ;;
    esac
    ;;
  patch) echo "$name" >> "$S/patched" ;;
  delete) echo "$name" >> "$S/deleted" ;;
esac
exit 0
`

// saMergeHarness renders the script and prepares a state dir + stub kubectl.
type saMergeHarness struct {
	script string
	state  string
	binDir string
}

func newSAMergeHarness(t *testing.T, namespace string, desired []string) *saMergeHarness {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "kubectl"), []byte(saMergeStubKubectl), 0o755); err != nil {
		t.Fatal(err)
	}
	return &saMergeHarness{
		script: renderSAMergeScript(t, namespace, desired),
		state:  state,
		binDir: binDir,
	}
}

// set writes a stub response/fixture file.
func (h *saMergeHarness) set(name, content string) {
	if err := os.WriteFile(filepath.Join(h.state, name), []byte(content), 0o644); err != nil {
		panic(err)
	}
}

// run executes the rendered script with the stub kubectl on PATH.
func (h *saMergeHarness) run(t *testing.T) {
	t.Helper()
	scriptPath := filepath.Join(h.state, "..", "merge.sh")
	if err := os.WriteFile(scriptPath, []byte(h.script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Env = append(os.Environ(),
		"KUBECTL_STATE="+h.state,
		"PATH="+h.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\noutput:\n%s", err, out)
	}
	t.Logf("script output:\n%s", out)
}

// deleted returns the pod names the script asked kubectl to delete.
func (h *saMergeHarness) deleted() []string {
	b, err := os.ReadFile(filepath.Join(h.state, "deleted"))
	if err != nil {
		return nil
	}
	return strings.Fields(string(b))
}

// TestSAMerge_BouncesStalePodWhenSAAlreadyPatched reproduces the production
// race: both ServiceAccounts already carry the desired secret (patched on an
// earlier tick), so THIS run patches nothing — yet a Pod admitted before that
// patch is stuck in ImagePullBackOff with a secret-less spec and must still be
// recovered. The buggy version gated the bounce on "an SA was patched this run"
// and therefore left this Pod stuck forever.
func TestSAMerge_BouncesStalePodWhenSAAlreadyPatched(t *testing.T) {
	h := newSAMergeHarness(t, "ollama-system", []string{"suse-ai-pull-combined"})
	// Both SAs already converged -> no patch happens this run.
	h.set("sa_list", "ollama")
	h.set("sa_ollama_ips", "suse-ai-pull-combined\n")
	h.set("sa_default_ips", "suse-ai-pull-combined\n")
	// Stuck pod admitted before the SA was patched: spec has no combined secret.
	h.set("pod_reasons", "ollama-0=ImagePullBackOff,\n")
	h.set("pod_ollama-0_ips", "")

	h.run(t)

	got := h.deleted()
	if len(got) != 1 || got[0] != "ollama-0" {
		t.Fatalf("stale stuck pod not recovered: want [ollama-0] deleted, got %v", got)
	}
}

// TestSAMerge_DoesNotBounceConvergedFailingPod guards against churn: a Pod that
// already carries the desired secret in its own spec (admitted AFTER the SA was
// patched) but is still failing — e.g. a genuinely bad image ref — must be left
// alone so it doesn't get deleted-and-recreated on every CronJob tick.
func TestSAMerge_DoesNotBounceConvergedFailingPod(t *testing.T) {
	h := newSAMergeHarness(t, "ollama-system", []string{"suse-ai-pull-combined"})
	h.set("sa_list", "ollama")
	h.set("sa_ollama_ips", "suse-ai-pull-combined\n")
	h.set("sa_default_ips", "suse-ai-pull-combined\n")
	// Stuck pod that ALREADY has the desired secret -> genuine failure, no churn.
	h.set("pod_reasons", "ollama-0=ImagePullBackOff,\n")
	h.set("pod_ollama-0_ips", "suse-ai-pull-combined ")

	h.run(t)

	if got := h.deleted(); len(got) != 0 {
		t.Fatalf("converged failing pod should not be bounced (churn), got deleted %v", got)
	}
}

// TestSAMerge_LeavesHealthyPodsAlone ensures a running Pod that is not stuck is
// never bounced, even if it predates the SA patch.
func TestSAMerge_LeavesHealthyPodsAlone(t *testing.T) {
	h := newSAMergeHarness(t, "ollama-system", []string{"suse-ai-pull-combined"})
	h.set("sa_list", "ollama")
	h.set("sa_ollama_ips", "suse-ai-pull-combined\n")
	h.set("sa_default_ips", "suse-ai-pull-combined\n")
	// Running pod: no waiting reason at all.
	h.set("pod_reasons", "ollama-0=\n")
	h.set("pod_ollama-0_ips", "")

	h.run(t)

	if got := h.deleted(); len(got) != 0 {
		t.Fatalf("healthy pod should never be bounced, got deleted %v", got)
	}
}
