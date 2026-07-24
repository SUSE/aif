package settings

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

func TestReconcileRancherCatalogClient(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = aiplatformv1alpha1.AddToScheme(scheme)

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rc-token", Namespace: "aif"},
		Data:       map[string][]byte{"token": []byte("token-abc:xyz")},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tokenSecret).Build()
	holder := rancher.NewHolder()
	r := &SettingsReconciler{Client: cl, Scheme: scheme, OperatorNamespace: "aif", CatalogHolder: holder}

	s := &aiplatformv1alpha1.Settings{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "aif"}}

	// No token ref configured -> client disabled (holder nil).
	r.reconcileRancherCatalogClient(context.Background(), s)
	if holder.Get() != nil {
		t.Fatal("expected nil catalog client when no token ref configured")
	}

	// Token ref present -> client built and swapped in.
	s.Spec.RancherCatalog.TokenSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "rc-token", Key: "token"}
	r.reconcileRancherCatalogClient(context.Background(), s)
	if holder.Get() == nil {
		t.Fatal("expected a catalog client once a token secret is configured")
	}

	// Token ref pointing at a missing secret -> disabled again (nil), no panic.
	s.Spec.RancherCatalog.TokenSecretRef = &aiplatformv1alpha1.SecretKeyRef{Name: "missing", Key: "token"}
	r.reconcileRancherCatalogClient(context.Background(), s)
	if holder.Get() != nil {
		t.Fatal("expected nil catalog client when the token secret is missing")
	}
}
