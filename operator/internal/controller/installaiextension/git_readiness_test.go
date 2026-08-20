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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// gitReleaseName is what reconcileGitSource installs under — the extension name,
// not deriveReleaseName's output. Spelled out because the whole readiness lookup
// selects on it, and pointing it at releaseName would make these tests watch a
// workload the git path never creates.
const gitReleaseName = "aif-ui"

// rollingDeployment is a Deployment the deployment controller has not finished
// rolling out: the spec update is observed, but the new revision's replica is
// not up yet. The mid-rollout shape, which is what a git install races.
func rollingDeployment(instance string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       instance,
			Namespace:  wiringNamespace,
			Labels:     map[string]string{"app.kubernetes.io/instance": instance},
			Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           1,
			UpdatedReplicas:    0,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
}

// The git path applied a chart and declared UIPluginReady=True in the very next
// statement, with nothing between the two.
//
// That was survivable while Helm's up.Wait blocked until the rollout finished —
// the wait was just happening a layer down. Turning Wait off is the point of this
// branch, and the replacement was written into the Helm path only, so an upgrade
// to a broken image tag reported Installed the moment Helm returned and stayed
// there: the CR says the extension is running, the old pod is still serving, and
// the new one is in CrashLoopBackOff.
func TestGitSourceWaitsForItsRolloutToFinish(t *testing.T) {
	ext := gitExtension()
	r := readinessReconciler(t, ext, interceptor.Funcs{}, rollingDeployment(gitReleaseName))

	result, err := r.reconcileGitSource(context.Background(), ext, wiringNamespace)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeDeploymentReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "NotReady" {
		t.Fatalf("DeploymentReady = %+v, want False/NotReady; the git path reported the "+
			"extension installed while its rollout was still in progress", cond)
	}
	if result.RequeueAfter != readinessRequeue {
		t.Errorf("RequeueAfter = %v, want %v; the rollout finishing produces no event",
			result.RequeueAfter, readinessRequeue)
	}
	if ext.Annotations[annotationWaitingSince] == "" {
		t.Error("no waiting-since marker recorded, so the git wait is unbounded")
	}
}

// The wait the git path gets is the same bounded one the Helm path gets, not an
// open-ended poll. Without this, fixing the previous test by requeuing forever
// would pass it.
func TestGitReadinessWaitIsBounded(t *testing.T) {
	ext := gitExtension()
	backdate(ext, annotationWaitingSince, readinessTimeout+time.Minute)
	r := readinessReconciler(t, ext, interceptor.Funcs{}, rollingDeployment(gitReleaseName))

	result, err := r.reconcileGitSource(context.Background(), ext, wiringNamespace)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
		t.Errorf("Phase = %s, want Failed; the wait exhausted", ext.Status.Phase)
	}
	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeDeploymentReady)
	if cond == nil || cond.Reason != reasonReadinessTimedOut {
		t.Errorf("DeploymentReady = %+v, want %s", cond, reasonReadinessTimedOut)
	}
	if result.RequeueAfter != healthCheckInterval {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, healthCheckInterval)
	}
}

// The constraint that decides the git path's policy, and the reason it is not
// simply the Helm path's.
//
// A Rancher UI-plugin chart may legitimately contain nothing but a UIPlugin CR
// and a ClusterRepo — no Deployment anywhere. Hold that to "a Deployment must
// exist and be ready" and a correct extension waits out the full
// ReadinessTimeout and then reports Failed, on every install, forever: strictly
// worse than the missing wait this commit adds.
//
// So absence is tolerated here and only here. The Helm path's counterpart is the
// "deployment never becomes ready" case in TestReadinessWaitsAreBounded, which
// seeds no Deployment either and must still refuse to report ready.
func TestGitSourceWithoutADeploymentStillInstalls(t *testing.T) {
	ext := gitExtension()
	r := readinessReconciler(t, ext, interceptor.Funcs{})

	result, err := r.reconcileGitSource(context.Background(), ext, wiringNamespace)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0; there is nothing to wait for and waiting "+
			"anyway ends in a timeout the chart can never clear", result.RequeueAfter)
	}
	if ext.Status.Phase == v1alpha1.InstallAIExtensionPhaseFailed {
		t.Error("Phase = Failed for a chart that installed exactly as designed")
	}
	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeUIPlugin)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("UIPluginReady = %+v, want True", cond)
	}
	if _, ok := ext.Annotations[annotationWaitingSince]; ok {
		t.Error("a waiting-since marker was stamped for a wait that never started; " +
			"the next real rollout would inherit its clock")
	}
}

// The revision re-anchoring applies to the git path too — it lives in the shared
// helper, and this is what stops that from being an accident of where the code
// happens to sit. Same shape as TestANewRevisionRestartsTheReadinessClock: a
// stamp left by the rollout that timed out must not be charged to the next one.
func TestGitReadinessClockRestartsOnANewRevision(t *testing.T) {
	ext := gitExtension()
	ext.Status.HelmReleaseRevision = 1
	backdate(ext, annotationWaitingSince, readinessTimeout+20*time.Minute)

	r := readinessReconciler(t, ext, interceptor.Funcs{}, rollingDeployment(gitReleaseName))
	r.helmClientFor = atRevision(2)

	result, err := r.reconcileGitSource(context.Background(), ext, wiringNamespace)
	if err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	if result.RequeueAfter != readinessRequeue {
		t.Errorf("RequeueAfter = %v, want %v; revision 2 is a different rollout from the "+
			"one that timed out", result.RequeueAfter, readinessRequeue)
	}
	cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeDeploymentReady)
	if cond != nil && cond.Reason == reasonReadinessTimedOut {
		t.Errorf("DeploymentReady = %+v; a rollout that just started was timed out against "+
			"the previous revision's clock", cond)
	}
}
