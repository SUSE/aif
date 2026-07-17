package aiworkload

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// bundleRenderCurrent reports whether a Bundle was generated from our current render
// (digest label matches) AND Fleet has processed that Bundle spec (observedGeneration current).
func bundleRenderCurrent(b *unstructured.Unstructured, expectedDigest string) bool {
	if b == nil {
		return false
	}
	if b.GetLabels()[renderDigestLabel] != expectedDigest {
		return false
	}
	observed, _, _ := unstructured.NestedInt64(b.Object, "status", "observedGeneration")
	return observed == b.GetGeneration()
}

// bundleCertified reports whether the Bundle is render-current AND fully rolled out to exactly
// the expected number of clusters. expectedClusters==0 never certifies (vacuous-success guard).
func bundleCertified(b *unstructured.Unstructured, expectedDigest string, expectedClusters int) bool {
	if expectedClusters <= 0 || !bundleRenderCurrent(b, expectedDigest) {
		return false
	}
	unavailable, _, _ := unstructured.NestedInt64(b.Object, "status", "unavailable")
	if unavailable != 0 {
		return false
	}
	ready, _, _ := unstructured.NestedInt64(b.Object, "status", "summary", "ready")
	desired, _, _ := unstructured.NestedInt64(b.Object, "status", "summary", "desiredReady")
	return desired == int64(expectedClusters) && ready == desired
}

// matrixCellPhase computes one (component, cluster) cell, render-gated: only a render-current
// parent Bundle whose BundleDeployment applied the current deploymentID yields a terminal
// Running/Failed; otherwise Pending (prevents stale ErrApplied from failing a new render).
func matrixCellPhase(bd *unstructured.Unstructured, parentRenderCurrent bool) aiplatformv1alpha1.AIWorkloadClusterPhase {
	if bd == nil || !parentRenderCurrent {
		return aiplatformv1alpha1.AIWorkloadClusterPhasePending
	}
	desired, _, _ := unstructured.NestedString(bd.Object, "spec", "deploymentID")
	applied, _, _ := unstructured.NestedString(bd.Object, "status", "appliedDeploymentID")
	if applied == "" || applied != desired {
		return aiplatformv1alpha1.AIWorkloadClusterPhasePending
	}
	state, _, _ := unstructured.NestedString(bd.Object, "status", "display", "state")
	switch state {
	case "Ready", "Modified":
		return aiplatformv1alpha1.AIWorkloadClusterPhaseRunning
	case "ErrApplied":
		return aiplatformv1alpha1.AIWorkloadClusterPhaseFailed
	default:
		return aiplatformv1alpha1.AIWorkloadClusterPhasePending
	}
}

// getBundle fetches a Fleet Bundle, returning (nil, nil) when it does not exist.
func (r *AIWorkloadReconciler) getBundle(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(bundleGVK)
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, b); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return b, nil
}

// certifyDeployedSource sets w.Status.DeployedSource to the current spec version when EVERY
// desired HelmOp Bundle is certified against its expected per-HelmOp digest. Idempotent: an
// unchanged {version, aggregateDigest} leaves CertifiedAt untouched. Empty keys never certify.
func (r *AIWorkloadReconciler) certifyDeployedSource(
	ctx context.Context,
	w *aiplatformv1alpha1.AIWorkload,
	keys []HelmOpKey,
	expectedDigests map[string]string,
) error {
	if len(keys) == 0 {
		return nil
	}
	entries := make([]AggregateEntry, 0, len(keys))
	for _, k := range keys {
		digest := expectedDigests[k.Namespace+"/"+k.Name]
		b, err := r.getBundle(ctx, k.Namespace, k.Name)
		if err != nil {
			return err
		}
		if !bundleCertified(b, digest, k.ExpectedClusters) {
			return nil // not (yet) certified; leave deployedSource untouched
		}
		entries = append(entries, AggregateEntry{Namespace: k.Namespace, Name: k.Name, Digest: digest})
	}
	agg := aggregateRenderDigest(entries)
	version := ""
	if w.Spec.Source.Blueprint != nil {
		version = w.Spec.Source.Blueprint.Version
	}
	if w.Status.DeployedSource != nil &&
		w.Status.DeployedSource.Version == version &&
		w.Status.DeployedSource.RenderDigest == agg {
		return nil // unchanged — no churn
	}
	w.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{
		Version:      version,
		RenderDigest: agg,
		CertifiedAt:  metav1.Now(),
	}
	return nil
}
