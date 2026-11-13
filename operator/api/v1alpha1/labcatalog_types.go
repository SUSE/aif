package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LabCatalogPhase is the state of a LabCatalog.
// +kubebuilder:validation:Enum=Ready;Error
type LabCatalogPhase string

const (
	LabCatalogPhaseReady LabCatalogPhase = "Ready"
	LabCatalogPhaseError LabCatalogPhase = "Error"
)

// LabCatalogBuiltin is the name of the catalog backed by the embedded catalog.json.
const LabCatalogBuiltin = "builtin"

// LabCatalogEntry is a catalog item: Lab.spec minus enabled, plus name.
type LabCatalogEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	// +kubebuilder:validation:Enum=experimental;preview
	// +optional
	Maturity  string          `json:"maturity,omitempty"`
	Source    ExtensionSource `json:"source"`
	Extension ExtensionConfig `json:"extension"`
	// +optional
	UI *LabUI `json:"ui,omitempty"`
}

// LabCatalogSpec defines the desired state of LabCatalog.
type LabCatalogSpec struct {
	// URL of the catalog JSON document (array of entries). https only; empty only for "builtin".
	// +kubebuilder:validation:Pattern=`^(https://.+)?$`
	// +optional
	URL string `json:"url,omitempty"`
	// +optional
	DisplayName string `json:"displayName,omitempty"`
}

// LabCatalogStatus defines the observed state of LabCatalog.
type LabCatalogStatus struct {
	// +optional
	Phase LabCatalogPhase `json:"phase,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	LastFetched *metav1.Time `json:"lastFetched,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Skipped counts entries dropped (invalid, duplicate, or failing name rules).
	// +optional
	Skipped int `json:"skipped,omitempty"`
	// Entries are the validated catalog entries. Bounded so the API server's CEL
	// cost estimate for the rules inside each entry stays within budget.
	// +kubebuilder:validation:MaxItems=100
	// +optional
	Entries []LabCatalogEntry `json:"entries,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=aiflabcat
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.spec.url`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="(has(self.spec.url) && self.spec.url != '') || self.metadata.name == 'builtin'",message="spec.url may be empty only for the builtin catalog"

// LabCatalog is a source of lab entries (the builtin one or an external HTTPS document).
type LabCatalog struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec LabCatalogSpec `json:"spec"`
	// +optional
	Status LabCatalogStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// LabCatalogList contains a list of LabCatalog.
type LabCatalogList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LabCatalog `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LabCatalog{}, &LabCatalogList{})
}
