package rancher

import (
	"bytes"
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func caScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

func TestDiscoverInternalCA(t *testing.T) {
	const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

	t.Run("returns tls.crt when present", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: InternalCAName, Namespace: InternalCANamespace},
			Data: map[string][]byte{
				"tls.crt": []byte(pem),
				"tls.key": []byte("PRIVATE KEY MUST NOT BE RETURNED"),
			},
		}
		c := fake.NewClientBuilder().WithScheme(caScheme(t)).WithObjects(sec).Build()

		got, err := DiscoverInternalCA(context.Background(), c)
		if err != nil {
			t.Fatalf("DiscoverInternalCA: %v", err)
		}
		if string(got) != pem {
			t.Fatalf("got %q want %q", got, pem)
		}
	})

	t.Run("never returns the private key", func(t *testing.T) {
		const key = "PRIVATE KEY MUST NOT BE RETURNED"
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: InternalCAName, Namespace: InternalCANamespace},
			Data: map[string][]byte{
				"tls.crt": []byte(pem),
				"tls.key": []byte(key),
			},
		}
		c := fake.NewClientBuilder().WithScheme(caScheme(t)).WithObjects(sec).Build()

		got, err := DiscoverInternalCA(context.Background(), c)
		if err != nil {
			t.Fatalf("DiscoverInternalCA: %v", err)
		}
		if bytes.Contains(got, []byte(key)) {
			t.Fatal("returned bundle contains the CA private key")
		}
	})

	t.Run("ErrCANotFound when the Secret is absent", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(caScheme(t)).Build()

		_, err := DiscoverInternalCA(context.Background(), c)
		if !errors.Is(err, ErrCANotFound) {
			t.Fatalf("err=%v want ErrCANotFound", err)
		}
	})

	t.Run("ErrCANotFound when tls.crt is missing or empty", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: InternalCAName, Namespace: InternalCANamespace},
			Data:       map[string][]byte{"tls.key": []byte("x")},
		}
		c := fake.NewClientBuilder().WithScheme(caScheme(t)).WithObjects(sec).Build()

		_, err := DiscoverInternalCA(context.Background(), c)
		if !errors.Is(err, ErrCANotFound) {
			t.Fatalf("err=%v want ErrCANotFound", err)
		}
	})
}
