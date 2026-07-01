package credentials_test

import (
	"context"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/SUSE/aif-operator/internal/credentials"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEffectiveRefs_PrefersExplicitSettings(t *testing.T) {
	explicit := &aiplatformv1alpha1.SecretKeyRef{Name: "custom", Key: "user"}
	token := &aiplatformv1alpha1.SecretKeyRef{Name: "custom", Key: "token"}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		// The explicit ref target — must resolve for explicit refs to win.
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "custom", Namespace: "aif-operator"},
			Data:       map[string][]byte{"user": []byte("cu"), "token": []byte("ct")},
		},
		// A well-known secret that discovery would otherwise pick.
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: "aif-operator"},
			Data:       map[string][]byte{"user": []byte("u"), "token": []byte("p")},
		},
	).Build()

	u, tok := credentials.EffectiveRefs(context.Background(), c, "aif-operator", explicit, token, credentials.RegistryApplicationCollection)
	if u.Name != "custom" || tok.Key != "token" {
		t.Fatalf("expected explicit refs, got user=%+v token=%+v", u, tok)
	}
}

func TestEffectiveRefs_DiscoversAppco(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "appco", Namespace: "aif-operator"},
			Data:       map[string][]byte{"user": []byte("u"), "token": []byte("p")},
		},
	).Build()

	u, tok := credentials.EffectiveRefs(context.Background(), c, "aif-operator", nil, nil, credentials.RegistryApplicationCollection)
	if u == nil || tok == nil || u.Name != "appco" || u.Key != "user" || tok.Key != "token" {
		t.Fatalf("expected appco refs, got user=%+v token=%+v", u, tok)
	}
}

func TestReadPair_UserTokenKeys(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "nvidia", Namespace: "aif-operator"},
			Data: map[string][]byte{
				"user":  []byte("$oauthtoken"),
				"token": []byte("nvapi-test"),
			},
		},
	).Build()

	userRef := &aiplatformv1alpha1.SecretKeyRef{Name: "nvidia", Key: "user"}
	tokenRef := &aiplatformv1alpha1.SecretKeyRef{Name: "nvidia", Key: "token"}
	user, token, ok, err := credentials.ReadPair(context.Background(), c, "aif-operator", userRef, tokenRef)
	if err != nil || !ok {
		t.Fatalf("ReadPair: ok=%v err=%v", ok, err)
	}
	if user != "$oauthtoken" || token != "nvapi-test" {
		t.Fatalf("got user=%q token=%q", user, token)
	}
}

func TestWireSpec_FillsMissingRefs(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "suse-registry", Namespace: "aif-operator"},
			Data:       map[string][]byte{"user": []byte("regcode"), "token": []byte("secret")},
		},
	).Build()

	spec := &aiplatformv1alpha1.SettingsSpec{}
	changed, err := credentials.WireSpec(context.Background(), c, spec, "aif-operator")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected spec to change")
	}
	if spec.SUSERegistry.UserSecretRef == nil || spec.SUSERegistry.UserSecretRef.Name != "suse-registry" {
		t.Fatalf("suseRegistry refs not wired: %+v", spec.SUSERegistry)
	}
}

func TestEffectiveRefs_OverridesUnresolvableExplicitRefs(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "suse-registry", Namespace: "aif-operator"},
			Data:       map[string][]byte{"user": []byte("regcode"), "token": []byte("secret")},
		},
	).Build()

	// Corrupt explicit refs: token ref points at an empty key (exactly the
	// live nvidia state we hit). These must NOT silently win — discovery of
	// the well-known secret should self-heal them.
	badUser := &aiplatformv1alpha1.SecretKeyRef{Name: "suse-registry", Key: "token"}
	badToken := &aiplatformv1alpha1.SecretKeyRef{Name: "suse-registry", Key: ""}

	u, tok := credentials.EffectiveRefs(context.Background(), c, "aif-operator", badUser, badToken, credentials.RegistrySUSERegistry)
	if u == nil || tok == nil || u.Key != "user" || tok.Key != "token" {
		t.Fatalf("expected discovery to override corrupt refs, got user=%+v token=%+v", u, tok)
	}
}

func TestEffectiveRefs_NvidiaTokenOnly_NormalizesAndDiscovers(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "nvidia", Namespace: "aif-operator"},
			// Only a token key — the "official" kubectl setup with the
			// $oauthtoken username dropped (e.g. fish shell ate it).
			Data: map[string][]byte{"token": []byte("nvapi-test")},
		},
	).Build()

	u, tok := credentials.EffectiveRefs(context.Background(), c, "aif-operator", nil, nil, credentials.RegistryNvidia)
	if u == nil || tok == nil || u.Name != "nvidia" || u.Key != "user" || tok.Key != "token" {
		t.Fatalf("expected nvidia refs after normalization, got user=%+v token=%+v", u, tok)
	}

	var sec corev1.Secret
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "aif-operator", Name: "nvidia"}, &sec); err != nil {
		t.Fatal(err)
	}
	if got := string(sec.Data["user"]); got != credentials.NvidiaDefaultUsername {
		t.Fatalf("nvidia secret user key = %q, want %q", got, credentials.NvidiaDefaultUsername)
	}
}

func TestIsWellKnownSecret(t *testing.T) {
	if !credentials.IsWellKnownSecret("appco") || !credentials.IsWellKnownSecret("nvidia-registry") {
		t.Fatal("expected well-known names")
	}
	if credentials.IsWellKnownSecret("random") {
		t.Fatal("random should not be well-known")
	}
}
