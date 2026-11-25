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

func TestMirrorURLAndAnonymousMirror(t *testing.T) {
	spec := &aiplatformv1alpha1.SettingsSpec{RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{
		ApplicationCollection: "oci://m/ac", SUSERegistry: "oci://m/sr", Nvidia: "oci://m/nv",
	}}
	for r, want := range map[credentials.Registry]string{
		credentials.RegistryApplicationCollection: "oci://m/ac",
		credentials.RegistrySUSERegistry:          "oci://m/sr",
		credentials.RegistryNvidia:                "oci://m/nv",
	} {
		if got := credentials.MirrorURL(spec, r); got != want {
			t.Errorf("MirrorURL(%d) = %q, want %q", r, got, want)
		}
	}
	if got := credentials.MirrorURL(&aiplatformv1alpha1.SettingsSpec{}, credentials.RegistryNvidia); got != "" {
		t.Errorf("MirrorURL without endpoints = %q, want empty", got)
	}

	ref := &aiplatformv1alpha1.SecretKeyRef{Name: "n", Key: "k"}
	tests := []struct {
		name        string
		spec        *aiplatformv1alpha1.SettingsSpec
		registry    credentials.Registry
		user, token *aiplatformv1alpha1.SecretKeyRef
		want        bool
	}{
		{"nvidia mirror without refs", spec, credentials.RegistryNvidia, nil, nil, true},
		{"nvidia mirror with a partial pair", spec, credentials.RegistryNvidia, ref, nil, true},
		{"nvidia mirror with refs", spec, credentials.RegistryNvidia, ref, ref, false},
		{"nvidia connected without refs", &aiplatformv1alpha1.SettingsSpec{}, credentials.RegistryNvidia, nil, nil, false},
		{"application collection mirror without refs", spec, credentials.RegistryApplicationCollection, nil, nil, false},
		{"suse registry mirror without refs", spec, credentials.RegistrySUSERegistry, nil, nil, false},
	}
	for _, tt := range tests {
		if got := credentials.AnonymousMirror(tt.spec, tt.registry, tt.user, tt.token); got != tt.want {
			t.Errorf("%s: AnonymousMirror = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestResolveGitHTTPSUsername(t *testing.T) {
	tests := []struct {
		name       string
		explicit   string
		secretData map[string][]byte
		want       string
	}{
		{
			name:       "explicit value wins",
			explicit:   "settings-user",
			secretData: map[string][]byte{corev1.BasicAuthUsernameKey: []byte("secret-user")},
			want:       "settings-user",
		},
		{
			name:       "secret username is used",
			secretData: map[string][]byte{corev1.BasicAuthUsernameKey: []byte("secret-user")},
			want:       "secret-user",
		},
		{
			name:       "credential-only secret uses default",
			secretData: map[string][]byte{corev1.BasicAuthPasswordKey: []byte("secret-token")},
			want:       credentials.DefaultGitHTTPSUsername,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := credentials.ResolveGitHTTPSUsername(tt.explicit, tt.secretData); got != tt.want {
				t.Fatalf("ResolveGitHTTPSUsername() = %q, want %q", got, tt.want)
			}
		})
	}
}
