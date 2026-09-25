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
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalogSource{
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

// A reserved catalog name is a per-catalog config mistake, not a reason to
// halt the whole Settings reconcile (registry mirrors, Fleet repo, status,
// etc. must still apply). Reconcile succeeds; the offending catalog is simply
// never materialized as a GitRepo.
func TestReconcile_RejectsReservedCatalogName(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalogSource{
				{Name: aiplatformv1alpha1.BlueprintCatalogDefault, GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example/x"}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns}}); err != nil {
		t.Fatalf("reconcile must not fail the whole Settings object over one bad catalog: %v", err)
	}

	reserved := &unstructured.Unstructured{}
	reserved.SetGroupVersionKind(gitRepoGVK())
	name := "blueprint-catalog-" + aiplatformv1alpha1.BlueprintCatalogDefault
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "fleet-local"}, reserved); err == nil {
		t.Fatal("expected no GitRepo for the reserved catalog name")
	}
}

// One catalog failing to apply (bad credential ref) must not stop a sibling
// catalog from reconciling successfully.
func TestReconcile_OneBadCatalogDoesNotBlockAnother(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalogSource{
				{Name: "broken", GitRepoSource: aiplatformv1alpha1.GitRepoSource{
					RepoURL: "https://git.example/broken",
					CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{
						Name: "does-not-exist", Key: "token",
					},
				}},
				{Name: "healthy", GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example/healthy"}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns}}); err != nil {
		t.Fatalf("reconcile must not fail over one broken catalog: %v", err)
	}

	healthy := &unstructured.Unstructured{}
	healthy.SetGroupVersionKind(gitRepoGVK())
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-healthy", Namespace: "fleet-local"}, healthy); err != nil {
		t.Fatalf("expected healthy catalog GitRepo despite sibling failure: %v", err)
	}

	broken := &unstructured.Unstructured{}
	broken.SetGroupVersionKind(gitRepoGVK())
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-broken", Namespace: "fleet-local"}, broken); err == nil {
		t.Fatal("expected no GitRepo for the catalog with an unresolvable credential")
	}
}

func TestReconcile_PrunesRemovedCatalog(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	// Pre-existing catalog GitRepo + credential mirror from a prior reconcile,
	// now not in spec.
	stale := &unstructured.Unstructured{}
	stale.SetGroupVersionKind(gitRepoGVK())
	stale.SetName("blueprint-catalog-old")
	stale.SetNamespace("fleet-local")
	stale.SetLabels(map[string]string{credentials.CatalogRepoLabel: credentials.LabelValueTrue})

	staleMirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "blueprint-catalog-old-cred", Namespace: "fleet-local"},
		Type:       corev1.SecretTypeBasicAuth,
		Data:       map[string][]byte{"username": []byte("bot"), "password": []byte("stale-token")},
	}

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalogSource{
				{Name: "kept", GitRepoSource: aiplatformv1alpha1.GitRepoSource{RepoURL: "https://git.example/kept"}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, stale, staleMirror).
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

	var oldMirror corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-old-cred", Namespace: "fleet-local"}, &oldMirror); err == nil {
		t.Fatal("expected stale catalog credential mirror to be pruned")
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
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalogSource{{
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
	const wantMirrorName = "blueprint-catalog-partner-acme-cred"
	csn, _, _ := unstructured.NestedString(gr.Object, "spec", "clientSecretName")
	if csn != wantMirrorName {
		t.Fatalf("clientSecretName=%q want %q", csn, wantMirrorName)
	}
	mirror := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: wantMirrorName, Namespace: "fleet-local"}, mirror); err != nil {
		t.Fatalf("expected mirrored git cred secret in fleet-local: %v", err)
	}
}

// Two catalogs whose source credential Secrets share a name in the Settings
// namespace must not collide on the same mirrored Secret in fleet-local — the
// mirror name is derived from the catalog, not from the source Secret's name.
func TestReconcile_CatalogCredentialMirrorsDoNotCollide(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	credA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: ns},
		Data:       map[string][]byte{"token": []byte("token-a"), "username": []byte("bot-a")},
	}
	// Simulate a second catalog's differently-scoped credential Secret that
	// happens to share the literal name "git-creds" — plausible if two teams
	// independently follow the same naming convention.
	credB := credA.DeepCopy()
	credB.Data = map[string][]byte{"token": []byte("token-b"), "username": []byte("bot-b")}
	credB.ObjectMeta = metav1.ObjectMeta{Name: "git-creds-team-b", Namespace: ns}

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			BlueprintCatalogs: []aiplatformv1alpha1.BlueprintCatalogSource{
				{Name: "team-a", GitRepoSource: aiplatformv1alpha1.GitRepoSource{
					RepoURL:       "https://git.example/team-a",
					CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-creds", Key: "token"},
				}},
				{Name: "team-b", GitRepoSource: aiplatformv1alpha1.GitRepoSource{
					RepoURL:       "https://git.example/team-b",
					CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-creds-team-b", Key: "token"},
				}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, credA, credB).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var mirrorA corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-team-a-cred", Namespace: "fleet-local"}, &mirrorA); err != nil {
		t.Fatalf("expected team-a mirror: %v", err)
	}
	if string(mirrorA.Data["password"]) != "token-a" {
		t.Fatalf("team-a mirror password=%q want token-a (got overwritten by team-b?)", mirrorA.Data["password"])
	}

	var mirrorB corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "blueprint-catalog-team-b-cred", Namespace: "fleet-local"}, &mirrorB); err != nil {
		t.Fatalf("expected team-b mirror: %v", err)
	}
	if string(mirrorB.Data["password"]) != "token-b" {
		t.Fatalf("team-b mirror password=%q want token-b", mirrorB.Data["password"])
	}
}
