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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

// errRancher is what a rejected write looks like from the reconciler's side.
// The wording is the shape of the real thing — Rancher's admission webhook
// refusing an object while it restarts — because the message ends up in the
// condition a user reads.
var errRancher = errors.New("admission webhook denied the request")

// stubRancherManager fails whichever Ensure call a test names and succeeds at
// everything else, so a case reaches exactly the branch it is about.
type stubRancherManager struct {
	clusterRepoErr error
	uiPluginErr    error
}

func (s *stubRancherManager) CheckCRDs(context.Context, []string) error { return nil }

func (s *stubRancherManager) EnsureClusterRepo(context.Context, *v1alpha1.InstallAIExtension, string) error {
	return s.clusterRepoErr
}

func (s *stubRancherManager) EnsureUIPlugin(
	context.Context, *v1alpha1.InstallAIExtension, string, string,
) error {
	return s.uiPluginErr
}

func (s *stubRancherManager) DeleteClusterRepo(context.Context, string) error { return nil }

func (s *stubRancherManager) DeleteUIPlugin(context.Context, string, string) error { return nil }

// TestRancherFailuresAreRetried covers the four places a reconcile gives up on
// a Rancher object — a ClusterRepo and a UIPlugin on each source path.
//
// All four are transients the cluster resolves without anything happening to
// the CR: an admission webhook mid-restart, a CRD not yet served, the extension
// server not answering its index request yet. The controller watches
// InstallAIExtension and nothing else, so a pass that returns a zero Result
// here parks the extension in Failed until someone edits it or the informer
// resyncs — ten hours later.
//
// Untested until now because both Ensure calls sat behind the real Rancher
// manager: no case drove one to failure, and deleting the RequeueAfter from any
// of the four left the whole suite green.
func TestRancherFailuresAreRetried(t *testing.T) {
	tests := []struct {
		name     string
		condType string
		// stub decides which of the two calls fails.
		stub *stubRancherManager
		// helm marks the cases that run the Helm path; the rest run the git path.
		helm bool
		// ensureErr fails the git path's chart install, which is how that path
		// reaches its UIPlugin branch — it installs a release rather than writing
		// a UIPlugin directly.
		ensureErr error
	}{
		{
			name:     "helm source cannot write its ClusterRepo",
			condType: conditionTypeClusterRepo,
			stub:     &stubRancherManager{clusterRepoErr: errRancher},
			helm:     true,
		},
		{
			name:     "helm source cannot write its UIPlugin",
			condType: conditionTypeUIPlugin,
			stub:     &stubRancherManager{uiPluginErr: errRancher},
			helm:     true,
		},
		{
			name:     "git source cannot write its ClusterRepo",
			condType: conditionTypeClusterRepo,
			stub:     &stubRancherManager{clusterRepoErr: errRancher},
		},
		{
			name:      "git source cannot install its UI-plugin chart",
			condType:  conditionTypeUIPlugin,
			stub:      &stubRancherManager{},
			ensureErr: errRancher,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				ext    *v1alpha1.InstallAIExtension
				objs   []client.Object
				result ctrl.Result
				err    error
			)

			if tt.helm {
				ext = helmExtension()
				// The Helm path only reaches Rancher once the release is running and
				// its Service has an endpoint; without both it requeues on readiness
				// and never gets as far as the branch under test.
				objs = []client.Object{
					readyDeployment(),
					service(corev1.ServicePort{Name: "http", Port: 8080}),
				}
			} else {
				ext = gitExtension()
				objs = []client.Object{readyDeployment()}
			}

			r := readinessReconciler(t, ext, interceptor.Funcs{}, objs...)
			r.rancherMgr = tt.stub
			if tt.ensureErr != nil {
				r.helmClientFor = func(string) (helmClient.HelmClient, error) {
					return &stubHelmClient{ensureErr: tt.ensureErr}, nil
				}
			}

			if tt.helm {
				result, err = r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
			} else {
				result, err = r.reconcileGitSource(context.Background(), ext, wiringNamespace)
			}

			// Not an error return: this is the cluster's problem to fix, and
			// surfacing it as one would log a stack trace every minute for as long
			// as it lasts. See the shutdown-guard tests for the same distinction.
			if err != nil {
				t.Fatalf("reconcile error = %v, want nil with the failure recorded on the CR", err)
			}
			if result.RequeueAfter != healthCheckInterval {
				t.Errorf("RequeueAfter = %v, want %v; nothing else wakes this CR, so the "+
					"extension stays Failed until the informer resyncs",
					result.RequeueAfter, healthCheckInterval)
			}

			cond := meta.FindStatusCondition(ext.Status.Conditions, tt.condType)
			if cond == nil || cond.Status != metav1.ConditionFalse {
				t.Errorf("%s = %+v, want False; the retry has to be visible as a reason, "+
					"not just a requeue", tt.condType, cond)
			}
			// Ready is what the user and any gate read. A pass that left it True
			// while requeuing would advertise a working extension for as long as the
			// failure lasted.
			ready := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeReady)
			if ready == nil || ready.Status != metav1.ConditionFalse {
				t.Errorf("Ready = %+v, want False", ready)
			}
			if ext.Status.Phase != v1alpha1.InstallAIExtensionPhaseFailed {
				t.Errorf("Phase = %q, want %q", ext.Status.Phase, v1alpha1.InstallAIExtensionPhaseFailed)
			}
		})
	}
}
