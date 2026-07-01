package cluster_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/SUSE/aif-operator/internal/cluster"
)

func TestLocalClient_ApplySecret_CreatesNew(t *testing.T) {
	scheme := newCorev1Scheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	lc := cluster.NewLocalClient(c, scheme)

	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-secret", Namespace: "target-ns"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
	}
	if err := lc.ApplySecret(context.Background(), sec); err != nil {
		t.Fatalf("ApplySecret: %v", err)
	}

	var got corev1.Secret
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: "target-ns", Name: "ngc-secret"}, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Type != corev1.SecretTypeDockerConfigJson {
		t.Errorf("type: got %v want %v", got.Type, corev1.SecretTypeDockerConfigJson)
	}
}

func TestLocalClient_ApplySecret_IsIdempotent(t *testing.T) {
	scheme := newCorev1Scheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	lc := cluster.NewLocalClient(c, scheme)

	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-secret", Namespace: "target-ns"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
	}
	// Apply twice — second call should be a no-op (or an update with no diff).
	for i := 0; i < 2; i++ {
		if err := lc.ApplySecret(context.Background(), sec); err != nil {
			t.Fatalf("ApplySecret iteration %d: %v", i, err)
		}
	}
	var list corev1.SecretList
	if err := c.List(context.Background(), &list); err != nil {
		t.Fatalf("List: %v", err)
	}
	count := 0
	for _, s := range list.Items {
		if s.Name == "ngc-secret" && s.Namespace == "target-ns" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 ngc-secret in target-ns, got %d", count)
	}
}

func TestLocalClient_ApplySecret_UpdatesExisting(t *testing.T) {
	scheme := newCorev1Scheme(t)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-secret", Namespace: "target-ns"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"old.example":{}}}`)},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	lc := cluster.NewLocalClient(c, scheme)

	updated := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ngc-secret", Namespace: "target-ns"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"new.example":{}}}`)},
	}
	if err := lc.ApplySecret(context.Background(), updated); err != nil {
		t.Fatalf("ApplySecret: %v", err)
	}

	var got corev1.Secret
	if err := c.Get(context.Background(),
		types.NamespacedName{Namespace: "target-ns", Name: "ngc-secret"}, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Data[corev1.DockerConfigJsonKey]) != `{"auths":{"new.example":{}}}` {
		t.Errorf("data not updated: got %s", got.Data[corev1.DockerConfigJsonKey])
	}
}

func newCorev1Scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	return s
}
