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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

const wiringNamespace = "cattle-ui-plugin-system"

// These drive reconcileHelmSource and reconcileGitSource themselves rather than
// handlePendingRelease directly. Testing the helper only proves the helper works;
// it stays green when a call site is deleted, which is exactly the regression
// worth guarding — the two paths handled the same cluster state differently
// before, and nothing structural stops them diverging again.
func wiringReconciler(t *testing.T, ext *v1alpha1.InstallAIExtension, stub *stubHelmClient) *InstallAIExtensionReconciler {
	t.Helper()
	return wiringReconcilerWith(t, ext, stub, interceptor.Funcs{})
}

// wiringReconcilerWith is the same, with hooks on the client so a test can make a
// specific call fail — the annotation write in particular, which is the only way
// to reach the paths that decide what happens when persisting metadata fails.
func wiringReconcilerWith(
	t *testing.T,
	ext *v1alpha1.InstallAIExtension,
	stub *stubHelmClient,
	funcs interceptor.Funcs,
) *InstallAIExtensionReconciler {
	t.Helper()

	scheme := kruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	// ClusterRepo is reconciled as unstructured against the live cluster, so the
	// fake client needs the GVK to route it.
	gv := schema.GroupVersion{Group: "catalog.cattle.io", Version: "v1"}
	scheme.AddKnownTypeWithName(gv.WithKind("ClusterRepo"), &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gv.WithKind("ClusterRepoList"), &unstructured.UnstructuredList{})
	metav1.AddToGroupVersion(scheme, gv)

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.InstallAIExtension{}).
		WithObjects(ext.DeepCopy()).
		WithInterceptorFuncs(funcs).
		Build()

	// Update is optimistic-concurrency checked, so ext has to carry the stored
	// resourceVersion the way the reconcile loop's own Get leaves it.
	var stored v1alpha1.InstallAIExtension
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(ext), &stored); err != nil {
		t.Fatalf("get seeded object: %v", err)
	}
	ext.ResourceVersion = stored.ResourceVersion

	return &InstallAIExtensionReconciler{
		Client:             c,
		Scheme:             scheme,
		ExtensionNamespace: wiringNamespace,
		rancherMgr:         rancher.NewManager(c),
		helmClientFor:      func(string) (helmClient.HelmClient, error) { return stub, nil },
	}
}

func pendingStub() *stubHelmClient {
	return &stubHelmClient{
		deployed:  &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 1},
		last:      &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusPendingUpgrade, Revision: 2},
		ensureErr: pendingErr("pending-upgrade"),
	}
}

func helmExtension() *v1alpha1.InstallAIExtension {
	return &v1alpha1.InstallAIExtension{
		ObjectMeta: metav1.ObjectMeta{Name: "aif-ui"},
		Spec: v1alpha1.InstallAIExtensionSpec{
			Extension: v1alpha1.ExtensionConfig{Name: "aif-ui", Version: requestedVersion},
			Source: v1alpha1.ExtensionSource{
				Kind: v1alpha1.ExtensionSourceKindHelm,
				Helm: &v1alpha1.HelmSource{
					ChartURL: "oci://ghcr.io/suse/chart/aif-ui",
					Version:  requestedVersion,
				},
			},
		},
	}
}

// assertPendingOutcome checks what a reconcile pass over a wedged release must
// produce, whichever path produced it.
func assertPendingOutcome(t *testing.T, result ctrl.Result, err error, ext *v1alpha1.InstallAIExtension, condType string) {
	t.Helper()

	if err != nil {
		t.Fatalf("reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != pendingReleaseRequeue {
		t.Errorf("RequeueAfter = %v, want %v; a pending release must be waited out, not failed",
			result.RequeueAfter, pendingReleaseRequeue)
	}
	if ext.Status.Phase == v1alpha1.InstallAIExtensionPhaseFailed {
		t.Errorf("Phase = %s, want anything but Failed; the operation is still in flight", ext.Status.Phase)
	}
	if ext.Annotations[annotationReleasePendingSince] == "" {
		t.Error("no pending marker recorded, so the wait is unbounded")
	}

	for _, ct := range []string{condType, conditionTypeReady} {
		cond := meta.FindStatusCondition(ext.Status.Conditions, ct)
		if cond == nil {
			t.Errorf("%s condition missing", ct)
			continue
		}
		if cond.Status != metav1.ConditionFalse || cond.Reason != "ReleasePending" {
			t.Errorf("%s condition = %s/%s, want False/ReleasePending", ct, cond.Status, cond.Reason)
		}
	}
}

func TestReconcileHelmSource_WaitsOutAPendingRelease(t *testing.T) {
	ext := helmExtension()
	r := wiringReconciler(t, ext, pendingStub())

	result, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)

	assertPendingOutcome(t, result, err, ext, conditionTypeHelmInstalled)
}

func TestReconcileGitSource_WaitsOutAPendingRelease(t *testing.T) {
	ext := gitExtension()
	r := wiringReconciler(t, ext, pendingStub())

	result, err := r.reconcileGitSource(context.Background(), ext, wiringNamespace)

	assertPendingOutcome(t, result, err, ext, conditionTypeUIPlugin)
}

// Same cluster state, both source kinds, same verdict. Stated as its own test
// because the requirement is the symmetry, not either path's behaviour alone.
func TestBothSourceKindsAgreeOnAPendingRelease(t *testing.T) {
	helmExt := helmExtension()
	helmResult, helmErr := wiringReconciler(t, helmExt, pendingStub()).
		reconcileHelmSource(context.Background(), helmExt, wiringNamespace)

	gitExt := gitExtension()
	gitResult, gitErr := wiringReconciler(t, gitExt, pendingStub()).
		reconcileGitSource(context.Background(), gitExt, wiringNamespace)

	if helmErr != nil || gitErr != nil {
		t.Fatalf("errors: helm = %v, git = %v", helmErr, gitErr)
	}
	if helmResult.RequeueAfter != gitResult.RequeueAfter {
		t.Errorf("RequeueAfter: helm = %v, git = %v; the same cluster state must requeue the same way",
			helmResult.RequeueAfter, gitResult.RequeueAfter)
	}

	helmReady := meta.FindStatusCondition(helmExt.Status.Conditions, conditionTypeReady)
	gitReady := meta.FindStatusCondition(gitExt.Status.Conditions, conditionTypeReady)
	if helmReady == nil || gitReady == nil {
		t.Fatalf("Ready conditions: helm = %+v, git = %+v", helmReady, gitReady)
	}
	if helmReady.Status != gitReady.Status || helmReady.Reason != gitReady.Reason {
		t.Errorf("Ready: helm = %s/%s, git = %s/%s; one path advertises health the other does not",
			helmReady.Status, helmReady.Reason, gitReady.Status, gitReady.Reason)
	}
}

// Past the bound, both paths have to stop requeuing and name the manual step.
func TestBothSourceKindsTimeOutAPendingRelease(t *testing.T) {
	tests := []struct {
		name     string
		ext      *v1alpha1.InstallAIExtension
		condType string
		run      func(*InstallAIExtensionReconciler, *v1alpha1.InstallAIExtension) (ctrl.Result, error)
	}{
		{
			name: "helm", ext: helmExtension(), condType: conditionTypeHelmInstalled,
			run: func(r *InstallAIExtensionReconciler, ext *v1alpha1.InstallAIExtension) (ctrl.Result, error) {
				return r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
			},
		},
		{
			name: "git", ext: gitExtension(), condType: conditionTypeUIPlugin,
			run: func(r *InstallAIExtensionReconciler, ext *v1alpha1.InstallAIExtension) (ctrl.Result, error) {
				return r.reconcileGitSource(context.Background(), ext, wiringNamespace)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			markPendingSince(tt.ext, pendingReleaseTimeout+time.Minute)
			r := wiringReconciler(t, tt.ext, pendingStub())

			result, err := tt.run(r, tt.ext)
			if err != nil {
				t.Fatalf("reconcile error = %v", err)
			}
			if result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0; the wait is over", result.RequeueAfter)
			}
			if tt.ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
				t.Errorf("Phase = %s, want Failed", tt.ext.Status.Phase)
			}
			cond := meta.FindStatusCondition(tt.ext.Status.Conditions, tt.condType)
			if cond == nil || cond.Reason != "ReleasePendingTimedOut" {
				t.Fatalf("%s condition = %+v, want ReleasePendingTimedOut", tt.condType, cond)
			}
			if !containsAll(cond.Message, "helm rollback", "helm uninstall") {
				t.Errorf("message %q does not name the manual fix", cond.Message)
			}
		})
	}
}

// A release that is not pending must not leave the marker behind, on either path,
// or the next genuine wait inherits this window and times out immediately.
func TestBothSourceKindsClearTheMarkerOnSuccess(t *testing.T) {
	settled := func() *stubHelmClient {
		return &stubHelmClient{
			deployed: &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 2},
			last:     &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 2},
		}
	}

	t.Run("helm", func(t *testing.T) {
		ext := helmExtension()
		markPendingSince(ext, time.Minute)
		r := wiringReconciler(t, ext, settled())

		if _, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace); err != nil {
			t.Fatalf("reconcile error = %v", err)
		}
		if _, ok := ext.Annotations[annotationReleasePendingSince]; ok {
			t.Error("pending marker survived a settled release")
		}
	})

	t.Run("git", func(t *testing.T) {
		ext := gitExtension()
		markPendingSince(ext, time.Minute)
		r := wiringReconciler(t, ext, settled())

		if _, err := r.reconcileGitSource(context.Background(), ext, wiringNamespace); err != nil {
			t.Fatalf("reconcile error = %v", err)
		}
		if _, ok := ext.Annotations[annotationReleasePendingSince]; ok {
			t.Error("pending marker survived a settled release")
		}
	})
}
