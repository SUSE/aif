package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BlueprintPhase represents the current phase of a Blueprint
type BlueprintPhase string

const (
	BlueprintPhaseActive     BlueprintPhase = "Active"
	BlueprintPhaseDeprecated BlueprintPhase = "Deprecated"
	BlueprintPhaseWithdrawn  BlueprintPhase = "Withdrawn"
)

// BlueprintSourceType indicates how the Blueprint was created
type BlueprintSourceType string

const (
	BlueprintSourceWrapsVendorChart BlueprintSourceType = "WrapsVendorChart"
	BlueprintSourcePublished        BlueprintSourceType = "Published"
)

// BlueprintSpec defines the desired state of Blueprint
type BlueprintSpec struct {
	// BlueprintName is the lineage name
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	BlueprintName string `json:"blueprintName"`

	// Version is the semantic version
	// +kubebuilder:validation:Pattern=`^\d+\.\d+\.\d+$`
	Version string `json:"version"`

	// UseCase categorizes the Blueprint's purpose
	// +kubebuilder:validation:Enum=rag;vision;fine-tuning;inference;other
	UseCase string `json:"useCase"`

	// Description is free-text description
	// +optional
	// +kubebuilder:validation:MaxLength=1024
	Description string `json:"description,omitempty"`

	// ChangeDescription describes what changed in this version
	// +optional
	// +kubebuilder:validation:MaxLength=2048
	ChangeDescription string `json:"changeDescription,omitempty"`

	// Source indicates how this Blueprint was created
	Source BlueprintSource `json:"source"`

	// Components are the pinned components
	// +kubebuilder:validation:MinItems=1
	Components []ComponentRef `json:"components"`

	// ValueOverrides contains per-component Helm values YAML
	// +optional
	ValueOverrides map[string]string `json:"valueOverrides,omitempty"`

	// PublishedBy is the username of the approver (for Published) or "aif-system" (for WrapsVendorChart)
	PublishedBy string `json:"publishedBy"`

	// PublishedAt is the publish timestamp
	PublishedAt metav1.Time `json:"publishedAt"`
}

// BlueprintSource is a discriminated union indicating Blueprint origin
// TODO: Add cross-field validation to ensure exactly one field is set per Type value (requires CEL/webhook)
type BlueprintSource struct {
	// Type indicates the source type
	// +kubebuilder:validation:Enum=WrapsVendorChart;Published
	Type BlueprintSourceType `json:"type"`

	// VendorChartRef is populated when Type=WrapsVendorChart
	// +optional
	VendorChartRef *VendorChartRef `json:"vendorChartRef,omitempty"`

	// PublishedFrom is populated when Type=Published
	// +optional
	PublishedFrom *PublishedFromRef `json:"publishedFrom,omitempty"`
}

// VendorChartRef points to a vendor-published Reference Blueprint Helm chart
type VendorChartRef struct {
	// Provider identifies the vendor
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider"`

	// Repo is the Helm repository URL
	// +kubebuilder:validation:MinLength=1
	Repo string `json:"repo"`

	// Chart is the chart name
	// +kubebuilder:validation:MinLength=1
	Chart string `json:"chart"`

	// Version is the chart version
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}

// PublishedFromRef references the source Bundle
type PublishedFromRef struct {
	// BundleNamespace is the namespace of the source Bundle
	BundleNamespace string `json:"bundleNamespace"`

	// BundleName is the name of the source Bundle
	BundleName string `json:"bundleName"`

	// BundleGeneration is the Bundle generation at submit time
	BundleGeneration int64 `json:"bundleGeneration"`
}

// BlueprintStatus defines the observed state of Blueprint
type BlueprintStatus struct {
	// Phase is the current lifecycle phase
	// +kubebuilder:validation:Enum=Active;Deprecated;Withdrawn
	// +optional
	Phase BlueprintPhase `json:"phase,omitempty"`

	// Deprecation is set when phase != Active
	// +optional
	Deprecation *DeprecationStatus `json:"deprecation,omitempty"`

	// DeploymentCount is the number of currently-deployed Workloads sourced from this Blueprint version
	// +optional
	DeploymentCount int32 `json:"deploymentCount,omitempty"`

	// Conditions represent the latest available observations of the Blueprint's state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// DeprecationStatus tracks deprecation or withdrawal details
type DeprecationStatus struct {
	// Reason for the deprecation or withdrawal
	// +optional
	Reason string `json:"reason,omitempty"`

	// ActionedBy is the username who actioned the deprecation/withdrawal
	ActionedBy string `json:"actionedBy"`

	// ActionedAt is when the action occurred
	ActionedAt metav1.Time `json:"actionedAt"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=bp
// +kubebuilder:printcolumn:name="Lineage",type=string,JSONPath=`.spec.blueprintName`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Use Case",type=string,JSONPath=`.spec.useCase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Blueprint is the Schema for the blueprints API
type Blueprint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BlueprintSpec   `json:"spec,omitempty"`
	Status BlueprintStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BlueprintList contains a list of Blueprint
type BlueprintList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Blueprint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Blueprint{}, &BlueprintList{})
}
