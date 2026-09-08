package settings

import (
	"context"
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
	const ns = "aif-operator"

	// Pre-existing ClusterRepo with the custom-repo label
	existingRepo := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "catalog.cattle.io/v1",
			"kind":       "ClusterRepo",
			"metadata": map[string]any{
				"name": "custom-myrepo",
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
					Name:           "myrepo",
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
	if err := c.Get(context.Background(), types.NamespacedName{Name: "custom-myrepo"}, &repo); err != nil {
		if apierrors.IsNotFound(err) {
			t.Fatal("ClusterRepo should NOT be deleted on transient auth error")
		}
		t.Fatalf("Get ClusterRepo: %v", err)
	}
}
