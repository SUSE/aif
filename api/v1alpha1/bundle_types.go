package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BundlePhase represents the current phase of a Bundle
type BundlePhase string

const (
	BundlePhaseDraft            BundlePhase = "Draft"
	BundlePhaseSubmitted        BundlePhase = "Submitted"
	BundlePhaseChangesRequested BundlePhase = "ChangesRequested"
)

// TestDeployResult represents the outcome of a test deployment
type TestDeployResult string

const (
	TestDeployResultSuccess TestDeployResult = "Success"
	TestDeployResultFailed  TestDeployResult = "Failed"
	TestDeployResultRunning TestDeployResult = "Running"
)

// BundleSpec defines the desired state of Bundle
type BundleSpec struct {
	// Title is the human-readable name of the Bundle
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	Title string `json:"title"`

	// Description is free-text description of the Bundle
	// +optional
	// +kubebuilder:validation:MaxLength=1024
	Description string `json:"description,omitempty"`

	// TargetBlueprint is the Blueprint lineage name this Bundle publishes into
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	TargetBlueprint string `json:"targetBlueprint"`

	// UseCase categorizes the Bundle's purpose
	// +kubebuilder:validation:Enum=rag;vision;fine-tuning;inference;other
	UseCase string `json:"useCase"`

	// Authors is a list of author display names
	// +optional
	Authors []string `json:"authors,omitempty"`

	// Components are the Apps and/or Blueprints to include
	// +kubebuilder:validation:MinItems=1
	Components []ComponentRef `json:"components"`

	// ValueOverrides contains per-component Helm values YAML, keyed by ComponentRef.name
	// +optional
	ValueOverrides map[string]string `json:"valueOverrides,omitempty"`

	// Paused suspends reconciliation when true
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// BundleStatus defines the observed state of Bundle
type BundleStatus struct {
	// Phase is the current lifecycle phase
	// +kubebuilder:validation:Enum=Draft;Submitted;ChangesRequested
	// +optional
	Phase BundlePhase `json:"phase,omitempty"`

	// Submission is set when phase != Draft
	// +optional
	Submission *SubmissionStatus `json:"submission,omitempty"`

	// Review is set when phase == ChangesRequested
	// +optional
	Review *ReviewStatus `json:"review,omitempty"`

	// TestDeploys contains recent test deploy records (capped at 10)
	// +kubebuilder:validation:MaxItems=10
	// +optional
	TestDeploys []TestDeployRecord `json:"testDeploys,omitempty"`

	// PublishedVersions is the history of published Blueprint versions
	// +kubebuilder:validation:MaxItems=100
	// +optional
	PublishedVersions []PublishedVersionRef `json:"publishedVersions,omitempty"`

	// Conditions represent the latest available observations of the Bundle's state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// SubmissionStatus tracks Bundle submission details
type SubmissionStatus struct {
	// ProposedVersion is the semantic version proposed for publication
	ProposedVersion string `json:"proposedVersion"`

	// ChangeDescription describes what changed in this version
	// +optional
	ChangeDescription string `json:"changeDescription,omitempty"`

	// SubmittedBy is the Rancher username who submitted
	SubmittedBy string `json:"submittedBy"`

	// SubmittedAt is the submission timestamp
	SubmittedAt metav1.Time `json:"submittedAt"`

	// GenerationAtSubmit is the Bundle generation at submit time
	GenerationAtSubmit int64 `json:"generationAtSubmit"`
}

// ReviewStatus tracks Bundle review details
type ReviewStatus struct {
	// ReviewerComment is the reviewer's feedback
	ReviewerComment string `json:"reviewerComment"`

	// ReviewedBy is the Rancher username who reviewed
	ReviewedBy string `json:"reviewedBy"`

	// ReviewedAt is the review timestamp
	ReviewedAt metav1.Time `json:"reviewedAt"`
}

// TestDeployRecord tracks a single test deployment
type TestDeployRecord struct {
	// WorkloadRef is the namespace/name of the test Workload
	WorkloadRef string `json:"workloadRef"`

	// TargetClusters are the cluster IDs where the test was deployed
	// +optional
	TargetClusters []string `json:"targetClusters,omitempty"`

	// StartedAt is when the test deploy started
	StartedAt metav1.Time `json:"startedAt"`

	// Result is the test outcome
	// +kubebuilder:validation:Enum=Success;Failed;Running
	Result TestDeployResult `json:"result"`
}

// PublishedVersionRef tracks a published Blueprint version
type PublishedVersionRef struct {
	// BlueprintName is the lineage name
	BlueprintName string `json:"blueprintName"`

	// Version is the semantic version
	Version string `json:"version"`

	// PublishedAt is when the version was published
	PublishedAt metav1.Time `json:"publishedAt"`

	// PublishedBy is the Rancher username who approved publication
	PublishedBy string `json:"publishedBy"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=bnd
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetBlueprint`
// +kubebuilder:printcolumn:name="Use Case",type=string,JSONPath=`.spec.useCase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Bundle is the Schema for the bundles API
type Bundle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BundleSpec   `json:"spec,omitempty"`
	Status BundleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BundleList contains a list of Bundle
type BundleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Bundle `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Bundle{}, &BundleList{})
}
