package aiworkload

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

// bundleRenderCurrent reports whether a Bundle was generated from our current render
// (digest label matches) AND Fleet has processed that Bundle spec (observedGeneration current).
func bundleRenderCurrent(b *unstructured.Unstructured, expectedDigest string) bool {
	if b == nil {
		return false
	}
	if b.GetLabels()[renderDigestLabel] != renderDigestLabelValue(expectedDigest) {
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

// bundleDeploymentsApplied reports whether exactly expectedClusters live
// BundleDeployments have promoted (staged == deploymentID) and applied their
// current deployment and report Ready, or Modified when allowModified. The
// Bundle summary alone is not enough: Fleet's first status write after a spec
// change carries the new observedGeneration with a summary of the old deployments.
func bundleDeploymentsApplied(bds []unstructured.Unstructured, expectedClusters int, allowModified bool) bool {
	n := 0
	for i := range bds {
		if bds[i].GetDeletionTimestamp() != nil {
			continue
		}
		staged, _, _ := unstructured.NestedString(bds[i].Object, "spec", "stagedDeploymentID")
		switch appliedDeploymentState(&bds[i]) {
		case "Ready":
		case "Modified":
			if !allowModified {
				return false
			}
		default:
			return false
		}
		want, _, _ := unstructured.NestedString(bds[i].Object, "spec", "deploymentID")
		if staged != want {
			return false
		}
		n++
	}
	return n == expectedClusters
}

// appliedDeploymentState returns the BundleDeployment's display state when it
// has applied its current deploymentID, else "".
func appliedDeploymentState(bd *unstructured.Unstructured) string {
	desired, _, _ := unstructured.NestedString(bd.Object, "spec", "deploymentID")
	applied, _, _ := unstructured.NestedString(bd.Object, "status", "appliedDeploymentID")
	if applied == "" || applied != desired {
		return ""
	}
	state, _, _ := unstructured.NestedString(bd.Object, "status", "display", "state")
	return state
}

// bundleRolledOut reports whether k's Bundle is certified at digest (settled
// with Modified counted when allowModified) and its BundleDeployments confirm
// it. They are listed only once the Bundle passes; unstructured reads bypass
// the manager cache.
func (r *AIWorkloadReconciler) bundleRolledOut(ctx context.Context, k HelmOpKey, digest string, allowModified bool) (bool, error) {
	b, err := r.getBundle(ctx, k.Namespace, k.Name)
	if err != nil {
		return false, err
	}
	ok := bundleCertified(b, digest, k.ExpectedClusters)
	if allowModified {
		ok = dependencySettled(b, digest, k.ExpectedClusters)
	}
	if !ok {
		return false, nil
	}
	bds, err := r.listBundleDeployments(ctx, k.Namespace, k.Name)
	if err != nil {
		return false, err
	}
	return bundleDeploymentsApplied(bds, k.ExpectedClusters, allowModified), nil
}

// matrixCellPhase computes one (component, cluster) cell, render-gated: only a render-current
// parent Bundle whose BundleDeployment applied the current deploymentID yields a terminal
// Running/Failed; otherwise Pending (prevents stale ErrApplied from failing a new render).
func matrixCellPhase(bd *unstructured.Unstructured, parentRenderCurrent bool) aiplatformv1alpha1.AIWorkloadClusterPhase {
	if bd == nil || !parentRenderCurrent {
		return aiplatformv1alpha1.AIWorkloadClusterPhasePending
	}
	switch appliedDeploymentState(bd) {
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
// unchanged {version, aggregateDigest} leaves CertifiedAt untouched, unless an in-flight
// Upgrade/Rollback awaits certification since its request. Empty keys never certify.
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
		entries = append(entries, AggregateEntry{Namespace: k.Namespace, Name: k.Name, Digest: expectedDigests[k.Namespace+"/"+k.Name]})
	}
	agg := aggregateRenderDigest(entries)
	version := ""
	if w.Spec.Source.Blueprint != nil {
		version = w.Spec.Source.Blueprint.Version
	}
	if w.Status.DeployedSource != nil &&
		w.Status.DeployedSource.Version == version &&
		w.Status.DeployedSource.RenderDigest == agg &&
		!versionOpAwaitingCertification(w) {
		return nil // unchanged — no churn
	}
	for i, k := range keys {
		ok, err := r.bundleRolledOut(ctx, k, entries[i].Digest, false)
		if err != nil || !ok {
			return err // not (yet) certified; leave deployedSource untouched
		}
	}
	w.Status.DeployedSource = &aiplatformv1alpha1.DeployedSourceSnapshot{
		Version:      version,
		RenderDigest: agg,
		CertifiedAt:  metav1.Now(),
	}
	return nil
}

// acceptedConditionFingerprint returns a sha256 hash of the Accepted condition's
// {status,reason,message,lastUpdateTime} fields. Returns empty string when the
// condition is absent or the HelmOp is nil.
func acceptedConditionFingerprint(ho *unstructured.Unstructured) string {
	if ho == nil {
		return ""
	}
	conds, _, _ := unstructured.NestedSlice(ho.Object, "status", "conditions")
	for _, c := range conds {
		m, ok := c.(map[string]any)
		if !ok || m["type"] != "Accepted" {
			continue
		}
		b, _ := json.Marshal([]any{m["status"], m["reason"], m["message"], m["lastUpdateTime"]})
		return sha256Hex(b)
	}
	return ""
}

// acceptedConditionMessage is the message of the HelmOp's Accepted condition,
// prefixed with its reason when both are set.
func acceptedConditionMessage(ho *unstructured.Unstructured) string {
	conds, _, _ := unstructured.NestedSlice(ho.Object, "status", "conditions")
	for _, c := range conds {
		if m, ok := c.(map[string]any); ok && m["type"] == "Accepted" {
			reason, _ := m["reason"].(string)
			msg, _ := m["message"].(string)
			if reason != "" && msg != "" {
				return reason + ": " + msg
			}
			return reason + msg
		}
	}
	return ""
}

// acceptedConditionIsFalse reports whether the HelmOp has an Accepted condition
// with status="False".
func acceptedConditionIsFalse(ho *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(ho.Object, "status", "conditions")
	for _, c := range conds {
		if m, ok := c.(map[string]any); ok && m["type"] == "Accepted" {
			return m["status"] == "False"
		}
	}
	return false
}

// acceptedFalseTerminal reports whether a HelmOp Accepted=False should terminally fail the
// CURRENT render attempt: baseline identity (UID,digest,epoch,generation) must match AND the
// live Accepted fingerprint must differ from the baseline (a genuine post-baseline change).
func acceptedFalseTerminal(ho *unstructured.Unstructured, baseline *aiplatformv1alpha1.RenderBaseline, curDigest string, curEpoch, curGeneration int64) bool {
	if ho == nil || baseline == nil {
		return false
	}
	if baseline.HelmOpUID != string(ho.GetUID()) ||
		baseline.RenderDigest != curDigest ||
		baseline.RetryEpoch != curEpoch ||
		baseline.HelmOpGeneration != curGeneration {
		return false
	}
	if acceptedConditionFingerprint(ho) == baseline.AcceptedFingerprint {
		return false
	}
	return acceptedConditionIsFalse(ho)
}

// upsertRenderBaseline inserts a RenderBaseline for a HelmOp UID, or replaces the existing entry
// ONLY when the render attempt identity (digest, retry epoch, or HelmOp generation) has changed.
// When the identity is unchanged, the existing entry — including its AcceptedFingerprint captured
// at first observation of this attempt — is preserved untouched. This is essential: re-recording
// the current fingerprint every reconcile would make acceptedFalseTerminal's "fingerprint differs
// from baseline" check permanently false, so a Fleet-driven Accepted=False rejection would never
// terminally fail the attempt and the cell would stay Pending forever.
func upsertRenderBaseline(baselines []aiplatformv1alpha1.RenderBaseline, entry aiplatformv1alpha1.RenderBaseline) []aiplatformv1alpha1.RenderBaseline {
	for i := range baselines {
		if baselines[i].HelmOpUID == entry.HelmOpUID {
			if baselines[i].RenderDigest != entry.RenderDigest ||
				baselines[i].RetryEpoch != entry.RetryEpoch ||
				baselines[i].HelmOpGeneration != entry.HelmOpGeneration {
				baselines[i] = entry
			}
			return baselines
		}
	}
	return append(baselines, entry)
}

// certifiedSince reports whether ds was certified at or after t.
func certifiedSince(ds *aiplatformv1alpha1.DeployedSourceSnapshot, t metav1.Time) bool {
	return ds != nil && !ds.CertifiedAt.Before(&t)
}

// versionOpAwaitingCertification reports whether an in-flight Upgrade/Rollback has not been
// certified since it was requested.
func versionOpAwaitingCertification(w *aiplatformv1alpha1.AIWorkload) bool {
	op, ok := readJournal(w)
	if !ok || op.State != aiplatformv1alpha1.OperationStateInProgress {
		return false
	}
	if op.Type != aiplatformv1alpha1.OperationTypeUpgrade && op.Type != aiplatformv1alpha1.OperationTypeRollback {
		return false
	}
	return !certifiedSince(w.Status.DeployedSource, op.RequestedAt)
}
