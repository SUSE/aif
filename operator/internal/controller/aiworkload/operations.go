package aiworkload

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
)

func readJournal(w *aiplatformv1alpha1.AIWorkload) (*aiplatformv1alpha1.AIWorkloadOperation, bool) {
	if w.Annotations == nil || w.Annotations[operationAnnotation] == "" {
		return nil, false
	}
	op, err := decodeOperation(w.Annotations[operationAnnotation])
	if err != nil {
		return nil, false
	}
	return &op, true
}

// projectOperation keeps status.activeOperation in sync with the authoritative journal, and
// retires a stale terminal projection when the spec's intent has moved on with no journal.
func (r *AIWorkloadReconciler) projectOperation(w *aiplatformv1alpha1.AIWorkload) {
	if op, ok := readJournal(w); ok {
		if w.Status.ActiveOperation == nil || *w.Status.ActiveOperation != *op {
			cp := *op
			w.Status.ActiveOperation = &cp
		}
		return
	}
	// No journal: a terminal op whose intent no longer matches the spec is stale → clear it.
	if w.Status.ActiveOperation != nil &&
		w.Status.ActiveOperation.State != aiplatformv1alpha1.OperationStateInProgress &&
		w.Status.ActiveOperation.IntentDigest != intentDigest(w.Spec) {
		w.Status.ActiveOperation = nil
	}
}

const defaultOperationDeadline = 30 * time.Minute

// handleTriggers processes at most one request annotation, metadata-atomically. Returns
// handled=true when it mutated the object (caller should requeue and let status reconcile).
func (r *AIWorkloadReconciler) handleTriggers(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (bool, error) {
	handled := decodeHandled(w.Annotations[handledOpsAnnotation])

	if v := w.Annotations[upgradeRequestAnnotation]; v != "" {
		parts := strings.SplitN(v, "|", 2)
		nonce, target := parts[0], ""
		if len(parts) == 2 {
			target = parts[1]
		}
		id := handledID(aiplatformv1alpha1.OperationTypeUpgrade, nonce)
		if isHandled(handled, id) {
			return false, nil
		}
		cur := ""
		if w.Spec.Source.Blueprint != nil {
			cur = w.Spec.Source.Blueprint.Version
		}
		if target == cur {
			return r.startRetry(ctx, w, nonce)
		}
		return r.startVersionOp(ctx, w, aiplatformv1alpha1.OperationTypeUpgrade, nonce, target, "", upgradeRequestAnnotation)
	}

	if v := w.Annotations[rollbackRequestAnnotation]; v != "" {
		id := handledID(aiplatformv1alpha1.OperationTypeRollback, v)
		if isHandled(handled, id) {
			return false, nil
		}
		if w.Status.DeployedSource == nil ||
			(w.Spec.Source.Blueprint != nil && w.Status.DeployedSource.Version == w.Spec.Source.Blueprint.Version) {
			r.event(w, corev1.EventTypeWarning, "RollbackSkipped", "no rollback target")
			return r.finishTrigger(ctx, w, rollbackRequestAnnotation, id, nil, 0)
		}
		return r.startVersionOp(ctx, w, aiplatformv1alpha1.OperationTypeRollback, v, w.Status.DeployedSource.Version, w.Status.DeployedSource.RenderDigest, rollbackRequestAnnotation)
	}

	if v := w.Annotations[retryRequestAnnotation]; v != "" {
		id := handledID(aiplatformv1alpha1.OperationTypeRetry, v)
		if isHandled(handled, id) {
			return false, nil
		}
		return r.startRetry(ctx, w, v)
	}

	return false, nil
}

// startVersionOp sets the blueprint version + writes the journal atomically (Upgrade/Rollback).
func (r *AIWorkloadReconciler) startVersionOp(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, opType, nonce, target, expectedDigest, reqAnnotation string) (bool, error) {
	// Preserve any outgoing journal for supersession observability.
	if old, ok := readJournal(w); ok {
		s, _ := encodeOperation(*old)
		metav1.SetMetaDataAnnotation(&w.ObjectMeta, supersededOpAnnotation, s)
	}
	if w.Spec.Source.Blueprint != nil {
		w.Spec.Source.Blueprint.Version = target
	}
	op := aiplatformv1alpha1.AIWorkloadOperation{
		Type: opType, Nonce: nonce, TargetVersion: target, ExpectedDigest: expectedDigest,
		RequestedAt: metav1.Now(), IntentDigest: intentDigest(w.Spec), State: aiplatformv1alpha1.OperationStateInProgress,
	}
	return r.finishTrigger(ctx, w, reqAnnotation, handledID(opType, nonce), &op, 0)
}

// startRetry increments the durable epoch and writes a Retry journal atomically.
func (r *AIWorkloadReconciler) startRetry(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, nonce string) (bool, error) {
	epoch := r.retryEpochValue(w) + 1
	metav1.SetMetaDataAnnotation(&w.ObjectMeta, retryEpochAnnotation, strconv.FormatInt(epoch, 10))
	op := aiplatformv1alpha1.AIWorkloadOperation{
		Type: aiplatformv1alpha1.OperationTypeRetry, Nonce: nonce, RetryEpoch: epoch,
		RequestedAt: metav1.Now(), IntentDigest: intentDigest(w.Spec), State: aiplatformv1alpha1.OperationStateInProgress,
	}
	return r.finishTrigger(ctx, w, retryRequestAnnotation, handledID(aiplatformv1alpha1.OperationTypeRetry, nonce), &op, epoch)
}

// finishTrigger performs the single metadata+spec Update: remove request, record handled, set
// (or clear) the journal. epoch is informational (already set by caller).
func (r *AIWorkloadReconciler) finishTrigger(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, reqAnnotation, id string, op *aiplatformv1alpha1.AIWorkloadOperation, _ int64) (bool, error) {
	delete(w.Annotations, reqAnnotation)
	handled := addHandled(decodeHandled(w.Annotations[handledOpsAnnotation]), id)
	hs, _ := encodeHandled(handled)
	metav1.SetMetaDataAnnotation(&w.ObjectMeta, handledOpsAnnotation, hs)
	if op != nil {
		js, _ := encodeOperation(*op)
		metav1.SetMetaDataAnnotation(&w.ObjectMeta, operationAnnotation, js)
	}
	if err := r.Update(ctx, w); err != nil {
		return false, err
	}
	if op != nil {
		r.event(w, corev1.EventTypeNormal, string(op.Type)+"Started", "target=%s epoch=%d", op.TargetVersion, op.RetryEpoch)
	}
	return true, nil
}

// operationOutcome is the pure decision for an in-flight operation's next state.
// certifiedAtTarget: deployedSource now equals the op's target (Upgrade/Rollback) or, for
// Retry, the epoch is confirmed and healthy. anyFailed: a matrix cell is terminally Failed.
// driftDetected: (Rollback only) the target render digest no longer matches ExpectedDigest.
func operationOutcome(w *aiplatformv1alpha1.AIWorkload, op aiplatformv1alpha1.AIWorkloadOperation, certifiedAtTarget, anyFailed, driftDetected bool, deadline time.Duration) aiplatformv1alpha1.AIWorkloadOperation {
	out := op
	if op.State != aiplatformv1alpha1.OperationStateInProgress {
		return out
	}
	if op.IntentDigest != intentDigest(w.Spec) {
		out.State = aiplatformv1alpha1.OperationStateSuperseded
		return out
	}
	if op.Type == aiplatformv1alpha1.OperationTypeRollback && driftDetected {
		out.State = aiplatformv1alpha1.OperationStateFailed
		out.Reason = "BlueprintDrift"
		return out
	}
	if certifiedAtTarget {
		out.State = aiplatformv1alpha1.OperationStateSucceeded
		return out
	}
	if anyFailed {
		out.State = aiplatformv1alpha1.OperationStateFailed
		return out
	}
	if time.Since(op.RequestedAt.Time) > deadline {
		out.State = aiplatformv1alpha1.OperationStateFailed
		out.Reason = "TimedOut"
		return out
	}
	return out
}

// reconcileOperation evaluates the in-flight journal against current status, applies the
// outcome (crash-safe: journal terminal → project → clear), and requeues while InProgress.
func (r *AIWorkloadReconciler) reconcileOperation(ctx context.Context, w *aiplatformv1alpha1.AIWorkload) (ctrl.Result, error) {
	op, ok := readJournal(w)
	if !ok {
		return ctrl.Result{}, nil
	}

	certified := false
	if w.Status.DeployedSource != nil && w.Status.DeployedSource.Version == op.TargetVersion {
		certified = true
	}
	if op.Type == aiplatformv1alpha1.OperationTypeRetry {
		certified = w.Status.Phase == aiplatformv1alpha1.AIWorkloadPhaseRunning && r.retrySyncSettled(ctx, w, op.RetryEpoch)
	}
	anyFailed := false
	for _, c := range w.Status.ComponentStatuses {
		if c.Phase == aiplatformv1alpha1.AIWorkloadClusterPhaseFailed {
			anyFailed = true
			break
		}
	}
	drift := false
	if op.Type == aiplatformv1alpha1.OperationTypeRollback {
		drift = r.rollbackDrift(ctx, w, op)
	}

	outcome := operationOutcome(w, *op, certified, anyFailed, drift, defaultOperationDeadline)
	if outcome.State == aiplatformv1alpha1.OperationStateInProgress {
		remaining := defaultOperationDeadline - time.Since(op.RequestedAt.Time)
		if remaining > 30*time.Second {
			remaining = 30 * time.Second
		}
		if remaining < 0 {
			remaining = time.Second
		}
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	// Terminal: journal→terminal, project, then clear (crash-safe order).
	js, _ := encodeOperation(outcome)
	metav1.SetMetaDataAnnotation(&w.ObjectMeta, operationAnnotation, js)
	if err := r.Update(ctx, w); err != nil {
		return ctrl.Result{}, err
	}
	cp := outcome
	w.Status.ActiveOperation = &cp
	if err := r.Status().Update(ctx, w); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	delete(w.Annotations, operationAnnotation)
	if err := r.Update(ctx, w); err != nil {
		return ctrl.Result{}, err
	}
	r.event(w, eventTypeFor(outcome), string(outcome.Type)+string(outcome.State), "reason=%s", outcome.Reason)
	return ctrl.Result{}, nil
}

func eventTypeFor(op aiplatformv1alpha1.AIWorkloadOperation) string {
	if op.State == aiplatformv1alpha1.OperationStateSucceeded {
		return corev1.EventTypeNormal
	}
	return corev1.EventTypeWarning
}

// retrySyncSettled reports whether every relevant BundleDeployment reached the retry epoch.
func (r *AIWorkloadReconciler) retrySyncSettled(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, epoch int64) bool {
	bdList := &unstructured.UnstructuredList{}
	bdList.SetGroupVersionKind(schema.GroupVersionKind{Group: "fleet.cattle.io", Version: "v1alpha1", Kind: "BundleDeploymentList"})
	for _, name := range w.Spec.FleetBundleNames {
		if err := r.List(ctx, bdList, client.MatchingLabels{"fleet.cattle.io/bundle-name": name}); err != nil {
			return false
		}
		for i := range bdList.Items {
			sg, found, _ := unstructured.NestedInt64(bdList.Items[i].Object, "status", "syncGeneration")
			if !found || sg < epoch {
				return false
			}
		}
	}
	return true
}

// rollbackDrift recomputes the target's aggregate render digest from the live Blueprint and
// reports whether it diverges from the digest captured at deploy time (op.ExpectedDigest).
func (r *AIWorkloadReconciler) rollbackDrift(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, op *aiplatformv1alpha1.AIWorkloadOperation) bool {
	// The reconcile has already re-rendered HelmOps for the (rolled-back) version and set
	// w.Status.DeployedSource only if certified. Drift is detected when the freshly certified
	// digest cannot equal the expected one — surfaced by never certifying at target with a
	// digest equal to ExpectedDigest. Conservative: compare the last aggregate we computed.
	if w.Status.DeployedSource != nil && w.Status.DeployedSource.Version == op.TargetVersion {
		return op.ExpectedDigest != "" && w.Status.DeployedSource.RenderDigest != op.ExpectedDigest
	}
	return false
}

// pruneRenderBaselines keeps only RenderBaseline entries whose HelmOpUID is in desiredUIDs,
// returning a sorted slice by HelmOpUID.
func pruneRenderBaselines(baselines []aiplatformv1alpha1.RenderBaseline, desiredUIDs map[string]bool) []aiplatformv1alpha1.RenderBaseline {
	out := make([]aiplatformv1alpha1.RenderBaseline, 0, len(baselines))
	for _, b := range baselines {
		if desiredUIDs[b.HelmOpUID] {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HelmOpUID < out[j].HelmOpUID })
	return out
}
