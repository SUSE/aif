package conditions

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Set adds or updates the given condition on the conditions slice. It delegates
// to k8s.io/apimachinery's meta.SetStatusCondition, which preserves
// LastTransitionTime when the status hasn't changed.
//
// Controllers MUST use this helper instead of hand-rolled condition merging or
// pre-setting LastTransitionTime themselves — pre-setting LastTransitionTime
// breaks meta.SetStatusCondition's transition-detection contract.
func Set(conds *[]metav1.Condition, c metav1.Condition) {
	meta.SetStatusCondition(conds, c)
}
