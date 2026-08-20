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

package helm

import (
	"testing"

	"helm.sh/helm/v3/pkg/action"
)

// TestUpgradeDoesNotWaitForReadiness pins the fix for a failure mode that only
// appears in a real cluster, so nothing else in this suite can catch a
// regression.
//
// Waiting here holds the reconcile context open for as long as the rollout
// takes. That context is the manager's, cancelled on SIGTERM — and the Helm
// upgrade that bumps the bundled extension is requested by the very `helm
// upgrade` that rolls the operator pod. The outgoing pod therefore starts the
// extension upgrade and is killed roughly twelve seconds later, whereupon
// helm's failRelease stamps the revision `failed: context canceled` and leaves
// it in the release history for good.
//
// Readiness is not dropped by this, it moves: the controller polls
// kubernetes.IsDeploymentReady on a requeue loop, which outlives a restarting
// operator in a way an in-process wait cannot.
func TestUpgradeDoesNotWaitForReadiness(t *testing.T) {
	up := newUpgradeAction(&action.Configuration{}, ReleaseSpec{
		Name:      "aif-ui-server",
		Namespace: "cattle-ui-plugin-system",
		Version:   "2.2.0",
	})

	if up.Wait {
		t.Error("upgrade is configured to wait for readiness; a pod rollout will cancel it " +
			"mid-flight and record a failed revision. Readiness belongs to the controller's " +
			"IsDeploymentReady requeue loop.")
	}
	if up.Atomic {
		t.Error("upgrade is atomic; a cancelled upgrade would roll back the release rather " +
			"than leave it for the next reconcile to retry")
	}
}

// TestUpgradeActionCarriesSpec guards the wiring the seam above depends on: an
// extracted constructor is only safe if it still configures what the caller
// used to configure inline.
func TestUpgradeActionCarriesSpec(t *testing.T) {
	spec := ReleaseSpec{
		Name:      "aif-ui-server",
		Namespace: "cattle-ui-plugin-system",
		Version:   "2.2.0",
		RepoURL:   "https://example.invalid/charts",
	}

	up := newUpgradeAction(&action.Configuration{}, spec)

	if up.Namespace != spec.Namespace {
		t.Errorf("Namespace = %q, want %q", up.Namespace, spec.Namespace)
	}
	if up.Version != spec.Version {
		t.Errorf("Version = %q, want %q", up.Version, spec.Version)
	}
	if up.RepoURL != spec.RepoURL {
		t.Errorf("RepoURL = %q, want %q", up.RepoURL, spec.RepoURL)
	}
	if up.Timeout <= 0 {
		t.Error("Timeout is unset; hooks would be allowed to run unbounded")
	}
}

// TestUpgradeActionOmitsEmptyRepoURL keeps the behaviour the inline code had:
// an OCI chart ref carries no separate repo URL, and setting an empty one makes
// helm resolve the chart against "" instead of the ref.
func TestUpgradeActionOmitsEmptyRepoURL(t *testing.T) {
	up := newUpgradeAction(&action.Configuration{}, ReleaseSpec{
		Name:      "aif-ui-server",
		Namespace: "cattle-ui-plugin-system",
		ChartRef:  "oci://ghcr.io/suse/chart/aif-ui",
	})

	if up.RepoURL != "" {
		t.Errorf("RepoURL = %q, want empty for an OCI chart ref", up.RepoURL)
	}
}
