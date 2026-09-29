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
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

// helmReleaseNameAnnotation identifies the Helm release a resource belongs to.
// It is an annotation (not a label), so it cannot be used as a server-side list
// selector; callers filter on it in memory.
const helmReleaseNameAnnotation = "meta.helm.sh/release-name"

// podHardFailureReasons are the container waiting reasons that mean a pod is
// stuck rather than still starting. A workload whose pods report one of these is
// Degraded immediately — no grace period — because more time will not resolve a
// crash loop, an unpullable image, or a bad config. Reasons absent here (e.g.
// ContainerCreating, PodInitializing) are the normal in-progress states a slow
// image pull or model load passes through and must not trip Degraded, so a
// legitimately slow deploy stays Pending for as long as it needs.
//
// The set is the union of the kubelet's terminal-ish waiting reasons; the exact
// strings are the kubelet's own (k8s.io/kubernetes/pkg/kubelet). They are fixed
// enum values, so surfacing them in a status message leaks nothing about the
// user-supplied target namespace.
const (
	reasonCrashLoopBackOff = "CrashLoopBackOff"
	reasonImagePullBackOff = "ImagePullBackOff"
)

var podHardFailureReasons = map[string]bool{
	reasonCrashLoopBackOff:       true,
	reasonImagePullBackOff:       true,
	"ErrImagePull":               true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
	"InvalidImageName":           true,
	"RunContainerError":          true,
}

// podScheduleFailure is the pod-condition reason the scheduler sets when a pod
// cannot be placed (no node fits its requests/affinity/taints). Like the waiting
// reasons above it is a stuck signal, not a transient one, and it is a fixed enum
// value safe to surface.
const podScheduleFailure = "Unschedulable"

// workloadReadiness is the readiness of a single Helm-managed workload controller
// (Deployment / StatefulSet / DaemonSet) in the target namespace, plus the pod
// selector used to locate its pods when it is not ready.
type workloadReadiness struct {
	kind     string
	name     string
	ready    bool
	selector labels.Selector
}

// helmPhaseFromReadiness derives the workload phase from how many controllers are
// not ready and the pod failure reasons observed among them. The caller must only
// invoke this once the Helm release is known to exist.
//
// The classification is pod-state driven rather than time driven. All controllers
// ready (or none found — a chart with no long-running workloads) is Running. A
// not-ready controller whose pods report a hard failure (crash loop, unpullable
// image, unschedulable, …) is Degraded straight away, since waiting will not fix
// it. A not-ready controller with no such reason is merely still starting — an
// image pull or model load in flight — and stays Pending for as long as it takes,
// so a genuinely slow deploy is never mislabeled Degraded on a timer.
func helmPhaseFromReadiness(notReadyCount int, failureReasons []string) (aiplatformv1alpha1.AIWorkloadPhase, string, string) {
	if notReadyCount == 0 {
		return aiplatformv1alpha1.AIWorkloadPhaseRunning, "WorkloadsReady", "All workload pods are ready"
	}
	if len(failureReasons) > 0 {
		msg := fmt.Sprintf("%d workload(s) not ready; pod status: %s",
			notReadyCount, strings.Join(failureReasons, ", "))
		return aiplatformv1alpha1.AIWorkloadPhaseDegraded, "WorkloadsDegraded", msg
	}
	msg := fmt.Sprintf("Waiting for %d workload(s) to become ready", notReadyCount)
	return aiplatformv1alpha1.AIWorkloadPhasePending, "WorkloadsStarting", msg
}

// classifyHelmPhase turns the release's controllers into a phase, a condition
// reason and a user-facing message. For each not-ready controller it inspects the
// controller's own pods (located via its selector) for a hard-failure reason, so
// the distinction between "still pulling" (Pending) and "stuck" (Degraded) is
// made from actual pod state rather than elapsed time.
//
// The message names only pod failure reasons (fixed kubelet enum values) and
// counts — never resource names — because spec.targetNamespace is user-supplied
// and the message is surfaced on the CR's status, which a different tenant may be
// able to read.
func (r *AIWorkloadReconciler) classifyHelmPhase(ctx context.Context, namespace string, controllers []workloadReadiness) (aiplatformv1alpha1.AIWorkloadPhase, string, string, error) {
	notReady := 0
	seen := map[string]bool{}
	var reasons []string
	for _, c := range controllers {
		if c.ready {
			continue
		}
		notReady++
		podReasons, err := r.podFailureReasons(ctx, namespace, c.selector)
		if err != nil {
			return "", "", "", err
		}
		for _, pr := range podReasons {
			if !seen[pr] {
				seen[pr] = true
				reasons = append(reasons, pr)
			}
		}
	}
	sort.Strings(reasons) // deterministic message so the status does not churn
	phase, reason, msg := helmPhaseFromReadiness(notReady, reasons)
	return phase, reason, msg, nil
}

// podFailureReasons lists the pods matching selector and returns the distinct
// hard-failure reasons among them, or nil when every pod is merely starting.
func (r *AIWorkloadReconciler) podFailureReasons(ctx context.Context, namespace string, selector labels.Selector) ([]string, error) {
	if selector == nil {
		return nil, nil
	}
	var pods corev1.PodList
	if err := r.controllerReader().List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for i := range pods.Items {
		if reason := podHardFailureReason(&pods.Items[i]); reason != "" && !seen[reason] {
			seen[reason] = true
			out = append(out, reason)
		}
	}
	return out, nil
}

// podHardFailureReason returns the first hard-failure reason a pod reports — a
// terminal-ish container waiting reason (init or main) or an unschedulable
// condition — or "" when the pod is only still starting.
func podHardFailureReason(p *corev1.Pod) string {
	for _, cs := range p.Status.InitContainerStatuses {
		if r := waitingHardFailure(cs.State.Waiting); r != "" {
			return r
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if r := waitingHardFailure(cs.State.Waiting); r != "" {
			return r
		}
	}
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodScheduled &&
			cond.Status == corev1.ConditionFalse &&
			cond.Reason == podScheduleFailure {
			return podScheduleFailure
		}
	}
	return ""
}

func waitingHardFailure(w *corev1.ContainerStateWaiting) string {
	if w != nil && podHardFailureReasons[w.Reason] {
		return w.Reason
	}
	return ""
}

// helmManagedControllers lists the Deployments, StatefulSets and DaemonSets that
// the given Helm release owns in namespace, reporting each one's readiness and
// pod selector. The resources are matched by Helm's managed-by label and
// release-name annotation so unrelated workloads sharing the namespace (and
// completed Job pods) are ignored.
//
// Reads go through the uncached APIReader: caching these types would spin up
// cluster-wide informers for every StatefulSet and DaemonSet in the cluster (the
// manager's cache only scopes Secrets and ConfigMaps) purely to poll a handful of
// release-owned ones, and a direct namespaced list needs only the `list` verb, so
// no `watch` RBAC is required. Readiness is derived from the shared rollout-status
// helpers so a broken upgrade is not masked by the previous revision's pods still
// counting as ready.
func (r *AIWorkloadReconciler) helmManagedControllers(ctx context.Context, namespace, release string) ([]workloadReadiness, error) {
	reader := r.controllerReader()
	sel := client.MatchingLabels{chartManagedByLabel: chartManagedByHelm}

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
		out = append(out, workloadReadiness{
			kind: "Deployment", name: d.Name,
			ready:    kubernetes.RolloutIncomplete(d) == "",
			selector: selectorOrNil(d.Spec.Selector),
		})
	}
	for i := range sts.Items {
		s := &sts.Items[i]
		if s.Annotations[helmReleaseNameAnnotation] != release {
			continue
		}
		out = append(out, workloadReadiness{
			kind: "StatefulSet", name: s.Name,
			ready:    kubernetes.StatefulSetRolloutIncomplete(s) == "",
			selector: selectorOrNil(s.Spec.Selector),
		})
	}
	for i := range ds.Items {
		d := &ds.Items[i]
		if d.Annotations[helmReleaseNameAnnotation] != release {
			continue
		}
		out = append(out, workloadReadiness{
			kind: "DaemonSet", name: d.Name,
			ready:    kubernetes.DaemonSetRolloutIncomplete(d) == "",
			selector: selectorOrNil(d.Spec.Selector),
		})
	}

	return out, nil
}

// selectorOrNil converts a controller's label selector into a matcher, returning
// nil when it is absent or unparsable so the pod lookup is simply skipped (the
// controller still counts as not-ready, just without a pod-level reason).
func selectorOrNil(ls *metav1.LabelSelector) labels.Selector {
	if ls == nil {
		return nil
	}
	sel, err := metav1.LabelSelectorAsSelector(ls)
	if err != nil {
		return nil
	}
	return sel
}

// controllerReader returns the uncached client used to poll release-owned
// workload controllers and their pods.
//
// The fallback to Client exists for reconcilers built by hand in tests, which
// never go through SetupWithManager. It is safe there because a fake client is
// the API server — there is no second view to be stale relative to — and it is
// unreachable in a running operator, where SetupWithManager always wires
// APIReader to the manager's uncached reader.
func (r *AIWorkloadReconciler) controllerReader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}
