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
	"io"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func newBlueprintGitOpsRemote(t *testing.T) string {
	t.Helper()
	remoteDir := t.TempDir()
	if _, err := gogit.PlainInit(remoteDir, true); err != nil {
		t.Fatalf("init bare git remote: %v", err)
	}
	return "file://" + remoteDir
}

func readBlueprintGitOpsFile(t *testing.T, remoteURL, filePath string) string {
	t.Helper()
	repo, err := gogit.PlainClone(t.TempDir(), false, &gogit.CloneOptions{
		URL:           remoteURL,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		t.Fatalf("clone git remote: %v", err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("open git worktree: %v", err)
	}
	file, err := worktree.Filesystem.Open(filePath)
	if err != nil {
		t.Fatalf("open %s: %v", filePath, err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read %s: %v", filePath, err)
	}
	return string(content)
}

func newGitOpsTestWorkload() *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "private-source-workload", Namespace: "aif-operator"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: "application-system",
			TargetClusters:  []string{"local"},
		},
	}
}

func TestEnsureBlueprintGitFile_TracksClusterRepoEndpointChange(t *testing.T) {
	ctx := context.Background()
	remoteURL := newBlueprintGitOpsRemote(t)
	scheme := gitRepoTestScheme()
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{RepoURL: remoteURL, Branch: "main"},
		},
	}
	sourceA := repoObj("source-a", map[string]any{"url": "oci://registry-a.example/charts"})
	sourceBAuth := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "source-b-auth", Namespace: "cattle-system"},
		Type:       corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte("robot"),
			corev1.BasicAuthPasswordKey: []byte("secret"),
		},
	}
	workload := newGitOpsTestWorkload()
	client := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(settings, sourceA, sourceBAuth, workload).
		Build()
	reconciler := &AIWorkloadReconciler{
		Client:            client,
		Scheme:            scheme,
		OperatorNamespace: "aif-operator",
	}
	component := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo:    "source-a",
		ChartName:    "airgap-smoke",
		ChartVersion: "1.0.0",
		Vendor:       aiplatformv1alpha1.ComponentVendorSUSE,
	}
	const bundleName = "private-source-workload-airgap-smoke"
	const filePath = "workloads/private-source-workload-airgap-smoke.yaml"

	if _, err := reconciler.ensureBlueprintGitFile(ctx, workload, component, bundleName); err != nil {
		t.Fatalf("publish first source: %v", err)
	}

	if err := client.Get(ctx, types.NamespacedName{Name: sourceA.GetName()}, sourceA); err != nil {
		t.Fatalf("refresh ClusterRepo: %v", err)
	}
	_ = unstructured.SetNestedField(sourceA.Object, "oci://registry-b.example/charts", "spec", "url")
	_ = unstructured.SetNestedMap(sourceA.Object, map[string]any{
		"name":      "source-b-auth",
		"namespace": "cattle-system",
	}, "spec", "clientSecret")
	if err := client.Update(ctx, sourceA); err != nil {
		t.Fatalf("change ClusterRepo endpoint: %v", err)
	}
	if _, err := reconciler.ensureBlueprintGitFile(ctx, workload, component, bundleName); err != nil {
		t.Fatalf("publish switched source: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(readBlueprintGitOpsFile(t, remoteURL, filePath)), &object); err != nil {
		t.Fatalf("decode switched HelmOp: %v", err)
	}
	repo, _, _ := unstructured.NestedString(object, "spec", "helm", "repo")
	if repo != "oci://registry-b.example/charts/airgap-smoke" {
		t.Fatalf("switched HelmOp repo = %q", repo)
	}
	helmSecret, _, _ := unstructured.NestedString(object, "spec", "helmSecretName")
	if helmSecret != "source-b-auth" {
		t.Fatalf("switched HelmOp auth secret = %q", helmSecret)
	}

	copiedAuth := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: "fleet-local", Name: "source-b-auth"}, copiedAuth); err != nil {
		t.Fatalf("get switched source auth: %v", err)
	}
	if string(copiedAuth.Data[corev1.BasicAuthUsernameKey]) != "robot" ||
		string(copiedAuth.Data[corev1.BasicAuthPasswordKey]) != "secret" {
		t.Fatal("Fleet auth secret did not preserve source credentials")
	}
}

func TestEnsureBlueprintGitFile_RepublishesToChangedFleetRepository(t *testing.T) {
	ctx := context.Background()
	firstRemote := newBlueprintGitOpsRemote(t)
	secondRemote := newBlueprintGitOpsRemote(t)
	scheme := gitRepoTestScheme()
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: "aif-operator"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{RepoURL: firstRemote, Branch: "main"},
		},
	}
	source := repoObj("private-charts", map[string]any{"url": "oci://registry.example/charts"})
	workload := newGitOpsTestWorkload()
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(settings, source, workload).Build()
	reconciler := &AIWorkloadReconciler{
		Client:            client,
		Scheme:            scheme,
		OperatorNamespace: "aif-operator",
	}
	component := aiplatformv1alpha1.BlueprintComponent{
		ChartRepo:    "private-charts",
		ChartName:    "airgap-smoke",
		ChartVersion: "1.0.0",
	}
	const bundleName = "private-source-workload-airgap-smoke"
	const filePath = "workloads/private-source-workload-airgap-smoke.yaml"

	if _, err := reconciler.ensureBlueprintGitFile(ctx, workload, component, bundleName); err != nil {
		t.Fatalf("publish to first Git repository: %v", err)
	}
	firstContent := readBlueprintGitOpsFile(t, firstRemote, filePath)

	if err := client.Get(ctx, types.NamespacedName{Name: operatorSettingsName, Namespace: "aif-operator"}, settings); err != nil {
		t.Fatalf("refresh Settings: %v", err)
	}
	settings.Spec.Fleet.RepoURL = secondRemote
	if err := client.Update(ctx, settings); err != nil {
		t.Fatalf("change Fleet repository: %v", err)
	}
	if _, err := reconciler.ensureBlueprintGitFile(ctx, workload, component, bundleName); err != nil {
		t.Fatalf("republish to second Git repository: %v", err)
	}
	if secondContent := readBlueprintGitOpsFile(t, secondRemote, filePath); secondContent != firstContent {
		t.Fatal("republished GitOps manifest changed despite an unchanged Blueprint source")
	}
}
