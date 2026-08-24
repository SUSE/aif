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

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func applicationResolutionReconciler(t *testing.T, applications ...*aiplatformv1alpha1.Application) *AIWorkloadReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	objects := make([]runtime.Object, 0, len(applications))
	for _, application := range applications {
		objects = append(objects, application)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
	return &AIWorkloadReconciler{Client: client, Scheme: scheme}
}

func TestResolveBlueprintComponentsApplicationReference(t *testing.T) {
	application := &aiplatformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "nvidia.rag"},
		Spec: aiplatformv1alpha1.ApplicationSpec{
			Chart:             aiplatformv1alpha1.ApplicationChart{Name: "rag", SourceRef: "private-gitea"},
			CredentialProfile: aiplatformv1alpha1.ComponentVendorNvidia,
		},
	}
	values := &apixv1.JSON{Raw: []byte(`{"replicas":2}`)}
	input := aiplatformv1alpha1.BlueprintComponent{
		ApplicationRef: &aiplatformv1alpha1.ApplicationReference{Name: "nvidia.rag", Version: "2.6.0"},
		Values:         values,
		ReleaseName:    "qualified-rag",
	}

	resolved, err := applicationResolutionReconciler(t, application).resolveBlueprintComponents(context.Background(), []aiplatformv1alpha1.BlueprintComponent{input})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected one component, got %d", len(resolved))
	}
	got := resolved[0]
	if got.ChartRepo != "private-gitea" || got.ChartName != "rag" || got.ChartVersion != "2.6.0" {
		t.Fatalf("unexpected resolved chart: repo=%q name=%q version=%q", got.ChartRepo, got.ChartName, got.ChartVersion)
	}
	if got.Vendor != aiplatformv1alpha1.ComponentVendorNvidia {
		t.Fatalf("expected nvidia credential profile, got %q", got.Vendor)
	}
	if got.Values != values || got.ReleaseName != "qualified-rag" {
		t.Fatalf("component configuration was not preserved: %#v", got)
	}
	if input.ChartRepo != "" || input.ChartName != "" || input.ChartVersion != "" {
		t.Fatalf("resolver mutated the stored component: %#v", input)
	}
}

func TestResolveBlueprintComponentsLegacyPassThrough(t *testing.T) {
	input := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo:    "application-collection",
		ChartName:    "ollama",
		ChartVersion: "1.55.0",
	}

	resolved, err := applicationResolutionReconciler(t).resolveBlueprintComponents(context.Background(), []aiplatformv1alpha1.BlueprintComponent{input})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0] != input {
		t.Fatalf("legacy component changed: %#v", resolved)
	}
}

func TestResolveBlueprintComponentsMissingApplication(t *testing.T) {
	input := aiplatformv1alpha1.BlueprintComponent{
		ApplicationRef: &aiplatformv1alpha1.ApplicationReference{Name: "suse.missing", Version: "1.0.0"},
	}

	_, err := applicationResolutionReconciler(t).resolveBlueprintComponents(context.Background(), []aiplatformv1alpha1.BlueprintComponent{input})
	var missing *applicationNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("expected applicationNotFoundError, got %v", err)
	}
	if missing.name != "suse.missing" {
		t.Fatalf("unexpected missing application: %q", missing.name)
	}
}

func TestResolveBlueprintComponentsDefaultsEmptyProfileToSUSE(t *testing.T) {
	application := &aiplatformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "suse.ollama"},
		Spec: aiplatformv1alpha1.ApplicationSpec{
			Chart: aiplatformv1alpha1.ApplicationChart{Name: "ollama", SourceRef: "application-collection"},
		},
	}
	input := aiplatformv1alpha1.BlueprintComponent{
		ApplicationRef: &aiplatformv1alpha1.ApplicationReference{Name: "suse.ollama", Version: "1.55.0"},
	}

	resolved, err := applicationResolutionReconciler(t, application).resolveBlueprintComponents(context.Background(), []aiplatformv1alpha1.BlueprintComponent{input})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Vendor != aiplatformv1alpha1.ComponentVendorSUSE {
		t.Fatalf("expected SUSE default profile, got %q", resolved[0].Vendor)
	}
}

func TestApplicationReferenceResolvesPrivateGitClusterRepo(t *testing.T) {
	application := &aiplatformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "example.private-agent"},
		Spec: aiplatformv1alpha1.ApplicationSpec{
			Chart: aiplatformv1alpha1.ApplicationChart{Name: "agent", SourceRef: "private-gitea"},
		},
	}
	repo := repoObj("private-gitea", map[string]any{
		"gitRepo":   "https://gitea.internal.example/charts",
		"gitBranch": "qualified",
	})
	scheme := gitRepoTestScheme()
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(application, repo).Build()
	reconciler := &AIWorkloadReconciler{Client: client, Scheme: scheme}

	resolved, err := reconciler.resolveBlueprintComponents(context.Background(), []aiplatformv1alpha1.BlueprintComponent{{
		ApplicationRef: &aiplatformv1alpha1.ApplicationReference{Name: application.Name, Version: "1.2.3"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	info, err := reconciler.resolveClusterRepo(context.Background(), resolved[0].ChartRepo)
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != repoKindGit || info.GitRepo != "https://gitea.internal.example/charts" || info.GitBranch != "qualified" {
		t.Fatalf("unexpected private Git source: %+v", info)
	}
	if resolved[0].ChartName != "agent" || resolved[0].ChartVersion != "1.2.3" {
		t.Fatalf("unexpected resolved package: %#v", resolved[0])
	}
}
