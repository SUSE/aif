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
	"sort"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const (
	helmStatusNS      = "aiq-aira-system"
	helmStatusRelease = "aiq-aira"
	appSelectorKey    = "app"
)

func newHelmStatusScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := aiplatformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add aiplatform scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}
	return scheme
}

// helmReleaseSecret builds a Helm release secret at the given revision/status.
// Pass status "" to mimic older Helm secrets that carry no status label.
func helmReleaseSecret(version, status string) *corev1.Secret {
	labels := map[string]string{"owner": "helm", "name": helmStatusRelease}
	if version != "" {
		labels["version"] = version
	}
	if status != "" {
		labels["status"] = status
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1." + helmStatusRelease + ".v" + version,
			Namespace: helmStatusNS,
			Labels:    labels,
		},
	}
}

func helmStatefulSet(name string, ready int32) *appsv1.StatefulSet {
	one := int32(1)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   helmStatusNS,
			Labels:      map[string]string{chartManagedByLabel: chartManagedByHelm},
			Annotations: map[string]string{helmReleaseNameAnnotation: helmStatusRelease},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{appSelectorKey: name}},
		},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: ready},
	}
}

func helmDeployment(name, release string) *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   helmStatusNS,
			Generation:  1,
			Labels:      map[string]string{chartManagedByLabel: chartManagedByHelm},
			Annotations: map[string]string{helmReleaseNameAnnotation: release},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{appSelectorKey: name}},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
		},
	}
}

// crashingPod is a pod matching a controller's {app: <owner>} selector whose
// container is stuck in CrashLoopBackOff.
func crashingPod(name, owner string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: helmStatusNS,
			Labels:    map[string]string{appSelectorKey: owner},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: reasonCrashLoopBackOff},
				},
			}},
		},
	}
}

func helmStatusWorkload() *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{Name: "wl", Namespace: "default"},
		Spec: aiplatformv1alpha1.AIWorkloadSpec{
			TargetNamespace: helmStatusNS,
			DeployStrategy:  aiplatformv1alpha1.AIWorkloadDeployHelm,
			Source: aiplatformv1alpha1.AIWorkloadSource{
				SourceType: aiplatformv1alpha1.AIWorkloadSourceApp,
				App:        &aiplatformv1alpha1.AppSource{Release: helmStatusRelease},
			},
		},
	}
}

func reconcileHelm(t *testing.T, objs ...client.Object) *aiplatformv1alpha1.AIWorkload {
	t.Helper()
	scheme := newHelmStatusScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	r := &AIWorkloadReconciler{Client: c, APIReader: c, Scheme: scheme}
	w := helmStatusWorkload()
	if err := r.reconcileHelmStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileHelmStatus: %v", err)
	}
	return w
}

// A not-ready StatefulSet whose pods are merely still starting (no hard failure)
// is a normal slow deploy → Pending, indefinitely, with no time-based flip.
func TestReconcileHelmStatus_NotReadyStarting_Pending(t *testing.T) {
	sts := helmStatefulSet("aiq-aira-nim-llm", 0) // 0/1 ready, no pods
	w := reconcileHelm(t, helmReleaseSecret("1", "deployed"), sts)
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhasePending {
		t.Errorf("phase = %q, want Pending", w.Status.Phase)
	}
}

// A not-ready StatefulSet with a pod stuck in CrashLoopBackOff is Degraded
// immediately — waiting will not fix a crash loop.
func TestReconcileHelmStatus_CrashLoopPod_Degraded(t *testing.T) {
	sts := helmStatefulSet("aiq-aira-nim-llm", 0)
	pod := crashingPod("aiq-aira-nim-llm-0", "aiq-aira-nim-llm")
	w := reconcileHelm(t, helmReleaseSecret("1", "deployed"), sts, pod)
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseDegraded {
		t.Errorf("phase = %q, want Degraded", w.Status.Phase)
	}
	c := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if c == nil || c.Status != metav1.ConditionFalse {
		t.Fatalf("Ready condition = %+v, want status False", c)
	}
	if !strings.Contains(c.Message, reasonCrashLoopBackOff) {
		t.Errorf("condition message %q does not name the pod failure reason", c.Message)
	}
	// The message must not leak the user-supplied target namespace.
	if strings.Contains(c.Message, helmStatusNS) {
		t.Errorf("condition message %q leaks the target namespace", c.Message)
	}
}

// All controllers ready → Running with a True Ready condition.
func TestReconcileHelmStatus_AllReady_Running(t *testing.T) {
	sts := helmStatefulSet("aiq-aira-nim-llm", 1) // 1/1 ready
	w := reconcileHelm(t, helmReleaseSecret("1", "deployed"), sts)
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseRunning {
		t.Errorf("phase = %q, want Running", w.Status.Phase)
	}
	if c := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("Ready condition = %+v, want status True", c)
	}
}

// No Helm release secret → Unknown.
func TestReconcileHelmStatus_NoRelease_Unknown(t *testing.T) {
	w := reconcileHelm(t)
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseUnknown {
		t.Errorf("phase = %q, want Unknown", w.Status.Phase)
	}
}

// A failed Helm install can leave no workload controllers behind; the release
// secret's own status must still drive the phase to Degraded rather than the
// empty controller scan reading as Running.
func TestReconcileHelmStatus_ReleaseFailed_Degraded(t *testing.T) {
	w := reconcileHelm(t, helmReleaseSecret("1", "failed"))
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseDegraded {
		t.Errorf("phase = %q, want Degraded", w.Status.Phase)
	}
}

// A release still installing/upgrading is Pending until Helm settles.
func TestReconcileHelmStatus_ReleasePending_Pending(t *testing.T) {
	w := reconcileHelm(t, helmReleaseSecret("1", "pending-install"))
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhasePending {
		t.Errorf("phase = %q, want Pending", w.Status.Phase)
	}
}

// The newest revision's status wins: an upgrade that failed over a previously
// deployed revision is Degraded, not Running.
func TestReconcileHelmStatus_NewestRevisionWins_Degraded(t *testing.T) {
	w := reconcileHelm(t,
		helmReleaseSecret("1", "superseded"),
		helmReleaseSecret("2", "failed"),
	)
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseDegraded {
		t.Errorf("phase = %q, want Degraded", w.Status.Phase)
	}
}

// helmManagedControllers must match only the release's own resources: right
// managed-by label AND right release annotation.
func TestHelmManagedControllers_Filtering(t *testing.T) {
	scheme := newHelmStatusScheme(t)

	ours := helmDeployment("aiq-aira-api", helmStatusRelease)
	otherRelease := helmDeployment("someone-else", "other-release") // wrong annotation
	notHelm := helmStatefulSet("bare-sts", 1)
	notHelm.Labels = nil // no managed-by=Helm label → excluded by the list selector
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "aiq-aira-ds",
			Namespace:   helmStatusNS,
			Labels:      map[string]string{chartManagedByLabel: chartManagedByHelm},
			Annotations: map[string]string{helmReleaseNameAnnotation: helmStatusRelease},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{appSelectorKey: "aiq-aira-ds"}},
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{Type: appsv1.RollingUpdateDaemonSetStrategyType},
		},
		Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, UpdatedNumberScheduled: 0, NumberAvailable: 0},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(ours, otherRelease, notHelm, ds).Build()
	r := &AIWorkloadReconciler{Client: c, APIReader: c, Scheme: scheme}

	got, err := r.helmManagedControllers(context.Background(), helmStatusNS, helmStatusRelease)
	if err != nil {
		t.Fatalf("helmManagedControllers: %v", err)
	}

	type kr struct {
		kr    string
		ready bool
	}
	names := make([]kr, 0, len(got))
	for _, w := range got {
		names = append(names, kr{w.kind + "/" + w.name, w.ready})
	}
	sort.Slice(names, func(i, j int) bool { return names[i].kr < names[j].kr })

	want := []kr{
		{"DaemonSet/aiq-aira-ds", false}, // 0/1 available
		{"Deployment/aiq-aira-api", true},
	}
	if len(names) != len(want) {
		t.Fatalf("matched %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("controller[%d] = %v, want %v", i, names[i], want[i])
		}
	}
}
