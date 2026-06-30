package aiworkload

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func rotationScheme(t *testing.T) *runtime.Scheme {
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

// A rotation of a well-known registry credential secret in the operator
// namespace must re-enqueue EVERY AIWorkload, so the operator rebuilds and
// re-delivers the dockerconfigjson pull secrets it derives from those creds
// (suse-ai-pull-combined, ngc-secret, ngc-api). Without this, a rotated key
// leaves already-delivered pull secrets stale.
func TestCredentialSecretToAIWorkloads_EnqueuesAllOnWellKnownSecret(t *testing.T) {
	s := rotationScheme(t)
	w1 := &aiplatformv1alpha1.AIWorkload{ObjectMeta: metav1.ObjectMeta{Name: "litellm", Namespace: "litellm-system"}}
	w2 := &aiplatformv1alpha1.AIWorkload{ObjectMeta: metav1.ObjectMeta{Name: "rag", Namespace: "rag-system"}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(w1, w2).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: s, OperatorNamespace: "aif-operator"}

	for _, name := range []string{"application-collection", "nvidia-registry", "suse-registry", "appco", "nvidia"} {
		sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "aif-operator"}}
		reqs := r.credentialSecretToAIWorkloads(context.Background(), sec)
		if len(reqs) != 2 {
			t.Errorf("well-known secret %q: expected 2 enqueued AIWorkloads, got %d", name, len(reqs))
		}
	}
}

func TestCredentialSecretToAIWorkloads_IgnoresUnrelatedSecrets(t *testing.T) {
	s := rotationScheme(t)
	w1 := &aiplatformv1alpha1.AIWorkload{ObjectMeta: metav1.ObjectMeta{Name: "litellm", Namespace: "litellm-system"}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(w1).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: s, OperatorNamespace: "aif-operator"}

	cases := []struct {
		desc      string
		name      string
		namespace string
	}{
		{"well-known name, wrong namespace", "application-collection", "default"},
		{"unknown name, operator namespace", "some-other-secret", "aif-operator"},
		{"helm release secret", "sh.helm.release.v1.litellm.v1", "litellm-system"},
	}
	for _, tc := range cases {
		sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tc.name, Namespace: tc.namespace}}
		reqs := r.credentialSecretToAIWorkloads(context.Background(), sec)
		if len(reqs) != 0 {
			t.Errorf("%s: expected 0 enqueued AIWorkloads, got %d", tc.desc, len(reqs))
		}
	}
}
