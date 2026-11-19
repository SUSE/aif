package aiworkload

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// dependencyHoldRequeue is how soon a workload with held components looks again.
const dependencyHoldRequeue = 15 * time.Second

// pendingDependencies returns the enabled dependencies of c whose Bundles have
// not settled (dependencySettled) at the digest rendered for them in this
// reconcile. Fleet's own dependsOn reads a dependency's Bundle status, which
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
			b, err := r.getBundle(ctx, k.Namespace, k.Name)
			if err != nil {
				return nil, err
			}
			if !dependencySettled(b, digest, k.ExpectedClusters) {
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
