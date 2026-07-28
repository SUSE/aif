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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/credentials"
)

func selfSignedPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31-1, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb})
	return
}

func TestResolveHelmTLS_Nil(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	got, err := credentials.ResolveHelmTLS(context.Background(), c, "aif-operator", nil)
	if err != nil || got != nil {
		t.Fatalf("nil tls: want (nil,nil) got (%v,%v)", got, err)
	}
}

func TestResolveHelmTLS_CA(t *testing.T) {
	caPEM, _ := selfSignedPEM(t)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "aif-operator"},
			Data: map[string][]byte{"ca.crt": caPEM}},
	).Build()
	got, err := credentials.ResolveHelmTLS(context.Background(), c, "aif-operator",
		&aiplatformv1alpha1.HelmTLS{CASecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ca", Key: "ca.crt"}})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.RootCAs == nil {
		t.Fatalf("expected RootCAs set")
	}
}

func TestResolveHelmTLS_CABundle(t *testing.T) {
	caPEM1, _ := selfSignedPEM(t)
	caPEM2, _ := selfSignedPEM(t)
	bundle := append(append([]byte{}, caPEM1...), caPEM2...)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "aif-operator"},
			Data: map[string][]byte{"ca.crt": bundle}},
	).Build()
	got, err := credentials.ResolveHelmTLS(context.Background(), c, "aif-operator",
		&aiplatformv1alpha1.HelmTLS{CASecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ca", Key: "ca.crt"}})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.RootCAs == nil {
		t.Fatalf("expected RootCAs set for multi-cert bundle")
	}
}

func TestResolveHelmTLS_Insecure(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	got, err := credentials.ResolveHelmTLS(context.Background(), c, "aif-operator",
		&aiplatformv1alpha1.HelmTLS{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.InsecureSkipVerify {
		t.Fatalf("expected InsecureSkipVerify")
	}
}

func TestResolveHelmTLS_ClientCert(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cli", Namespace: "aif-operator"},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}},
	).Build()
	got, err := credentials.ResolveHelmTLS(context.Background(), c, "aif-operator",
		&aiplatformv1alpha1.HelmTLS{ClientTLSSecretRef: &aiplatformv1alpha1.LocalSecretRef{Name: "cli"}})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Certificates) != 1 {
		t.Fatalf("expected 1 client certificate")
	}
}

func TestResolveHelmTLS_BadCA(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "aif-operator"},
			Data: map[string][]byte{"ca.crt": []byte("not a pem")}},
	).Build()
	_, err := credentials.ResolveHelmTLS(context.Background(), c, "aif-operator",
		&aiplatformv1alpha1.HelmTLS{CASecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "ca", Key: "ca.crt"}})
	if err == nil {
		t.Fatal("expected error for invalid CA PEM")
	}
}

func TestResolveHelmTLS_MissingCASecret(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	_, err := credentials.ResolveHelmTLS(context.Background(), c, "aif-operator",
		&aiplatformv1alpha1.HelmTLS{CASecretRef: &aiplatformv1alpha1.SecretKeyRef{Name: "nope", Key: "ca.crt"}})
	if err == nil {
		t.Fatal("expected error for missing CA secret")
	}
}

func TestResolveHelmTLS_ClientCertMissingKeys(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cli", Namespace: "aif-operator"},
			Type: corev1.SecretTypeTLS, Data: map[string][]byte{}},
	).Build()
	_, err := credentials.ResolveHelmTLS(context.Background(), c, "aif-operator",
		&aiplatformv1alpha1.HelmTLS{ClientTLSSecretRef: &aiplatformv1alpha1.LocalSecretRef{Name: "cli"}})
	if err == nil {
		t.Fatal("expected error for client TLS secret missing tls.crt/tls.key")
	}
}
