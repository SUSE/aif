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

package aiworkload

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	igit "github.com/SUSE/aif-operator/internal/git"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
	"github.com/SUSE/aif-operator/internal/naming"
)

// seedPublished creates what the GitRepo deploys from the component's file.
func seedPublished(t *testing.T, e *gitOpsSwitchEnv, docs []map[string]any) {
	t.Helper()
	for _, d := range docs {
		if err := e.c.Create(context.Background(), &unstructured.Unstructured{Object: d}); err != nil {
			t.Fatal(err)
		}
	}
}

// backfillAndCleanup runs the two steps of a reconcile pass that remove stale objects.
func backfillAndCleanup(r *AIWorkloadReconciler, w *aiplatformv1alpha1.AIWorkload, desired []HelmOpKey) error {
	legacy, err := r.backfillOwnerLabels(context.Background(), w)
	if err != nil {
		return err
	}
	return r.cleanupStaleHelmOps(context.Background(), w, desired, legacy)
}

func (e *gitOpsSwitchEnv) cleanup(t *testing.T) {
	t.Helper()
	keys := desiredHelmOpKeys(e.w.Name, e.w.Spec.TargetClusters, []aiplatformv1alpha1.BlueprintComponent{e.comp}, e.w.Spec.DeployStrategy)
	if err := backfillAndCleanup(e.r, e.w, keys); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

// Removing local from a local+downstream workload leaves the fleet-local object
// stale while the same name is still desired in fleet-default: the shared file
// must survive cleanup and keep exactly the downstream object.
func testGitOpsCleanupKeepsFileDesiredElsewhere(t *testing.T, overlay bool) {
	e := newGitOpsSwitchEnv(t)
	e.w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployGitOps
	e.w.Spec.TargetClusters = []string{"local", "c-m-downstream"}
	if overlay {
		e.addOverlay()
	}
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("first render: %v", err)
	}
	docs := gitOpsDocs(t, e.remote, gitOpsSwitchBundle)
	if len(docs) != 2 {
		t.Fatalf("published %d documents, want one per workspace", len(docs))
	}
	seedPublished(t, e, docs)

	e.w.Spec.TargetClusters = []string{"c-m-downstream"}
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("render without local: %v", err)
	}
	e.cleanup(t)
	// Even a brief removal would let the GitRepo prune the downstream release.
	if !gitOpsFileExists(t, e.remote, gitOpsFilePath(gitOpsSwitchBundle)) {
		t.Fatalf("%s was removed although the name is still desired in fleet-default", gitOpsFilePath(gitOpsSwitchBundle))
	}
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("render after cleanup: %v", err)
	}
	got := gitOpsDocs(t, e.remote, gitOpsSwitchBundle)
	if len(got) != 1 || (&unstructured.Unstructured{Object: got[0]}).GetNamespace() != "fleet-default" {
		t.Fatalf("published documents = %v, want exactly one in fleet-default", got)
	}
}

func TestGitOpsCleanup_BundleKeepsFileDesiredInOtherWorkspace(t *testing.T) {
	testGitOpsCleanupKeepsFileDesiredElsewhere(t, true)
}

func TestGitOpsCleanup_HelmOpKeepsFileDesiredInOtherWorkspace(t *testing.T) {
	testGitOpsCleanupKeepsFileDesiredElsewhere(t, false)
}

// When the name goes away entirely the file is removed and its publication hash
// forgotten, so rendering the same content again later writes the file again.
func TestGitOpsCleanup_RemovesFileAndForgetsHash(t *testing.T) {
	for _, overlay := range []bool{false, true} {
		e := newGitOpsSwitchEnv(t)
		e.w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployGitOps
		if overlay {
			e.addOverlay()
		}
		if err := e.render(t, gitOpsSwitchBundle); err != nil {
			t.Fatalf("render: %v", err)
		}
		docs := gitOpsDocs(t, e.remote, gitOpsSwitchBundle)
		seedPublished(t, e, docs)
		if err := backfillAndCleanup(e.r, e.w, nil); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		if gitOpsFileExists(t, e.remote, gitOpsFilePath(gitOpsSwitchBundle)) {
			t.Fatalf("overlay=%v: file of a name no longer desired must be removed", overlay)
		}
		if _, ok := e.w.Annotations[gitFileHashAnnotation(gitOpsSwitchBundle)]; ok {
			t.Fatalf("overlay=%v: publication hash kept after the file was removed", overlay)
		}
		// The GitRepo prunes what the removed file had deployed.
		for _, d := range docs {
			if err := e.c.Delete(context.Background(), &unstructured.Unstructured{Object: d}); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.render(t, gitOpsSwitchBundle); err != nil {
			t.Fatalf("re-render: %v", err)
		}
		if !gitOpsFileExists(t, e.remote, gitOpsFilePath(gitOpsSwitchBundle)) {
			t.Fatalf("overlay=%v: re-rendering the same content did not publish the file again", overlay)
		}
	}
}

// legacyBundle is an embedded-chart Bundle as the previous release wrote it:
// fingerprint annotation and render-digest label, no owner label.
func legacyBundle(name string) *unstructured.Unstructured {
	b := seedBundle(name, map[string]string{renderDigestLabel: "x"}, false)
	b.SetAnnotations(map[string]string{chartFingerprintAnnotation: "fp"})
	return b
}

func TestCleanup_LegacyBundleAdoptedAndRemovedOnExclusion(t *testing.T) {
	w := switchWorkload()
	w.Spec.FleetBundleNames = []string{switchBundle}
	r, c := switchReconciler(t, legacyBundle(switchBundle))
	if err := backfillAndCleanup(r, w, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := objExists(t, c, false, switchBundle); ok {
		t.Fatalf("legacy Bundle of an excluded component survived cleanup")
	}
}

func TestCleanup_UnlistedUnlabelledBundleIsLeftAlone(t *testing.T) {
	w := switchWorkload()
	w.Spec.FleetBundleNames = []string{"wl-other"}
	r, c := switchReconciler(t, legacyBundle(switchBundle))
	if err := backfillAndCleanup(r, w, nil); err != nil {
		t.Fatal(err)
	}
	b, ok := objExists(t, c, false, switchBundle)
	if !ok || b.GetLabels()[workloadUIDLabel] != "" {
		t.Fatalf("a Bundle not named in FleetBundleNames must not be adopted or removed (exists=%v labels=%v)", ok, b.GetLabels())
	}
}

// A legacy Bundle of a component that now renders as a HelmOp is adopted
// before rendering, so the switch proceeds instead of refusing.
func TestReconcile_LegacyBundleSwitchesToHelmOp(t *testing.T) {
	scheme := clusterRepoErrorScheme(t)
	w := switchWorkload()
	w.Spec.FleetBundleNames = []string{switchBundle}
	bp := &aiplatformv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: naming.BlueprintCRName("mini", "1.0.0")}}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{{ChartRepo: "application-collection", ChartName: "comp", ChartVersion: "1.0.0", Vendor: "suse"}}
	repo := repoObj("application-collection", map[string]any{"url": "https://charts.example.com"})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(w, bp, repo, legacyBundle(switchBundle)).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: "aif-operator"}
	if _, _, err := r.reconcileBlueprintStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	ready := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if ready == nil || ready.Reason != "Switching" {
		t.Fatalf("Ready = %+v, want the switch to a HelmOp in progress", ready)
	}
	b, _ := objExists(t, c, false, switchBundle)
	if keep, _, _ := unstructured.NestedBool(b.Object, "spec", "keepResources"); !keep || b.GetLabels()[workloadUIDLabel] != switchUID {
		t.Fatalf("legacy Bundle labels=%v keep=%v, want it adopted and keepResources set", b.GetLabels(), keep)
	}
}

// In GitOps mode the GitRepo owns the deployed Bundle, so a legacy one is not
// patched: the render republishes the file with the owner label instead.
func TestGitOpsRender_RepublishesLegacyBundleWithOwnerLabel(t *testing.T) {
	e := newGitOpsSwitchEnv(t)
	e.w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployGitOps
	e.w.Spec.FleetBundleNames = []string{gitOpsSwitchBundle}
	e.addOverlay()
	calls := 0
	tgz := compChartTgz(t)
	e.r.ChartPuller = func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { calls++; return tgz, nil }
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("render: %v", err)
	}

	// Rewind to the previous release: its file and deployed Bundle had no owner label.
	doc := gitOpsDocs(t, e.remote, gitOpsSwitchBundle)[0]
	legacy := &unstructured.Unstructured{Object: doc}
	labels := legacy.GetLabels()
	delete(labels, workloadUIDLabel)
	legacy.SetLabels(labels)
	content, err := marshalGitResources([]map[string]any{legacy.Object})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := igit.NewForRepo(e.remote, "main", nil, nil, false).WriteFile(context.Background(), gitOpsFilePath(gitOpsSwitchBundle), content, "legacy"); err != nil {
		t.Fatal(err)
	}
	metav1.SetMetaDataAnnotation(&e.w.ObjectMeta, gitFileHashAnnotation(gitOpsSwitchBundle), gitManifestHash(e.remote+"\x00main\x00"+content))
	if err := e.c.Create(context.Background(), legacy.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("render after upgrade: %v", err)
	}
	if calls != 2 {
		t.Fatalf("chart fetched %d times, want a rebuild for the unlabelled Bundle", calls)
	}
	published := &unstructured.Unstructured{Object: gitOpsDocs(t, e.remote, gitOpsSwitchBundle)[0]}
	if published.GetLabels()[workloadUIDLabel] != switchUID {
		t.Fatalf("republished labels = %v, want the owner label", published.GetLabels())
	}
	if published.GetAnnotations()[chartFingerprintAnnotation] != legacy.GetAnnotations()[chartFingerprintAnnotation] {
		t.Fatalf("fingerprint changed on republish; it must stay stable across the upgrade")
	}
	deployed, _ := objExists(t, e.c, false, gitOpsSwitchBundle)
	if deployed.GetLabels()[workloadUIDLabel] != "" {
		t.Fatalf("the GitRepo-owned Bundle must not be patched in GitOps mode")
	}
}

// An excluded component's legacy Bundle in GitOps mode still has its file removed.
func TestGitOpsCleanup_LegacyBundleFileRemovedOnExclusion(t *testing.T) {
	e := newGitOpsSwitchEnv(t)
	e.w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployGitOps
	e.w.Spec.FleetBundleNames = []string{gitOpsSwitchBundle}
	e.addOverlay()
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("render: %v", err)
	}
	legacy := &unstructured.Unstructured{Object: gitOpsDocs(t, e.remote, gitOpsSwitchBundle)[0]}
	labels := legacy.GetLabels()
	delete(labels, workloadUIDLabel)
	legacy.SetLabels(labels)
	if err := e.c.Create(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	if err := backfillAndCleanup(e.r, e.w, nil); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if gitOpsFileExists(t, e.remote, gitOpsFilePath(gitOpsSwitchBundle)) {
		t.Fatalf("the excluded legacy component's file must be removed")
	}
}

// A labelled workload costs the backfill one List per kind and namespace and
// no per-name Gets; only unlabelled objects named in FleetBundleNames change.
func TestBackfillOwnerLabels_ListsInsteadOfPerNameGets(t *testing.T) {
	w := switchWorkload()
	w.Spec.FleetBundleNames = []string{switchBundle, "wl-other", "wl-third"}
	var gets, lists int
	scheme := clusterRepoErrorScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(seedOperatorBundle(false), seedHelmOp("wl-other", switchUID, false)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				gets++
				return cl.Get(ctx, key, obj, opts...)
			},
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				lists++
				return cl.List(ctx, list, opts...)
			},
		}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	if _, err := r.backfillOwnerLabels(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if gets != 0 || lists != 2*len(fleetNamespaces) {
		t.Fatalf("gets=%d lists=%d, want no Gets and one List per kind and namespace", gets, lists)
	}

	if err := c.Create(context.Background(), legacyBundle("wl-third")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.backfillOwnerLabels(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if b, _ := objExists(t, c, false, "wl-third"); b.GetLabels()[workloadUIDLabel] != switchUID {
		t.Fatalf("legacy Bundle labels = %v, want it adopted", b.GetLabels())
	}
}

// The shipped role lets the operator patch HelmOps but not update them, so the
// backfill must work without Update.
func TestBackfillOwnerLabels_NeedsNoUpdate(t *testing.T) {
	w := switchWorkload()
	w.Spec.FleetBundleNames = []string{"wl-other"}
	scheme := clusterRepoErrorScheme(t)
	legacy := seedHelmOp("wl-other", switchUID, false)
	legacy.SetLabels(nil)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(legacy).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if obj.GetObjectKind().GroupVersionKind().Kind == "HelmOp" {
					return apierrors.NewForbidden(schema.GroupResource{Group: "fleet.cattle.io", Resource: "helmops"}, obj.GetName(), errors.New("update is not allowed"))
				}
				return cl.Update(ctx, obj, opts...)
			},
		}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	if _, err := r.backfillOwnerLabels(context.Background(), w); err != nil {
		t.Fatalf("backfill needs Update on HelmOps: %v", err)
	}
	if ho, _ := objExists(t, c, true, "wl-other"); ho.GetLabels()[workloadUIDLabel] != switchUID {
		t.Fatalf("HelmOp labels = %v, want it adopted", ho.GetLabels())
	}
}

// Status computed earlier in the pass survives the metadata writes that
// publishing and removing a file make.
func TestGitOpsFileWrites_KeepInMemoryStatus(t *testing.T) {
	e := newGitOpsSwitchEnv(t)
	e.w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployGitOps
	e.w.Status.ObservedGeneration = 7
	e.w.Status.RenderBaselines = []aiplatformv1alpha1.RenderBaseline{{HelmOpUID: "u1", RenderDigest: "d1"}}
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("render: %v", err)
	}
	if e.w.Status.ObservedGeneration != 7 || len(e.w.Status.RenderBaselines) != 1 {
		t.Fatalf("status after publish = %+v, want the in-memory status kept", e.w.Status)
	}
	if err := e.r.removeGitFile(context.Background(), e.w, gitOpsSwitchBundle); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if e.w.Status.ObservedGeneration != 7 || len(e.w.Status.RenderBaselines) != 1 {
		t.Fatalf("status after file removal = %+v, want the in-memory status kept", e.w.Status)
	}
	if _, ok := e.w.Annotations[gitFileHashAnnotation(gitOpsSwitchBundle)]; ok {
		t.Fatalf("publication hash not cleared")
	}
}
