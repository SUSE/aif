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
	ReasonPublishedBlueprintMissing = "PublishedBlueprintMissing" // Self-healing detected missing Blueprint

	// Workload-specific
	ReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	ReasonRollbackExhausted        = "RollbackExhausted"

	// Webhook / immutability
	ReasonImmutableSpec = "ImmutableSpec" // Blueprint spec mutation attempted

	// Pull-secret reconciler (P7-2)
	ReasonPullSecretReconcileBlocked = "PullSecretReconcileBlocked"
	ReasonSourceSecretMissing        = "SourceSecretMissing"
)
