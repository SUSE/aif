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

func TestEnsureCombinedPullSecret_AppCollectionHostFromOCIURL(t *testing.T) {
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
		ObjectMeta: metav1.ObjectMeta{Name: "ac-user", Namespace: opNS},
		Data:       map[string][]byte{"username": []byte("u")},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ac-token", Namespace: opNS},
		Data:       map[string][]byte{"token": []byte("p")},
	}
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec: aiplatformv1alpha1.SettingsSpec{
			RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{
				ApplicationCollection: "oci://registry.example.com/charts",
			},
			ApplicationCollection: aiplatformv1alpha1.ApplicationCollectionSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "ac-user", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ac-token", Key: "token"},
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
	// The override is a full OCI chart-repo URL; the auths entry must be keyed by
	// the registry host, not the whole URL.
	if _, ok := cfg.Auths["registry.example.com"]; !ok {
		t.Fatalf("expected registry.example.com auth entry (base of OCI URL), got: %v", cfg.Auths)
	}
}

// TestEnsureCombinedPullSecret_NvidiaAlwaysNvcrIO pins the invariant that the NVIDIA
// image-pull-secret host is always nvcr.io, even when registryEndpoints.nvidia points at
// a mirrored OCI chart repo. That field is a chart-repo URL, not an image host — air-gap
// image redirection is a node-level concern, so the auths entry must never use the mirror host.
func TestEnsureCombinedPullSecret_NvidiaAlwaysNvcrIO(t *testing.T) {
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
			RegistryEndpoints: &aiplatformv1alpha1.RegistryEndpointsSettings{
				Nvidia: "oci://mirror.example.com/nvidia",
			},
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
	if _, ok := cfg.Auths["nvcr.io"]; !ok {
		t.Fatalf("expected nvcr.io auth entry, got: %v", cfg.Auths)
	}
	if _, ok := cfg.Auths["mirror.example.com"]; ok {
		t.Errorf("did not expect the mirror host as an image-pull auth entry, got: %v", cfg.Auths)
	}
}

