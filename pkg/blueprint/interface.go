package blueprint

import (
	"context"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
)

// Manager handles Blueprint business logic
type Manager interface {
	// ValidateSpec validates Blueprint spec fields
	// Returns error if semver invalid or source.type invalid
	ValidateSpec(bp *aifv1.Blueprint) error

	// ComputeDeploymentCount counts Workloads sourced from this Blueprint
	// Takes the full Workload list (controller provides it)
	ComputeDeploymentCount(bp *aifv1.Blueprint, workloads []aifv1.Workload) int32
}

// Wrapper auto-wraps detected Reference Blueprint charts as immutable
// single-component Blueprint CRs and withdraws orphaned wrappings.
type Wrapper interface {
	WrapDetectedCharts(ctx context.Context) error
}

// EventEmitter records domain events for wrapped Blueprint lifecycle.
type EventEmitter interface {
	BlueprintWrappedFromVendorChart(bp Blueprint)
	BlueprintWithdrawn(bp Blueprint)
}
