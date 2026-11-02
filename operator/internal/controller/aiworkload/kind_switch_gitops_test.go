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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	infrahelm "github.com/SUSE/aif-operator/internal/infra/helm"
)

type gitOpsSwitchEnv struct {
	remote string
	r      *AIWorkloadReconciler
	c      client.Client
	w      *aiplatformv1alpha1.AIWorkload
	comp   aiplatformv1alpha1.BlueprintComponent
}

const gitOpsSwitchBundle = "private-source-workload-comp"

func newGitOpsSwitchEnv(t *testing.T, objs ...client.Object) *gitOpsSwitchEnv {
	t.Helper()
	remote := newBlueprintGitOpsRemote(t)
	scheme := gitRepoTestScheme()
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: remote, Branch: "main"}},
		},
	}
	w := newGitOpsTestWorkload()
	w.UID = switchUID
	source := repoObj("private-charts", map[string]any{"url": "oci://registry.example/charts"})
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append([]client.Object{settings, source, w}, objs...)...).Build()
	tgz := compChartTgz(t)
	r := &AIWorkloadReconciler{
		Client: c, Scheme: scheme, OperatorNamespace: "aif-operator",
		ChartPuller: func(context.Context, infrahelm.ChartArchiveRequest) ([]byte, error) { return tgz, nil },
	}
	return &gitOpsSwitchEnv{remote: remote, r: r, c: c, w: w,
		comp: aiplatformv1alpha1.BlueprintComponent{ChartRepo: "private-charts", ChartName: "comp", ChartVersion: "1.0.0"}}
}

func (e *gitOpsSwitchEnv) render(t *testing.T, bundleName string) error {
	t.Helper()
	_, err := e.r.ensureBlueprintGitFile(context.Background(), e.w, e.comp, bundleName)
	return err
}

func (e *gitOpsSwitchEnv) addOverlay() {
	e.w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{{ComponentName: "comp", Kustomize: inlineOverlay("inst", emptyPatches)}}
}

func gitOpsDocs(t *testing.T, remote, bundleName string) []map[string]any {
	t.Helper()
	raw := strings.Split(readBlueprintGitOpsFile(t, remote, "workloads/"+bundleName+".yaml"), "\n---\n")
	docs := make([]map[string]any, 0, len(raw))
	for _, d := range raw {
		var m map[string]any
		if err := json.Unmarshal([]byte(d), &m); err != nil {
			t.Fatalf("decode published document: %v", err)
		}
		docs = append(docs, m)
	}
	return docs
}

func gitOpsFileExists(t *testing.T, remote, filePath string) bool {
	t.Helper()
	repo, err := gogit.PlainClone(t.TempDir(), false, &gogit.CloneOptions{
		URL: remote, ReferenceName: plumbing.NewBranchReferenceName("main"), SingleBranch: true, Depth: 1,
	})
	if err != nil {
		t.Fatalf("clone git remote: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	_, err = wt.Filesystem.Stat(filePath)
	return err == nil
}

func requireSwitching(t *testing.T, err error) {
	t.Helper()
	var switching *componentSwitchingError
	if !errors.As(err, &switching) {
		t.Fatalf("want a componentSwitchingError, got %v", err)
	}
}

func TestGitOpsSwitch_PublishesKeepFirst(t *testing.T) {
	e := newGitOpsSwitchEnv(t)
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("initial HelmOp publish: %v", err)
	}
	// The GitRepo deployed that HelmOp to the cluster.
	if err := e.c.Create(context.Background(), seedHelmOp(gitOpsSwitchBundle, switchUID, false)); err != nil {
		t.Fatal(err)
	}
	e.addOverlay()
	requireSwitching(t, e.render(t, gitOpsSwitchBundle))
	for _, doc := range gitOpsDocs(t, e.remote, gitOpsSwitchBundle) {
		if doc["kind"] != "HelmOp" {
			t.Fatalf("published kind = %v, want the old HelmOp kept until it is gone", doc["kind"])
		}
		if keep, _ := doc["spec"].(map[string]any)["keepResources"].(bool); !keep {
			t.Fatalf("keepResources not republished: %v", doc["spec"])
		}
	}
}

func TestGitOpsSwitch_MissingFileIsAnError(t *testing.T) {
	// The cluster still has the old HelmOp, but nothing for it was published:
	// keepResources cannot be set, so the switch must say so instead of
	// silently making no progress.
	e := newGitOpsSwitchEnv(t, seedHelmOp(gitOpsSwitchBundle, switchUID, false))
	e.addOverlay()
	err := e.render(t, gitOpsSwitchBundle)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want the missing published file reported", err)
	}
}

func TestGitOpsSwitch_RemovesAfterApplied(t *testing.T) {
	e := newGitOpsSwitchEnv(t, seedHelmOp(gitOpsSwitchBundle, switchUID, true),
		seedBundle(gitOpsSwitchBundle, map[string]string{fleetHelmOpLabel: gitOpsSwitchBundle, workloadUIDLabel: switchUID}, true),
		seedBD(gitOpsSwitchBundle, "d1", "d1", true))
	if err := e.render(t, gitOpsSwitchBundle); err != nil { // overlay-free: publishes the HelmOp file
		t.Fatalf("initial publish: %v", err)
	}
	e.addOverlay()
	requireSwitching(t, e.render(t, gitOpsSwitchBundle))
	if gitOpsFileExists(t, e.remote, "workloads/"+gitOpsSwitchBundle+".yaml") {
		t.Fatalf("the component's file should be removed once keepResources is applied")
	}
}

func TestGitOpsSwitch_CreatesAfterGone(t *testing.T) {
	e := newGitOpsSwitchEnv(t)
	e.addOverlay()
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("render with nothing left of the old kind: %v", err)
	}
	if docs := gitOpsDocs(t, e.remote, gitOpsSwitchBundle); docs[0]["kind"] != bundleGVK.Kind {
		t.Fatalf("published kind = %v, want Bundle", docs[0]["kind"])
	}
}

func TestGitOpsSwitch_OtherComponentsUnchanged(t *testing.T) {
	e := newGitOpsSwitchEnv(t)
	const other = "private-source-workload-other"
	if err := e.render(t, other); err != nil {
		t.Fatalf("publish other component: %v", err)
	}
	before := readBlueprintGitOpsFile(t, e.remote, "workloads/"+other+".yaml")

	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("initial publish: %v", err)
	}
	if err := e.c.Create(context.Background(), seedHelmOp(gitOpsSwitchBundle, switchUID, false)); err != nil {
		t.Fatal(err)
	}
	e.addOverlay()
	e.w.Spec.ComponentValues[0].ComponentName = "comp"
	requireSwitching(t, e.render(t, gitOpsSwitchBundle))

	if after := readBlueprintGitOpsFile(t, e.remote, "workloads/"+other+".yaml"); after != before {
		t.Fatalf("another component's file changed during the switch")
	}
}

func TestGitOpsRelease_RepublishesWithoutKeep(t *testing.T) {
	e := newGitOpsSwitchEnv(t)
	if err := e.render(t, gitOpsSwitchBundle); err != nil {
		t.Fatalf("initial HelmOp publish: %v", err)
	}
	if err := e.c.Create(context.Background(), seedHelmOp(gitOpsSwitchBundle, switchUID, false)); err != nil {
		t.Fatal(err)
	}
	e.addOverlay()
	requireSwitching(t, e.render(t, gitOpsSwitchBundle))
	// The GitRepo has applied the keepResources republish.
	ho, _ := objExists(t, e.c, true, gitOpsSwitchBundle)
	_ = unstructured.SetNestedField(ho.Object, true, "spec", "keepResources")
	if err := e.c.Update(context.Background(), ho); err != nil {
		t.Fatal(err)
	}

	e.w.Spec.DeployStrategy = aiplatformv1alpha1.AIWorkloadDeployGitOps
	e.w.Spec.FleetBundleNames = []string{gitOpsSwitchBundle}
	wait, err := e.r.releaseKeepResources(context.Background(), e.w)
	if err != nil || !wait {
		t.Fatalf("wait=%v err=%v, want wait after republishing", wait, err)
	}
	for _, doc := range gitOpsDocs(t, e.remote, gitOpsSwitchBundle) {
		u := unstructured.Unstructured{Object: doc}
		if keep, found, _ := unstructured.NestedBool(doc, "spec", "keepResources"); found || keep {
			t.Fatalf("keepResources still published: %v", doc["spec"])
		}
		if g, _, _ := unstructured.NestedFieldNoCopy(doc, "spec", "forceSyncGeneration"); g != float64(1) {
			t.Fatalf("forceSyncGeneration = %v, want it bumped to 1", g)
		}
		if u.GetAnnotations()[keepReleaseGenAnnotation] == "" || u.GetAnnotations()[keepReleasedAnnotation] == "" {
			t.Fatalf("release not recorded in the published file: %v", u.GetAnnotations())
		}
	}
}
