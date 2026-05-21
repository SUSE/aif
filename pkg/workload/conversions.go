// Package workload — CR↔domain translation.
//
// This file is the canonical home for aifv1 imports in pkg/workload (per
// CLAUDE.md: only repository.go and conversions.go may import api/v1alpha1).
// All other files in this package speak in domain types defined in
// types.go.
package workload

import (
	"fmt"

	aifv1 "github.com/SUSE/aif/api/v1alpha1"
)

// WorkloadToDeployRequest projects an aifv1.Workload into the
// framework-agnostic DeployRequest the Deployer port consumes.
//
// Defaults applied:
//   - Replicas: nil → 1 (matches the +kubebuilder default)
//
// status.componentReleases is read into Previous so the deployer can
// detect drift orphans on subsequent reconciles.
func WorkloadToDeployRequest(w *aifv1.Workload) DeployRequest {
	req := DeployRequest{
		Namespace: w.Namespace,
		ID:        w.Name,
		SpecName:  w.Spec.Name,
		Replicas:  1,
		Overrides: w.Spec.ValueOverrides,
		Source:    sourceRefFromCR(w.Spec.Source),
		Previous:  ComponentReleasesFromCR(w.Status.ComponentReleases),
	}
	if w.Spec.Replicas != nil {
		req.Replicas = *w.Spec.Replicas
	}
	return req
}

func sourceRefFromCR(s aifv1.WorkloadSource) SourceRef {
	out := SourceRef{Kind: SourceKind(s.Kind)}
	if s.App != nil {
		out.App = &AppRef{Repo: s.App.Repo, Chart: s.App.Chart, Version: s.App.Version}
	}
	if s.Blueprint != nil {
		out.Blueprint = &BlueprintRef{Name: s.Blueprint.Name, Version: s.Blueprint.Version}
	}
	if s.BundleTest != nil {
		out.BundleTest = &BundleTestRef{
			Namespace:  s.BundleTest.Namespace,
			Name:       s.BundleTest.Name,
			Generation: s.BundleTest.Generation,
		}
	}
	return out
}

// ApplyDeployResult writes the deployer's per-component outcome and the
// observed Bundle generation back into the CR's status. Does NOT touch
// Phase: the controller computes Phase via RecomputePhase after this
// function returns. Does NOT touch Conditions/Replicas/DeploymentHistory.
// P5-2 will own Replicas/ReadyReplicas writes.
func ApplyDeployResult(w *aifv1.Workload, r DeployResult) {
	w.Status.ObservedBundleGeneration = r.ObservedBundleGeneration

	w.Status.ComponentReleases = nil
	for _, c := range r.Components {
		w.Status.ComponentReleases = append(w.Status.ComponentReleases, aifv1.ComponentReleaseStatus{
			Name:        c.Name,
			ReleaseName: c.ReleaseName,
			Status:      c.Status,
			Revision:    c.Revision,
		})
	}
}

// PhaseInputFromCR projects an *aifv1.Workload into the domain PhaseInput
// that RecomputePhase consumes. Defaults applied:
//   - DesiredReplicas: spec.replicas is nil → 0. The kubebuilder default of
//     1 normally fills spec.replicas at admission; this fallback covers
//     envtest paths where the defaulting webhook isn't installed.
//   - ReadyReplicas: status.readyReplicas, BUT — in the pre-P5-2 "no pod
//     informer wired" world — defaults to DesiredReplicas so rule 4
//     ("ready >= desired") fires and produces Running for healthy deploys.
//     P5-2 will replace this with the real informer-populated count.
//   - FailureThreshold: nil/zero → DefaultFailureThreshold. Handles envtest
//     paths without the defaulting webhook and older CRs that pre-date
//     the strategy.automaticRecovery field.
//
// The threshold lives at a nested path
// (spec.strategy.automaticRecovery.failureThreshold) with two nil checks;
// keeping the projection here lets phase.go stay aifv1-free.
func PhaseInputFromCR(w *aifv1.Workload) PhaseInput {
	in := PhaseInput{
		Components:           ComponentReleasesFromCR(w.Status.ComponentReleases),
		DesiredReplicas:      0,
		ReadyReplicas:        w.Status.ReadyReplicas,
		RecoveryFailureCount: w.Status.RecoveryFailureCount,
		FailureThreshold:     DefaultFailureThreshold,
		PriorPhase:           Phase(w.Status.Phase),
	}
	if w.Spec.Replicas != nil {
		in.DesiredReplicas = *w.Spec.Replicas
	}
	// Pre-P5-2 default: with no pod informer, status.readyReplicas is always 0,
	// which would force rule 4 to fail for any DesiredReplicas > 0. Synthesise
	// "ready == desired" so the success path is reachable. P5-2 replaces this
	// when the informer starts writing real counts.
	if in.ReadyReplicas == 0 {
		in.ReadyReplicas = in.DesiredReplicas
	}
	if w.Spec.Strategy != nil && w.Spec.Strategy.AutomaticRecovery != nil {
		// Enabled is a value type (no nil check beyond the parent pointers).
		// Defaults to false (matches kubebuilder default) when the nested
		// struct is absent. Keys the three branches of ARCHITECTURE.md §4.4
		// rule 2 (recovery off → Failed immediate; recovery on →
		// Degraded/RecoveryInProgress based on count vs threshold).
		in.AutomaticRecoveryEnabled = w.Spec.Strategy.AutomaticRecovery.Enabled
		if w.Spec.Strategy.AutomaticRecovery.FailureThreshold != nil &&
			*w.Spec.Strategy.AutomaticRecovery.FailureThreshold > 0 {
			in.FailureThreshold = *w.Spec.Strategy.AutomaticRecovery.FailureThreshold
		}
	}
	return in
}

// PhaseToCR converts a domain Phase to the wire-type aifv1.WorkloadPhase.
// Trivial cast; symbolic helper so callers don't sprinkle aifv1 casts
// throughout the controller.
func PhaseToCR(p Phase) aifv1.WorkloadPhase {
	return aifv1.WorkloadPhase(p)
}

// blueprintCRName encodes (lineage, version) as the CR's metadata.name.
// Blueprint CRs are cluster-scoped, immutable per version, named by joining
// lineage and version with a hyphen.
func blueprintCRName(name, version string) string {
	return name + "-" + version
}

// ComponentReleasesFromCR projects a slice of aifv1.ComponentReleaseStatus
// into the framework-agnostic ComponentRelease type. Used by both
// WorkloadToDeployRequest (for req.Previous) and the reconciler's
// finalizer block (for Teardown input).
func ComponentReleasesFromCR(crs []aifv1.ComponentReleaseStatus) []ComponentRelease {
	if len(crs) == 0 {
		return nil
	}
	out := make([]ComponentRelease, 0, len(crs))
	for _, c := range crs {
		out = append(out, ComponentRelease{
			Name:        c.Name,
			ReleaseName: c.ReleaseName,
			Status:      c.Status,
			Revision:    c.Revision,
		})
	}
	return out
}

// ComponentsFromBlueprintCR projects a Blueprint CR's components +
// per-component value overrides into the deployer's internal
// desiredComponent slice. Returns (nil, 0, ErrNestedBlueprintNotSupported)
// on first nested-Blueprint child, (nil, 0, ErrSourceNotResolved) on
// missing App ref. The int64 return is always 0 for Blueprints (kept
// for symmetry with ComponentsFromBundleCR).
func ComponentsFromBlueprintCR(bp *aifv1.Blueprint) ([]desiredComponent, int64, error) {
	return componentsFromCRComponents(bp.Spec.Components, bp.Spec.ValueOverrides)
}

// ComponentsFromBundleCR projects a Bundle CR's components + per-component
// value overrides + current generation into the deployer's internal
// projection. The Generation is recorded as observedGen for BundleTest
// drift detection.
func ComponentsFromBundleCR(b *aifv1.Bundle) ([]desiredComponent, int64, error) {
	components, _, err := componentsFromCRComponents(b.Spec.Components, b.Spec.ValueOverrides)
	if err != nil {
		return nil, 0, err
	}
	return components, b.Generation, nil
}

// componentsFromCRComponents translates aifv1.ComponentRef[] into the
// internal desiredComponent[]. Rejects nested Blueprints per P4-2 spec
// (recursive expansion is a future-story concern; sentinel surfaces as
// Ready=False Reason=UnsupportedComposition in the reconciler).
//
// Returns (components, observedGen=0, nil) on success.
// Returns ErrNestedBlueprintNotSupported on first nested-Blueprint child.
// Returns ErrSourceNotResolved on missing App ref.
//
// overrides may be nil; when present, overrides[componentName] is copied
// into desiredComponent.blueprintOverride.
func componentsFromCRComponents(refs []aifv1.ComponentRef, overrides map[string]string) ([]desiredComponent, int64, error) {
	out := make([]desiredComponent, 0, len(refs))
	for _, r := range refs {
		if r.Kind == aifv1.ComponentKindBlueprint {
			return nil, 0, fmt.Errorf("%w: child %q has Kind=Blueprint", ErrNestedBlueprintNotSupported, r.Name)
		}
		if r.App == nil {
			return nil, 0, fmt.Errorf("%w: child %q missing App ref", ErrSourceNotResolved, r.Name)
		}
		out = append(out, desiredComponent{
			name:              r.Name,
			repo:              r.App.Repo,
			chart:             r.App.Chart,
			version:           r.App.Version,
			blueprintOverride: overrides[r.Name],
		})
	}
	return out, 0, nil
}
