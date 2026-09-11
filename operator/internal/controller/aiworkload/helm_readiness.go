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
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/infra/kubernetes"
)

// helmReadinessRequeue is how often a Helm/App workload with an existing release
// is re-reconciled so its phase tracks pod health over time. No watch fires on a
// pod's readiness flipping (the operator only watches Helm release secrets), so
// without this poll a workload that crashes after becoming Running would keep a
// stale badge until the informer's periodic resync.
const helmReadinessRequeue = 30 * time.Second

// helmManagedLabel is the label Helm stamps on every resource it manages.
const helmManagedLabel = "app.kubernetes.io/managed-by"

// helmReleaseNameAnnotation identifies the Helm release a resource belongs to.
// It is an annotation (not a label), so it cannot be used as a server-side list
// selector; callers filter on it in memory.
const helmReleaseNameAnnotation = "meta.helm.sh/release-name"

// workloadReadiness is the readiness of a single Helm-managed workload controller
// (Deployment / StatefulSet / DaemonSet) in the target namespace.
type workloadReadiness struct {
	kind  string
	name  string
	ready bool
}

// helmPhaseFromReadiness derives the workload phase from the readiness of the
// Helm-managed workload controllers. The caller must only invoke this once the
// Helm release is known to exist.
//
// All controllers ready (or none found — a chart with no long-running workloads)
// is Running. Otherwise the workload is still settling: within the initial
// deploy grace period it reports Pending (a normal slow start — image pulls,
// model loads), and past it Degraded, so a genuinely stuck workload (e.g. a NIM
// pod that cannot fit in GPU memory) is surfaced rather than shown as Running.
func helmPhaseFromReadiness(controllers []workloadReadiness, createdAt time.Time) (aiplatformv1alpha1.AIWorkloadPhase, string) {
	var notReady []string
	for _, c := range controllers {
		if !c.ready {
			notReady = append(notReady, c.kind+"/"+c.name)
		}
	}
	if len(notReady) == 0 {
		return aiplatformv1alpha1.AIWorkloadPhaseRunning, ""
	}
	msg := fmt.Sprintf("Waiting for %d workload(s) to become ready: %s", len(notReady), strings.Join(notReady, ", "))
	if time.Since(createdAt) < initialDeployGracePeriod {
		return aiplatformv1alpha1.AIWorkloadPhasePending, msg
	}
	return aiplatformv1alpha1.AIWorkloadPhaseDegraded, msg
}

// helmManagedControllers lists the Deployments, StatefulSets and DaemonSets that
// the given Helm release owns in namespace, reporting each one's readiness. The
// resources are matched by Helm's managed-by label and release-name annotation so
// unrelated workloads sharing the namespace (and completed Job pods) are ignored.
//
// Reads go through the uncached APIReader: caching these types would spin up
// cluster-wide informers for every StatefulSet and DaemonSet in the cluster (the
// manager's cache only scopes Secrets and ConfigMaps) purely to poll a handful of
// release-owned ones, and a direct namespaced list needs only the `list` verb, so
// no `watch` RBAC is required. Readiness is derived from the shared rollout-status
// helpers so a broken upgrade is not masked by the previous revision's pods still
// counting as ready.
func (r *AIWorkloadReconciler) helmManagedControllers(ctx context.Context, namespace, release string) ([]workloadReadiness, error) {
	reader := r.reader()
	sel := client.MatchingLabels{helmManagedLabel: "Helm"}

	var deps appsv1.DeploymentList
	if err := reader.List(ctx, &deps, client.InNamespace(namespace), sel); err != nil {
		return nil, err
	}
	var sts appsv1.StatefulSetList
	if err := reader.List(ctx, &sts, client.InNamespace(namespace), sel); err != nil {
		return nil, err
	}
	var ds appsv1.DaemonSetList
	if err := reader.List(ctx, &ds, client.InNamespace(namespace), sel); err != nil {
		return nil, err
	}

	out := make([]workloadReadiness, 0, len(deps.Items)+len(sts.Items)+len(ds.Items))
	for i := range deps.Items {
		d := &deps.Items[i]
		if d.Annotations[helmReleaseNameAnnotation] != release {
			continue
		}
		out = append(out, workloadReadiness{kind: "Deployment", name: d.Name, ready: kubernetes.RolloutIncomplete(d) == ""})
	}
	for i := range sts.Items {
		s := &sts.Items[i]
		if s.Annotations[helmReleaseNameAnnotation] != release {
			continue
		}
		out = append(out, workloadReadiness{kind: "StatefulSet", name: s.Name, ready: kubernetes.StatefulSetRolloutIncomplete(s) == ""})
	}
	for i := range ds.Items {
		d := &ds.Items[i]
		if d.Annotations[helmReleaseNameAnnotation] != release {
			continue
		}
		out = append(out, workloadReadiness{kind: "DaemonSet", name: d.Name, ready: kubernetes.DaemonSetRolloutIncomplete(d) == ""})
	}

	return out, nil
}

// reader returns the uncached client used to poll release-owned workload
// controllers, falling back to the cached client when no APIReader is wired (unit
// tests build the reconciler with a single fake client).
func (r *AIWorkloadReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}
