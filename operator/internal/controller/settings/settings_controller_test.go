// aif-operator/internal/controller/settings/settings_controller_test.go
package settings_test

import (
	"context"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/SUSE/aif-operator/internal/controller/settings"
	"github.com/SUSE/aif-operator/internal/credentials"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSettingsController_CreatesFleetGitRepo(t *testing.T) {
	s := newScheme(t)
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "suse-ai-system"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				RepoURL: "https://github.com/example/ai-workloads",
				Branch:  "main",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: "suse-ai-system"}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: "suse-ai-system"},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	gitRepo := &unstructured.Unstructured{}
	gitRepo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepo",
	})
	err = c.Get(context.Background(), types.NamespacedName{
		Name: "suse-ai-fleet-repo", Namespace: "fleet-local",
	}, gitRepo)
	if err != nil {
		t.Fatalf("expected GitRepo to be created: %v", err)
	}
	repo, _, _ := unstructured.NestedString(gitRepo.Object, "spec", "repo")
	if repo != "https://github.com/example/ai-workloads" {
		t.Errorf("expected repo URL %q, got %q", "https://github.com/example/ai-workloads", repo)
	}
}

func TestSettingsController_DeletesFleetGitRepoWhenURLCleared(t *testing.T) {
	s := newScheme(t)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepo",
	})
	existing.SetName("suse-ai-fleet-repo")
	existing.SetNamespace("fleet-local")

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "suse-ai-system"},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, existing).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: "suse-ai-system"}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: "suse-ai-system"},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepo",
	})
	err = c.Get(context.Background(), types.NamespacedName{
		Name: "suse-ai-fleet-repo", Namespace: "fleet-local",
	}, got)
	if err == nil {
		t.Fatal("expected GitRepo to be deleted, but it still exists")
	}
}

func TestSettingsController_MirrorsGitCredSecret_TokenAuth(t *testing.T) {
	s := newScheme(t)
	srcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "suse-ai-system"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"token": []byte("mytoken")},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "suse-ai-system"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				RepoURL:  "https://github.com/example/ai-workloads",
				AuthType: "token",
				CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{
					Name: "git-creds",
					Key:  "token",
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, srcSecret).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: "suse-ai-system"}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: "suse-ai-system"},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var mirror corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: "git-creds", Namespace: "fleet-local",
	}, &mirror); err != nil {
		t.Fatalf("expected mirror secret in fleet-local: %v", err)
	}
	if mirror.Type != corev1.SecretTypeBasicAuth {
		t.Errorf("expected secret type %q, got %q", corev1.SecretTypeBasicAuth, mirror.Type)
	}
	if string(mirror.Data["password"]) != "mytoken" {
		t.Errorf("expected password=mytoken, got %q", string(mirror.Data["password"]))
	}
	if string(mirror.Data["username"]) != "token" {
		t.Errorf("expected username=token, got %q", string(mirror.Data["username"]))
	}
}

func TestSettingsController_MirrorsGitCredSecret_TypeChangeRecreates(t *testing.T) {
	s := newScheme(t)
	srcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "suse-ai-system"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"token": []byte("newtoken")},
	}
	// Stale mirror with wrong type already exists in fleet-local
	staleSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "fleet-local"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"token": []byte("oldtoken")},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "suse-ai-system"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				RepoURL:  "https://github.com/example/ai-workloads",
				AuthType: "token",
				CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{
					Name: "git-creds",
					Key:  "token",
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, srcSecret, staleSecret).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: "suse-ai-system"}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: "suse-ai-system"},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var mirror corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: "git-creds", Namespace: "fleet-local",
	}, &mirror); err != nil {
		t.Fatalf("expected mirror secret in fleet-local after type change: %v", err)
	}
	if mirror.Type != corev1.SecretTypeBasicAuth {
		t.Errorf("expected secret type %q after recreate, got %q", corev1.SecretTypeBasicAuth, mirror.Type)
	}
	if string(mirror.Data["password"]) != "newtoken" {
		t.Errorf("expected password=newtoken, got %q", string(mirror.Data["password"]))
	}
}

func TestSettingsController_StatusUpdateSurvivesTransientConflict(t *testing.T) {
	s := newScheme(t)
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}

	// Inject one transient conflict on the first status write, mimicking the
	// optimistic-concurrency race we observed live (the object is modified
	// between the spec patch / secret re-enqueue and the status write).
	conflicts := 0
	conflict := func() error {
		conflicts++
		if conflicts == 1 {
			return apierrors.NewConflict(
				schema.GroupResource{Group: "ai-factory.suse.com", Resource: "settings"},
				credentials.SettingsName,
				context.DeadlineExceeded, // any wrapped error; only the Conflict status matters
			)
		}
		return nil
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, cl client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if err := conflict(); err != nil {
					return err
				}
				return cl.Status().Update(ctx, obj, opts...)
			},
		}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile should survive a transient status conflict, got: %v", err)
	}

	var updated aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.SettingsName, Namespace: ns}, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.LastApplied == nil {
		t.Fatal("expected status.lastApplied to be set after retry")
	}
}

func TestSettingsController_PrunesClusterRepoWhenCredsRemoved(t *testing.T) {
	s := newScheme(t)
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})

	const ns = "aif-operator"
	// Settings with no refs, and no well-known secrets present — creds gone.
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	// Leftover ClusterRepo + cattle-system mirror from when creds existed.
	leftoverRepo := &unstructured.Unstructured{}
	leftoverRepo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	})
	leftoverRepo.SetName(credentials.ClusterRepoApplicationCollection)
	leftoverMirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system"},
		Type:       corev1.SecretTypeBasicAuth,
		Data:       map[string][]byte{"username": []byte("u"), "password": []byte("p")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, leftoverRepo, leftoverMirror).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	})
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.ClusterRepoApplicationCollection}, repo); err == nil {
		t.Fatal("expected application-collection ClusterRepo to be pruned, but it still exists")
	}
	var mirror corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system",
	}, &mirror); err == nil {
		t.Fatal("expected application-collection-auth mirror to be pruned, but it still exists")
	}
}

func TestSettingsController_WiresWellKnownSecretsAndCreatesClusterRepos(t *testing.T) {
	s := newScheme(t)
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})

	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	appco := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: ns},
		Data: map[string][]byte{
			"user":  []byte("user@suse.com"),
			"token": []byte("appco-token"),
		},
	}
	nvidia := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nvidia", Namespace: ns},
		Data: map[string][]byte{
			"user":  []byte("$oauthtoken"),
			"token": []byte("nvapi-test"),
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, appco, nvidia).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var updated aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.SettingsName, Namespace: ns}, &updated); err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if updated.Spec.ApplicationCollection.UserSecretRef == nil || updated.Spec.ApplicationCollection.UserSecretRef.Name != "appco" {
		t.Fatalf("expected appco wired into settings, got %+v", updated.Spec.ApplicationCollection)
	}
	if updated.Spec.Nvidia.UserSecretRef == nil || updated.Spec.Nvidia.UserSecretRef.Name != "nvidia" {
		t.Fatalf("expected nvidia wired into settings, got %+v", updated.Spec.Nvidia)
	}

	var acAuth corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system",
	}, &acAuth); err != nil {
		t.Fatalf("expected application-collection-auth in cattle-system: %v", err)
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	})
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.ClusterRepoApplicationCollection}, repo); err != nil {
		t.Fatalf("expected application-collection ClusterRepo: %v", err)
	}
	secretName, _, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name")
	if secretName != credentials.AuthSecretApplicationCollection {
		t.Errorf("ClusterRepo clientSecret = %q, want %q", secretName, credentials.AuthSecretApplicationCollection)
	}

	// The blueprint repo (https://helm.ngc.nvidia.com/nvidia/blueprint) is PUBLIC,
	// so it must be created ANONYMOUS just like the /nvidia charts repo. Presenting
	// an NGC key that is not entitled to a path makes NGC return 403 (surfaced by
	// Rancher as "no API version specified"); anonymous access serves the public
	// index. Regression guard.
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.ClusterRepoNvidiaBlueprint}, repo); err != nil {
		t.Fatalf("expected nvidia-blueprints ClusterRepo: %v", err)
	}
	if nvSecret, found, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name"); found && nvSecret != "" {
		t.Errorf("nvidia-blueprints ClusterRepo must be anonymous, got clientSecret = %q", nvSecret)
	}

	// In connected mode both NGC repos are anonymous, so the ngc-helm-auth mirror
	// is unused and must not be created in any consuming namespace. Guards against
	// an orphan/stale mirror lingering after a connected-mode reconcile.
	for _, ns := range []string{"cattle-system", "fleet-local", "fleet-default"} {
		authSec := &corev1.Secret{}
		err := c.Get(context.Background(), types.NamespacedName{Name: credentials.AuthSecretNvidia, Namespace: ns}, authSec)
		if err == nil {
			t.Errorf("expected no %s in namespace %s (connected mode is anonymous)", credentials.AuthSecretNvidia, ns)
		} else if !apierrors.IsNotFound(err) {
			t.Errorf("unexpected error checking %s in %s: %v", credentials.AuthSecretNvidia, ns, err)
		}
	}

	// The public NGC charts catalog must also be ANONYMOUS (no clientSecret).
	pubRepo := &unstructured.Unstructured{}
	pubRepo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	})
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.ClusterRepoNvidia}, pubRepo); err != nil {
		t.Fatalf("expected nvidia ClusterRepo: %v", err)
	}
	if pubSecret, found, _ := unstructured.NestedString(pubRepo.Object, "spec", "clientSecret", "name"); found && pubSecret != "" {
		t.Errorf("public nvidia ClusterRepo must be anonymous, got clientSecret = %q", pubSecret)
	}
}

// registerClusterRepoTypes teaches the fake client about the unstructured
// ClusterRepo GVKs used across the rotation tests below.
func registerClusterRepoTypes(s *runtime.Scheme) {
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList",
	}, &unstructured.UnstructuredList{})
}

func getClusterRepo(t *testing.T, c client.Client, name string) *unstructured.Unstructured {
	t.Helper()
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	})
	if err := c.Get(context.Background(), types.NamespacedName{Name: name}, repo); err != nil {
		t.Fatalf("get ClusterRepo %s: %v", name, err)
	}
	return repo
}

// A rotated registry credential must make the operator nudge the ClusterRepo
// (spec.forceUpdate) so Rancher re-reads the clientSecret and re-authenticates.
// Updating the mirror secret alone leaves Rancher serving the cached (often
// 401) auth state until its ~1h periodic retry.
func TestSettingsController_ForceUpdatesClusterRepoOnCredentialChange(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	// Well-known source secret carrying the NEW token.
	src := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.ClusterRepoApplicationCollection, Namespace: ns},
		Data:       map[string][]byte{"user": []byte("u@suse.com"), "token": []byte("new-token")},
	}
	// Existing cattle-system mirror still holding the OLD token (pre-rotation).
	mirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system"},
		Type:       corev1.SecretTypeBasicAuth,
		Data:       map[string][]byte{"username": []byte("u@suse.com"), "password": []byte("old-token")},
	}
	// Existing ClusterRepo with no forceUpdate yet.
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	repo.SetName(credentials.ClusterRepoApplicationCollection)
	_ = unstructured.SetNestedField(repo.Object, credentials.DefaultApplicationCollectionURL, "spec", "url")

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, src, mirror, repo).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got := getClusterRepo(t, c, credentials.ClusterRepoApplicationCollection)
	if fu, _, _ := unstructured.NestedString(got.Object, "spec", "forceUpdate"); fu == "" {
		t.Errorf("expected spec.forceUpdate to be set after credential change, got empty")
	}
}

// When the mirror already matches the source credentials (no rotation), the
// operator must NOT bump forceUpdate — otherwise every reconcile would churn
// the ClusterRepo into a re-download.
func TestSettingsController_NoForceUpdateWhenCredentialsUnchanged(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	src := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.ClusterRepoApplicationCollection, Namespace: ns},
		Data:       map[string][]byte{"user": []byte("u@suse.com"), "token": []byte("same-token")},
	}
	// Mirror already in sync with the source.
	mirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system"},
		Type:       corev1.SecretTypeBasicAuth,
		Data:       map[string][]byte{"username": []byte("u@suse.com"), "password": []byte("same-token")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, src, mirror).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got := getClusterRepo(t, c, credentials.ClusterRepoApplicationCollection)
	if fu, found, _ := unstructured.NestedString(got.Object, "spec", "forceUpdate"); found && fu != "" {
		t.Errorf("expected no forceUpdate when credentials unchanged, got %q", fu)
	}
}
