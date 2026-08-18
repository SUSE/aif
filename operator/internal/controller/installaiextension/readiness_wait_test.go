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

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

// readinessTimeout is short enough to be crossed by backdating a marker and long
// enough that no test crosses it by accident.
const readinessTimeout = 5 * time.Minute

// releaseName is what deriveReleaseName produces for helmExtension's chart URL,
// and therefore the app.kubernetes.io/instance value the readiness lookups
// select on.
const releaseName = "aif-ui-server"

// readinessReconciler builds a reconciler whose cluster contains exactly the
// workload objects a test seeds, so the readiness lookups fail or succeed for
// the reason the test is about rather than for a missing scheme entry.
func readinessReconciler(
	t *testing.T,
	ext *v1alpha1.InstallAIExtension,
	funcs interceptor.Funcs,
	objs ...client.Object,
) *InstallAIExtensionReconciler {
	t.Helper()

	scheme := kruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add appsv1: %v", err)
	}
	gv := schema.GroupVersion{Group: "catalog.cattle.io", Version: "v1"}
	for _, kind := range []string{"ClusterRepo", "UIPlugin"} {
		scheme.AddKnownTypeWithName(gv.WithKind(kind), &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gv.WithKind(kind+"List"), &unstructured.UnstructuredList{})
	}
	metav1.AddToGroupVersion(scheme, gv)

	seeded := append([]client.Object{ext.DeepCopy()}, objs...)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.InstallAIExtension{}).
		WithObjects(seeded...).
		WithInterceptorFuncs(funcs).
		Build()

	var stored v1alpha1.InstallAIExtension
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(ext), &stored); err != nil {
		t.Fatalf("get seeded object: %v", err)
	}
	ext.ResourceVersion = stored.ResourceVersion

	return &InstallAIExtensionReconciler{
		Client:             c,
		Scheme:             scheme,
		ExtensionNamespace: wiringNamespace,
		ReadinessTimeout:   readinessTimeout,
		rancherMgr:         rancher.NewManager(c),
		helmClientFor: func(string) (helmClient.HelmClient, error) {
			return &stubHelmClient{
				deployed: &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 1},
				last:     &helmClient.ReleaseInfo{Version: requestedVersion, Status: helmClient.StatusDeployed, Revision: 1},
			}, nil
		},
	}
}

func releaseLabels() map[string]string {
	return map[string]string{"app.kubernetes.io/instance": releaseName}
}

// readyDeployment is a Deployment that has finished rolling out its current
// spec. Every count is set, not just ReadyReplicas: readiness means "the
// applied revision is serving", so a status carrying a ready pod but no
// updated replicas describes a rollout still in progress — and is not a state
// the API server can produce in any case.
func readyDeployment() *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: releaseName, Namespace: wiringNamespace, Labels: releaseLabels(), Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
}

func service(ports ...corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: releaseName, Namespace: wiringNamespace, Labels: releaseLabels()},
		Spec:       corev1.ServiceSpec{Ports: ports},
	}
}

// failListOf makes List fail for one list type, leaving every other client call
// working — the only way to reach the "readiness check itself errored" branch.
func failListOf(want client.ObjectList) interceptor.Funcs {
	return interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if sameListType(list, want) {
				return errors.New("connection refused")
			}
			return c.List(ctx, list, opts...)
		},
	}
}

func sameListType(got, want client.ObjectList) bool {
	switch want.(type) {
	case *appsv1.DeploymentList:
		_, ok := got.(*appsv1.DeploymentList)
		return ok
	case *corev1.ServiceList:
		_, ok := got.(*corev1.ServiceList)
		return ok
	}
	return false
}

func backdate(ext *v1alpha1.InstallAIExtension, key string, age time.Duration) {
	if ext.Annotations == nil {
		ext.Annotations = make(map[string]string)
	}
	ext.Annotations[key] = time.Now().Add(-age).Format(time.RFC3339)
}

// Each of these install steps used to requeue every readinessRequeue with
// nothing bounding it. The CR never reached a terminal state, so nothing ever
// stopped the reconcile — and every pass re-entered EnsureRelease, six times per
// health-check interval, for as long as the CR existed.
func TestReadinessWaitsAreBounded(t *testing.T) {
	tests := []struct {
		name       string
		annotation string
		condType   string
		reason     string
		funcs      interceptor.Funcs
		objs       []client.Object
	}{
		{
			name:       "deployment readiness check errors",
			annotation: annotationWaitingSince,
			condType:   conditionTypeDeploymentReady,
			reason:     "CheckFailed",
			funcs:      failListOf(&appsv1.DeploymentList{}),
		},
		{
			name:       "deployment never becomes ready",
			annotation: annotationWaitingSince,
			condType:   conditionTypeDeploymentReady,
			reason:     "NotReady",
		},
		{
			name:       "service never appears",
			annotation: annotationServiceWaitingSince,
			condType:   conditionTypeServiceReady,
			reason:     "ServiceFailed",
			objs:       []client.Object{readyDeployment()},
		},
		{
			name:       "service lookup errors",
			annotation: annotationServiceWaitingSince,
			condType:   conditionTypeServiceReady,
			reason:     "ServiceFailed",
			funcs:      failListOf(&corev1.ServiceList{}),
			objs:       []client.Object{readyDeployment()},
		},
		{
			name:       "service has no usable endpoint",
			annotation: annotationServiceWaitingSince,
			condType:   conditionTypeServiceReady,
			reason:     "ServiceFailed",
			objs:       []client.Object{readyDeployment(), service()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("first pass starts the clock and requeues", func(t *testing.T) {
				ext := helmExtension()
				r := readinessReconciler(t, ext, tt.funcs, tt.objs...)

				result, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
				if err != nil {
					t.Fatalf("reconcile error = %v", err)
				}
				if result.RequeueAfter != readinessRequeue {
					t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, readinessRequeue)
				}
				if ext.Annotations[tt.annotation] == "" {
					t.Errorf("no %s marker recorded, so the wait is unbounded", tt.annotation)
				}
				// The first observation is the one a gate is most likely to read; it
				// must not still be advertising the previous pass's success.
				cond := meta.FindStatusCondition(ext.Status.Conditions, tt.condType)
				if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != tt.reason {
					t.Errorf("%s condition = %+v, want False/%s", tt.condType, cond, tt.reason)
				}
			})

			t.Run("past the bound it fails terminally", func(t *testing.T) {
				ext := helmExtension()
				backdate(ext, tt.annotation, readinessTimeout+time.Minute)
				r := readinessReconciler(t, ext, tt.funcs, tt.objs...)

				result, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
				if err != nil {
					t.Fatalf("reconcile error = %v", err)
				}
				// The wait is over; the CR is not. A rollout that overran its bound can
				// still finish, and nothing would tell the controller — so the pass
				// drops to the health-check cadence rather than stopping. The bound's
				// purpose survives intact: what it exists to prevent is this CR
				// re-entering EnsureRelease every readinessRequeue forever.
				if result.RequeueAfter != healthCheckInterval {
					t.Errorf("RequeueAfter = %v, want %v; the wait is over",
						result.RequeueAfter, healthCheckInterval)
				}
				if result.RequeueAfter <= readinessRequeue {
					t.Errorf("RequeueAfter = %v, want slower than the %v readiness poll; "+
						"the bound exists to stop that loop", result.RequeueAfter, readinessRequeue)
				}
				if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
					t.Errorf("Phase = %s, want Failed", ext.Status.Phase)
				}
				cond := meta.FindStatusCondition(ext.Status.Conditions, tt.condType)
				if cond == nil || cond.Reason != reasonReadinessTimedOut {
					t.Fatalf("%s condition = %+v, want %s", tt.condType, cond, reasonReadinessTimedOut)
				}
				if !containsAll(cond.Message, readinessTimeout.String()) {
					t.Errorf("message %q does not say how long was waited", cond.Message)
				}
			})
		})
	}
}

// The Service wait needs its own annotation, not the deployment's.
//
// A pass that reaches the Service lookup has, by definition, just seen the
// deployment go ready — and that clears annotationWaitingSince. Had the Service
// wait shared the key, every pass would have wiped its own start time and
// re-stamped it, so time.Since(start) would never exceed the timeout and the
// bound added here would do nothing at all.
func TestServiceWaitSurvivesTheDeploymentMarkerBeingCleared(t *testing.T) {
	ext := helmExtension()
	// The deployment wait is live and already at its bound, as it would be for a
	// deployment that took a while to come up.
	backdate(ext, annotationWaitingSince, readinessTimeout+time.Minute)
	r := readinessReconciler(t, ext, interceptor.Funcs{}, readyDeployment())
	ctx := context.Background()

	if _, err := r.reconcileHelmSource(ctx, ext, wiringNamespace); err != nil {
		t.Fatalf("first pass error = %v", err)
	}
	if _, ok := ext.Annotations[annotationWaitingSince]; ok {
		t.Fatal("deployment marker survived the deployment going ready")
	}
	started := ext.Annotations[annotationServiceWaitingSince]
	if started == "" {
		t.Fatal("no service marker recorded")
	}

	// Backdate past the bound, then reconcile again. If the clear above had also
	// dropped this marker, the pass would stamp a fresh one and requeue forever.
	backdate(ext, annotationServiceWaitingSince, readinessTimeout+time.Minute)
	result, err := r.reconcileHelmSource(ctx, ext, wiringNamespace)
	if err != nil {
		t.Fatalf("second pass error = %v", err)
	}
	// Past its bound, so the pass drops to the health-check cadence. Had the
	// marker been dropped and re-stamped, this would still be the readinessRequeue
	// of a wait that had just started — which is what the assertion below rules
	// out, and the reason 60s and 10s must not be conflated here.
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("RequeueAfter = %v, want %v; the service wait is past its bound",
			result.RequeueAfter, healthCheckInterval)
	}
	if result.RequeueAfter == readinessRequeue {
		t.Error("RequeueAfter is the readiness poll interval, so the service marker was " +
			"cleared and re-stamped and the bound will never fire")
	}
	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
		t.Errorf("Phase = %s, want Failed", ext.Status.Phase)
	}
}

// Left behind, the marker would make the next Service outage time out on its
// first observation instead of being waited out.
func TestServiceMarkerIsClearedOnceTheServiceResolves(t *testing.T) {
	ext := helmExtension()
	backdate(ext, annotationServiceWaitingSince, time.Minute)
	r := readinessReconciler(t, ext, interceptor.Funcs{},
		readyDeployment(), service(corev1.ServicePort{Port: 8080}))

	if _, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}
	if _, ok := ext.Annotations[annotationServiceWaitingSince]; ok {
		t.Error("service marker survived the service becoming resolvable")
	}

	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeServiceReady)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("ServiceReady = %+v, want True", cond)
	}
}

// A wait that has not reached its bound keeps requeuing rather than failing, and
// keeps the clock it started with. Guards against a bound so eager that a slow
// but healthy install is failed, and against the start time being re-stamped on
// every pass, which is the same defect as sharing an annotation key.
func TestReadinessWaitKeepsItsStartTimeWhileInsideTheBound(t *testing.T) {
	ext := helmExtension()
	backdate(ext, annotationWaitingSince, time.Minute)
	started := ext.Annotations[annotationWaitingSince]
	r := readinessReconciler(t, ext, interceptor.Funcs{})
	ctx := context.Background()

	for pass := 1; pass <= 3; pass++ {
		result, err := r.reconcileHelmSource(ctx, ext, wiringNamespace)
		if err != nil {
			t.Fatalf("pass %d error = %v", pass, err)
		}
		if result.RequeueAfter != readinessRequeue {
			t.Errorf("pass %d RequeueAfter = %v, want %v", pass, result.RequeueAfter, readinessRequeue)
		}
		if got := ext.Annotations[annotationWaitingSince]; got != started {
			t.Fatalf("pass %d moved the start time from %s to %s; the wait would never time out",
				pass, started, got)
		}
		if ext.Status.Phase == v1alpha1.InstallAIExtensionPhaseFailed {
			t.Fatalf("pass %d failed a wait that is still inside its bound", pass)
		}
	}
}

func TestAwaitReadinessSurfacesAnAnnotationWriteFailure(t *testing.T) {
	ext := helmExtension()
	wantErr := errors.New("conflict")
	r := readinessReconciler(t, ext, interceptor.Funcs{
		Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
			return wantErr
		},
	})

	_, err := r.awaitReadiness(context.Background(), ext, annotationWaitingSince,
		conditionTypeDeploymentReady, "NotReady", "waiting")

	if !errors.Is(err, wantErr) {
		t.Errorf("awaitReadiness() error = %v, want %v; an unpersisted start time means an unbounded wait", err, wantErr)
	}
}
