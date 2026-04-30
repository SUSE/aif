package v1alpha1

// ComponentKind indicates whether a component is an App or Blueprint
type ComponentKind string

const (
	ComponentKindApp       ComponentKind = "App"
	ComponentKindBlueprint ComponentKind = "Blueprint"
)

// ComponentRef references an App or Blueprint component
// TODO: Add cross-field validation to ensure exactly one field is set per Kind value (requires CEL/webhook)
type ComponentRef struct {
	// Name is the local handle used as a key for valueOverrides
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Kind is the component type
	// +kubebuilder:validation:Enum=App;Blueprint
	Kind ComponentKind `json:"kind"`

	// App reference, populated when Kind=App
	// +optional
	App *AppRef `json:"app,omitempty"`

	// Blueprint reference, populated when Kind=Blueprint
	// +optional
	Blueprint *BlueprintRef `json:"blueprint,omitempty"`
}

// AppRef references a Helm chart from a repository
type AppRef struct {
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

// BlueprintRef references a Blueprint by name and version
type BlueprintRef struct {
	// Name is the Blueprint lineage name
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Version is the semantic version
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}
