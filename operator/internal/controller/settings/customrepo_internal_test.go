package settings

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const metadataNameKey = "name"

func TestCustomRepoSpecMap(t *testing.T) {
	helm := customRepoSpecMap(aiplatformv1alpha1.CustomRepoSpec{Type: "helm", URL: "https://charts.example.com"})
	if helm["url"] != "https://charts.example.com" || helm["gitRepo"] != "" || helm["gitBranch"] != "" {
		t.Fatalf("helm map wrong: %#v", helm)
	}
	git := customRepoSpecMap(aiplatformv1alpha1.CustomRepoSpec{Type: "git", GitRepo: "https://git.example.com/r.git", GitBranch: "main", InsecureSkipTLSVerify: true})
	if git["gitRepo"] != "https://git.example.com/r.git" || git["gitBranch"] != "main" || git["url"] != "" {
		t.Fatalf("git map wrong: %#v", git)
	}
	if git["insecureSkipTLSVerify"] != true {
		t.Fatalf("git map should honor insecureSkipTLSVerify: %#v", git)
	}
}

func TestReconcileCustomRepos_PreservesRepoOnAuthError(t *testing.T) {
	s := newTestScheme(t)
	const (
		ns       = "aif-operator"
		repoName = "myrepo"
	)

	// Pre-existing ClusterRepo with the custom-repo label
	existingRepo := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "catalog.cattle.io/v1",
			"kind":       "ClusterRepo",
			"metadata": map[string]any{
				metadataNameKey: repoName,
				"labels": map[string]any{
					credentials.ManagedRepoLabel: credentials.LabelValueTrue,
					credentials.CustomRepoLabel:  credentials.LabelValueTrue,
				},
			},
			"spec": map[string]any{"url": "https://charts.example.com"},
		},
	}

	// Client that returns a transient error (Internal, not NotFound) on secret reads
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(existingRepo).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, isSecret := obj.(*corev1.Secret); isSecret && key.Name == "repo-creds" {
					return apierrors.NewInternalError(fmt.Errorf("transient API error"))
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			CustomRepos: []aiplatformv1alpha1.CustomRepoSpec{
				{
					Name:           repoName,
					Type:           "helm",
					URL:            "https://charts.example.com",
					UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "repo-creds", Key: "user"},
					TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "repo-creds", Key: "token"},
				},
			},
		},
	}

	err := r.reconcileCustomRepos(context.Background(), settings)
	if err == nil {
		t.Fatal("expected error on transient auth secret read failure, got nil")
	}

	// Verify the ClusterRepo still exists (not pruned)
	var repo unstructured.Unstructured
	repo.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo",
	})
	if err := c.Get(context.Background(), types.NamespacedName{Name: repoName}, &repo); err != nil {
		if apierrors.IsNotFound(err) {
			t.Fatal("ClusterRepo should NOT be deleted on transient auth error")
		}
		t.Fatalf("Get ClusterRepo: %v", err)
	}
}

func TestReconcileCustomRepos_SetsCABundleOnSpec(t *testing.T) {
	s := newTestScheme(t)
	const ns = "aif-operator"

	caPEM := testCAPEM(t)
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "repo-ca", Namespace: ns},
		Data:       map[string][]byte{"ca.crt": []byte(caPEM)},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(caSecret).Build()
	r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			CustomRepos: []aiplatformv1alpha1.CustomRepoSpec{{
				Name:              "privca",
				Type:              "helm",
				URL:               "https://charts.example.com",
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "repo-ca", Key: "ca.crt"},
			}},
		},
	}

	if err := r.reconcileCustomRepos(context.Background(), settings); err != nil {
		t.Fatalf("reconcileCustomRepos: %v", err)
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	if err := c.Get(context.Background(), types.NamespacedName{Name: "privca"}, repo); err != nil {
		t.Fatalf("Get ClusterRepo: %v", err)
	}
	got, found, err := unstructured.NestedString(repo.Object, "spec", "caBundle")
	if err != nil || !found {
		t.Fatalf("spec.caBundle missing: found=%v err=%v", found, err)
	}
	// spec.caBundle is a []byte field: the value must be the base64 of the PEM, so
	// Rancher decodes it back to the certificate (raw PEM would be double-decoded).
	decoded, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("spec.caBundle is not base64: %v", err)
	}
	if string(decoded) != caPEM {
		t.Error("decoded spec.caBundle does not match the configured CA PEM")
	}
}

// TestReconcileCustomRepos_OmitsEmptyCABundle guards against writing spec.caBundle=""
// when no CA is configured. Rancher declares caBundle as []byte (OpenAPI format:
// byte) and the real API server rejects an empty string ("must be of type byte"),
// so the field must be omitted entirely rather than set to "". The fake client does
// not enforce format:byte, so this asserts absence directly.
func TestReconcileCustomRepos_OmitsEmptyCABundle(t *testing.T) {
	s := newTestScheme(t)
	const ns = "aif-operator"

	c := fake.NewClientBuilder().WithScheme(s).Build()
	r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			CustomRepos: []aiplatformv1alpha1.CustomRepoSpec{{
				Name: "noca",
				Type: "helm",
				URL:  "https://charts.example.com",
			}},
		},
	}

	if err := r.reconcileCustomRepos(context.Background(), settings); err != nil {
		t.Fatalf("reconcileCustomRepos: %v", err)
	}

	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	if err := c.Get(context.Background(), types.NamespacedName{Name: "noca"}, repo); err != nil {
		t.Fatalf("Get ClusterRepo: %v", err)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(repo.Object, "spec", "caBundle"); found {
		t.Error("spec.caBundle must be absent when no CA bundle is configured, not set to an empty value")
	}
}

func TestPruneCustomRepos_SkipsMislabeledBuiltIn(t *testing.T) {
	s := newTestScheme(t)
	const ns = "aif-operator"

	// A built-in repo that was (incorrectly) stamped with the custom label, plus a
	// genuine custom repo that is no longer desired.
	builtIn := customLabeledRepo("application-collection")
	stale := customLabeledRepo("stale")
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(builtIn, stale).Build()
	r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

	if err := r.pruneCustomRepos(context.Background(), map[string]bool{}); err != nil {
		t.Fatalf("pruneCustomRepos: %v", err)
	}

	gvk := schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"}
	builtInGot := &unstructured.Unstructured{}
	builtInGot.SetGroupVersionKind(gvk)
	if err := c.Get(context.Background(), types.NamespacedName{Name: "application-collection"}, builtInGot); err != nil {
		t.Fatalf("mislabeled built-in must NOT be pruned, got err=%v", err)
	}
	staleGot := &unstructured.Unstructured{}
	staleGot.SetGroupVersionKind(gvk)
	err := c.Get(context.Background(), types.NamespacedName{Name: "stale"}, staleGot)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("genuine custom repo should be pruned, got err=%v", err)
	}
}

// A custom repo whose name matches a ClusterRepo the custom-repo feature
// doesn't own must not take that repo over: the existing ClusterRepo keeps its
// spec and labels, no auth secret is written, and the conflict is reported.
func TestReconcileCustomRepos_RefusesNameTakenByOtherClusterRepo(t *testing.T) {
	s := newTestScheme(t)
	const (
		ns        = "aif-operator"
		takenName = "partner-charts"
		authNS    = "cattle-system"
	)

	foreign := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "catalog.cattle.io/v1",
			"kind":       "ClusterRepo",
			"metadata": map[string]any{
				metadataNameKey: takenName,
				"labels":        map[string]any{"owner": "someone-else"},
			},
			"spec": map[string]any{"url": "https://partner.example.com/charts"},
		},
	}
	creds := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "repo-creds", Namespace: ns},
		Data:       map[string][]byte{"user": []byte("robot"), "token": []byte("secret123")},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(foreign, creds).Build()
	r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: ns},
		Spec: aiplatformv1alpha1.SettingsSpec{
			CustomRepos: []aiplatformv1alpha1.CustomRepoSpec{{
				Name:           takenName,
				Type:           "helm",
				URL:            "https://charts.example.com",
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "repo-creds", Key: "user"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "repo-creds", Key: "token"},
			}},
		},
	}

	if err := r.reconcileCustomRepos(context.Background(), settings); err == nil {
		t.Fatal("expected an error reporting the taken name, got nil")
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	if err := c.Get(context.Background(), types.NamespacedName{Name: takenName}, got); err != nil {
		t.Fatalf("existing ClusterRepo must survive, got err=%v", err)
	}
	if url, _, _ := unstructured.NestedString(got.Object, "spec", "url"); url != "https://partner.example.com/charts" {
		t.Errorf("existing ClusterRepo url = %q, must not be overwritten", url)
	}
	if _, ok := got.GetLabels()[credentials.CustomRepoLabel]; ok {
		t.Error("existing ClusterRepo must not be stamped with the custom-repo label")
	}

	var secret corev1.Secret
	err := c.Get(context.Background(), types.NamespacedName{
		Name: credentials.CustomRepoAuthSecretName(takenName), Namespace: authNS,
	}, &secret)
	if !apierrors.IsNotFound(err) {
		t.Errorf("no auth secret should be written for a refused repo, got err=%v", err)
	}
}

func customLabeledRepo(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "catalog.cattle.io/v1",
			"kind":       "ClusterRepo",
			"metadata": map[string]any{
				metadataNameKey: name,
				"labels": map[string]any{
					credentials.ManagedRepoLabel: credentials.LabelValueTrue,
					credentials.CustomRepoLabel:  credentials.LabelValueTrue,
				},
			},
			"spec": map[string]any{"url": "https://charts.example.com"},
		},
	}
}

func TestApplyCustomClusterRepo_StampsDisplayNameAnnotation(t *testing.T) {
	s := newTestScheme(t)
	const ns = "aif-operator"
	c := fake.NewClientBuilder().WithScheme(s).Build()
	r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

	repo := aiplatformv1alpha1.CustomRepoSpec{
		Name:        "prometheus-community",
		DisplayName: "Prometheus Community",
		Type:        "helm",
		URL:         "https://prometheus-community.github.io/helm-charts",
	}
	if err := r.applyCustomClusterRepo(context.Background(), "prometheus-community", repo, "", nil,
		map[string]string{credentials.CustomRepoLabel: credentials.LabelValueTrue}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	if err := c.Get(context.Background(), client.ObjectKey{Name: "prometheus-community"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if v := got.GetAnnotations()[credentials.DisplayNameAnnotation]; v != "Prometheus Community" {
		t.Fatalf("display-name annotation = %q, want %q", v, "Prometheus Community")
	}
}

func TestApplyCustomClusterRepo_OmitsDisplayNameAnnotationWhenEmpty(t *testing.T) {
	s := newTestScheme(t)
	const ns = "aif-operator"
	c := fake.NewClientBuilder().WithScheme(s).Build()
	r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

	repo := aiplatformv1alpha1.CustomRepoSpec{Name: "internal", Type: "helm", URL: "https://charts.example.com"}
	if err := r.applyCustomClusterRepo(context.Background(), "internal", repo, "", nil,
		map[string]string{credentials.CustomRepoLabel: credentials.LabelValueTrue}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "ClusterRepo"})
	if err := c.Get(context.Background(), client.ObjectKey{Name: "internal"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, ok := got.GetAnnotations()[credentials.DisplayNameAnnotation]; ok {
		t.Fatalf("display-name annotation present, want absent")
	}
}
