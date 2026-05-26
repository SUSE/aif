package workload

import "errors"

// Sentinel errors. Callers classify failures via errors.Is, never with
// strings.Contains on the error message (CLAUDE.md forbidden pattern).
//
// Underlying causes (helm.ErrPullFailed, helm.ErrMissingImageRepository,
// nvidia.ErrInvalidGPUCount, etc.) stay reachable because Deploy()
// aggregates with errors.Join.
var (
	// ErrSourceNotResolved is returned when the Workload's source
	// (Blueprint CR or Bundle CR) cannot be fetched from the K8s API
	// (typically NotFound; the source may still appear later, so the
	// reconciler requeues).
	ErrSourceNotResolved = errors.New("workload: source CR not found")

	// ErrNestedBlueprintNotSupported is returned when a Blueprint source
	// contains a child component with Kind=Blueprint. P4-2 does not
	// implement recursive Blueprint expansion. Terminal until spec changes.
	ErrNestedBlueprintNotSupported = errors.New("workload: nested Blueprint composition not supported (P4-2)")

	// ErrComponentInstallFailed wraps any per-component install failure
	// (helm pull/install/upgrade failure, NIM GenerateValues failure,
	// post-merge image.repository missing). The underlying cause is
	// reachable via errors.Is.
	ErrComponentInstallFailed = errors.New("workload: component install failed")

	// ErrComponentUninstallFailed wraps any orphan-cleanup uninstall
	// failure. Phase stays Deploying until cleanup succeeds.
	ErrComponentUninstallFailed = errors.New("workload: orphan component uninstall failed")

	// P5-3 upgrade workflow sentinels. Classified by errors.Is at the
	// internal/api boundary, where each maps to a specific HTTP status:
	//   ErrWorkloadNotFound          → 404 NOT_FOUND
	//   ErrSourceNotBlueprint        → 400 INVALID_INPUT
	//   ErrBlueprintVersionNotFound  → 404 NOT_FOUND
	//   ErrCrossLineageUpgrade       → 400 INVALID_INPUT
	//   ErrTargetWithdrawn           → 409 INVALID_TRANSITION
	//   ErrDowngradeNotSupported     → 409 INVALID_TRANSITION
	//   ErrUpgradeConflict           → 409 CONFLICT (resourceVersion mismatch)
	ErrWorkloadNotFound         = errors.New("workload: not found")
	ErrSourceNotBlueprint       = errors.New("workload: upgrade requires source.kind=Blueprint")
	ErrBlueprintVersionNotFound = errors.New("workload: target Blueprint version not found")
	ErrCrossLineageUpgrade      = errors.New("workload: cross-lineage upgrade not allowed")
	ErrTargetWithdrawn          = errors.New("workload: cannot upgrade to a Withdrawn Blueprint version")
	ErrDowngradeNotSupported    = errors.New("workload: upgrade must target a higher version (downgrade is not supported in v1)")
	ErrUpgradeConflict          = errors.New("workload: upgrade conflict -- workload was modified concurrently")
)
