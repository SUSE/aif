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
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func TestNvidiaInjector_CreatesBothSecrets(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "rag"

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
		Data:       map[string][]byte{"token": []byte("nvapi-xyz")},
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
	inj := &nvidiaInjector{r: r}

	vals := map[string]any{}
	if err := inj.Apply(context.Background(), targetNS, clusterRepoInfo{}, vals); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pull := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: nvidiaImagePullSecretName}, pull); err != nil {
		t.Fatalf("get %s: %v", nvidiaImagePullSecretName, err)
	}
	if pull.Type != corev1.SecretTypeDockerConfigJson {
		t.Errorf("ngc-secret type = %v, want %v", pull.Type, corev1.SecretTypeDockerConfigJson)
	}
	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(pull.Data[corev1.DockerConfigJsonKey], &cfg); err != nil {
		t.Fatalf("parse dockerconfigjson: %v", err)
	}
	entry, ok := cfg.Auths["nvcr.io"]
	if !ok {
		t.Fatalf("expected nvcr.io entry, got: %v", cfg.Auths)
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != "$oauthtoken:nvapi-xyz" {
		t.Errorf("auth payload = %q, want %q", string(decoded), "$oauthtoken:nvapi-xyz")
	}

	api := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: nvidiaAPISecretName}, api); err != nil {
		t.Fatalf("get %s: %v", nvidiaAPISecretName, err)
	}
	if api.Type != corev1.SecretTypeOpaque {
		t.Errorf("ngc-api type = %v, want %v", api.Type, corev1.SecretTypeOpaque)
	}
	if got := string(api.Data[nvidiaAPISecretKey]); got != "nvapi-xyz" {
		t.Errorf("NGC_API_KEY = %q, want %q", got, "nvapi-xyz")
	}
}

func TestNvidiaInjector_HostOverride(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "rag"
	const customHost = "mirror.example.com"

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
		Data:       map[string][]byte{"token": []byte("nvapi-xyz")},
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
	inj := &nvidiaInjector{r: r}

	if err := inj.Apply(context.Background(), targetNS, clusterRepoInfo{}, map[string]any{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pull := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: nvidiaImagePullSecretName}, pull); err != nil {
		t.Fatalf("get pull secret: %v", err)
	}
	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(pull.Data[corev1.DockerConfigJsonKey], &cfg); err != nil {
		t.Fatalf("parse dockerconfigjson: %v", err)
	}
	if _, ok := cfg.Auths[customHost]; !ok {
		t.Errorf("expected %q auth entry, got %v", customHost, cfg.Auths)
	}
	if _, ok := cfg.Auths["nvcr.io"]; ok {
		t.Errorf("did not expect default nvcr.io entry when override set, got %v", cfg.Auths)
	}
}

func TestNvidiaInjector_NoCreds_NoOp(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "rag"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec:       aiplatformv1alpha1.SettingsSpec{}, // no Nvidia creds
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(settings).Build()
	r := &AIWorkloadReconciler{Client: c, OperatorNamespace: opNS}
	inj := &nvidiaInjector{r: r}

	vals := map[string]any{}
	if err := inj.Apply(context.Background(), targetNS, clusterRepoInfo{}, vals); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(vals) != 0 {
		t.Errorf("vals was mutated despite missing creds: %v", vals)
	}
	pull := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: nvidiaImagePullSecretName}, pull); err == nil {
		t.Errorf("ngc-secret should not exist when creds are missing")
	}
}

func TestNvidiaInjector_MissingTokenSecret(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "rag"

	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	settings := &aiplatformv1alpha1.Settings{
		ObjectMeta: metav1.ObjectMeta{Name: operatorSettingsName, Namespace: opNS},
		Spec: aiplatformv1alpha1.SettingsSpec{
			Nvidia: aiplatformv1alpha1.NvidiaSettings{
				UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "missing", Key: "username"},
				TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "missing", Key: "token"},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(settings).Build()
	r := &AIWorkloadReconciler{Client: c, OperatorNamespace: opNS}
	inj := &nvidiaInjector{r: r}

	if err := inj.Apply(context.Background(), targetNS, clusterRepoInfo{}, map[string]any{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	pull := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: targetNS, Name: nvidiaImagePullSecretName}, pull); err == nil {
		t.Errorf("ngc-secret should not exist when referenced secret is missing")
	}
}

func TestNvidiaInjector_WritesBothPathShapes(t *testing.T) {
	const opNS = "suse-ai-operator"
	const targetNS = "rag"

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
		Data:       map[string][]byte{"token": []byte("nvapi-xyz")},
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
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(userSecret, tokenSecret, settings).Build()
	r := &AIWorkloadReconciler{Client: c, OperatorNamespace: opNS}
	inj := &nvidiaInjector{r: r}

	vals := map[string]any{}
	if err := inj.Apply(context.Background(), targetNS, clusterRepoInfo{}, vals); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Standard k8s pod-spec shape at top level.
	topList, ok := vals["imagePullSecrets"].([]any)
	if !ok || len(topList) != 1 {
		t.Fatalf("imagePullSecrets = %#v, want one entry", vals["imagePullSecrets"])
	}
	entry, ok := topList[0].(map[string]any)
	if !ok || entry["name"] != nvidiaImagePullSecretName {
		t.Errorf("imagePullSecrets[0] = %#v, want {name: %q}", topList[0], nvidiaImagePullSecretName)
	}

	// k8s-nim-operator's flat-string shape.
	image, ok := vals["image"].(map[string]any)
	if !ok {
		t.Fatalf("image = %#v, want map", vals["image"])
	}
	imgList, ok := image["pullSecrets"].([]any)
	if !ok || len(imgList) != 1 || imgList[0] != nvidiaImagePullSecretName {
		t.Errorf("image.pullSecrets = %#v, want [%q]", image["pullSecrets"], nvidiaImagePullSecretName)
	}

	// Must not touch global.
	if _, ok := vals["global"]; ok {
		t.Errorf("global key should not be set by nvidiaInjector, got %#v", vals["global"])
	}
}

func TestNvidiaInjector_PreservesAuthorPullSecrets(t *testing.T) {
	_, r := buildNvidiaInjectorFixture(t)
	inj := &nvidiaInjector{r: r}

	vals := map[string]any{
		"imagePullSecrets": []any{map[string]any{"name": "author-secret"}},
		"image":            map[string]any{"pullSecrets": []any{"author-string"}},
	}
	if err := inj.Apply(context.Background(), "rag", clusterRepoInfo{}, vals); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	topList := vals["imagePullSecrets"].([]any)
	if len(topList) != 2 ||
		topList[0].(map[string]any)["name"] != nvidiaImagePullSecretName ||
		topList[1].(map[string]any)["name"] != "author-secret" {
		t.Errorf("imagePullSecrets = %#v, want [ngc-secret, author-secret]", topList)
	}
	imgList := vals["image"].(map[string]any)["pullSecrets"].([]any)
	if len(imgList) != 2 || imgList[0] != nvidiaImagePullSecretName || imgList[1] != "author-string" {
		t.Errorf("image.pullSecrets = %#v, want [ngc-secret, author-string]", imgList)
	}
}

func TestNvidiaInjector_IdempotentSelfEntry(t *testing.T) {
	_, r := buildNvidiaInjectorFixture(t)
	inj := &nvidiaInjector{r: r}

	vals := map[string]any{}
	if err := inj.Apply(context.Background(), "rag", clusterRepoInfo{}, vals); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := inj.Apply(context.Background(), "rag", clusterRepoInfo{}, vals); err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	topList := vals["imagePullSecrets"].([]any)
	if len(topList) != 1 {
		t.Errorf("imagePullSecrets duplicated after re-Apply: %#v", topList)
	}
	imgList := vals["image"].(map[string]any)["pullSecrets"].([]any)
	if len(imgList) != 1 {
		t.Errorf("image.pullSecrets duplicated after re-Apply: %#v", imgList)
	}
}

func TestNvidiaInjector_LeavesUnexpectedShapesAlone(t *testing.T) {
	_, r := buildNvidiaInjectorFixture(t)
	inj := &nvidiaInjector{r: r}

	// Author wrote an integer where we expect a slice — refuse to mutate.
	vals := map[string]any{"imagePullSecrets": 42}
	if err := inj.Apply(context.Background(), "rag", clusterRepoInfo{}, vals); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if vals["imagePullSecrets"] != 42 {
		t.Errorf("imagePullSecrets was mutated despite unexpected shape: %#v", vals["imagePullSecrets"])
	}
}

// buildNvidiaInjectorFixture sets up a fake client with valid Nvidia
// credentials wired up. Used by tests that focus on values-merge behavior.
func buildNvidiaInjectorFixture(t *testing.T) (client.Client, *AIWorkloadReconciler) {
	t.Helper()
	const opNS = "suse-ai-operator"
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
		Data:       map[string][]byte{"token": []byte("nvapi-xyz")},
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
	return c, r
}
