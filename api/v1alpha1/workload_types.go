package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// WorkloadPhase represents the current phase of a Workload
type WorkloadPhase string

const (
	WorkloadPhasePending            WorkloadPhase = "Pending"
	WorkloadPhaseDeploying          WorkloadPhase = "Deploying"
	WorkloadPhaseRunning            WorkloadPhase = "Running"
	WorkloadPhaseDegraded           WorkloadPhase = "Degraded"
	WorkloadPhaseFailed             WorkloadPhase = "Failed"
	WorkloadPhaseRecoveryInProgress WorkloadPhase = "RecoveryInProgress"
)

// WorkloadSourceKind indicates the source type of a Workload
type WorkloadSourceKind string

const (
	WorkloadSourceKindApp        WorkloadSourceKind = "App"
	WorkloadSourceKindBlueprint  WorkloadSourceKind = "Blueprint"
	WorkloadSourceKindBundleTest WorkloadSourceKind = "BundleTest"
)

// StrategyType defines the deployment strategy type
type StrategyType string

const (
	StrategyTypeRollingUpdate     StrategyType = "RollingUpdate"
	StrategyTypeBlueGreen         StrategyType = "BlueGreen"
	StrategyTypeCanary            StrategyType = "Canary"
	StrategyTypeAutomaticRecovery StrategyType = "AutomaticRecovery"
)

// DeployStrategyType defines the deployment method
type DeployStrategyType string

const (
	DeployStrategyTypeHelm   DeployStrategyType = "helm"
	DeployStrategyTypeGitOps DeployStrategyType = "gitops"
)

// VPAUpdateMode defines the VPA update mode
type VPAUpdateMode string

const (
	VPAUpdateModeAuto    VPAUpdateMode = "Auto"
	VPAUpdateModeInitial VPAUpdateMode = "Initial"
	VPAUpdateModeOff     VPAUpdateMode = "Off"
)

// WorkloadSpec defines the desired state of Workload
type WorkloadSpec struct {
	// Name is the workload display name
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Source indicates the provenance of this Workload
	Source WorkloadSource `json:"source"`

	// TargetClusters are the cluster IDs (informational)
	// +optional
	TargetClusters []string `json:"targetClusters,omitempty"`

	// ValueOverrides contains per-component Helm values overrides
	// +optional
	ValueOverrides map[string]string `json:"valueOverrides,omitempty"`

	// DeployStrategy indicates the deployment method
	// +kubebuilder:validation:Enum=helm;gitops
	// +kubebuilder:default=helm
	// +optional
	DeployStrategy DeployStrategyType `json:"deployStrategy,omitempty"`

	// Replicas is the desired replica count
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Strategy defines the deployment strategy
	// +optional
	Strategy *DeploymentStrategy `json:"strategy,omitempty"`

	// Scaling defines scaling configuration
	// +optional
	Scaling *ScalingConfig `json:"scaling,omitempty"`

	// Paused suspends reconciliation when true
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// WorkloadSource is a discriminated union indicating Workload provenance
// TODO: Add cross-field validation to ensure exactly one field is set per Kind value (requires CEL/webhook)
type WorkloadSource struct {
	// Kind is a source-provenance discriminator, not a Kubernetes TypeMeta.Kind.
	// "App" and "Blueprint" correspond to CRDs of the same name; "BundleTest" has
	// no corresponding CRD — it denotes a test deployment created from a Bundle.
	// +kubebuilder:validation:Enum=App;Blueprint;BundleTest
	Kind WorkloadSourceKind `json:"kind"`

	// App is populated when Kind=App
	// +optional
	App *AppRef `json:"app,omitempty"`

	// Blueprint is populated when Kind=Blueprint
	// +optional
	Blueprint *BlueprintRef `json:"blueprint,omitempty"`

	// BundleTest is populated when Kind=BundleTest
	// +optional
	BundleTest *BundleTestRef `json:"bundleTest,omitempty"`
}

// BundleTestRef references a Bundle for test deployment
type BundleTestRef struct {
	// Namespace is the Bundle namespace
	Namespace string `json:"namespace"`

	// Name is the Bundle name
	Name string `json:"name"`

	// Generation is the Bundle generation snapshot at test-deploy time
	Generation int64 `json:"generation"`
}

// DeploymentStrategy defines deployment strategy configuration
type DeploymentStrategy struct {
	// Type is the strategy type
	// +kubebuilder:validation:Enum=RollingUpdate;BlueGreen;Canary;AutomaticRecovery
	// +kubebuilder:default=RollingUpdate
	// +optional
	Type StrategyType `json:"type,omitempty"`

	// RollingUpdate configuration
	// +optional
	RollingUpdate *RollingUpdateStrategy `json:"rollingUpdate,omitempty"`

	// BlueGreen configuration
	// +optional
	BlueGreen *BlueGreenStrategy `json:"blueGreen,omitempty"`

	// Canary configuration
	// +optional
	Canary *CanaryStrategy `json:"canary,omitempty"`

	// AutomaticRecovery configuration
	// +optional
	AutomaticRecovery *AutomaticRecoveryStrategy `json:"automaticRecovery,omitempty"`
}

// RollingUpdateStrategy defines rolling update parameters
type RollingUpdateStrategy struct {
	// MaxSurge is the maximum number of pods that can be created over the desired replicas
	// +kubebuilder:default="1"
	// +optional
	MaxSurge *intstr.IntOrString `json:"maxSurge,omitempty"`

	// MaxUnavailable is the maximum number of pods that can be unavailable during the update
	// +kubebuilder:default="0"
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// BlueGreenStrategy defines blue-green deployment parameters
type BlueGreenStrategy struct {
	// AutoPromotionSeconds is seconds before auto-promoting preview (0 = manual)
	// +kubebuilder:default=0
	// +optional
	AutoPromotionSeconds *int32 `json:"autoPromotionSeconds,omitempty"`
}

// CanaryStrategy defines canary deployment parameters
type CanaryStrategy struct {
	// Steps define the canary rollout steps
	// +optional
	Steps []CanaryStep `json:"steps,omitempty"`
}

// CanaryStep defines a single canary deployment step
type CanaryStep struct {
	// Weight is the traffic weight (0-100)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`

	// PauseSeconds is the pause duration between steps
	// +optional
	PauseSeconds *int32 `json:"pauseSeconds,omitempty"`
}

// AutomaticRecoveryStrategy defines automatic recovery parameters
type AutomaticRecoveryStrategy struct {
	// Enabled enables auto-rollback on ProgressDeadlineExceeded
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// FailureThreshold is consecutive failures before rollback
	// +kubebuilder:default=3
	// +optional
	FailureThreshold *int32 `json:"failureThreshold,omitempty"`
}

// ScalingConfig defines scaling configuration
type ScalingConfig struct {
	// MinReplicas is the HPA minimum
	// +kubebuilder:default=1
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// MaxReplicas is the HPA maximum
	// +optional
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// TargetCPUUtilizationPercent is the HPA CPU target
	// +optional
	TargetCPUUtilizationPercent *int32 `json:"targetCPUUtilizationPercent,omitempty"`

	// VPA configuration
	// +optional
	VPA *VPAConfig `json:"vpa,omitempty"`
}

// VPAConfig defines VPA configuration
type VPAConfig struct {
	// Enabled enables VPA
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// UpdateMode is the VPA update mode
	// +kubebuilder:validation:Enum=Auto;Initial;Off
	// +kubebuilder:default=Auto
	// +optional
	UpdateMode VPAUpdateMode `json:"updateMode,omitempty"`
}

// WorkloadStatus defines the observed state of Workload
type WorkloadStatus struct {
	// Phase is the current lifecycle phase
	// +kubebuilder:validation:Enum=Pending;Deploying;Running;Degraded;Failed;RecoveryInProgress
	// +optional
	Phase WorkloadPhase `json:"phase,omitempty"`

	// Replicas is the current total replicas
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas is the replicas passing readiness check
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// ComponentReleases tracks per-component Helm release status
	// +kubebuilder:validation:MaxItems=100
	// +optional
	ComponentReleases []ComponentReleaseStatus `json:"componentReleases,omitempty"`

	// Conditions represent the latest available observations of the Workload's state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DeploymentHistory is the ordered deployment revision history
	// +kubebuilder:validation:MaxItems=100
	// +optional
	DeploymentHistory []DeploymentRecord `json:"deploymentHistory,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// ComponentReleaseStatus tracks a single component's Helm release status
type ComponentReleaseStatus struct {
	// Name is the component name
	Name string `json:"name"`

	// ReleaseName is the Helm release name
	ReleaseName string `json:"releaseName"`

	// Status is the Helm release status
	Status string `json:"status"`

	// Revision is the Helm release revision
	// +optional
	Revision int32 `json:"revision,omitempty"`
}

// DeploymentRecord tracks a single deployment revision
type DeploymentRecord struct {
	// Revision is the deployment revision number
	Revision int64 `json:"revision"`

	// Source is the source reference at this revision
	Source WorkloadSource `json:"source"`

	// ValueOverridesHash is a hash of the valueOverrides at this revision
	// +optional
	ValueOverridesHash string `json:"valueOverridesHash,omitempty"`

	// HelmRevisions maps component names to Helm release revisions
	// +optional
	HelmRevisions map[string]int `json:"helmRevisions,omitempty"`

	// Phase is the phase this revision reached
	Phase WorkloadPhase `json:"phase"`

	// Timestamp is when this revision was deployed
	Timestamp metav1.Time `json:"timestamp"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wl
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Workload is the Schema for the workloads API
type Workload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkloadSpec   `json:"spec,omitempty"`
	Status WorkloadStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkloadList contains a list of Workload
type WorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workload `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workload{}, &WorkloadList{})
}
