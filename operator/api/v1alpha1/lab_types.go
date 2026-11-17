package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LabPhase is the lifecycle phase of a Lab.
// +kubebuilder:validation:Enum=Disabled;Pending;Installing;Ready;Failed
type LabPhase string

const (
	LabPhaseDisabled   LabPhase = "Disabled"
	LabPhasePending    LabPhase = "Pending"
	LabPhaseInstalling LabPhase = "Installing"
	LabPhaseReady      LabPhase = "Ready"
	LabPhaseFailed     LabPhase = "Failed"
)

// LabUI describes how the lab is surfaced in the AIF UI.
type LabUI struct {
	// +optional
	Label string `json:"label,omitempty"`
	// +optional
	Icon string `json:"icon,omitempty"`
	// +optional
	RouteName string `json:"routeName,omitempty"`
}

// LabCatalogRef points at an entry of a LabCatalog.
type LabCatalogRef struct {
	// Catalog is the LabCatalog name.
	// +kubebuilder:validation:MinLength=1
	Catalog string `json:"catalog"`
	// Entry is the entry name inside the catalog.
	// +kubebuilder:validation:MinLength=1
	Entry string `json:"entry"`
	// Version is the entry version the admin accepted. Once the child
	// InstallAIExtension exists, catalog changes are not applied until Version is
	// set to the version the catalog now offers (status.updateAvailable). Empty
	// accepts whatever the catalog offers on first install only.
	// +optional
	Version string `json:"version,omitempty"`
}

// LabSpec defines the desired state of Lab.
// +kubebuilder:validation:XValidation:rule="has(self.catalogRef) ? (!has(self.source) && !has(self.extension)) : (has(self.source) && has(self.extension))",message="set exactly one of catalogRef, or both source and extension"
type LabSpec struct {
	// Enabled turns the lab on (child InstallAIExtension exists) or off.
	Enabled bool `json:"enabled"`
	// +optional
	DisplayName string `json:"displayName,omitempty"`
	// +optional
	Description string `json:"description,omitempty"`
	// +kubebuilder:validation:Enum=experimental;preview
	// +optional
	Maturity string `json:"maturity,omitempty"`
	// CatalogRef takes source and extension from a LabCatalog entry.
	// +optional
	CatalogRef *LabCatalogRef `json:"catalogRef,omitempty"`
	// Source and Extension are set for a hand-made lab (no catalogRef).
	// +optional
	Source *ExtensionSource `json:"source,omitempty"`
	// +optional
	Extension *ExtensionConfig `json:"extension,omitempty"`
	// +optional
	UI *LabUI `json:"ui,omitempty"`
}

// LabStatus defines the observed state of Lab.
type LabStatus struct {
	// +optional
	Phase LabPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// ResolvedChartURL and ResolvedVersion are the effective chart reference.
	// +optional
	ResolvedChartURL string `json:"resolvedChartURL,omitempty"`
	// +optional
	ResolvedVersion string `json:"resolvedVersion,omitempty"`
	// UpdateAvailable is the version the catalog offers when it differs from the
	// installed one. Set spec.catalogRef.version to it to upgrade.
	// +optional
	UpdateAvailable string `json:"updateAvailable,omitempty"`
	// ExtensionRef is the name of the child InstallAIExtension.
	// +optional
	ExtensionRef string `json:"extensionRef,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=aiflab
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Lab is an optional AIF extension that admins can enable or disable.
type Lab struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec LabSpec `json:"spec"`
	// +optional
	Status LabStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// LabList contains a list of Lab.
type LabList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Lab `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Lab{}, &LabList{})
}
