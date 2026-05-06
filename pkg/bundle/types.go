package bundle

import (
	aifv1 "github.com/SUSE/aif/api/v1alpha1"
)

// Bundle is the domain model for Bundle business logic
type Bundle struct {
	// Identity
	Namespace string
	Name      string

	// Workflow state
	Phase      aifv1.BundlePhase
	Submission *aifv1.SubmissionStatus
	Review     *aifv1.ReviewStatus

	// Content
	Title           string
	TargetBlueprint string
	UseCase         string
	Components      []aifv1.ComponentRef
	ValueOverrides  map[string]string

	// Reconciliation control
	Paused bool

	// Metadata
	Description string
	Authors     []string

	// History
	PublishedVersions []aifv1.PublishedVersionRef
}
