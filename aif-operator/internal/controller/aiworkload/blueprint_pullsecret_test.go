package aiworkload

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureCombinedPullSecret_IncludesNvidia(t *testing.T) {
	const opNS = "aif-operator"
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

	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	name, err := r.ensureCombinedPullSecret(context.Background(), r.localCC(), targetNS, clusterRepoInfo{})
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
	const opNS = "aif-operator"
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
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	name, err := r.ensureCombinedPullSecret(context.Background(), r.localCC(), targetNS, clusterRepoInfo{})
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
	const opNS = "aif-operator"
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
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}

	name, err := r.ensureCombinedPullSecret(context.Background(), r.localCC(), targetNS, clusterRepoInfo{})
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
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}
	inj := &nvidiaInjector{r: r}

	vals := map[string]any{}
	if _, err := inj.Apply(context.Background(), r.localCC(), targetNS, clusterRepoInfo{}, vals); err != nil {
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
	// All three NVIDIA env-var conventions must carry the same token so
	// charts that read any one of them work without per-chart tuning.
	for _, k := range nvidiaAPISecretKeys {
		if got := string(api.Data[k]); got != "nvapi-xyz" {
			t.Errorf("%s = %q, want %q", k, got, "nvapi-xyz")
		}
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
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}
	inj := &nvidiaInjector{r: r}

	if _, err := inj.Apply(context.Background(), r.localCC(), targetNS, clusterRepoInfo{}, map[string]any{}); err != nil {
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
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}
	inj := &nvidiaInjector{r: r}

	vals := map[string]any{}
	if _, err := inj.Apply(context.Background(), r.localCC(), targetNS, clusterRepoInfo{}, vals); err != nil {
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
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}
	inj := &nvidiaInjector{r: r}

	if _, err := inj.Apply(context.Background(), r.localCC(), targetNS, clusterRepoInfo{}, map[string]any{}); err != nil {
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
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}
	inj := &nvidiaInjector{r: r}

	vals := map[string]any{}
	if _, err := inj.Apply(context.Background(), r.localCC(), targetNS, clusterRepoInfo{}, vals); err != nil {
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
	if _, err := inj.Apply(context.Background(), r.localCC(), "rag", clusterRepoInfo{}, vals); err != nil {
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

	cc := r.localCC()
	vals := map[string]any{}
	if _, err := inj.Apply(context.Background(), cc, "rag", clusterRepoInfo{}, vals); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if _, err := inj.Apply(context.Background(), cc, "rag", clusterRepoInfo{}, vals); err != nil {
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
	if _, err := inj.Apply(context.Background(), r.localCC(), "rag", clusterRepoInfo{}, vals); err != nil {
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
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme, OperatorNamespace: opNS}
	return c, r
}

func TestInjectorFor_VendorNvidia(t *testing.T) {
	r := &AIWorkloadReconciler{}
	if _, ok := r.injectorFor(aiplatformv1alpha1.ComponentVendorNvidia).(*nvidiaInjector); !ok {
		t.Errorf("vendor nvidia did not yield *nvidiaInjector")
	}
}

func TestInjectorFor_VendorSUSE(t *testing.T) {
	r := &AIWorkloadReconciler{}
	if _, ok := r.injectorFor(aiplatformv1alpha1.ComponentVendorSUSE).(*suseInjector); !ok {
		t.Errorf("vendor suse did not yield *suseInjector")
	}
}

func TestInjectorFor_VendorEmptyDefaultsToSUSE(t *testing.T) {
	r := &AIWorkloadReconciler{}
	if _, ok := r.injectorFor("").(*suseInjector); !ok {
		t.Errorf("empty vendor did not default to *suseInjector")
	}
}

func TestInjectNvidiaPullSecretRefs_OperatorImagePullSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want []any // expected operator.image.pullSecrets
	}{
		{
			name: "empty values — creates operator.image with pull secret",
			in:   map[string]any{},
			want: []any{nvidiaImagePullSecretName},
		},
		{
			name: "operator present but no image — adds image.pullSecrets",
			in:   map[string]any{"operator": map[string]any{"replicas": 2}},
			want: []any{nvidiaImagePullSecretName},
		},
		{
			name: "operator.image present but no pullSecrets — adds list",
			in:   map[string]any{"operator": map[string]any{"image": map[string]any{"tag": "main"}}},
			want: []any{nvidiaImagePullSecretName},
		},
		{
			name: "operator.image.pullSecrets already has other entry — prepends ours",
			in: map[string]any{"operator": map[string]any{
				"image": map[string]any{"pullSecrets": []any{"my-regcred"}},
			}},
			want: []any{nvidiaImagePullSecretName, "my-regcred"},
		},
		{
			name: "operator.image.pullSecrets already contains ours — left alone",
			in: map[string]any{"operator": map[string]any{
				"image": map[string]any{"pullSecrets": []any{nvidiaImagePullSecretName}},
			}},
			want: []any{nvidiaImagePullSecretName},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			injectNvidiaPullSecretRefs(tc.in)
			op, _ := tc.in["operator"].(map[string]any)
			img, _ := op["image"].(map[string]any)
			got, _ := img["pullSecrets"].([]any)
			if !equalAnyStringSlice(got, tc.want) {
				t.Errorf("operator.image.pullSecrets: got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestInjectNvidiaPullSecretRefs_OperatorImagePullSecretsLeavesUnexpected(t *testing.T) {
	// If operator is present but not a map, leave it alone.
	vals := map[string]any{"operator": "not-a-map"}
	injectNvidiaPullSecretRefs(vals)
	if got := vals["operator"]; got != "not-a-map" {
		t.Errorf("expected operator string to be untouched, got %+v", got)
	}
	// If operator.image is present but not a map, leave it alone.
	vals = map[string]any{"operator": map[string]any{"image": "not-a-map"}}
	injectNvidiaPullSecretRefs(vals)
	op, _ := vals["operator"].(map[string]any)
	if got := op["image"]; got != "not-a-map" {
		t.Errorf("expected operator.image string to be untouched, got %+v", got)
	}
}

func TestDisableChartSecretCreation(t *testing.T) {
	tests := []struct {
		name    string
		initial map[string]any
		key     string
		secret  string
		wantKey map[string]any
	}{
		{
			name:    "absent → create:false + name",
			initial: map[string]any{},
			key:     "imagePullSecret",
			secret:  "ngc-secret",
			wantKey: map[string]any{"create": false, "name": "ngc-secret"},
		},
		{
			name:    "wrong shape (string) → replace",
			initial: map[string]any{"imagePullSecret": "garbage"},
			key:     "imagePullSecret",
			secret:  "ngc-secret",
			wantKey: map[string]any{"create": false, "name": "ngc-secret"},
		},
		{
			name: "existing map with create:true and other fields → flip create, preserve rest, set name if absent",
			initial: map[string]any{
				"imagePullSecret": map[string]any{
					"create":   true,
					"registry": "nvcr.io",
					"username": "$oauthtoken",
					"password": "",
				},
			},
			key:    "imagePullSecret",
			secret: "ngc-secret",
			wantKey: map[string]any{
				"create":   false,
				"name":     "ngc-secret",
				"registry": "nvcr.io",
				"username": "$oauthtoken",
				"password": "",
			},
		},
		{
			name: "existing map with user-provided name → honor it",
			initial: map[string]any{
				"imagePullSecret": map[string]any{"create": true, "name": "user-override"},
			},
			key:     "imagePullSecret",
			secret:  "ngc-secret",
			wantKey: map[string]any{"create": false, "name": "user-override"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			disableChartSecretCreation(tc.initial, tc.key, tc.secret)
			got, ok := tc.initial[tc.key].(map[string]any)
			if !ok {
				t.Fatalf("vals[%q] = %T, want map[string]any", tc.key, tc.initial[tc.key])
			}
			if len(got) != len(tc.wantKey) {
				t.Errorf("len(vals[%q]) = %d, want %d (got=%+v want=%+v)", tc.key, len(got), len(tc.wantKey), got, tc.wantKey)
			}
			for k, want := range tc.wantKey {
				if got[k] != want {
					t.Errorf("vals[%q][%q] = %v, want %v", tc.key, k, got[k], want)
				}
			}
		})
	}
}

// equalAnyStringSlice compares two []any treating each element as a string.
// Used by the operator.image.pullSecrets tests. Add at the bottom of the
// test file if not already present.
func equalAnyStringSlice(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		as, _ := a[i].(string)
		bs, _ := b[i].(string)
		if as != bs {
			return false
		}
	}
	return true
}
