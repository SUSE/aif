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

// InstallAIExtension-specific condition Types
const (
	TypeDeploymentReady  = "DeploymentReady"  // container-mode Deployment is available
	TypeServiceReady     = "ServiceReady"     // container-mode Service has a ClusterIP
	TypeClusterRepoReady = "ClusterRepoReady" // ClusterRepo exists and index is synced
	TypeHelmInstalled    = "HelmInstalled"    // Helm chart successfully installed from ClusterRepo
	TypeUIPluginReady    = "UIPluginReady"    // UIPlugin CR exists (created by Helm chart)
)

// Condition Reasons used across controllers
const (
	// Generic
	ReasonReconciled       = "Reconciled"       // happy-path success
	ReasonReconcileFailed  = "ReconcileFailed"  // reconciliation encountered an error
	ReasonInvalidSpec      = "InvalidSpec"      // spec validation failed

	// Bundle-specific
	ReasonSecretNotFound           = "SecretNotFound"           // Settings credential Secret missing
	ReasonInvalidSecretKey         = "InvalidSecretKey"         // Secret exists but referenced key missing
	ReasonPublishedBlueprintMissing = "PublishedBlueprintMissing" // Self-healing detected missing Blueprint

	// Workload-specific
	ReasonProgressDeadlineExceeded = "ProgressDeadlineExceeded"
	ReasonRollbackExhausted        = "RollbackExhausted"

	// P4-2: Workload deployer reasons (ReasonInstalled is shared with InstallAIExtension below).
	ReasonInstalling             = "Installing"             // Ready=False, transient (deploy in progress)
	ReasonInstalled              = "Installed"              // Resource installed successfully (used by InstallAIExtension and Workload)
	ReasonComponentInstallFailed = "ComponentInstallFailed" // Ready=False, recoverable
	ReasonOrphanCleanupPending   = "OrphanCleanupPending"   // Ready=False, transient (drift cleanup)
	ReasonSourceNotResolved      = "SourceNotResolved"      // Ready=False, recoverable (source CR not yet present)
	ReasonUnsupportedComposition = "UnsupportedComposition" // Ready=False, terminal until spec change

	// P5-1: Workload phase-driven Ready condition reasons (six reasons, one per phase).
	ReasonWorkloadRunning            = "WorkloadRunning"
	ReasonWorkloadPending            = "WorkloadPending"
	ReasonWorkloadDeploying          = "WorkloadDeploying"
	ReasonWorkloadDegraded           = "WorkloadDegraded"
	ReasonWorkloadRecoveryInProgress = "WorkloadRecoveryInProgress"
	ReasonWorkloadFailed             = "WorkloadFailed"

	// P5-3: Workload upgrade reasons.
	ReasonUpgradeStarted = "UpgradeStarted" // Workload upgraded to a newer Blueprint version via REST API

	// Webhook / immutability
	ReasonImmutableSpec = "ImmutableSpec" // Blueprint spec mutation attempted

	// Pull-secret reconciler (P7-2)
	ReasonPullSecretReconcileBlocked = "PullSecretReconcileBlocked"
	ReasonSourceSecretMissing        = "SourceSecretMissing"
	// ReasonPullSecretNotReady is set on Workload Ready=False when the
	// reconciler can't yet read suse-registry-creds from the operator
	// namespace. Resolves once SettingsReconciler materializes the secret.
	ReasonPullSecretNotReady = "PullSecretNotReady"

	// Blueprint validation
	ReasonBlueprintValidated = "BlueprintValidated"       // Blueprint spec validation passed
	ReasonBlueprintInvalid   = "BlueprintInvalid"         // Blueprint spec validation failed

	// Blueprint source events
	ReasonBlueprintPublished              = "BlueprintPublished"              // Blueprint from Published source
	ReasonBlueprintWrappedFromVendorChart = "BlueprintWrappedFromVendorChart" // Blueprint from WrapsVendorChart source
	ReasonBlueprintWithdrawn              = "BlueprintWithdrawn"              // Wrapped Blueprint's vendor chart removed from catalog

	// Blueprint deletion
	ReasonBlueprintHasActiveWorkloads = "BlueprintHasActiveWorkloads" // Deletion blocked due to active Workloads

	// InstallAIExtension-specific
	ReasonUIPluginCRDMissing = "UIPluginCRDMissing" // UIPlugin CRD not found in cluster
	ReasonChartPullFailed    = "ChartPullFailed"    // Failed to pull Helm chart
	ReasonInstallFailed      = "InstallFailed"      // Helm chart installation failed
	ReasonUIPluginNotCreated = "UIPluginNotCreated" // UIPlugin resource creation failed
	// ReasonInstalled is defined above (shared with Workload)

	// InstallAIExtension sub-resource lifecycle reasons
	ReasonDeploymentCreated   = "DeploymentCreated"   // Deployment created, waiting for availability
	ReasonDeploymentAvailable = "DeploymentAvailable"  // Deployment has minimum available replicas
	ReasonDeploymentFailed    = "DeploymentFailed"     // Deployment creation or rollout failed
	ReasonServiceCreated      = "ServiceCreated"       // Service created successfully
	ReasonServiceFailed       = "ServiceFailed"        // Service creation failed
	ReasonClusterRepoCreated  = "ClusterRepoCreated"   // ClusterRepo created, waiting for sync
	ReasonClusterRepoSynced   = "ClusterRepoSynced"    // ClusterRepo has synced its index
	ReasonClusterRepoFailed   = "ClusterRepoFailed"    // ClusterRepo creation or sync failed
	ReasonHelmInstallStarted  = "HelmInstallStarted"   // Helm install initiated
	ReasonUIPluginVerified    = "UIPluginVerified"      // UIPlugin exists and matches expected name
)
