package settings_test

import (
	"context"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/controller/settings"
	"github.com/SUSE/aif-operator/internal/credentials"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func gitRepoGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepo"}
}

func TestReconcile_CreatesCatalogGitRepos(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalog{
				{Name: "partner-acme", Paths: []string{"catalog/blueprints"},
					GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example/acme", Branch: "main"}},
				{Name: "partner-beta",
					GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example/beta"}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// acme: explicit path
	acme := &unstructured.Unstructured{}
	acme.SetGroupVersionKind(gitRepoGVK())
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-partner-acme", Namespace: "fleet-local"}, acme); err != nil {
		t.Fatalf("acme GitRepo missing: %v", err)
	}
	paths, _, _ := unstructured.NestedStringSlice(acme.Object, "spec", "paths")
	if len(paths) != 1 || paths[0] != "catalog/blueprints" {
		t.Fatalf("acme paths=%v want [catalog/blueprints]", paths)
	}
	if acme.GetLabels()[credentials.CatalogRepoLabel] != credentials.LabelValueTrue {
		t.Fatalf("acme missing catalog marker label")
	}

	// beta: default path
	beta := &unstructured.Unstructured{}
	beta.SetGroupVersionKind(gitRepoGVK())
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-partner-beta", Namespace: "fleet-local"}, beta); err != nil {
		t.Fatalf("beta GitRepo missing: %v", err)
	}
	bpaths, _, _ := unstructured.NestedStringSlice(beta.Object, "spec", "paths")
	if len(bpaths) != 1 || bpaths[0] != "blueprints" {
		t.Fatalf("beta paths=%v want [blueprints]", bpaths)
	}
}

func TestReconcile_RejectsReservedCatalogName(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalog{
				{Name: aiplatformv1alpha1.BlueprintCatalogDefault,
					GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example/x"}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns}}); err == nil {
		t.Fatal("expected error for reserved catalog name suse-default")
	}
}

func TestReconcile_PrunesRemovedCatalog(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	// Pre-existing catalog GitRepo from a prior reconcile, now not in spec.
	stale := &unstructured.Unstructured{}
	stale.SetGroupVersionKind(gitRepoGVK())
	stale.SetName("blueprint-catalog-old")
	stale.SetNamespace("fleet-local")
	stale.SetLabels(map[string]string{credentials.CatalogRepoLabel: credentials.LabelValueTrue})

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalog{
				{Name: "kept", GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example/kept"}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, stale).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	old := &unstructured.Unstructured{}
	old.SetGroupVersionKind(gitRepoGVK())
	err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-old", Namespace: "fleet-local"}, old)
	if err == nil {
		t.Fatal("expected stale catalog GitRepo to be pruned")
	}

	kept := &unstructured.Unstructured{}
	kept.SetGroupVersionKind(gitRepoGVK())
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-kept", Namespace: "fleet-local"}, kept); err != nil {
		t.Fatalf("expected kept catalog GitRepo to survive: %v", err)
	}
}

func TestReconcile_CatalogWiresPrivateCredential(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	credSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-git", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("s3cr3t"), "username": []byte("acme-bot")},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalog{{
				Name: "partner-acme",
				GitRepoSource: aiplatformv1alpha1.GitRepoSource{
					RepoURL:       "https://git.example/acme",
					CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "acme-git", Key: "token"},
				},
			}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, credSecret).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	gr := &unstructured.Unstructured{}
	gr.SetGroupVersionKind(gitRepoGVK())
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-partner-acme", Namespace: "fleet-local"}, gr); err != nil {
		t.Fatalf("catalog GitRepo missing: %v", err)
	}
	csn, _, _ := unstructured.NestedString(gr.Object, "spec", "clientSecretName")
	if csn != "acme-git" {
		t.Fatalf("clientSecretName=%q want acme-git", csn)
	}
	mirror := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "acme-git", Namespace: "fleet-local"}, mirror); err != nil {
		t.Fatalf("expected mirrored git cred secret in fleet-local: %v", err)
	}
}
