package bundle

import (
	aifv1 "github.com/SUSE/aif/api/v1alpha1"
)

// BundleFromCR converts a Bundle CR to the domain model.
// Title and Paused are required Bundle.Spec fields; previously dropped on
// roundtrip (caught by critical-review audit finding H2).
func BundleFromCR(cr *aifv1.Bundle) Bundle {
	return Bundle{
		Namespace:         cr.Namespace,
		Name:              cr.Name,
		Phase:             cr.Status.Phase,
		Submission:        cr.Status.Submission,
		Review:            cr.Status.Review,
		Title:             cr.Spec.Title,
		TargetBlueprint:   cr.Spec.TargetBlueprint,
		UseCase:           cr.Spec.UseCase,
		Components:        cr.Spec.Components,
		ValueOverrides:    cr.Spec.ValueOverrides,
		Paused:            cr.Spec.Paused,
		Description:       cr.Spec.Description,
		Authors:           cr.Spec.Authors,
		PublishedVersions: cr.Status.PublishedVersions,
	}
}

// BundleToCR updates a Bundle CR's spec and status from the domain model.
// Preserves metadata (ResourceVersion, Generation, etc.).
// Not used in P1-1, but included for Phase 3.
func BundleToCR(b Bundle, cr *aifv1.Bundle) {
	// Update spec
	cr.Spec.Title = b.Title
	cr.Spec.TargetBlueprint = b.TargetBlueprint
	cr.Spec.UseCase = b.UseCase
	cr.Spec.Components = b.Components
	cr.Spec.ValueOverrides = b.ValueOverrides
	cr.Spec.Paused = b.Paused
	cr.Spec.Description = b.Description
	cr.Spec.Authors = b.Authors

	// Update status
	cr.Status.Phase = b.Phase
	cr.Status.Submission = b.Submission
	cr.Status.Review = b.Review
	cr.Status.PublishedVersions = b.PublishedVersions
}
