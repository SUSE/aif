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
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	helmClient "github.com/SUSE/aif-operator/internal/infra/helm"
)

const servicePort = 8080

// helmSourceReconciler builds a reconciler for which the Helm path's server
// release is running and its Service resolvable, so a pass reaches the
// extension chart, with every Helm call answered by stub.
func helmSourceReconciler(
	t *testing.T,
	ext *v1alpha1.InstallAIExtension,
	stub *stubHelmClient,
) (*InstallAIExtensionReconciler, string) {
	t.Helper()

	svc := service(corev1.ServicePort{Name: "http", Port: servicePort})
	r := readinessReconciler(t, ext, interceptor.Funcs{}, readyDeployment(), svc)
	r.helmClientFor = func(string) (helmClient.HelmClient, error) { return stub, nil }
	return r, fmt.Sprintf("http://%s.%s:%d", svc.Name, svc.Namespace, servicePort)
}

// Rancher's Extensions page pairs an installed UIPlugin with its catalog chart
// through a release named after the plugin that names the ClusterRepo it came
// from. That pairing is the card's logo, description and certification, so
// both source kinds have to install the chart that way.
func TestBothSourceKindsInstallTheExtensionChartWithTheClusterRepoLabel(t *testing.T) {
	tests := []struct {
		name string
		ext  *v1alpha1.InstallAIExtension
		run  func(*InstallAIExtensionReconciler, *v1alpha1.InstallAIExtension) (ctrl.Result, error)
	}{
		{
			name: "helm", ext: helmExtension(),
			run: func(r *InstallAIExtensionReconciler, ext *v1alpha1.InstallAIExtension) (ctrl.Result, error) {
				return r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
			},
		},
		{
			name: "git", ext: gitExtension(),
			run: func(r *InstallAIExtensionReconciler, ext *v1alpha1.InstallAIExtension) (ctrl.Result, error) {
				return r.reconcileGitSource(context.Background(), ext, wiringNamespace)
			},
		},
	}

	wantLabels := map[string]string{"catalog.cattle.io/cluster-repo-name": extensionReleaseName}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubHelmClient{}
			r, _ := helmSourceReconciler(t, tt.ext, stub)

			if _, err := tt.run(r, tt.ext); err != nil {
				t.Fatalf("reconcile error = %v", err)
			}

			spec, ok := stub.specFor(extensionReleaseName)
			if !ok {
				t.Fatalf("no release named after the extension was ensured; got %+v", stub.ensureSpecs)
			}
			if spec.ChartRef != "aif-ui" || spec.Version != requestedVersion {
				t.Errorf("chart = %s@%s, want aif-ui@%s", spec.ChartRef, spec.Version, requestedVersion)
			}
			if !reflect.DeepEqual(spec.Labels, wantLabels) {
				t.Errorf("release labels = %v, want %v", spec.Labels, wantLabels)
			}

			cond := meta.FindStatusCondition(tt.ext.Status.Conditions, conditionTypeUIPlugin)
			if cond == nil || cond.Status != metav1.ConditionTrue {
				t.Errorf("%s = %+v, want True", conditionTypeUIPlugin, cond)
			}
		})
	}
}

// The chart's own endpoint defaults name a Service that is not the extension
// server's, so the Helm path has to point the UIPlugin at the server it just
// resolved, and pull the chart from that same server.
func TestHelmSourcePointsTheExtensionChartAtItsServer(t *testing.T) {
	ext := helmExtension()
	stub := &stubHelmClient{}
	r, svcURL := helmSourceReconciler(t, ext, stub)

	if _, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace); err != nil {
		t.Fatalf("reconcile error = %v", err)
	}

	spec, ok := stub.specFor(extensionReleaseName)
	if !ok {
		t.Fatalf("extension chart not ensured; got %+v", stub.ensureSpecs)
	}
	if spec.RepoURL != svcURL {
		t.Errorf("RepoURL = %q, want the server's %q", spec.RepoURL, svcURL)
	}
	want := map[string]interface{}{
		"plugin": map[string]interface{}{
			"endpoint":           svcURL + "/plugin/aif-ui-" + requestedVersion,
			"compressedEndpoint": svcURL + "/plugin/aif-ui-" + requestedVersion + ".tgz",
		},
	}
	if !reflect.DeepEqual(spec.Values, want) {
		t.Errorf("values = %v, want %v", spec.Values, want)
	}

	// The server release is ensured first and untouched by any of this.
	if server, ok := stub.specFor(releaseName); !ok || server.Labels != nil {
		t.Errorf("server release spec = %+v (found %v), want it ensured without the ClusterRepo label", server, ok)
	}
}

func markExtensionPendingSince(ext *v1alpha1.InstallAIExtension, age time.Duration) string {
	if ext.Annotations == nil {
		ext.Annotations = make(map[string]string)
	}
	stamp := time.Now().Add(-age).Format(time.RFC3339)
	ext.Annotations[annotationExtensionReleasePendingSince] = stamp
	return stamp
}

// The Helm path ensures two releases in one pass. A wedged extension release
// has to keep its own clock, which the server release settling on every pass
// must not reset, or the wait never times out.
func TestHelmSourceTimesTheExtensionReleaseOnItsOwnMarker(t *testing.T) {
	pending := func() *stubHelmClient {
		return &stubHelmClient{ensureErrFor: map[string]error{extensionReleaseName: pendingErr("pending-install")}}
	}

	t.Run("waiting", func(t *testing.T) {
		ext := helmExtension()
		stamp := markExtensionPendingSince(ext, time.Minute)
		r, _ := helmSourceReconciler(t, ext, pending())

		result, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace)
		if err != nil {
			t.Fatalf("reconcile error = %v", err)
		}
		if result.RequeueAfter != pendingReleaseRequeue {
			t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, pendingReleaseRequeue)
		}
		if got := ext.Annotations[annotationExtensionReleasePendingSince]; got != stamp {
			t.Errorf("extension marker = %q, want the original %q; the server settling restarted the clock",
				got, stamp)
		}
		cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeUIPlugin)
		if cond == nil || cond.Reason != reasonReleasePending {
			t.Errorf("%s = %+v, want %s", conditionTypeUIPlugin, cond, reasonReleasePending)
		}
	})

	t.Run("timed out", func(t *testing.T) {
		ext := helmExtension()
		markExtensionPendingSince(ext, pendingReleaseTimeout+time.Minute)
		r, _ := helmSourceReconciler(t, ext, pending())

		if _, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace); err != nil {
			t.Fatalf("reconcile error = %v", err)
		}
		cond := meta.FindStatusCondition(ext.Status.Conditions, conditionTypeUIPlugin)
		if cond == nil || cond.Reason != reasonReleasePendingTimedOut {
			t.Errorf("%s = %+v, want %s", conditionTypeUIPlugin, cond, reasonReleasePendingTimedOut)
		}
	})

	t.Run("settled", func(t *testing.T) {
		ext := helmExtension()
		markExtensionPendingSince(ext, time.Minute)
		r, _ := helmSourceReconciler(t, ext, &stubHelmClient{})

		if _, err := r.reconcileHelmSource(context.Background(), ext, wiringNamespace); err != nil {
			t.Fatalf("reconcile error = %v", err)
		}
		if _, ok := ext.Annotations[annotationExtensionReleasePendingSince]; ok {
			t.Error("extension marker survived a settled release")
		}
	})
}

// Deleting the CR has to uninstall the extension chart for the Helm source as
// well, now that it installs one, along with the server release.
func TestCleanupUninstallsTheExtensionChartForEitherSourceKind(t *testing.T) {
	tests := []struct {
		name string
		ext  *v1alpha1.InstallAIExtension
		want []string
	}{
		{name: "helm", ext: helmExtension(), want: []string{extensionReleaseName, releaseName}},
		{name: "git", ext: gitExtension(), want: []string{extensionReleaseName}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubHelmClient{}
			if tt.ext.Spec.Source.Kind == v1alpha1.ExtensionSourceKindHelm {
				tt.ext.Status.HelmReleaseName = releaseName
			}
			r := wiringReconciler(t, tt.ext, stub)
			r.rancherMgr = &stubRancherManager{}

			if err := r.cleanup(context.Background(), tt.ext); err != nil {
				t.Fatalf("cleanup() error = %v", err)
			}
			for _, name := range tt.want {
				if !slices.Contains(stub.deleted, name) {
					t.Errorf("release %q not uninstalled; uninstalled %v", name, stub.deleted)
				}
			}
		})
	}
}
