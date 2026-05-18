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
	}
	if w.Spec.Replicas != nil {
		req.Replicas = *w.Spec.Replicas
	}
	for _, prior := range w.Status.ComponentReleases {
		req.Previous = append(req.Previous, ComponentRelease{
			Name:        prior.Name,
			ReleaseName: prior.ReleaseName,
			Status:      prior.Status,
			Revision:    prior.Revision,
		})
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

// ApplyDeployResult writes the domain DeployResult back into the CR's
// status fields. Does NOT touch unrelated fields (Conditions, Replicas,
// DeploymentHistory) — the reconciler manages Conditions separately via
// meta.SetStatusCondition. P5-2 will own Replicas/ReadyReplicas writes.
func ApplyDeployResult(w *aifv1.Workload, r DeployResult) {
	w.Status.Phase = aifv1.WorkloadPhase(r.Phase)
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

// blueprintCRName encodes (lineage, version) as the CR's metadata.name.
// Blueprint CRs are cluster-scoped, immutable per version, named by joining
// lineage and version with a hyphen.
func blueprintCRName(name, version string) string {
	return name + "-" + version
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
