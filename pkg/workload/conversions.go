// Package workload — CR↔domain translation.
//
// This file is the canonical home for aifv1 imports in pkg/workload (per
// CLAUDE.md: only repository.go and conversions.go may import api/v1alpha1).
// All other files in this package speak in domain types defined in
// types.go.
package workload

import (
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
