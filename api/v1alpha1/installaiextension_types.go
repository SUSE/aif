package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstallAIExtensionPhase represents the current installation phase
type InstallAIExtensionPhase string

const (
	InstallAIExtensionPhaseInstalling InstallAIExtensionPhase = "Installing"
	InstallAIExtensionPhaseInstalled  InstallAIExtensionPhase = "Installed"
	InstallAIExtensionPhaseFailed     InstallAIExtensionPhase = "Failed"
)

// InstallAIExtensionSpec defines the desired state of InstallAIExtension
type InstallAIExtensionSpec struct {
	// Helm configuration for the UIPlugin chart
	Helm HelmConfig `json:"helm"`

	// Extension configuration
	Extension ExtensionConfig `json:"extension"`
}

// HelmConfig defines Helm chart configuration
type HelmConfig struct {
	// Name is the Helm release name
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// URL is the Helm chart repository URL
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// Version is the chart version
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}

// ExtensionConfig defines UI extension configuration
type ExtensionConfig struct {
	// Name is the extension display name
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Version is the extension version
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}

// InstallAIExtensionStatus defines the observed state of InstallAIExtension
type InstallAIExtensionStatus struct {
	// Phase is the current installation phase
	// +kubebuilder:validation:Enum=Installing;Installed;Failed
	// +optional
	Phase InstallAIExtensionPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the InstallAIExtension state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=aifext
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Extension",type=string,JSONPath=`.spec.extension.name`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.extension.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// InstallAIExtension is the Schema for the installaiextensions API
type InstallAIExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstallAIExtensionSpec   `json:"spec,omitempty"`
	Status InstallAIExtensionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InstallAIExtensionList contains a list of InstallAIExtension
type InstallAIExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstallAIExtension `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InstallAIExtension{}, &InstallAIExtensionList{})
}
