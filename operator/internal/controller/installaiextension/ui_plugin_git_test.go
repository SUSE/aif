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

package controller

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

// stubHelmClient stands in for the Helm backend so the reconcile paths can be
// driven over a chosen release state without a cluster or a chart.
type stubHelmClient struct {
	deployed    *helmClient.ReleaseInfo
	last        *helmClient.ReleaseInfo
	ensureErr   error
	ensureCalls int
	ensureSpec  helmClient.ReleaseSpec
}

func (s *stubHelmClient) EnsureRelease(_ context.Context, spec helmClient.ReleaseSpec) error {
	s.ensureCalls++
	s.ensureSpec = spec
	return s.ensureErr
}

func (s *stubHelmClient) DeleteRelease(_ context.Context, _ string) error { return nil }

func (s *stubHelmClient) LastRelease(_ context.Context, _ string) (*helmClient.ReleaseInfo, error) {
	return s.last, nil
}

func (s *stubHelmClient) DeployedRelease(_ context.Context, _ string) (*helmClient.ReleaseInfo, error) {
	return s.deployed, nil
}

// requestedVersion is what the spec asks for *and* what the release records
// below already carry. The tests are about a release wedged at the version that
// was requested, so the two are deliberately the same value rather than two
// literals that happen to agree.
const requestedVersion = "1.0.0"

func gitExtension() *v1alpha1.InstallAIExtension {
	return &v1alpha1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "aif-ui"},
		Spec: v1alpha1.InstallAIExtensionSpec{
			Extension: v1alpha1.ExtensionConfig{Name: "aif-ui", Version: requestedVersion},
			Source: v1alpha1.ExtensionSource{
				Kind: v1alpha1.ExtensionSourceKindGit,
				Git: &v1alpha1.GitSource{
					Repo:   "https://github.com/example/aif-ui",
					Branch: "main",
				},
			},
		},
	}
}

func withStub(stub *stubHelmClient) *InstallAIExtensionReconciler {
	return &InstallAIExtensionReconciler{
		helmClientFor: func(string) (helmClient.HelmClient, error) { return stub, nil },
	}
}

// The state a killed operation leaves behind when it was not a version change:
// the deployed revision already carries the requested version, and a pending
// revision sits above it. The git path used to answer that with its own
// version-equality check and return success without ever consulting Helm, while
// the Helm path requeued for the identical cluster state.
func TestEnsureUIPluginGit_SurfacesPendingReleaseAtRequestedVersion(t *testing.T) {
	stub := &stubHelmClient{
		deployed:  &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 1},
		last:      &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusPendingUpgrade, Revision: 2},
		ensureErr: pendingErr("pending-upgrade"),
	}
	r := withStub(stub)

	err := r.ensureUIPluginGit(context.Background(), gitExtension(),
		"https://raw.githubusercontent.com/example/aif-ui/main", "cattle-ui-plugin-system")

	if !errors.Is(err, helmClient.ErrReleasePending) {
		t.Fatalf("ensureUIPluginGit() error = %v, want ErrReleasePending", err)
	}
	if stub.ensureCalls != 1 {
		t.Errorf("EnsureRelease calls = %d, want 1; the pending state is only visible through it",
			stub.ensureCalls)
	}
}

// The skip decision still has to happen — it just belongs to EnsureRelease, which
// weighs values as well as version and knows about in-flight operations. This pins
// that removing the local check delegated the decision rather than dropping it.
func TestEnsureUIPluginGit_DelegatesTheSkipDecision(t *testing.T) {
	stub := &stubHelmClient{
		deployed: &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 1},
		last:     &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 1},
	}
	r := withStub(stub)

	if err := r.ensureUIPluginGit(context.Background(), gitExtension(),
		"https://raw.githubusercontent.com/example/aif-ui/main", "cattle-ui-plugin-system"); err != nil {
		t.Fatalf("ensureUIPluginGit() error = %v", err)
	}
	if stub.ensureCalls != 1 {
		t.Fatalf("EnsureRelease calls = %d, want 1", stub.ensureCalls)
	}
	if stub.ensureSpec.Version != requestedVersion || stub.ensureSpec.Name != "aif-ui" {
		t.Errorf("EnsureRelease spec = %+v, want name aif-ui at 1.0.0", stub.ensureSpec)
	}
	if stub.ensureSpec.RepoURL == "" {
		t.Error("EnsureRelease spec carries no RepoURL; the git source would not resolve")
	}
}
