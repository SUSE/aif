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
	stderrors "errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/rancher"
)

// gitBlueprintCR is newBlueprintCR's git-backed twin: no vendor, so the
// component reaches the catalog fetch without a secret injection detour.
func gitBlueprintCR(repo string) *aiplatformv1alpha1.Blueprint {
	bp := &aiplatformv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{Name: bpCRName("mini", "1.0.0")},
	}
	bp.Spec.Components = []aiplatformv1alpha1.BlueprintComponent{
		{ChartRepo: repo, ChartName: "app", ChartVersion: "1.0.0"},
	}
	return bp
}

func gitClusterRepo(name string) *unstructured.Unstructured {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(clusterRepoGVK)
	repo.SetName(name)
	_ = unstructured.SetNestedField(repo.Object, "https://git.rancher.io/charts", "spec", "gitRepo")
	_ = unstructured.SetNestedField(repo.Object, "release-v2.14", "spec", "gitBranch")
	_ = unstructured.SetNestedField(repo.Object, "commit-aaa", "status", "commit")
	return repo
}

func sentinelResultScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := clusterRepoErrorScheme(t)
	scheme.AddKnownTypeWithName(bundleGVK, &unstructured.Unstructured{})
	return scheme
}

// Each branch in reconcileBlueprintStatus's component-error ladder encodes a
// deliberate requeue-vs-terminal decision, and the sentinel-wrapping tests in
// gitchart_test.go cannot see any of it. In particular errChartTooLarge's empty
// Result is the whole point of that branch: returning the error instead spins
// the workqueue forever, re-downloading a multi-megabyte archive from Rancher on
// every backoff tick while the workload advertises its last good message. That
// was a real defect once. So assert the full outcome — reason, phase, requeue
// delay and whether the error escapes — for every branch at once.
func TestReconcileBlueprintStatus_ComponentErrorOutcomes(t *testing.T) {
	oversized := make([]byte, maxChartArchiveBytes+1)

	cases := []struct {
		name string
		// catalog is installed in a holder unless nil, which leaves
		// CatalogClient unset and triggers errCatalogClientNotConfigured.
		catalog     rancher.ChartFetcher
		wantReason  string
		wantRequeue time.Duration
		wantErr     bool
		wantPhase   aiplatformv1alpha1.AIWorkloadPhase
		requeueWhy  string
	}{
		{
			name:        "no catalog client requeues so a token set later is picked up",
			catalog:     nil,
			wantReason:  "CatalogClientNotConfigured",
			wantRequeue: time.Minute,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhaseFailed,
			requeueWhy:  "the catalog config is editable at runtime, so this must retry",
		},
		{
			name:        "a rejected token requeues so re-authorizing recovers",
			catalog:     fakeCatalog{err: rancher.ErrUnauthorized},
			wantReason:  "RancherTokenRejected",
			wantRequeue: time.Minute,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhaseFailed,
			requeueWhy:  "every token eventually expires and re-authorizing must recover",
		},
		{
			name:        "an oversized chart is terminal",
			catalog:     fakeCatalog{tgz: oversized},
			wantReason:  "ChartTooLarge",
			wantRequeue: 0,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhaseFailed,
			requeueWhy:  "retrying cannot shrink the chart; a requeue would re-download it forever",
		},
		{
			name:        "an unrecognized failure escapes to the workqueue",
			catalog:     fakeCatalog{err: stderrors.New("x509: certificate signed by unknown authority")},
			wantReason:  "ComponentReconcileFailed",
			wantRequeue: 0,
			wantErr:     true,
			wantPhase:   aiplatformv1alpha1.AIWorkloadPhaseFailed,
			requeueWhy:  "the workqueue's own backoff drives the retry, so Result must stay empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := sentinelResultScheme(t)
			// Past the grace window, so guardPhaseTransition lets Failed through.
			w := newBlueprintWorkload(time.Now().Add(-10 * time.Minute))
			bp := gitBlueprintCR("rancher-charts")
			cl := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(w, bp, gitClusterRepo("rancher-charts")).Build()
			r := &AIWorkloadReconciler{Client: cl, Scheme: scheme, OperatorNamespace: "aif-operator"}
			if tc.catalog != nil {
				holder := rancher.NewHolder()
				holder.Set(tc.catalog)
				r.CatalogClient = holder
			}

			result, err := r.reconcileBlueprintStatus(context.Background(), w)

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if result.RequeueAfter != tc.wantRequeue {
				t.Errorf("RequeueAfter = %v, want %v — %s", result.RequeueAfter, tc.wantRequeue, tc.requeueWhy)
			}
			cond := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
			if cond == nil {
				t.Fatalf("expected a %q condition", conditionTypeReady)
			}
			if cond.Status != metav1.ConditionFalse {
				t.Errorf("condition status = %v, want False", cond.Status)
			}
			if cond.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", cond.Reason, tc.wantReason)
			}
			if w.Status.Phase != tc.wantPhase {
				t.Errorf("phase = %v, want %v", w.Status.Phase, tc.wantPhase)
			}
		})
	}
}
