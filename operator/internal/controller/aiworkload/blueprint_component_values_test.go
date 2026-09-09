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
	"reflect"
	"strings"
	"testing"

	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

func rawJSON(t *testing.T, v map[string]any) *apixv1.JSON {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &apixv1.JSON{Raw: b}
}

func TestResolveComponentValues(t *testing.T) {
	t.Run("no override falls back to blueprint values unchanged", func(t *testing.T) {
		c := aiplatformv1alpha1.BlueprintComponent{
			ChartName: "milvus",
			Values:    rawJSON(t, map[string]any{"replicas": float64(1), "persistence": map[string]any{"size": "10Gi"}}),
		}
		w := &aiplatformv1alpha1.AIWorkload{}
		got, err := resolveComponentValues(w, c)
		if err != nil {
			t.Fatalf("resolveComponentValues: %v", err)
		}
		want := map[string]any{"replicas": float64(1), "persistence": map[string]any{"size": "10Gi"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v want %#v", got, want)
		}
	})

	t.Run("override deep-merges onto blueprint values", func(t *testing.T) {
		c := aiplatformv1alpha1.BlueprintComponent{
			ChartName: "milvus",
			Values: rawJSON(t, map[string]any{
				"replicas":    float64(1),
				"persistence": map[string]any{"size": "10Gi", "storageClass": "default"},
			}),
		}
		w := &aiplatformv1alpha1.AIWorkload{
			Spec: aiplatformv1alpha1.AIWorkloadSpec{
				ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{
					{ComponentName: "milvus", Values: rawJSON(t, map[string]any{"persistence": map[string]any{"size": "50Gi"}})},
				},
			},
		}
		got, err := resolveComponentValues(w, c)
		if err != nil {
			t.Fatalf("resolveComponentValues: %v", err)
		}
		want := map[string]any{
			"replicas":    float64(1),
			"persistence": map[string]any{"size": "50Gi", "storageClass": "default"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v want %#v", got, want)
		}
	})

	t.Run("override for a different component is ignored", func(t *testing.T) {
		c := aiplatformv1alpha1.BlueprintComponent{ChartName: "milvus", Values: rawJSON(t, map[string]any{"replicas": float64(1)})}
		w := &aiplatformv1alpha1.AIWorkload{
			Spec: aiplatformv1alpha1.AIWorkloadSpec{
				ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{
					{ComponentName: "open-webui", Values: rawJSON(t, map[string]any{"replicas": float64(5)})},
				},
			},
		}
		got, err := resolveComponentValues(w, c)
		if err != nil {
			t.Fatalf("resolveComponentValues: %v", err)
		}
		want := map[string]any{"replicas": float64(1)}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v want %#v — a mismatched componentName must not affect rendering", got, want)
		}
	})
}

func TestEnsureBlueprintHelmOp_AppliesComponentValueOverride(t *testing.T) {
	r := newRepoFakeClient(t)
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "aif-operator"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "install-ns",
			TargetClusters:  []string{"local"},
			ComponentValues: []aiplatformv1alpha1.ComponentValueOverride{
				{ComponentName: "milvus", Values: rawJSON(t, map[string]any{"persistence": map[string]any{"size": "50Gi"}})},
			},
		},
	}
	c := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo: "suse-ai", ChartName: "milvus", ChartVersion: "1.0.0",
		Values: rawJSON(t, map[string]any{"replicas": float64(1), "persistence": map[string]any{"size": "10Gi"}}),
	}
	if _, err := r.ensureBlueprintHelmOp(context.Background(), w, c, "wl-milvus"); err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}
	ho := &unstructured.Unstructured{}
	ho.SetGroupVersionKind(helmOpGVK)
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-milvus"}, ho); err != nil {
		t.Fatalf("get HelmOp: %v", err)
	}
	size, found, err := unstructured.NestedString(ho.Object, "spec", "helm", "values", "persistence", "size")
	if err != nil || !found {
		t.Fatalf("spec.helm.values.persistence.size: found=%v err=%v", found, err)
	}
	if size != "50Gi" {
		t.Errorf("persistence.size: got %q want 50Gi (override should win)", size)
	}
	replicas, found, err := unstructured.NestedInt64(ho.Object, "spec", "helm", "values", "replicas")
	if err != nil || !found || replicas != 1 {
		t.Errorf("replicas: found=%v err=%v value=%v (want 1, untouched sibling key must survive the merge)", found, err, replicas)
	}
}

// TestEnsureBlueprintHelmOp_NoOverrideRegression guards the default path every
// existing Blueprint install hits: with no ComponentValues set, rendering must
// be byte-identical to pre-feature behavior.
func TestEnsureBlueprintHelmOp_NoOverrideRegression(t *testing.T) {
	r := newRepoFakeClient(t)
	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "aif-operator"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "install-ns",
			TargetClusters:  []string{"local"},
		},
	}
	c := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo: "suse-ai", ChartName: "milvus", ChartVersion: "1.0.0",
		Values: rawJSON(t, map[string]any{"replicas": float64(3)}),
	}
	if _, err := r.ensureBlueprintHelmOp(context.Background(), w, c, "wl-plain"); err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}
	ho := &unstructured.Unstructured{}
	ho.SetGroupVersionKind(helmOpGVK)
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-plain"}, ho); err != nil {
		t.Fatalf("get HelmOp: %v", err)
	}
	replicas, found, err := unstructured.NestedInt64(ho.Object, "spec", "helm", "values", "replicas")
	if err != nil || !found || replicas != 3 {
		t.Errorf("replicas: found=%v err=%v value=%v (want 3, unchanged from blueprint default)", found, err, replicas)
	}
}

func TestEnsureBlueprintGitFile_AppliesComponentValueOverride(t *testing.T) {
	ctx := context.Background()
	remoteURL := newBlueprintGitOpsRemote(t)
	scheme := gitRepoTestScheme()
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
		Spec:       aiplatformv1alpha1.SettingsSpec{Fleet: aiplatformv1alpha1.FleetSettings{RepoURL: remoteURL, Branch: "main"}},
	}
	source := repoObj("suse-ai", map[string]any{"url": "https://charts.example.com"})
	workload := newGitOpsTestWorkload()
	workload.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "milvus", Values: rawJSON(t, map[string]any{"persistence": map[string]any{"size": "50Gi"}})},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(settings, source, workload).Build()
	reconciler := &AIWorkloadReconciler{Client: fakeClient, Scheme: scheme, OperatorNamespace: "aif-operator"}
	component := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo: "suse-ai", ChartName: "milvus", ChartVersion: "1.0.0",
		Values: rawJSON(t, map[string]any{"replicas": float64(1), "persistence": map[string]any{"size": "10Gi"}}),
	}
	const bundleName = "private-source-workload-milvus"
	const filePath = "workloads/private-source-workload-milvus.yaml"

	if _, err := reconciler.ensureBlueprintGitFile(ctx, workload, component, bundleName); err != nil {
		t.Fatalf("ensureBlueprintGitFile: %v", err)
	}
	content := readBlueprintGitOpsFile(t, remoteURL, filePath)
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.Split(content, "\n---\n")[0]), &doc); err != nil {
		t.Fatalf("decode git file: %v", err)
	}
	size, found, err := unstructured.NestedString(doc, "spec", "helm", "values", "persistence", "size")
	if err != nil || !found || size != "50Gi" {
		t.Errorf("persistence.size: found=%v err=%v value=%q (want 50Gi, override should win)", found, err, size)
	}
}

// TestEnsureBlueprintGitChartBundle_AppliesComponentValueOverride covers the
// third render path: a Blueprint component whose ClusterRepo is git-backed
// (repoInfo.Kind == repoKindGit), reached via ensureBlueprintHelmOp exactly
// the way TestEnsureBlueprintHelmOp_GitRepoEmitsBundle exercises it, but with
// a fake rancher.ChartFetcher wired through r.CatalogClient (same pattern as
// blueprint_gitrepo_test.go's fakeCatalog/rancher.NewHolder helpers) so the
// reconcile runs past the errCatalogClientNotConfigured gate and all the way
// to a rendered Bundle. Before the Step 3 fix, ensureBlueprintGitChartBundle
// built vals via a bare json.Unmarshal of c.Values and never consulted
// w.Spec.ComponentValues, so this override would be silently dropped.
func TestEnsureBlueprintGitChartBundle_AppliesComponentValueOverride(t *testing.T) {
	scheme := gitRepoTestScheme()
	repo := repoObj("rancher-charts", map[string]any{
		"gitRepo": "https://git.rancher.io/charts", "gitBranch": "release-v2.14",
	})
	tgz := makeChartTgz(t, map[string]string{
		"rancher-ai-agent/Chart.yaml":        "apiVersion: v2\nname: rancher-ai-agent\nversion: 109.0.1\n",
		"rancher-ai-agent/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n",
	})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	holder := rancher.NewHolder()
	holder.Set(fakeCatalog{tgz: tgz})
	r := &AIWorkloadReconciler{Client: cl, Scheme: scheme, CatalogClient: holder}

	w := &aiplatformv1alpha1.AIWorkload{}
	w.Name = "wl"
	w.Spec.TargetClusters = []string{"local"}
	w.Spec.ComponentValues = []aiplatformv1alpha1.ComponentValueOverride{
		{ComponentName: "rancher-ai-agent", Values: rawJSON(t, map[string]any{"persistence": map[string]any{"size": "50Gi"}})},
	}

	c := gitComponent()
	c.Values = rawJSON(t, map[string]any{"replicas": float64(1), "persistence": map[string]any{"size": "10Gi"}})

	if _, err := r.ensureBlueprintHelmOp(context.Background(), w, c, "wl-agent"); err != nil {
		t.Fatalf("ensureBlueprintHelmOp: %v", err)
	}

	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(bundleGVK)
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "fleet-local", Name: "wl-agent"}, b); err != nil {
		t.Fatalf("expected Bundle in fleet-local: %v", err)
	}
	size, found, err := unstructured.NestedString(b.Object, "spec", "helm", "values", "persistence", "size")
	if err != nil || !found {
		t.Fatalf("spec.helm.values.persistence.size: found=%v err=%v", found, err)
	}
	if size != "50Gi" {
		t.Errorf("persistence.size: got %q want 50Gi (override should win)", size)
	}
	replicas, found, err := unstructured.NestedInt64(b.Object, "spec", "helm", "values", "replicas")
	if err != nil || !found || replicas != 1 {
		t.Errorf("replicas: found=%v err=%v value=%v (want 1, untouched sibling key must survive the merge)", found, err, replicas)
	}
}
