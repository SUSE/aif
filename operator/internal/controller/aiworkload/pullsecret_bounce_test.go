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

package aiworkload

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const bounceTestSecret = "aif-custom-pull-private-charts"

// unlabelledBackOffPod builds a pod in ImagePullBackOff WITHOUT the Helm
// managed-by label (many charts label only their top-level resources), owned
// by a ReplicaSet named "<name>-rs", running under serviceAccount and carrying
// podSecrets in its spec.
func unlabelledBackOffPod(name, serviceAccount string, podSecrets ...string) *corev1.Pod {
	tru := true
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "test-ns",
			Labels: map[string]string{"app.kubernetes.io/name": name},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "ReplicaSet",
				Name: name + "-rs", UID: types.UID("rs-uid-" + name),
				Controller: &tru,
			}},
		},
		Spec: corev1.PodSpec{ServiceAccountName: serviceAccount},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name:  "init",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
			}},
		},
	}
	for _, s := range podSecrets {
		p.Spec.ImagePullSecrets = append(p.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: s})
	}
	return p
}

func serviceAccountWithSecrets(name string, secrets ...string) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns"}}
	for _, s := range secrets {
		sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{Name: s})
	}
	return sa
}

// runBounce runs restartImagePullBackOffPods with the delivered secret and
// reports the bounce count and whether the pod still exists.
func runBounce(t *testing.T, pod *corev1.Pod, objs ...client.Object) (int, bool) {
	t.Helper()
	scheme := newTestScheme(t)
	objs = append(objs, pod, helmReplicaSet(pod.Name, nil))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	bounced, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret})
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	err = c.Get(context.Background(), types.NamespacedName{Namespace: "test-ns", Name: pod.Name}, &corev1.Pod{})
	return bounced, err == nil
}

// A pod created before its ServiceAccount received the delivered secret has
// no pull secrets baked in. It must be recreated even when the chart does not
// put the Helm managed-by label on its pods.
func TestRestartImagePullBackOffPods_BouncesUnlabelledPodMissingSecretItsSACarries(t *testing.T) {
	pod := unlabelledBackOffPod("qdrant-0", "qdrant")
	bounced, exists := runBounce(t, pod, serviceAccountWithSecrets("qdrant", bounceTestSecret))
	if bounced != 1 || exists {
		t.Errorf("bounced=%d podExists=%v, want 1 bounce and pod deleted", bounced, exists)
	}
}

// An empty serviceAccountName means the namespace "default" ServiceAccount.
func TestRestartImagePullBackOffPods_EmptyServiceAccountNameMeansDefault(t *testing.T) {
	pod := unlabelledBackOffPod("worker", "")
	bounced, exists := runBounce(t, pod, serviceAccountWithSecrets("default", bounceTestSecret))
	if bounced != 1 || exists {
		t.Errorf("bounced=%d podExists=%v, want 1 bounce and pod deleted", bounced, exists)
	}
}

// The pod already references the delivered secret: recreating it cannot help
// (the kubelet re-reads referenced secrets on every pull attempt).
func TestRestartImagePullBackOffPods_SkipsUnlabelledPodAlreadyReferencingSecret(t *testing.T) {
	pod := unlabelledBackOffPod("qdrant-0", "qdrant", bounceTestSecret)
	bounced, exists := runBounce(t, pod, serviceAccountWithSecrets("qdrant", bounceTestSecret))
	if bounced != 0 || !exists {
		t.Errorf("bounced=%d podExists=%v, want no bounce", bounced, exists)
	}
}

// The pod's ServiceAccount does not carry the delivered secret, so a
// recreated pod would not get it either.
func TestRestartImagePullBackOffPods_SkipsUnlabelledPodWhoseSALacksSecret(t *testing.T) {
	pod := unlabelledBackOffPod("other", "other")
	bounced, exists := runBounce(t, pod, serviceAccountWithSecrets("other"))
	if bounced != 0 || !exists {
		t.Errorf("bounced=%d podExists=%v, want no bounce", bounced, exists)
	}
}

// The bounce counter lives on the pod's owning controller; StatefulSet-owned
// pods (a common shape for charts with persistent storage) must be handled.
func TestRestartImagePullBackOffPods_BouncesStatefulSetPod(t *testing.T) {
	pod := unlabelledBackOffPod("qdrant-0", "qdrant")
	tru := true
	pod.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "apps/v1", Kind: "StatefulSet", Name: "qdrant", UID: "sts-uid", Controller: &tru,
	}}
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "qdrant", Namespace: "test-ns", UID: "sts-uid"}}

	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, sts, serviceAccountWithSecrets("qdrant", bounceTestSecret)).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	bounced, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret})
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != 1 {
		t.Fatalf("bounced = %d, want 1", bounced)
	}
	got := &appsv1.StatefulSet{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "test-ns", Name: "qdrant"}, got); err != nil {
		t.Fatalf("get StatefulSet: %v", err)
	}
	if got.Annotations[chartPodBounceAnnotation] != "1" {
		t.Errorf("bounce annotation = %q, want \"1\"", got.Annotations[chartPodBounceAnnotation])
	}
}

// A controller whose pod template lists its own imagePullSecrets never gets
// the ServiceAccount's secrets merged in, so recreating its pods cannot help
// and would only use up the bounce budget.
func TestRestartImagePullBackOffPods_SkipsPodWhoseControllerTemplateSetsPullSecrets(t *testing.T) {
	pod := unlabelledBackOffPod("nim-0", "default", "ngc-secret")
	rs := helmReplicaSet(pod.Name, nil)
	rs.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "ngc-secret"}}

	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, rs, serviceAccountWithSecrets("default", "ngc-secret", "ngc-api")).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	bounced, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{"ngc-secret", "ngc-api"})
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != 0 {
		t.Errorf("bounced = %d, want 0 (recreated pods would still lack ngc-api)", bounced)
	}
}
