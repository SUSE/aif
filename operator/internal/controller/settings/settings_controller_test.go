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

	"github.com/SUSE/aif-operator/internal/catalog"
	"github.com/SUSE/aif-operator/internal/controller/settings"
	"github.com/SUSE/aif-operator/internal/credentials"
)

// Provenance markers stamped by the reconciler. Aliased to the exported
// credentials constants (the single source of truth the UI also mirrors) so a
// rename can't leave these tests passing against a stale literal.
const (
	managedRepoLabel = credentials.ManagedRepoLabel
	teamRepoLabel    = credentials.TeamRepoLabel
	markerValueTrue  = credentials.LabelValueTrue
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

func TestSettingsController_MirrorsGitHTTPSCredential(t *testing.T) {
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

func TestSettingsController_MirrorsGitHTTPSUsernameFromSecret(t *testing.T) {
	s := newScheme(t)
	srcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "suse-ai-system"},
		Type:       corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			corev1.BasicAuthUsernameKey: []byte("git-user"),
			corev1.BasicAuthPasswordKey: []byte("mytoken"),
		},
	}
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "suse-ai-system"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				RepoURL: "https://github.com/example/ai-workloads",
				CredSecretRef: &aiplatformv1alpha1.SecretKeyRef{
					Name: "git-creds",
					Key:  corev1.BasicAuthPasswordKey,
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
	if string(mirror.Data[corev1.BasicAuthUsernameKey]) != "git-user" {
		t.Errorf("expected username=git-user, got %q", string(mirror.Data[corev1.BasicAuthUsernameKey]))
	}
	if string(mirror.Data[corev1.BasicAuthPasswordKey]) != "mytoken" {
		t.Errorf("expected password=mytoken, got %q", string(mirror.Data[corev1.BasicAuthPasswordKey]))
	}
}

func TestSettingsController_MirrorsGitHTTPSDefaultUsername(t *testing.T) {
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
				RepoURL: "https://gitlab.example.com/example/ai-workloads",
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
	if got := string(mirror.Data[corev1.BasicAuthUsernameKey]); got != credentials.DefaultGitHTTPSUsername {
		t.Errorf("username=%q, want %q", got, credentials.DefaultGitHTTPSUsername)
	}
	if got := string(mirror.Data[corev1.BasicAuthPasswordKey]); got != "mytoken" {
		t.Errorf("password=%q, want mytoken", got)
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
	if string(mirror.Data["username"]) != "token" {
		t.Errorf("expected legacy username=token, got %q", string(mirror.Data["username"]))
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

	// The bundled catalog references gated NGC team repos, so connected-mode
	// reconcile MUST write ngc-helm-auth into every managed namespace — the gated
	// ClusterRepos' clientSecret resolves against it.
	for _, authNS := range []string{"cattle-system", "fleet-local", "fleet-default"} {
		var nvAuth corev1.Secret
		if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.AuthSecretNvidia, Namespace: authNS}, &nvAuth); err != nil {
			t.Errorf("expected ngc-helm-auth in %s (gated team repos present), got err=%v", authNS, err)
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

// The bundled catalog now references NVAIE NGC team repos, so connected-mode
// reconcile provisions them from the embedded classification: public repos
// anonymously, gated repos with the ngc-helm-auth clientSecret, plus the
// ngc-helm-auth mirror in every managed namespace. The org and blueprint repos
// remain anonymous. (Supersedes the former org-only dormant-feature guard.)
// Expectations are derived from ClassifyNGCTeamRepos so a weekly catalog refresh
// that changes the team-repo set does not silently drift this test.
func TestSettingsController_ProvisionsNGCTeamReposFromCatalog(t *testing.T) {
	teams := catalog.ClassifyNGCTeamRepos()
	if len(teams.Gated) == 0 {
		t.Fatalf("precondition: bundled catalog must reference >=1 gated NGC team repo, got %d public / %d gated",
			len(teams.Public), len(teams.Gated))
	}

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

	// The org and blueprint repos are created anonymously.
	for _, name := range []string{credentials.ClusterRepoNvidia, credentials.ClusterRepoNvidiaBlueprint} {
		repo := getClusterRepo(t, c, name)
		if secret, found, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name"); found && secret != "" {
			t.Errorf("org repo %s must be anonymous, got clientSecret %q", name, secret)
		}
	}

	// Every marker-labelled team repo the reconcile provisioned must match its NGC
	// classification: public -> anonymous, gated -> ngc-helm-auth clientSecret.
	pub := map[string]bool{}
	for _, u := range teams.Public {
		pub[strings.TrimRight(u, "/")] = true
	}
	gated := map[string]bool{}
	for _, u := range teams.Gated {
		gated[strings.TrimRight(u, "/")] = true
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepoList"})
	if err := c.List(context.Background(), list, client.MatchingLabels{teamRepoLabel: markerValueTrue}); err != nil {
		t.Fatalf("list team ClusterRepos: %v", err)
	}
	if got, want := len(list.Items), len(teams.Public)+len(teams.Gated); got != want {
		t.Errorf("provisioned %d team repos, want %d (from catalog classification)", got, want)
	}
	for i := range list.Items {
		repo := &list.Items[i]
		u, _, _ := unstructured.NestedString(repo.Object, "spec", "url")
		u = strings.TrimRight(u, "/")
		secret, _, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name")
		switch {
		case pub[u]:
			if secret != "" {
				t.Errorf("public team repo %s (%s) must be anonymous, got clientSecret %q", repo.GetName(), u, secret)
			}
		case gated[u]:
			if secret != credentials.AuthSecretNvidia {
				t.Errorf("gated team repo %s (%s) must use clientSecret %q, got %q", repo.GetName(), u, credentials.AuthSecretNvidia, secret)
			}
		default:
			t.Errorf("provisioned team repo %s has URL %q not in catalog classification", repo.GetName(), u)
		}
	}

	// The ngc-helm-auth mirror exists in every managed namespace (>=1 gated repo).
	for _, authNS := range []string{"cattle-system", "fleet-local", "fleet-default"} {
		var authSec corev1.Secret
		if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.AuthSecretNvidia, Namespace: authNS}, &authSec); err != nil {
			t.Errorf("expected ngc-helm-auth in %s (gated team repos present), got err=%v", authNS, err)
		}
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
	orphan.SetLabels(map[string]string{teamRepoLabel: markerValueTrue})
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

// Switching from connected NGC to an air-gap mirror preserves every catalog-
// derived team repo identity and repoints it, along with the org/Blueprint
// aliases, at the private mirror. AIWorkloads pin chartRepo by name, so deleting
// or renaming any of these aliases would break a later upgrade or redeploy.
func TestSettingsController_AirGapPreservesAllNvidiaRepoAliases(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"
	const mirrorURL = "oci://registry.internal/nvidia"
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

	// First reconcile in connected mode and capture the catalog-derived logical
	// identities before changing the endpoint.
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("connected reconcile: %v", err)
	}
	teams := catalog.ClassifyNGCTeamRepos()
	expectedTeamNames := make(map[string]string, len(teams.Public)+len(teams.Gated))
	gatedURLs := make(map[string]bool, len(teams.Gated))
	for _, u := range teams.Gated {
		gatedURLs[u] = true
	}
	for _, u := range append(teams.Public, teams.Gated...) {
		name, err := catalog.NGCClusterRepoName(u)
		if err != nil {
			t.Fatalf("derive ClusterRepo name for %q: %v", u, err)
		}
		expectedTeamNames[name] = u
		repo := getClusterRepo(t, c, name)
		if gotURL, _, _ := unstructured.NestedString(repo.Object, "spec", "url"); gotURL != u {
			t.Errorf("connected %s URL=%q want %q", name, gotURL, u)
		}
	}
	if len(expectedTeamNames) == 0 {
		t.Fatal("precondition: bundled catalog yielded no NVIDIA team repos")
	}

	// An obsolete operator-owned team alias should still be pruned in mirror mode.
	orphan := &unstructured.Unstructured{}
	orphan.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	orphan.SetName("nvidia-gone-from-catalog")
	orphan.SetLabels(map[string]string{teamRepoLabel: markerValueTrue})
	if err := c.Create(context.Background(), orphan); err != nil {
		t.Fatalf("create orphan team repo: %v", err)
	}

	var stored aiplatformv1alpha1.Settings
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.SettingsName, Namespace: ns}, &stored); err != nil {
		t.Fatalf("get Settings before mode switch: %v", err)
	}
	stored.Spec.RegistryEndpoints = &aiplatformv1alpha1.RegistryEndpointsSettings{Nvidia: mirrorURL}
	if err := c.Update(context.Background(), &stored); err != nil {
		t.Fatalf("switch Settings to mirror: %v", err)
	}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("mirror reconcile: %v", err)
	}

	gotOrphan := &unstructured.Unstructured{}
	gotOrphan.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	if err := c.Get(context.Background(), types.NamespacedName{Name: orphan.GetName()}, gotOrphan); !apierrors.IsNotFound(err) {
		t.Errorf("expected obsolete team repo pruned in air-gap, got err=%v", err)
	}

	// ngc-helm-auth preserved in cattle-system (air-gap mirror needs it).
	var authSec corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.AuthSecretNvidia, Namespace: "cattle-system"}, &authSec); err != nil {
		t.Errorf("air-gap must preserve ngc-helm-auth: %v", err)
	}

	allExpectedNames := []string{credentials.ClusterRepoNvidia, credentials.ClusterRepoNvidiaBlueprint}
	for name := range expectedTeamNames {
		allExpectedNames = append(allExpectedNames, name)
	}
	for _, name := range allExpectedNames {
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
		if repo.GetLabels()[managedRepoLabel] != markerValueTrue {
			t.Errorf("%s missing managed-repo label, got labels=%v", name, repo.GetLabels())
		}
		if _, isTeam := expectedTeamNames[name]; isTeam && repo.GetLabels()[teamRepoLabel] != markerValueTrue {
			t.Errorf("team alias %s missing team-repo label, got labels=%v", name, repo.GetLabels())
		}
	}

	// Switching back to connected mode must keep the same names, restore each
	// public source URL, and restore the source-specific authentication model.
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.SettingsName, Namespace: ns}, &stored); err != nil {
		t.Fatalf("get Settings before connected switch: %v", err)
	}
	stored.Spec.RegistryEndpoints = nil
	if err := c.Update(context.Background(), &stored); err != nil {
		t.Fatalf("switch Settings back to connected: %v", err)
	}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("connected restore reconcile: %v", err)
	}

	fixedURLs := map[string]string{
		credentials.ClusterRepoNvidia:          credentials.DefaultNvidiaChartsURL,
		credentials.ClusterRepoNvidiaBlueprint: credentials.DefaultNvidiaBlueprintURL,
	}
	for name, wantURL := range fixedURLs {
		repo := getClusterRepo(t, c, name)
		if gotURL, _, _ := unstructured.NestedString(repo.Object, "spec", "url"); gotURL != wantURL {
			t.Errorf("restored %s URL=%q want %q", name, gotURL, wantURL)
		}
		if secret, found, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name"); found && secret != "" {
			t.Errorf("restored org alias %s must be anonymous, got clientSecret %q", name, secret)
		}
	}
	for name, wantURL := range expectedTeamNames {
		repo := getClusterRepo(t, c, name)
		if gotURL, _, _ := unstructured.NestedString(repo.Object, "spec", "url"); gotURL != wantURL {
			t.Errorf("restored %s URL=%q want %q", name, gotURL, wantURL)
		}
		secret, _, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name")
		if gatedURLs[wantURL] && secret != credentials.AuthSecretNvidia {
			t.Errorf("restored gated alias %s clientSecret=%q want %q", name, secret, credentials.AuthSecretNvidia)
		}
		if !gatedURLs[wantURL] && secret != "" {
			t.Errorf("restored public alias %s must be anonymous, got clientSecret %q", name, secret)
		}
	}
}

func TestSettingsController_AirGapUnauthenticatedMirrorCreated(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{
				Nvidia: "oci://registry.internal/nvidia",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Both canonical aliases and every catalog-derived team alias exist, point at
	// the private URL, and remain anonymous.
	teams := catalog.ClassifyNGCTeamRepos()
	expectedNames := []string{credentials.ClusterRepoNvidia, credentials.ClusterRepoNvidiaBlueprint}
	for _, u := range append(teams.Public, teams.Gated...) {
		name, err := catalog.NGCClusterRepoName(u)
		if err != nil {
			t.Fatalf("derive ClusterRepo name for %q: %v", u, err)
		}
		expectedNames = append(expectedNames, name)
	}
	for _, name := range expectedNames {
		repo := getClusterRepo(t, c, name)
		if repo.GetLabels()[managedRepoLabel] != markerValueTrue {
			t.Errorf("air-gap alias %s missing managed-repo label, got labels=%v", name, repo.GetLabels())
		}
		if url, _, _ := unstructured.NestedString(repo.Object, "spec", "url"); url != "oci://registry.internal/nvidia" {
			t.Errorf("air-gap alias %s URL = %q, want oci://registry.internal/nvidia", name, url)
		}
		if sec, found, _ := unstructured.NestedString(repo.Object, "spec", "clientSecret", "name"); found && sec != "" {
			t.Errorf("unauthenticated air-gap alias %s must have no clientSecret, got %q", name, sec)
		}
	}
	// No auth secret should be written for an anonymous mirror.
	var authSec corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: credentials.AuthSecretNvidia, Namespace: "cattle-system"}, &authSec); !apierrors.IsNotFound(err) {
		t.Errorf("expected no ngc-helm-auth for anonymous mirror, got err=%v", err)
	}
}

func TestSettingsController_StampsManagedRepoLabel(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	appco := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("user@suse.com"), "token": []byte("appco-token")},
	}
	nvidia := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nvidia", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("$oauthtoken"), "token": []byte("nvapi-test")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, appco, nvidia).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Org/AC repo carries the provenance label.
	ac := getClusterRepo(t, c, credentials.ClusterRepoApplicationCollection)
	if ac.GetLabels()[managedRepoLabel] != markerValueTrue {
		t.Errorf("application-collection missing managed-repo label, got labels=%v", ac.GetLabels())
	}
	// Public NGC org repo carries it too.
	nv := getClusterRepo(t, c, credentials.ClusterRepoNvidia)
	if nv.GetLabels()[managedRepoLabel] != markerValueTrue {
		t.Errorf("nvidia repo missing managed-repo label, got labels=%v", nv.GetLabels())
	}
}

// Relabel-on-upgrade: an older operator created ClusterRepos WITHOUT the
// provenance label. After upgrade the reconciler must adopt the existing repo and
// stamp the managed-repo label — the migration path documented in
// docs/managed-clusterrepo-provenance.md, and what makes the label-reading UI
// discover pre-existing repos post-upgrade. Admin-added labels must survive. This
// works only because applyClusterRepo uses server-side apply; swapping it for
// ctrl.CreateOrUpdate would silently break the upgrade story, so this guards it.
func TestSettingsController_RelabelsExistingRepoOnUpgrade(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	appco := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("user@suse.com"), "token": []byte("appco-token")},
	}
	// Pre-existing AC repo from an older operator: no provenance label, plus an
	// admin-added label that must be preserved through the relabel.
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	existing.SetName(credentials.ClusterRepoApplicationCollection)
	existing.SetLabels(map[string]string{"admin.example.com/keep": "yes"})
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, appco, existing).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	ac := getClusterRepo(t, c, credentials.ClusterRepoApplicationCollection)
	if ac.GetLabels()[managedRepoLabel] != markerValueTrue {
		t.Errorf("existing repo not relabeled on upgrade, got labels=%v", ac.GetLabels())
	}
	if ac.GetLabels()["admin.example.com/keep"] != "yes" {
		t.Errorf("admin-added label must survive relabel, got labels=%v", ac.GetLabels())
	}
}

// Adoption neutralizes a squatter's source surface: a pre-existing ClusterRepo
// occupying a canonical name with a foreign git source and downgraded TLS must
// NOT keep those fields once the operator adopts it via server-side apply. This
// is the residual risk behind the trust model — SSA forces only the fields in the
// applied object, so managedRepoSpec must own the whole alternate-source surface.
func TestSettingsController_AdoptionNeutralizesForeignSource(t *testing.T) {
	s := newScheme(t)
	registerClusterRepoTypes(s)
	const ns = "aif-operator"
	cr := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: ns},
		Spec:       aiplatformv1alpha1.SettingsSpec{},
	}
	nvidia := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nvidia", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("$oauthtoken"), "token": []byte("nvapi-test")},
	}
	squatter := &unstructured.Unstructured{}
	squatter.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	squatter.SetName(credentials.ClusterRepoNvidia)
	_ = unstructured.SetNestedField(squatter.Object, "https://github.com/example/not-nvidia.git", "spec", "gitRepo")
	_ = unstructured.SetNestedField(squatter.Object, "main", "spec", "gitBranch")
	_ = unstructured.SetNestedField(squatter.Object, true, "spec", "insecureSkipTLSVerify")
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, nvidia, squatter).
		WithStatusSubresource(&aiplatformv1alpha1.Settings{}).Build()

	r := &settings.SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
	if _, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: credentials.SettingsName, Namespace: ns},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	nv := getClusterRepo(t, c, credentials.ClusterRepoNvidia)
	if nv.GetLabels()[managedRepoLabel] != markerValueTrue {
		t.Errorf("adopted repo missing managed-repo label, got labels=%v", nv.GetLabels())
	}
	if gitRepo, _, _ := unstructured.NestedString(nv.Object, "spec", "gitRepo"); gitRepo != "" {
		t.Errorf("adoption must clear foreign gitRepo, got %q", gitRepo)
	}
	if branch, _, _ := unstructured.NestedString(nv.Object, "spec", "gitBranch"); branch != "" {
		t.Errorf("adoption must clear foreign gitBranch, got %q", branch)
	}
	if insecure, _, _ := unstructured.NestedBool(nv.Object, "spec", "insecureSkipTLSVerify"); insecure {
		t.Errorf("adoption must reset insecureSkipTLSVerify to false")
	}
	if url, _, _ := unstructured.NestedString(nv.Object, "spec", "url"); url != credentials.DefaultNvidiaChartsURL {
		t.Errorf("adopted repo url = %q, want %q", url, credentials.DefaultNvidiaChartsURL)
	}
}
