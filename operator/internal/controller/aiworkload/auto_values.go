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
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiplatformv1alpha1 "github.com/SUSE/aif-operator/api/v1alpha1"
	"github.com/SUSE/aif-operator/internal/autovalues"
)

var errAutoValueUnresolved = stderrors.New("automatic value not resolved")

const (
	reasonAutoValueNeedsSingleTarget = "AutoValueNeedsSingleTarget"
	reasonAutoValueUnresolved        = "AutoValueUnresolved"
	reasonAutoValuePending           = "AutoValuePending"
	reasonLockedValueChanged         = "LockedValueChanged"
	reasonAutoValueNodeIPFallback    = "AutoValueNodeIPFallback"
	reasonAutoValueMultipleTargets   = "AutoValueMultipleTargets"

	conditionTypeAutoValueTarget = "AutoValueTarget"

	// ingressIPPendingTimeout bounds the wait for an ingress LoadBalancer IP;
	// clusters without a LoadBalancer provider never get one.
	ingressIPPendingTimeout = 5 * time.Minute
)

// autoValueError carries the Ready condition reason for an automatic value
// that could not be pinned. Pending marks a wait that resolves on its own,
// on ClusterID.
type autoValueError struct {
	Reason    string
	Message   string
	Pending   bool
	ClusterID string
}

func (e *autoValueError) Error() string { return e.Message }
func (e *autoValueError) Unwrap() error { return errAutoValueUnresolved }

func (r *AIWorkloadReconciler) clusterFacts() ClusterFacts {
	if r.ClusterFacts != nil {
		return r.ClusterFacts
	}
	return rancherClusterFacts{Client: r.controllerReader()}
}

func mergedComponentValues(w *aiplatformv1alpha1.AIWorkload, c aiplatformv1alpha1.BlueprintComponent) (map[string]any, error) {
	return autovalues.MergedValues(c, w.Spec.ComponentValues)
}

// applyResolvedValues replaces every sentinel in vals with its pinned value.
func applyResolvedValues(vals map[string]any, componentName string, pins []aiplatformv1alpha1.ResolvedValue) error {
	for _, s := range autovalues.Find(vals) {
		i := findPin(pins, componentName, s.Path)
		if i < 0 {
			return fmt.Errorf("%w: component %q value %q", errAutoValueUnresolved, componentName, s.Path)
		}
		autovalues.Set(vals, s.Path, pins[i].Value)
	}
	if autovalues.ContainsSentinel(vals) {
		return fmt.Errorf("%w: component %q has an automatic value inside a list or under a key containing \".\"; automatic values are not supported there", errAutoValueUnresolved, componentName)
	}
	return nil
}

func findPin(pins []aiplatformv1alpha1.ResolvedValue, componentName, path string) int {
	for i, p := range pins {
		if p.ComponentName == componentName && p.Path == path {
			return i
		}
	}
	return -1
}

// pinAutoValues records a concrete value in status for every automatic value
// of the enabled components. Pins are locked once recorded (see
// lockedValueChanges): a user-set value is pinned once, and an auto value is
// re-resolved only when the workload's single target cluster changes. Pins for
// paths a newer Blueprint no longer has are kept.
func (r *AIWorkloadReconciler) pinAutoValues(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, components []aiplatformv1alpha1.BlueprintComponent) (bool, error) {
	changed := false
	var ip string
	var kept []string
	upsert := func(pin aiplatformv1alpha1.ResolvedValue) {
		if i := findPin(w.Status.ResolvedValues, pin.ComponentName, pin.Path); i >= 0 {
			if w.Status.ResolvedValues[i] != pin {
				w.Status.ResolvedValues[i] = pin
				changed = true
			}
			return
		}
		w.Status.ResolvedValues = append(w.Status.ResolvedValues, pin)
		changed = true
	}

	for _, c := range components {
		if !isComponentEnabled(w, c.ChartName) {
			continue
		}
		base, err := autovalues.MergedValues(c, nil)
		if err != nil {
			return changed, err
		}
		merged, err := mergedComponentValues(w, c)
		if err != nil {
			return changed, err
		}
		for _, s := range autovalues.Find(base) {
			if findPin(w.Status.ResolvedValues, c.ChartName, s.Path) >= 0 {
				continue
			}
			if v, ok := autovalues.Get(merged, s.Path); ok && !autovalues.IsSentinel(v) {
				upsert(aiplatformv1alpha1.ResolvedValue{ComponentName: c.ChartName, Path: s.Path, Value: autovalues.ScalarString(v), Source: autovalues.SourceUser})
			}
		}
		for _, s := range autovalues.Find(merged) {
			i := findPin(w.Status.ResolvedValues, c.ChartName, s.Path)
			if i >= 0 && w.Status.ResolvedValues[i].Source != autovalues.SourceAuto {
				continue
			}
			clusterID, err := singleTarget(w)
			if err != nil && i >= 0 {
				// Only a new resolution needs a single target; an existing pin
				// keeps rendering, with a warning (see syncAutoValueTargetWarning).
				kept = append(kept, fmt.Sprintf("%s %s", c.ChartName, s.Path))
				continue
			}
			if err != nil {
				return changed, &autoValueError{
					Reason: reasonAutoValueNeedsSingleTarget,
					Message: fmt.Sprintf("Component %q value %q is set automatically from the target cluster, but the workload targets %s; set it explicitly.",
						c.ChartName, s.Path, err.Error()),
				}
			}
			var old aiplatformv1alpha1.ResolvedValue
			if i >= 0 {
				old = w.Status.ResolvedValues[i]
				if old.ClusterID == clusterID {
					continue
				}
				// Pins from before clusterId existed adopt the current target
				// unresolved, so upgrading the operator never moves a domain.
				if old.ClusterID == "" {
					old.ClusterID = clusterID
					upsert(old)
					continue
				}
			}
			value, err := r.resolveAutoValue(ctx, w, clusterID, c.ChartName, s, &ip)
			if stderrors.Is(err, errIngressIPPending) {
				return changed, &autoValueError{
					Reason:    reasonAutoValuePending,
					Message:   fmt.Sprintf("Waiting to resolve automatic value %q of component %q%s %v", s.Path, c.ChartName, pendingClusterTag(clusterID), err),
					Pending:   true,
					ClusterID: clusterID,
				}
			}
			if err != nil {
				return changed, &autoValueError{
					Reason:  reasonAutoValueUnresolved,
					Message: fmt.Sprintf("Cannot resolve automatic value %q of component %q: %v", s.Path, c.ChartName, err),
				}
			}
			upsert(aiplatformv1alpha1.ResolvedValue{ComponentName: c.ChartName, Path: s.Path, Value: value, Source: autovalues.SourceAuto, ClusterID: clusterID})
			if i >= 0 {
				r.event(w, corev1.EventTypeNormal, "AutoValueReresolved", "component %q value %q re-resolved for cluster %s: %q (was %q on cluster %s)",
					c.ChartName, s.Path, clusterID, value, old.Value, old.ClusterID)
			}
		}
		if err := applyResolvedValues(merged, c.ChartName, w.Status.ResolvedValues); err != nil {
			return changed, &autoValueError{Reason: reasonAutoValueUnresolved, Message: err.Error()}
		}
	}
	if changed {
		sort.Slice(w.Status.ResolvedValues, func(i, j int) bool {
			a, b := w.Status.ResolvedValues[i], w.Status.ResolvedValues[j]
			if a.ComponentName != b.ComponentName {
				return a.ComponentName < b.ComponentName
			}
			return a.Path < b.Path
		})
	}
	r.syncAutoValueTargetWarning(w, kept)
	return changed, nil
}

// resolveAutoValue computes an automatic value for clusterID; ip caches the
// ingress IP across the values of one pass.
func (r *AIWorkloadReconciler) resolveAutoValue(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, clusterID, component string, s autovalues.Sentinel, ip *string) (string, error) {
	if s.Kind == autovalues.KindIngressClass {
		return r.clusterFacts().DefaultIngressClass(ctx, clusterID)
	}
	if *ip == "" {
		var err error
		if *ip, err = r.ingressIP(ctx, w, clusterID, component, s.Path); err != nil {
			return "", err
		}
	}
	value := *ip + ".sslip.io"
	if s.Label != "" {
		value = s.Label + "." + value
	}
	return value, nil
}

// ingressIP resolves the cluster's ingress IP. While the ingress LoadBalancer
// is still being provisioned it waits (errIngressIPPending) for up to
// ingressIPPendingTimeout, measured from when the workload started waiting on
// this cluster, then falls back to a node IP as before. Without a node IP it
// keeps waiting, so the wait never restarts.
func (r *AIWorkloadReconciler) ingressIP(ctx context.Context, w *aiplatformv1alpha1.AIWorkload, clusterID, component, path string) (string, error) {
	ip, err := r.clusterFacts().IngressIP(ctx, clusterID)
	var pe *ingressIPPendingError
	if !stderrors.As(err, &pe) {
		return ip, err
	}
	if autoValuePendingFor(w, clusterID) < ingressIPPendingTimeout {
		return "", fmt.Errorf("%w (a node IP is used if none appears within %s)", err, ingressIPPendingTimeout)
	}
	if pe.FallbackErr != nil {
		return "", fmt.Errorf("%w; no node IP to fall back to (%v); waiting for the LoadBalancer", err, pe.FallbackErr)
	}
	r.event(w, corev1.EventTypeWarning, reasonAutoValueNodeIPFallback,
		"component %q value %q: %v after %s; using node IP %s instead. Set the value explicitly if it should use another address.",
		component, path, pe, ingressIPPendingTimeout, pe.FallbackIP)
	return pe.FallbackIP, nil
}

// pendingClusterTag names the cluster in the AutoValuePending message, which
// is how a wait is tied to its cluster.
func pendingClusterTag(clusterID string) string {
	return " on cluster " + clusterID + ":"
}

// pendingWait returns the Ready condition of a wait in progress on clusterID.
func pendingWait(w *aiplatformv1alpha1.AIWorkload, clusterID string) *metav1.Condition {
	c := meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady)
	if c == nil || c.Reason != reasonAutoValuePending || !strings.Contains(c.Message, pendingClusterTag(clusterID)) {
		return nil
	}
	return c
}

// autoValuePendingFor is how long the workload has been waiting on an
// automatic value of clusterID (see markAutoValuePending), or 0.
func autoValuePendingFor(w *aiplatformv1alpha1.AIWorkload, clusterID string) time.Duration {
	if c := pendingWait(w, clusterID); c != nil {
		return time.Since(c.LastTransitionTime.Time)
	}
	return 0
}

// markAutoValuePending sets Ready=False/AutoValuePending. Its
// LastTransitionTime records when the wait began (meta.SetStatusCondition
// only moves it on a status change, and Ready may already have been False),
// so the wait is bounded across reconciles and restarts; waiting on another
// cluster starts afresh. The phase of a running workload is kept: its release
// is unaffected.
func markAutoValuePending(w *aiplatformv1alpha1.AIWorkload, message, clusterID string) {
	since := metav1.Now()
	if c := pendingWait(w, clusterID); c != nil {
		since = c.LastTransitionTime
	}
	setCondition(&w.Status.Conditions, conditionTypeReady, metav1.ConditionFalse, reasonAutoValuePending, message, w.Generation)
	meta.FindStatusCondition(w.Status.Conditions, conditionTypeReady).LastTransitionTime = since
	switch w.Status.Phase {
	case aiplatformv1alpha1.AIWorkloadPhaseRunning, aiplatformv1alpha1.AIWorkloadPhaseDegraded:
	default:
		w.Status.Phase = aiplatformv1alpha1.AIWorkloadPhasePending
	}
}

// syncAutoValueTargetWarning keeps the AutoValueTarget condition on a workload
// whose existing auto pins render on more than one cluster (a value resolved
// for one cluster may be wrong on the others), with a Warning event when it
// first appears or changes.
func (r *AIWorkloadReconciler) syncAutoValueTargetWarning(w *aiplatformv1alpha1.AIWorkload, kept []string) {
	if len(kept) == 0 {
		meta.RemoveStatusCondition(&w.Status.Conditions, conditionTypeAutoValueTarget)
		return
	}
	msg := fmt.Sprintf("Automatic values (%s) were resolved for a single cluster but the workload targets %d clusters; "+
		"they keep their pinned values on every cluster. Target a single cluster, or set these values explicitly.",
		strings.Join(kept, ", "), len(w.Spec.TargetClusters))
	if c := meta.FindStatusCondition(w.Status.Conditions, conditionTypeAutoValueTarget); c == nil || c.Message != msg {
		r.event(w, corev1.EventTypeWarning, reasonAutoValueMultipleTargets, "%s", msg)
	}
	setCondition(&w.Status.Conditions, conditionTypeAutoValueTarget, metav1.ConditionTrue, reasonAutoValueMultipleTargets, msg, w.Generation)
}

// lockedValueChanges describes every pinned value the spec now overrides with
// a different value, or "" when there is none.
func lockedValueChanges(w *aiplatformv1alpha1.AIWorkload, components []aiplatformv1alpha1.BlueprintComponent) (string, error) {
	changes, err := autovalues.LockedChanges(filterEnabledComponents(w, components), w.Status.ResolvedValues, w.Spec.ComponentValues)
	if err != nil || len(changes) == 0 {
		return "", err
	}
	msgs := make([]string, 0, len(changes))
	for _, c := range changes {
		msgs = append(msgs, c.String())
	}
	return strings.Join(msgs, "; ") + ". Remove the override or set it back to the pinned value.", nil
}

// singleTarget returns the one cluster automatic values resolve against; an
// empty target list means the local cluster.
func singleTarget(w *aiplatformv1alpha1.AIWorkload) (string, error) {
	switch len(w.Spec.TargetClusters) {
	case 0:
		return "local", nil
	case 1:
		if isLocalCluster(w.Spec.TargetClusters[0]) {
			return "local", nil
		}
		return w.Spec.TargetClusters[0], nil
	default:
		return "", fmt.Errorf("%d clusters", len(w.Spec.TargetClusters))
	}
}
