package aiworkload

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// dependencyHoldRequeue is how soon a workload with held components looks again.
const dependencyHoldRequeue = 15 * time.Second

// pendingDependencies returns the enabled dependencies of c whose Bundles and
// BundleDeployments have not settled (bundleRolledOut) at the digest rendered
// for them in this reconcile. Fleet's own dependsOn reads a dependency's Bundle status, which
// lags a spec change, so it does not order updates; holding c here does. expectedDigests is keyed
// like certifyDeployedSource's ("namespace/name"); a dependency missing from
// it (held or switching itself) counts as pending.
func (r *AIWorkloadReconciler) pendingDependencies(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent, expectedDigests map[string]string) ([]string, error) {
	var pending []string
	for _, dep := range c.DependsOn {
		if !isComponentEnabled(w, dep) {
			continue
		}
		keys := desiredHelmOpKeys(w.Name, w.Spec.TargetClusters, []aiplatformv1alpha1.BlueprintComponent{{ChartName: dep}}, w.Spec.DeployStrategy)
		for _, k := range keys {
			digest := expectedDigests[k.Namespace+"/"+k.Name]
			if digest == "" {
				pending = append(pending, dep)
				break
			}
			settled, err := r.bundleRolledOut(ctx, k, digest, true)
			if err != nil {
				return nil, err
			}
			if !settled {
				pending = append(pending, dep)
				break
			}
		}
	}
	return pending, nil
}

// dependencySettled is bundleCertified with Modified clusters counted as rolled
// out: charts that mutate their own resources (e.g. gpu-operator) stay Modified
// for good and would otherwise hold their dependents forever.
func dependencySettled(b *unstructured.Unstructured, expectedDigest string, expectedClusters int) bool {
	if expectedClusters <= 0 || !bundleRenderCurrent(b, expectedDigest) {
		return false
	}
	ready, _, _ := unstructured.NestedInt64(b.Object, "status", "summary", "ready")
	modified, _, _ := unstructured.NestedInt64(b.Object, "status", "summary", "modified")
	desired, _, _ := unstructured.NestedInt64(b.Object, "status", "summary", "desiredReady")
	return desired == int64(expectedClusters) && ready+modified == desired
}

// heldComponent is a component not rendered this pass because its pending
// dependencies have not rolled out.
type heldComponent struct {
	name    string
	pending []string
}

// heldConditionMessage describes the held components for the Ready condition.
// A dependency with a Failed cell is named with its error, since the hold then
// only ends once the failure is fixed.
func heldConditionMessage(held []heldComponent, cells []aiplatformv1alpha1.AIWorkloadComponentStatus) string {
	parts := make([]string, 0, len(held))
	for _, h := range held {
		var waiting, failed []string
		for _, dep := range h.pending {
			if c, n := failedCell(cells, dep); c != nil {
				f := fmt.Sprintf("%s, which failed on %s", dep, c.ClusterID)
				if n > 1 {
					f += fmt.Sprintf(" (and %d more)", n-1)
				}
				if c.Message != "" {
					f += ": " + c.Message
				}
				failed = append(failed, f)
			} else {
				waiting = append(waiting, dep)
			}
		}
		var reasons []string
		if len(failed) > 0 {
			reasons = append(reasons, "waiting for "+strings.Join(failed, ", and for "))
		}
		if len(waiting) > 0 {
			reasons = append(reasons, "waiting for "+strings.Join(waiting, ", ")+" to roll out")
		}
		parts = append(parts, h.name+": "+strings.Join(reasons, "; also "))
	}
	return strings.Join(parts, "; ")
}

// failedCell returns the first Failed cell of component (nil if none) and how
// many of its cells failed.
func failedCell(cells []aiplatformv1alpha1.AIWorkloadComponentStatus, component string) (*aiplatformv1alpha1.AIWorkloadComponentStatus, int) {
	var first *aiplatformv1alpha1.AIWorkloadComponentStatus
	n := 0
	for i := range cells {
		if cells[i].ComponentName == component && cells[i].Phase == aiplatformv1alpha1.AIWorkloadClusterPhaseFailed {
			if first == nil {
				first = &cells[i]
			}
			n++
		}
	}
	return first, n
}
