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
	"errors"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const (
	bounceTestSecret = "aif-custom-pull-private-charts"
	bounceTestPodUID = "pod-uid-demo-0"
)

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

	bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
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
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "qdrant", Namespace: "test-ns", UID: "sts-uid",
		Annotations: map[string]string{helmReleaseNameAnnotation: testRelease}}}

	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, sts, serviceAccountWithSecrets("qdrant", bounceTestSecret)).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
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
	// Recreations for a missing delivered secret stop on their own (the new
	// pod carries the secret), so they leave the restart counter alone.
	if _, ok := got.Annotations[chartPodBounceAnnotation]; ok {
		t.Errorf("bounce annotation written: %v", got.Annotations)
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

	bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{"ngc-secret", "ngc-api"}, testReleases)
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != 0 {
		t.Errorf("bounced = %d, want 0 (recreated pods would still lack ngc-api)", bounced)
	}
}

// notStartedPod turns a pod into one that is scheduled but has not started
// any container yet (images still being pulled for the first time).
func notStartedPod(p *corev1.Pod) *corev1.Pod {
	p.Status = corev1.PodStatus{
		Phase: corev1.PodPending,
		InitContainerStatuses: []corev1.ContainerStatus{{
			Name:  "init",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}},
		}},
	}
	return p
}

// initRunningPod turns a pod into one that is still Pending while an init
// container runs (its main image is pulled only after init finishes).
func initRunningPod(p *corev1.Pod) *corev1.Pod {
	p.Status = corev1.PodStatus{
		Phase: corev1.PodPending,
		InitContainerStatuses: []corev1.ContainerStatus{{
			Name:  "init",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}},
	}
	return p
}

// A pod admitted without the delivered secret whose init container is already
// running has not reached its main image pull yet. It must be reported so the
// operator checks again once the pull fails, instead of settling and never
// looking at the namespace again (recreating it now would restart its init).
func TestRestartImagePullBackOffPods_ReportsPendingPodThatWillNeedRecreation(t *testing.T) {
	pod := initRunningPod(unlabelledBackOffPod("demo-0", "demo"))
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, helmReplicaSet(pod.Name, nil), serviceAccountWithSecrets("demo", bounceTestSecret)).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	bounced, pending, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != 0 || pending != 1 {
		t.Errorf("bounced=%d pending=%d, want 0 bounced and 1 pending", bounced, pending)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "test-ns", Name: pod.Name}, &corev1.Pod{}); err != nil {
		t.Errorf("pending pod must not be deleted: %v", err)
	}
}

// Pending pods that a recreation would not help are not reported, so the
// operator does not keep re-checking for nothing.
func TestRestartImagePullBackOffPods_DoesNotReportPendingPodsRecreationCannotHelp(t *testing.T) {
	cases := []struct {
		name        string
		pod         *corev1.Pod
		objs        []client.Object
		annotations map[string]string
	}{
		{
			name: "pod already references the secret",
			pod:  initRunningPod(unlabelledBackOffPod("a-0", "demo", bounceTestSecret)),
			objs: []client.Object{serviceAccountWithSecrets("demo", bounceTestSecret)},
		},
		{
			name: "ServiceAccount lacks the secret",
			pod:  initRunningPod(unlabelledBackOffPod("b-0", "demo")),
			objs: []client.Object{serviceAccountWithSecrets("demo")},
		},
		{
			name: "pod is running",
			pod: func() *corev1.Pod {
				p := unlabelledBackOffPod("d-0", "demo")
				p.Status = corev1.PodStatus{Phase: corev1.PodRunning}
				return p
			}(),
			objs: []client.Object{serviceAccountWithSecrets("demo", bounceTestSecret)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := newTestScheme(t)
			objs := append(tc.objs, tc.pod, helmReplicaSet(tc.pod.Name, tc.annotations))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

			bounced, pending, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
			if err != nil {
				t.Fatalf("restartImagePullBackOffPods: %v", err)
			}
			if bounced != 0 || pending != 0 {
				t.Errorf("bounced=%d pending=%d, want 0/0", bounced, pending)
			}
		})
	}
}

// With nothing left to merge, a namespace holding a pod that will need
// recreation is not settled, so the reconciler requeues and recreates the
// pod once its pull fails.
func TestReconcilePullSecretsForNamespace_UnsettledWhilePodAwaitsRecreation(t *testing.T) {
	pod := initRunningPod(unlabelledBackOffPod("demo-0", "demo"))
	demoSA := serviceAccountWithSecrets("demo", bounceTestSecret)
	demoSA.Labels = map[string]string{chartManagedByLabel: chartManagedByHelm}
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, helmReplicaSet(pod.Name, nil), demoSA, serviceAccountWithSecrets("default", bounceTestSecret)).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	settled, err := r.reconcilePullSecretsForNamespace(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases, logr.Discard())
	if err != nil {
		t.Fatalf("reconcilePullSecretsForNamespace: %v", err)
	}
	if settled {
		t.Error("settled = true, want false while a pod awaits recreation")
	}
}

// A pod admitted without the delivered secret that has not started any
// container is recreated right away: nothing is lost, and the recreated pod is
// admitted with the secret. This also avoids re-checking indefinitely for pods
// that stay Pending for unrelated reasons (e.g. unschedulable).
func TestRestartImagePullBackOffPods_RecreatesNotStartedPodMissingSecret(t *testing.T) {
	for _, tc := range []struct {
		name string
		pod  *corev1.Pod
	}{
		{name: "containers being created", pod: notStartedPod(unlabelledBackOffPod("e-0", "demo"))},
		{name: "not scheduled yet", pod: func() *corev1.Pod {
			p := unlabelledBackOffPod("f-0", "demo")
			p.Status = corev1.PodStatus{Phase: corev1.PodPending}
			return p
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bounced, exists := runBounce(t, tc.pod, serviceAccountWithSecrets("demo", bounceTestSecret))
			if bounced != 1 || exists {
				t.Errorf("bounced=%d podExists=%v, want the pod recreated", bounced, exists)
			}
		})
	}
}

// Charts that label their pods with the Helm managed-by label (the common
// scaffold) hit the same admission race and must be handled the same way.
func TestRestartImagePullBackOffPods_HandlesHelmLabelledPendingPods(t *testing.T) {
	labelled := func(p *corev1.Pod) *corev1.Pod {
		p.Labels[chartManagedByLabel] = chartManagedByHelm
		return p
	}

	bounced, exists := runBounce(t, labelled(notStartedPod(unlabelledBackOffPod("g-0", "demo"))),
		serviceAccountWithSecrets("demo", bounceTestSecret))
	if bounced != 1 || exists {
		t.Errorf("not started: bounced=%d podExists=%v, want the pod recreated", bounced, exists)
	}

	pod := labelled(initRunningPod(unlabelledBackOffPod("h-0", "demo")))
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, helmReplicaSet(pod.Name, nil), serviceAccountWithSecrets("demo", bounceTestSecret)).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	bounced, pending, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != 0 || pending != 1 {
		t.Errorf("init running: bounced=%d pending=%d, want 0 bounced and 1 pending", bounced, pending)
	}
}

// Several reconciles can run before the cache drops a pod the operator just
// deleted. Seeing the same pod again must not count another bounce, or one
// recreation uses up the controller's whole restart budget.
func TestRestartImagePullBackOffPods_CountsOneBouncePerPodAcrossStalePasses(t *testing.T) {
	pod := unlabelledBackOffPod("demo-0", "demo")
	pod.Labels[chartManagedByLabel] = chartManagedByHelm // the counted Helm-label path
	pod.UID = bounceTestPodUID
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, helmReplicaSet(pod.Name, nil), serviceAccountWithSecrets("demo", bounceTestSecret)).
		WithInterceptorFuncs(interceptor.Funcs{
			// The delete is accepted but the pod stays visible, as with a
			// cache that has not caught up yet.
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error { return nil },
		}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	for pass := 1; pass <= 3; pass++ {
		bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		want := 0
		if pass == 1 {
			want = 1
		}
		if bounced != want {
			t.Errorf("pass %d: bounced = %d, want %d", pass, bounced, want)
		}
	}
	rs := &appsv1.ReplicaSet{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "test-ns", Name: pod.Name + "-rs"}, rs); err != nil {
		t.Fatalf("get ReplicaSet: %v", err)
	}
	if got := rs.Annotations[chartPodBounceAnnotation]; got != "1" {
		t.Errorf("bounce count = %q, want \"1\"", got)
	}
}

// A pod that is already being deleted is on its way out; bouncing it again
// would only consume the restart budget.
func TestRestartImagePullBackOffPods_SkipsPodBeingDeleted(t *testing.T) {
	pod := unlabelledBackOffPod("demo-0", "demo")
	now := metav1.Now()
	pod.DeletionTimestamp = &now
	pod.Finalizers = []string{"test/hold"}
	bounced, _ := runBounce(t, pod, serviceAccountWithSecrets("demo", bounceTestSecret))
	if bounced != 0 {
		t.Errorf("bounced = %d, want 0 for a pod already being deleted", bounced)
	}
}

// A failed delete leaves the pod running. Later passes must retry it rather
// than treat it as already restarted, or a single-replica workload stays
// stuck in ImagePullBackOff.
func TestRestartImagePullBackOffPods_RetriesAfterFailedDelete(t *testing.T) {
	pod := unlabelledBackOffPod("demo-0", "demo")
	pod.Labels[chartManagedByLabel] = chartManagedByHelm // the counted Helm-label path
	pod.UID = bounceTestPodUID
	deletes := 0
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, helmReplicaSet(pod.Name, nil), serviceAccountWithSecrets("demo", bounceTestSecret)).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				deletes++
				if deletes == 1 {
					return errors.New("transient API error")
				}
				return cl.Delete(ctx, obj, opts...)
			},
		}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	if _, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases); err == nil {
		t.Fatal("first pass: want the delete error returned")
	}
	bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if bounced != 1 {
		t.Errorf("second pass: bounced = %d, want the pod retried and deleted", bounced)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "test-ns", Name: pod.Name}, &corev1.Pod{}); err == nil {
		t.Error("pod still exists after the retry")
	}
}

// A pod someone else already deleted (Delete returns NotFound) can still show
// up in a stale cache. It must be recorded as restarted so later passes do
// not spend more of the controller's restart budget on it.
func TestRestartImagePullBackOffPods_RecordsPodAlreadyGoneOnce(t *testing.T) {
	pod := unlabelledBackOffPod("demo-0", "demo")
	pod.Labels[chartManagedByLabel] = chartManagedByHelm // the counted Helm-label path
	pod.UID = bounceTestPodUID
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, helmReplicaSet(pod.Name, nil), serviceAccountWithSecrets("demo", bounceTestSecret)).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return apierrors.NewNotFound(corev1.Resource("pods"), "demo-0")
			},
		}).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	for pass := 1; pass <= 3; pass++ {
		if _, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	rs := &appsv1.ReplicaSet{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "test-ns", Name: pod.Name + "-rs"}, rs); err != nil {
		t.Fatalf("get ReplicaSet: %v", err)
	}
	if got := rs.Annotations[chartPodBounceAnnotation]; got != "1" {
		t.Errorf("bounce count = %q, want \"1\"", got)
	}
}

// Recreations for a missing delivered secret do not use the per-controller
// restart budget: every pod of a controller with many replicas recovers, and
// a controller already at the cap still gets its pods recreated.
func TestRestartImagePullBackOffPods_RecreatesEveryReplicaRegardlessOfCap(t *testing.T) {
	scheme := newTestScheme(t)
	rs := helmReplicaSet("demo", map[string]string{chartPodBounceAnnotation: "3"})
	objs := []client.Object{rs, serviceAccountWithSecrets("demo", bounceTestSecret)}
	tru := true
	for _, name := range []string{"demo-a", "demo-b", "demo-c", "demo-d", "demo-e"} {
		p := unlabelledBackOffPod(name, "demo")
		p.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &tru}}
		objs = append(objs, p)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
	if err != nil {
		t.Fatalf("restartImagePullBackOffPods: %v", err)
	}
	if bounced != 5 {
		t.Errorf("bounced = %d, want all 5 replicas recreated", bounced)
	}
}

// A pod listing a pull secret its ServiceAccount does not carry got it from
// somewhere else (e.g. a mutating webhook). The ServiceAccount's secrets are
// then not merged in at admission, so recreating the pod would not help.
func TestRestartImagePullBackOffPods_SkipsPodWithPullSecretNotFromItsServiceAccount(t *testing.T) {
	pod := unlabelledBackOffPod("demo-0", "demo", "injected-by-webhook")
	bounced, exists := runBounce(t, pod, serviceAccountWithSecrets("demo", bounceTestSecret))
	if bounced != 0 || !exists {
		t.Errorf("bounced=%d podExists=%v, want the pod left alone", bounced, exists)
	}

	// Secrets that did come from the ServiceAccount do not block recreation.
	pod = unlabelledBackOffPod("demo-1", "demo", "older-sa-secret")
	bounced, exists = runBounce(t, pod, serviceAccountWithSecrets("demo", "older-sa-secret", bounceTestSecret))
	if bounced != 1 || exists {
		t.Errorf("bounced=%d podExists=%v, want the pod recreated", bounced, exists)
	}
}

// Only pods of the workload's own Helm releases are recreated: another app's
// pods sharing the namespace (and often the default ServiceAccount) are left
// alone, on both the delivered-secret and the Helm-label paths.
func TestRestartImagePullBackOffPods_OnlyRecreatesPodsOfTheWorkloadReleases(t *testing.T) {
	other := map[string]string{helmReleaseNameAnnotation: "someone-elses-app"}

	pod := unlabelledBackOffPod("other-0", "default")
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, helmReplicaSet(pod.Name, other), serviceAccountWithSecrets("default", bounceTestSecret)).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
	if bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases); err != nil || bounced != 0 {
		t.Errorf("delivered-secret path: bounced=%d err=%v, want another release's pod left alone", bounced, err)
	}

	labelled := podWithContainerWaiting("other-1", false, "ImagePullBackOff")
	c = fake.NewClientBuilder().WithScheme(scheme).WithObjects(labelled, helmReplicaSet(labelled.Name, other)).Build()
	r = &AIWorkloadReconciler{Client: c, Scheme: scheme}
	if bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", nil, testReleases); err != nil || bounced != 0 {
		t.Errorf("Helm-label path: bounced=%d err=%v, want another release's pod left alone", bounced, err)
	}

	// Without any release in scope, nothing is recreated.
	pod = unlabelledBackOffPod("demo-0", "demo")
	c = fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(pod, helmReplicaSet(pod.Name, nil), serviceAccountWithSecrets("demo", bounceTestSecret)).Build()
	r = &AIWorkloadReconciler{Client: c, Scheme: scheme}
	if bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, nil); err != nil || bounced != 0 {
		t.Errorf("empty scope: bounced=%d err=%v, want nothing recreated", bounced, err)
	}
}

// A Deployment's ReplicaSet does not carry the Helm release annotation; the
// release is taken from the Deployment that controls it.
func TestRestartImagePullBackOffPods_ResolvesReleaseThroughTheDeployment(t *testing.T) {
	for _, tc := range []struct {
		release string
		want    int
	}{{testRelease, 1}, {"someone-elses-app", 0}} {
		tru := true
		dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "test-ns", UID: "dep-uid",
			Annotations: map[string]string{helmReleaseNameAnnotation: tc.release}}}
		rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "web-0-rs", Namespace: "test-ns", UID: "rs-uid-web-0",
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "web", UID: "dep-uid", Controller: &tru}}}}
		pod := unlabelledBackOffPod("web-0", "demo")
		scheme := newTestScheme(t)
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(dep, rs, pod, serviceAccountWithSecrets("demo", bounceTestSecret)).Build()
		r := &AIWorkloadReconciler{Client: c, Scheme: scheme}
		bounced, _, err := r.restartImagePullBackOffPods(context.Background(), "test-ns", []string{bounceTestSecret}, testReleases)
		if err != nil {
			t.Fatalf("release %s: %v", tc.release, err)
		}
		if bounced != tc.want {
			t.Errorf("Deployment release %q: bounced = %d, want %d", tc.release, bounced, tc.want)
		}
	}
}

// The downstream bundle's merge job recreates only pods of the workload's own
// releases, so the bundle must carry them for its namespace.
func TestDeliverPullSecrets_BundleCarriesTheWorkloadReleases(t *testing.T) {
	scheme := newTestScheme(t)
	bundleListGVK := schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleList"}
	scheme.AddKnownTypeWithName(bundleListGVK.GroupVersion().WithKind("Bundle"), &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(bundleListGVK, &unstructured.UnstructuredList{})
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, Scheme: scheme}

	w := &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec:       aiplatformv1alpha1.AIWorkloadSpec{TargetNamespace: "target-ns", TargetClusters: []string{"c-aaa"}},
		Status: aiplatformv1alpha1.AIWorkloadStatus{
			PullSecretDeliveries: []aiplatformv1alpha1.PullSecretDelivery{{Namespace: "target-ns", Names: []string{"ngc-secret"}}},
		},
	}
	scope := workloadScope{releases: map[string]map[string]bool{"target-ns": {"vdb": true, "api": true}}}
	if err := r.deliverPullSecrets(context.Background(), w, dummyPullSecretFactory, scope); err != nil {
		t.Fatalf("deliverPullSecrets: %v", err)
	}

	var bundles unstructured.UnstructuredList
	bundles.SetGroupVersionKind(bundleListGVK)
	if err := c.List(context.Background(), &bundles, client.InNamespace("fleet-default")); err != nil || len(bundles.Items) != 1 {
		t.Fatalf("list bundles: %v (got %d)", err, len(bundles.Items))
	}
	resources, _, _ := unstructured.NestedSlice(bundles.Items[0].Object, "spec", "resources")
	found := false
	for _, res := range resources {
		if content, _ := res.(map[string]any)["content"].(string); strings.Contains(content, "RELEASES='api vdb'") {
			found = true
		}
	}
	if !found {
		t.Errorf("no bundle resource carries RELEASES='api vdb'")
	}
}
