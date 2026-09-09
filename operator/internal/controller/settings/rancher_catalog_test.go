package settings

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
func TestEnqueueSettingsForSecret_MatchesReferencedSettingsSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = aiplatformv1alpha1.AddToScheme(scheme)

	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: credentials.SettingsName, Namespace: "aif"},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Fleet: aiplatformv1alpha1.FleetSettings{
				CredSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "git-creds", Key: "token"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-ca", Key: "ca.crt"},
			},
			ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "appco-ca", Key: "ca.crt"},
			},
			SUSERegistry: aiplatformv1alpha1.SUSERegistrySettings{
				UserSecretRef:     &aiplatformv1alpha1.SecretKeyRef{Name: "suse-creds", Key: "username"},
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "suse-ca", Key: "ca.crt"},
			},
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "nvidia-ca", Key: "ca.crt"},
			},
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
		{"Git credential", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "aif"}}, true},
		{"Git CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "git-ca", Namespace: "aif"}}, true},
		{"AppCo CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "appco-ca", Namespace: "aif"}}, true},
		{"SUSE explicit credentials", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "suse-creds", Namespace: "aif"}}, true},
		{"SUSE CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "suse-ca", Namespace: "aif"}}, true},
		{"NVIDIA CA bundle", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "nvidia-ca", Namespace: "aif"}}, true},
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

func newCATestReconciler(t *testing.T, objs ...client.Object) *SettingsReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = aiplatformv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &SettingsReconciler{
		Client: cl, Scheme: scheme, OperatorNamespace: "aif", CatalogHolder: rancher.NewHolder(),
	}
}

func TestResolveCABundle(t *testing.T) {
	const discoveredPEM = "-----BEGIN CERTIFICATE-----\nDISCOVERED\n-----END CERTIFICATE-----\n"
	const explicitPEM = "-----BEGIN CERTIFICATE-----\nEXPLICIT\n-----END CERTIFICATE-----\n"

	internalCA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rancher.InternalCAName,
			Namespace: rancher.InternalCANamespace,
		},
		Data: map[string][]byte{
			"tls.crt": []byte(discoveredPEM),
			"tls.key": []byte("PRIVATE KEY MUST NOT BE USED"),
		},
	}
	explicitCA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ca", Namespace: "aif"},
		Data:       map[string][]byte{"ca.crt": []byte(explicitPEM)},
	}

	settings := func(caRef *aiplatformv1alpha1.SecretKeyRef) *aiplatformv1alpha1.Settings {
		s := &aiplatformv1alpha1.Settings{
			ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "aif"},
		}
		s.Spec.RancherCatalog.CABundleSecretRef = caRef
		return s
	}
	ref := func(name, key string) *aiplatformv1alpha1.SecretKeyRef {
		return &aiplatformv1alpha1.SecretKeyRef{Name: name, Key: key}
	}

	cases := []struct {
		name       string
		objs       []client.Object
		caRef      *aiplatformv1alpha1.SecretKeyRef
		wantPEM    string
		wantSource string
	}{
		{
			name: "no ref, internal CA present -> discovered",
			objs: []client.Object{internalCA}, caRef: nil,
			wantPEM: discoveredPEM, wantSource: "discovered",
		},
		{
			name: "no ref, no internal CA -> system roots",
			objs: nil, caRef: nil,
			wantPEM: "", wantSource: "system",
		},
		{
			name: "explicit ref wins over discovery",
			objs: []client.Object{internalCA, explicitCA}, caRef: ref("my-ca", "ca.crt"),
			wantPEM: explicitPEM, wantSource: "settings",
		},
		{
			// The internal CA is present and discovery would succeed, but an
			// administrator pinned a CA. Substituting a different certificate
			// silently would be worse than failing.
			name: "unreadable explicit ref does not fall back to discovery",
			objs: []client.Object{internalCA}, caRef: ref("absent", "ca.crt"),
			wantPEM: "", wantSource: "settings-error",
		},
		{
			// readSecretKey returns ("", nil) for a Secret that exists but lacks
			// the key. Reporting "settings" would log caSource=settings
			// customCA=false — reading as "your configured CA is in use" when
			// nothing was loaded.
			name: "present ref, missing key -> settings-error, not settings",
			objs: []client.Object{internalCA, explicitCA}, caRef: ref("my-ca", "wrong-key"),
			wantPEM: "", wantSource: "settings-error",
		},
		{
			name: "present ref, empty value -> settings-error",
			objs: []client.Object{internalCA, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "blank-ca", Namespace: "aif"},
				Data:       map[string][]byte{"ca.crt": {}},
			}}, caRef: ref("blank-ca", "ca.crt"),
			wantPEM: "", wantSource: "settings-error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newCATestReconciler(t, tc.objs...)
			pem, source := r.resolveCABundle(context.Background(), settings(tc.caRef))
			if source != tc.wantSource {
				t.Errorf("caSource = %q, want %q", source, tc.wantSource)
			}
			if string(pem) != tc.wantPEM {
				t.Errorf("caPEM = %q, want %q", pem, tc.wantPEM)
			}
		})
	}
}
