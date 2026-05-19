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

	// ErrNestedBlueprintNotSupported is returned when a Blueprint or
	// BundleTest source contains a child component with Kind=Blueprint.
	// P4-2 does not implement recursive Blueprint expansion. Terminal
	// until spec changes.
	ErrNestedBlueprintNotSupported = errors.New("workload: nested Blueprint composition not supported (P4-2)")

	// ErrComponentInstallFailed wraps any per-component install failure
	// (helm pull/install/upgrade failure, NIM GenerateValues failure,
	// post-merge image.repository missing). The underlying cause is
	// reachable via errors.Is.
	ErrComponentInstallFailed = errors.New("workload: component install failed")

	// ErrComponentUninstallFailed wraps any orphan-cleanup uninstall
	// failure. Phase stays Deploying until cleanup succeeds.
	ErrComponentUninstallFailed = errors.New("workload: orphan component uninstall failed")
)
