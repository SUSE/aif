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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

const (
	helmStatusNS      = "aiq-aira-system"
	helmStatusRelease = "aiq-aira"
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

func helmReleaseSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sh.helm.release.v1." + helmStatusRelease + ".v1",
			Namespace: helmStatusNS,
			Labels:    map[string]string{"owner": "helm", "name": helmStatusRelease},
		},
	}
}

func helmStatefulSet(name string, replicas, ready int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   helmStatusNS,
			Labels:      map[string]string{helmManagedLabel: "Helm"},
			Annotations: map[string]string{helmReleaseNameAnnotation: helmStatusRelease},
		},
		Spec:   appsv1.StatefulSetSpec{Replicas: &replicas},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: ready},
	}
}

func helmStatusWorkload(createdAt time.Time) *aiplatformv1alpha1.AIWorkload {
	return &aiplatformv1alpha1.AIWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "wl",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(createdAt),
		},
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

// A not-ready StatefulSet past the grace period must yield Degraded — the bug was
// that a present Helm release secret alone reported Running regardless of pods.
func TestReconcileHelmStatus_NotReadyPastGrace_Degraded(t *testing.T) {
	scheme := newHelmStatusScheme(t)

	sts := helmStatefulSet("aiq-aira-nim-llm", 1, 0) // 0/1 ready
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(helmReleaseSecret(), sts).Build()
	r := &AIWorkloadReconciler{Client: c, APIReader: c, Scheme: scheme}

	w := helmStatusWorkload(time.Now().Add(-10 * time.Minute))
	if err := r.reconcileHelmStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileHelmStatus: %v", err)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseDegraded {
		t.Errorf("phase = %q, want Degraded", w.Status.Phase)
	}
}

// A not-ready StatefulSet within the grace period is a normal slow start → Pending.
func TestReconcileHelmStatus_NotReadyWithinGrace_Pending(t *testing.T) {
	scheme := newHelmStatusScheme(t)

	sts := helmStatefulSet("aiq-aira-nim-llm", 1, 0)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(helmReleaseSecret(), sts).Build()
	r := &AIWorkloadReconciler{Client: c, APIReader: c, Scheme: scheme}

	w := helmStatusWorkload(time.Now())
	if err := r.reconcileHelmStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileHelmStatus: %v", err)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhasePending {
		t.Errorf("phase = %q, want Pending", w.Status.Phase)
	}
}

// All controllers ready → Running.
func TestReconcileHelmStatus_AllReady_Running(t *testing.T) {
	scheme := newHelmStatusScheme(t)

	sts := helmStatefulSet("aiq-aira-nim-llm", 1, 1) // 1/1 ready
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(helmReleaseSecret(), sts).Build()
	r := &AIWorkloadReconciler{Client: c, APIReader: c, Scheme: scheme}

	w := helmStatusWorkload(time.Now().Add(-10 * time.Minute))
	if err := r.reconcileHelmStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileHelmStatus: %v", err)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseRunning {
		t.Errorf("phase = %q, want Running", w.Status.Phase)
	}
}

// No Helm release secret → Unknown (unchanged from prior behavior).
func TestReconcileHelmStatus_NoRelease_Unknown(t *testing.T) {
	scheme := newHelmStatusScheme(t)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AIWorkloadReconciler{Client: c, APIReader: c, Scheme: scheme}

	w := helmStatusWorkload(time.Now())
	if err := r.reconcileHelmStatus(context.Background(), w); err != nil {
		t.Fatalf("reconcileHelmStatus: %v", err)
	}
	if w.Status.Phase != aiplatformv1alpha1.AIWorkloadPhaseUnknown {
		t.Errorf("phase = %q, want Unknown", w.Status.Phase)
	}
}
