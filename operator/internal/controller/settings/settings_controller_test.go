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

// aif-operator/internal/controller/settings/settings_controller_test.go
package settings_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
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
	paths, found, err := unstructured.NestedStringSlice(gitRepo.Object, "spec", "paths")
	if err != nil || !found {
		t.Fatalf("GitRepo paths missing: found=%v err=%v", found, err)
	}
	if len(paths) != 2 || paths[0] != "blueprints" || paths[1] != "workloads" {
		t.Fatalf("GitRepo paths=%v, want [blueprints workloads]", paths)
	}
}

func TestSettingsController_FleetGitRepoUsesConfiguredPrivateCA(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	caPEM := registryTestCAPEM(t)
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				RepoURL:           "https://gitea.internal.example/aif.git",
				Branch:            "main",
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-ca", Key: "ca.crt"},
			},
		},
	}
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-ca", Namespace: ns},
		Data:       map[string][]byte{"ca.crt": caPEM},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, caSecret).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	gitRepo := &unstructured.Unstructured{}
	gitRepo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "GitRepo",
	})
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: "suse-ai-fleet-repo", Namespace: "fleet-local",
	}, gitRepo); err != nil {
		t.Fatalf("get GitRepo: %v", err)
	}
	got, found, err := unstructured.NestedString(gitRepo.Object, "spec", "caBundle")
	if err != nil || !found {
		t.Fatalf("GitRepo spec.caBundle missing: found=%v err=%v", found, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("GitRepo spec.caBundle is not JSON base64: %v", err)
	}
	if !bytes.Equal(decoded, caPEM) {
		t.Error("decoded GitRepo spec.caBundle does not match the configured Secret key")
	}
}

func TestSettingsController_RejectsInvalidFleetGitCA(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				RepoURL:           "https://gitea.internal.example/aif.git",
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-ca", Key: "ca.crt"},
			},
		},
	}
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-ca", Namespace: ns},
		Data:       map[string][]byte{"ca.crt": []byte("not a certificate")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, caSecret).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns},
	})
	if err == nil || !strings.Contains(err.Error(), "does not contain a PEM certificate") {
		t.Fatalf("reconcile error=%v, want invalid PEM error", err)
	}
}

func TestSettingsController_RejectsFleetGitAuthWithoutCredentials(t *testing.T) {
	s := newScheme(t)
	const ns = "suse-ai-system"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				RepoURL:  "https://gitea.internal.example/aif.git",
				AuthType: "token",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "settings", Namespace: ns},
	})
	if err == nil || !strings.Contains(err.Error(), "requires fleet.credSecretRef") {
		t.Fatalf("reconcile error=%v, want incomplete Git authentication error", err)
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
				Username: "git-user",
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
	if string(mirror.Data["username"]) != "git-user" {
		t.Errorf("expected username=git-user, got %q", string(mirror.Data["username"]))
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

	// The bundled catalog references only the two org repos, so there are no gated
	// team repos to consume ngc-helm-auth — the connected-mode reconcile must NOT
	// write it in any namespace. Regression guard: if a gated team repo is ever
	// re-added to the catalog, this expectation (and the dormant-feature tests)
	// must be revisited.
	for _, authNS := range []string{"cattle-system", "fleet-local", "fleet-default"} {
		var nvAuth corev1.Secret
		err := c.Get(context.Background(), types.NamespacedName{Name: credentials.AuthSecretNvidia, Namespace: authNS}, &nvAuth)
		if !apierrors.IsNotFound(err) {
			t.Errorf("expected no ngc-helm-auth in %s (no gated team repos in catalog), got err=%v", authNS, err)
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

func registryTestCAPEM(t *testing.T) []byte {
	t.Helper()
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
}

func TestSettingsController_MirrorsRegistryCAAndDetectsRotation(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"
	oldCA := append(registryTestCAPEM(t), '\n')
	newCA := registryTestCAPEM(t)

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
				UserSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "appco-creds", Key: "user"},
				TokenSecretRef:    &aiplatformv1alpha1.SecretKeyRef{Name: "appco-creds", Key: "token"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "registry-ca", Key: "ca.crt"},
			},
			RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{
				ApplicationCollection: "oci://harbor.example.test/appco/charts",
			},
		},
	}
	creds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco-creds", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("robot"), "token": []byte("secret")},
	}
	ca := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "registry-ca", Namespace: ns},
		Data:       map[string][]byte{"ca.crt": newCA},
	}
	staleMirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system"},
		Type:       corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			"username": []byte("robot"),
			"password": []byte("secret"),
			"cacerts":  oldCA,
		},
	}
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	repo.SetName(credentials.ClusterRepoApplicationCollection)
	_ = unstructured.SetNestedField(repo.Object, credentials.DefaultApplicationCollectionURL, "spec", "url")
	_ = unstructured.SetNestedField(repo.Object, "before-ca-rotation", "spec", "forceUpdate")

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, creds, ca, staleMirror, repo).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, targetNS := range []string{"cattle-system", "fleet-local", "fleet-default"} {
		var mirror corev1.Secret
		if err := c.Get(context.Background(), types.NamespacedName{
			Name: credentials.AuthSecretApplicationCollection, Namespace: targetNS,
		}, &mirror); err != nil {
			t.Fatalf("get %s auth mirror: %v", targetNS, err)
		}
		if mirror.Type != corev1.SecretTypeBasicAuth {
			t.Errorf("%s secret type=%q want %q", targetNS, mirror.Type, corev1.SecretTypeBasicAuth)
		}
		if !bytes.Equal(mirror.Data["cacerts"], newCA) {
			t.Errorf("%s cacerts was not updated to the configured CA", targetNS)
		}
	}

	gotRepo := getClusterRepo(t, c, credentials.ClusterRepoApplicationCollection)
	forceUpdate, _, _ := unstructured.NestedString(gotRepo.Object, "spec", "forceUpdate")
	if forceUpdate == "" || forceUpdate == "before-ca-rotation" {
		t.Errorf("expected CA rotation to force-update ClusterRepo, got %q", forceUpdate)
	}
}

func TestSettingsController_RemovesStaleRegistryCAWhenReferenceCleared(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "appco-creds", Key: "user"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "appco-creds", Key: "token"},
			},
		},
	}
	creds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco-creds", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("robot"), "token": []byte("secret")},
	}
	staleMirror := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system"},
		Type:       corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			"username": []byte("robot"),
			"password": []byte("secret"),
			"cacerts":  registryTestCAPEM(t),
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, creds, staleMirror).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var mirror corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system",
	}, &mirror); err != nil {
		t.Fatal(err)
	}
	if _, found := mirror.Data["cacerts"]; found {
		t.Error("stale cacerts key remains after caBundleSecretRef was cleared")
	}
}

func TestSettingsController_RejectsUnreadableRegistryCA(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
				UserSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "appco-creds", Key: "user"},
				TokenSecretRef:    &aiplatformv1alpha1.SecretKeyRef{Name: "appco-creds", Key: "token"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "registry-ca", Key: "ca.crt"},
			},
		},
	}
	creds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco-creds", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("robot"), "token": []byte("secret")},
	}
	badCA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "registry-ca", Namespace: ns},
		Data:       map[string][]byte{"ca.crt": []byte("not a certificate")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, creds, badCA).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	})
	if err == nil || !strings.Contains(err.Error(), "valid PEM certificate") {
		t.Fatalf("expected invalid CA error, got %v", err)
	}

	var mirror corev1.Secret
	err = c.Get(context.Background(), types.NamespacedName{
		Name: credentials.AuthSecretApplicationCollection, Namespace: "cattle-system",
	}, &mirror)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("auth mirror must not be created for invalid CA, got err=%v", err)
	}
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

// The bundled catalog references only the two org repos (/nvidia and
// /nvidia/blueprint), so connected-mode reconcile must provision NO NGC team
// repos and NO ngc-helm-auth — the team-repo feature is dormant until a team
// repo is re-added to the catalog. The org and blueprint repos are still created
// anonymously. Regression guard for the org-only catalog.
func TestSettingsController_OrgOnlyCatalogProvisionsNoTeamRepos(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"

	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
	}
	nvidia := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nvidia", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("$oauthtoken"), "token": []byte("nvapi-test")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, nvidia).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// The org and blueprint repos ARE created, anonymously.
	for _, name := range []string{credentials.ClusterRepoNvidia, credentials.ClusterRepoNvidiaBlueprint} {
		repo := getClusterRepo(t, c, name)
		if secret, found, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name"); found && secret != "" {
			t.Errorf("org repo %s must be anonymous, got clientSecret %q", name, secret)
		}
	}

	// No team repos are provisioned (formerly nvidia-omniverse public,
	// nvidia-cuopt gated — both sourced from repos no longer in the catalog).
	for _, name := range []string{"nvidia-omniverse", "nvidia-cuopt"} {
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
		if err := c.Get(context.Background(), types.NamespacedName{Name: name}, got); !apierrors.IsNotFound(err) {
			t.Errorf("expected no team repo %s from org-only catalog, got err=%v", name, err)
		}
	}

	// No ngc-helm-auth written (no gated repo consumes it).
	var authSec corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.AuthSecretNvidia, Namespace: "cattle-system"}, &authSec); !apierrors.IsNotFound(err) {
		t.Errorf("expected no ngc-helm-auth from org-only catalog, got err=%v", err)
	}
}

// A team ClusterRepo whose URL is no longer in the catalog is deleted on
// reconcile. Seed a bogus marker-labelled repo; it must be pruned.
func TestSettingsController_PrunesOrphanTeamRepo(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns}}
	nvidia := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nvidia", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("$oauthtoken"), "token": []byte("nvapi-test")},
	}
	orphan := &unstructured.Unstructured{}
	orphan.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	orphan.SetName("nvidia-gone-from-catalog")
	orphan.SetLabels(map[string]string{"ai-factory.suse.com/nvidia-team-repo": "true"})
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, nvidia, orphan).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	err := c.Get(context.Background(), types.NamespacedName{Name: "nvidia-gone-from-catalog"}, got)
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected orphan team repo pruned, got err=%v", err)
	}
}

// Switching to air-gap deletes team repos, preserves ngc-helm-auth, and keeps
// both stable org/Blueprint repo identities backed by the private mirror.
func TestSettingsController_AirGapKeepsStableNvidiaRepoAliases(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"
	const mirrorURL = "oci://registry.internal/nvidia"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{Nvidia: mirrorURL},
		},
	}
	nvidia := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nvidia", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("$oauthtoken"), "token": []byte("nvapi-test")},
	}
	staleTeam := &unstructured.Unstructured{}
	staleTeam.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	staleTeam.SetName("nvidia-cuopt")
	staleTeam.SetLabels(map[string]string{"ai-factory.suse.com/nvidia-team-repo": "true"})
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, nvidia, staleTeam).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()
	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Team repo deleted.
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	if err := c.Get(context.Background(), types.NamespacedName{Name: "nvidia-cuopt"}, got); !apierrors.IsNotFound(err) {
		t.Errorf("expected team repo pruned in air-gap, got err=%v", err)
	}
	// ngc-helm-auth preserved in cattle-system (air-gap mirror needs it).
	var authSec corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.AuthSecretNvidia, Namespace: "cattle-system"}, &authSec); err != nil {
		t.Errorf("air-gap must preserve ngc-helm-auth: %v", err)
	}

	for _, name := range []string{credentials.ClusterRepoNvidia, credentials.ClusterRepoNvidiaBlueprint} {
		repo := getClusterRepo(t, c, name)
		url, _, _ := unstructured.NestedString(repo.Object, "spec", "url")
		if url != mirrorURL {
			t.Errorf("%s URL=%q want private mirror %q", name, url, mirrorURL)
		}
		secretName, _, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name")
		secretNamespace, _, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "namespace")
		if secretName != credentials.AuthSecretNvidia || secretNamespace != "cattle-system" {
			t.Errorf("%s clientSecret=%s/%s want cattle-system/%s", name, secretNamespace, secretName, credentials.AuthSecretNvidia)
		}
		if forceUpdate, _, _ := unstructured.NestedString(repo.Object, "spec", "forceUpdate"); forceUpdate == "" {
			t.Errorf("%s was not force-updated after the initial mirror credential write", name)
		}
	}
}
