package conditions

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSet_PreservesLastTransitionTimeWhenStatusUnchanged is the regression
// guard for the bug the previous controllers had: hand-rolled setCondition
// implementations pre-set LastTransitionTime to Now() on every call, which
// breaks meta.SetStatusCondition's transition-detection contract. Set
// delegates to meta.SetStatusCondition; this test pins that behaviour by
// asserting LTT is NOT updated when the condition transitions to the same
// status it already had.
func TestSet_PreservesLastTransitionTimeWhenStatusUnchanged(t *testing.T) {
	original := metav1.Time{Time: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	conds := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Reconciled",
		Message:            "first reconcile",
		LastTransitionTime: original,
	}}

	// Same status, different message — LTT MUST be preserved.
	Set(&conds, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciled",
		Message: "second reconcile (status unchanged)",
	})

	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if !conds[0].LastTransitionTime.Equal(&original) {
		t.Errorf("LTT not preserved: original=%v, got=%v", original, conds[0].LastTransitionTime)
	}
	if conds[0].Message != "second reconcile (status unchanged)" {
		t.Errorf("Message not updated: got %q", conds[0].Message)
	}
}

// TestSet_AdvancesLastTransitionTimeWhenStatusFlips ensures the helper does
// update LTT when status genuinely changes (the other half of the contract).
func TestSet_AdvancesLastTransitionTimeWhenStatusFlips(t *testing.T) {
	original := metav1.Time{Time: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	conds := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "InvalidSpec",
		Message:            "broken",
		LastTransitionTime: original,
	}}

	before := metav1.Now()
	Set(&conds, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciled",
		Message: "fixed",
	})
	after := metav1.Now()

	if conds[0].LastTransitionTime.Equal(&original) {
		t.Errorf("LTT should have advanced on status flip, but stayed at %v", original)
	}
	if conds[0].LastTransitionTime.Before(&before) || after.Before(&conds[0].LastTransitionTime) {
		t.Errorf("LTT %v not within [%v, %v]", conds[0].LastTransitionTime, before, after)
	}
}

// TestSet_AppendsNewConditionType ensures a previously-absent condition type
// is appended (rather than silently swallowed).
func TestSet_AppendsNewConditionType(t *testing.T) {
	conds := []metav1.Condition{}
	Set(&conds, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciled",
		Message: "ok",
	})
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition appended, got %d", len(conds))
	}
	if conds[0].Type != "Ready" {
		t.Errorf("appended wrong type: %q", conds[0].Type)
	}
	if conds[0].LastTransitionTime.IsZero() {
		t.Errorf("LTT must be auto-set on insert; got zero")
	}
}
