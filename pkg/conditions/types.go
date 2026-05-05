// Package conditions defines shared condition Type and Reason constants used
// across all AIF controllers to prevent typo-induced silent failures.
//
// Per ARCHITECTURE.md §4.1, every Type and Reason string emitted by AIF
// controllers MUST be a Go constant from this package.
package conditions

// Standard condition Types (used across all CRDs)
const (
	TypeReady       = "Ready"       // resource fully reconciled and functioning
	TypeProgressing = "Progressing" // reconciliation actively making progress
	TypeDegraded    = "Degraded"    // resource running but in a degraded state
)

// Condition Reasons used across controllers
const (
	// Generic
	ReasonReconciled  = "Reconciled"  // happy-path success
	ReasonInvalidSpec = "InvalidSpec" // spec validation failed

	// Bundle-specific
	ReasonAwaitingDeployer         = "AwaitingDeployer"         // Workload waiting for deploy logic
	ReasonSecretNotFound           = "SecretNotFound"           // Settings credential Secret missing
	ReasonInvalidSecretKey         = "InvalidSecretKey"         // Secret exists but referenced key missing
	ReasonPublishedBlueprintMissing = "PublishedBlueprintMissing" // Self-healing detected missing Blueprint

	// Workload-specific
	ReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	ReasonRollbackExhausted        = "RollbackExhausted"

	// Webhook / immutability
	ReasonImmutableSpec = "ImmutableSpec" // Blueprint spec mutation attempted

	// Pull-secret reconciler (P7-2)
	ReasonPullSecretReconcileBlocked = "PullSecretReconcileBlocked"
	ReasonSourceSecretMissing        = "SourceSecretMissing"

	// Blueprint validation
	ReasonBlueprintValidated = "BlueprintValidated"       // Blueprint spec validation passed
	ReasonBlueprintInvalid   = "BlueprintInvalid"         // Blueprint spec validation failed

	// Blueprint source events
	ReasonBlueprintPublished              = "BlueprintPublished"              // Blueprint from Published source
	ReasonBlueprintWrappedFromVendorChart = "BlueprintWrappedFromVendorChart" // Blueprint from WrapsVendorChart source

	// Blueprint deletion
	ReasonBlueprintHasActiveWorkloads = "BlueprintHasActiveWorkloads" // Deletion blocked due to active Workloads

	// InstallAIExtension-specific
	ReasonUIPluginCRDMissing = "UIPluginCRDMissing"   // UIPlugin CRD not found in cluster
	ReasonChartPullFailed    = "ChartPullFailed"       // Failed to pull Helm chart
	ReasonInstallFailed      = "InstallFailed"         // Helm chart installation failed
	ReasonUIPluginNotCreated = "UIPluginNotCreated"    // UIPlugin resource creation failed
	ReasonInstalled          = "Installed"             // Extension installed successfully
)
