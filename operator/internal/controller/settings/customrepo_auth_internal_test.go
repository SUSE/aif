package settings

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestApplyCustomRepoAuthSecret(t *testing.T) {
	s := newTestScheme(t)
	const ns = "aif-operator"

	t.Run("BasicAuth", func(t *testing.T) {
		sourceCreds := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "repo-creds", Namespace: ns},
			Data:       map[string][]byte{"user": []byte("myuser"), "token": []byte("mytoken")},
		}
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(sourceCreds).Build()
		r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

		repo := aiplatformv1alpha1.CustomRepoSpec{
			Name:           "myrepo",
			Type:           "helm",
			UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "repo-creds", Key: "user"},
			TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "repo-creds", Key: "token"},
		}

		name, changed, err := r.applyCustomRepoAuthSecret(context.Background(), ns, repo)
		if err != nil {
			t.Fatalf("applyCustomRepoAuthSecret: %v", err)
		}
		expectedName := credentials.CustomRepoAuthSecretName("myrepo")
		if name != expectedName {
			t.Errorf("returned name = %q, want %q", name, expectedName)
		}
		if !changed {
			t.Errorf("expected changed=true on first write")
		}

		var mirror corev1.Secret
		if err := c.Get(context.Background(), types.NamespacedName{
			Name: expectedName, Namespace: "cattle-system",
		}, &mirror); err != nil {
			t.Fatalf("expected mirror in cattle-system: %v", err)
		}
		if mirror.Type != corev1.SecretTypeBasicAuth {
			t.Errorf("secret type = %q, want %q", mirror.Type, corev1.SecretTypeBasicAuth)
		}
		if string(mirror.Data["username"]) != "myuser" {
			t.Errorf("username = %q, want myuser", string(mirror.Data["username"]))
		}
		if string(mirror.Data["password"]) != "mytoken" {
			t.Errorf("password = %q, want mytoken", string(mirror.Data["password"]))
		}
	})

	t.Run("SSH", func(t *testing.T) {
		sshKey := "-----BEGIN OPENSSH PRIVATE KEY-----\ntest-key-data\n-----END OPENSSH PRIVATE KEY-----"
		sourceKey := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "git-ssh", Namespace: ns},
			Data:       map[string][]byte{"ssh-privatekey": []byte(sshKey)},
		}
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(sourceKey).Build()
		r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

		repo := aiplatformv1alpha1.CustomRepoSpec{
			Name:            "gitrepo",
			Type:            "git",
			SSHKeySecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "git-ssh", Key: "ssh-privatekey"},
		}

		name, changed, err := r.applyCustomRepoAuthSecret(context.Background(), ns, repo)
		if err != nil {
			t.Fatalf("applyCustomRepoAuthSecret: %v", err)
		}
		expectedName := credentials.CustomRepoAuthSecretName("gitrepo")
		if name != expectedName {
			t.Errorf("returned name = %q, want %q", name, expectedName)
		}
		if !changed {
			t.Errorf("expected changed=true on first write")
		}

		var mirror corev1.Secret
		if err := c.Get(context.Background(), types.NamespacedName{
			Name: expectedName, Namespace: "cattle-system",
		}, &mirror); err != nil {
			t.Fatalf("expected mirror in cattle-system: %v", err)
		}
		if mirror.Type != corev1.SecretTypeSSHAuth {
			t.Errorf("secret type = %q, want %q", mirror.Type, corev1.SecretTypeSSHAuth)
		}
		if string(mirror.Data[corev1.SSHAuthPrivateKey]) != sshKey {
			t.Errorf("ssh-privatekey mismatch, got %q", string(mirror.Data[corev1.SSHAuthPrivateKey]))
		}
	})

	t.Run("Anonymous", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).Build()
		r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

		repo := aiplatformv1alpha1.CustomRepoSpec{
			Name: "public",
			Type: "helm",
			// No auth refs
		}

		name, changed, err := r.applyCustomRepoAuthSecret(context.Background(), ns, repo)
		if err != nil {
			t.Fatalf("applyCustomRepoAuthSecret: %v", err)
		}
		if name != "" {
			t.Errorf("anonymous repo returned name = %q, want empty", name)
		}
		if changed {
			t.Errorf("anonymous repo returned changed=true, want false")
		}

		// Verify no secret was created
		var mirror corev1.Secret
		err = c.Get(context.Background(), types.NamespacedName{
			Name: credentials.CustomRepoAuthSecretName("public"), Namespace: "cattle-system",
		}, &mirror)
		if !apierrors.IsNotFound(err) {
			t.Errorf("expected no secret for anonymous repo, got err=%v", err)
		}
	})

	t.Run("CAOnlyMaterializesNoSecret", func(t *testing.T) {
		// A CA-only (anonymous + CABundleSecretRef) repo no longer materializes an
		// auth secret: Rancher reads the CA from ClusterRepo.spec.caBundle (set by
		// applyCustomClusterRepo), not from a clientSecret cacerts key.
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "ca-bundle", Namespace: ns},
			Data:       map[string][]byte{"ca.crt": []byte(testCAPEM(t))},
		}
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(caSecret).Build()
		r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}

		repo := aiplatformv1alpha1.CustomRepoSpec{
			Name:              "ca-only",
			Type:              "helm",
			CABundleSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ca-bundle", Key: "ca.crt"},
		}

		name, changed, err := r.applyCustomRepoAuthSecret(context.Background(), ns, repo)
		if err != nil {
			t.Fatalf("applyCustomRepoAuthSecret: %v", err)
		}
		if name != "" {
			t.Errorf("CA-only repo returned name = %q, want empty (CA lives on spec.caBundle)", name)
		}
		if changed {
			t.Errorf("CA-only repo returned changed=true, want false")
		}

		var mirror corev1.Secret
		err = c.Get(context.Background(), types.NamespacedName{
			Name: credentials.CustomRepoAuthSecretName("ca-only"), Namespace: "cattle-system",
		}, &mirror)
		if !apierrors.IsNotFound(err) {
			t.Errorf("expected no auth secret for CA-only repo, got err=%v", err)
		}
	})

	t.Run("AuthModeSwitchRecreatesSecret", func(t *testing.T) {
		// Secret.type is immutable; switching basic-auth -> ssh-auth must delete and
		// recreate rather than wedge on an "field is immutable" apply error.
		sshKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nk\n-----END OPENSSH PRIVATE KEY-----"
		creds := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "src", Namespace: ns},
			Data:       map[string][]byte{"user": []byte("u"), "token": []byte("t"), "ssh-privatekey": []byte(sshKey)},
		}
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(creds).Build()
		r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
		expectedName := credentials.CustomRepoAuthSecretName("switch")

		basic := aiplatformv1alpha1.CustomRepoSpec{
			Name:           "switch",
			Type:           "helm",
			UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "src", Key: "user"},
			TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "src", Key: "token"},
		}
		if _, _, err := r.applyCustomRepoAuthSecret(context.Background(), ns, basic); err != nil {
			t.Fatalf("apply basic-auth: %v", err)
		}

		ssh := aiplatformv1alpha1.CustomRepoSpec{
			Name:            "switch",
			Type:            "git",
			SSHKeySecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "src", Key: "ssh-privatekey"},
		}
		if _, _, err := r.applyCustomRepoAuthSecret(context.Background(), ns, ssh); err != nil {
			t.Fatalf("apply ssh-auth over basic-auth: %v", err)
		}

		var mirror corev1.Secret
		if err := c.Get(context.Background(), types.NamespacedName{Name: expectedName, Namespace: "cattle-system"}, &mirror); err != nil {
			t.Fatalf("expected recreated mirror: %v", err)
		}
		if mirror.Type != corev1.SecretTypeSSHAuth {
			t.Errorf("secret type = %q, want %q after switch", mirror.Type, corev1.SecretTypeSSHAuth)
		}
	})

	t.Run("ClearingCredentialsDeletesSecret", func(t *testing.T) {
		creds := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "src", Namespace: ns},
			Data:       map[string][]byte{"user": []byte("u"), "token": []byte("t")},
		}
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(creds).Build()
		r := &SettingsReconciler{Client: c, Scheme: s, OperatorNamespace: ns}
		expectedName := credentials.CustomRepoAuthSecretName("drop")

		basic := aiplatformv1alpha1.CustomRepoSpec{
			Name:           "drop",
			Type:           "helm",
			UserSecretRef:  &aiplatformv1alpha1.SecretKeyRef{Name: "src", Key: "user"},
			TokenSecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "src", Key: "token"},
		}
		if _, _, err := r.applyCustomRepoAuthSecret(context.Background(), ns, basic); err != nil {
			t.Fatalf("apply basic-auth: %v", err)
		}

		anon := aiplatformv1alpha1.CustomRepoSpec{Name: "drop", Type: "helm"}
		name, changed, err := r.applyCustomRepoAuthSecret(context.Background(), ns, anon)
		if err != nil {
			t.Fatalf("apply anonymous: %v", err)
		}
		if name != "" || changed {
			t.Errorf("clearing creds returned name=%q changed=%v, want empty/false", name, changed)
		}
		var mirror corev1.Secret
		err = c.Get(context.Background(), types.NamespacedName{Name: expectedName, Namespace: "cattle-system"}, &mirror)
		if !apierrors.IsNotFound(err) {
			t.Errorf("expected auth secret deleted after clearing creds, got err=%v", err)
		}
	})
}

// testCAPEM returns a valid self-signed CA certificate in PEM form.
func testCAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
}
