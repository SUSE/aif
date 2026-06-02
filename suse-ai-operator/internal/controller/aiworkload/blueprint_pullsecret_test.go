package aiworkload

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/suse-ai-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureCombinedPullSecret_IncludesNvidia(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "my-app"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-user", Namespace: opNS},
		Data:       map[string][]byte{"username": []byte("$oauthtoken")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-token", Namespace: opNS},
		Data:       map[string][]byte{"token": []byte("nvapi-secret")},
	}
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-token", Key: "token"},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(userSecret, tokenSecret, settings).Build()

	r := &AIWorkloadReconciler{Client: c, OperatorNamespace: opNS}

	name, err := r.ensureCombinedPullSecret(context.Background(), targetNS, clusterRepoInfo{})
	if err != nil {
		t.Fatalf("ensureCombinedPullSecret: %v", err)
	}
	if name == "" {
		t.Fatalf("expected a pull secret name, got empty")
	}

	got := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: name}, got); err != nil {
		t.Fatalf("get created secret: %v", err)
	}
	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(got.Data[corev1.DockerConfigJsonKey], &cfg); err != nil {
		t.Fatalf("parse dockerconfigjson: %v", err)
	}
	entry, ok := cfg.Auths["nvcr.io"]
	if !ok {
		t.Fatalf("expected nvcr.io auth entry, got: %v", cfg.Auths)
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		t.Fatalf("base64 decode auth: %v", err)
	}
	if !strings.HasPrefix(string(decoded), "$oauthtoken:nvapi-secret") {
		t.Errorf("unexpected auth payload: %q", string(decoded))
	}
}

func TestEnsureCombinedPullSecret_NvidiaHostOverride(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "my-app"
	const customHost = "registry.example.com"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-user", Namespace: opNS},
		Data:       map[string][]byte{"username": []byte("$oauthtoken")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-token", Namespace: opNS},
		Data:       map[string][]byte{"token": []byte("nvapi-secret")},
	}
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec: aiplatformv1alpha1.SettingsSpec{
			RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{Nvidia: customHost},
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ngc-token", Key: "token"},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(userSecret, tokenSecret, settings).Build()

	r := &AIWorkloadReconciler{Client: c, OperatorNamespace: opNS}

	name, err := r.ensureCombinedPullSecret(context.Background(), targetNS, clusterRepoInfo{})
	if err != nil {
		t.Fatalf("ensureCombinedPullSecret: %v", err)
	}

	got := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: name}, got); err != nil {
		t.Fatalf("get created secret: %v", err)
	}
	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(got.Data[corev1.DockerConfigJsonKey], &cfg); err != nil {
		t.Fatalf("parse dockerconfigjson: %v", err)
	}
	if _, ok := cfg.Auths[customHost]; !ok {
		t.Fatalf("expected %q auth entry, got: %v", customHost, cfg.Auths)
	}
	if _, ok := cfg.Auths["nvcr.io"]; ok {
		t.Errorf("did not expect default nvcr.io entry when override set, got: %v", cfg.Auths)
	}
}
