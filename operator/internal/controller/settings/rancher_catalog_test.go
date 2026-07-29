package settings

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
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

// Rotating the catalog token in place mutates no Settings field, so the Secret
// watch is the only thing that rebuilds the client before the next resync.
func TestEnqueueSettingsForSecret_MatchesRancherCatalogRefs(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = aiplatformv1alpha1.AddToScheme(scheme)

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: "aif"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			RancherCatalog: aiplatformv1alpha1.RancherCatalogSettings{
				TokenSecretRef:    &aiplatformv1alpha1.SecretKeyRef{Name: "rc-token", Key: "token"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "rc-ca", Key: "ca.crt"},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(settings).Build()
	r := &SettingsReconciler{Client: cl, Scheme: scheme, OperatorNamespace: "aif"}

	cases := []struct {
		name      string
		secret    *corev1.Secret
		wantMatch bool
	}{
		{"token secret", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rc-token", Namespace: "aif"}}, true},
		{"ca bundle secret", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rc-ca", Namespace: "aif"}}, true},
		{"unrelated secret", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "nope", Namespace: "aif"}}, false},
		{"right name wrong namespace", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rc-token", Namespace: "other"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqs := r.enqueueSettingsForSecret(context.Background(), tc.secret)
			if got := len(reqs) > 0; got != tc.wantMatch {
				t.Fatalf("enqueued=%v, want %v (reqs=%v)", got, tc.wantMatch, reqs)
			}
		})
	}
}
