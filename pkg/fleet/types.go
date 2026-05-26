// Package fleet wraps the rancher/fleet API as a port + adapter so the
// Workload controller can delegate deployStrategy=helm flows to Fleet
// without depending on the Fleet SDK directly.
//
// CLAUDE.md layering rule: this package MUST NOT import api/v1alpha1.
// All domain types live here; CR↔domain translation belongs in the
// consuming package (pkg/workload/conversions.go).
package fleet

import (
	"github.com/SUSE/aif/pkg/git"
)

// BundleDeploymentSpec is the input to FleetBundleEngine.Apply.
// All fields are framework-agnostic — the adapter translates to a
// fleet.cattle.io/v1alpha1 Bundle CR.
type BundleDeploymentSpec struct {
	// WorkloadID is the Workload's metadata.name. Drives the Fleet Bundle
	// name via fleetBundleName(ns, id).
	WorkloadID string

	// WorkloadNS is the namespace where the Fleet Bundle is created
	// (always the workload's own namespace).
	WorkloadNS string

	// TargetClusters lists Fleet Cluster.metadata.name values. One
	// BundleTarget entry is produced per name. Order is preserved.
	TargetClusters []string

	// Components carry already-merged Helm values (layers 1-5 done by
	// helm.ValueRenderer.Render before this struct is built). The first
	// component drives spec.helm.{chart, values}; the rest land in
	// spec.helm.valuesFiles[] backed by spec.resources[] entries.
	Components []ComponentBundle

	// PullSecretData is the raw .dockerconfigjson from suse-registry-creds.
	// Embedded as a Secret manifest in spec.resources[] so Fleet ships it
	// to every target cluster. Empty → no Secret resource emitted.
	PullSecretData []byte

	// Owner is the Workload CR that should own the Fleet Bundle
	// (cascade-delete via OwnerReferences).
	Owner OwnerRef
}

// ComponentBundle is one Helm component contributing to a Fleet Bundle.
type ComponentBundle struct {
	// Name is the logical component name (matches Blueprint component
	// names; used as the resource-file basename).
	Name string

	// ChartRef is the OCI reference, e.g.
	// "oci://registry.suse.com/ai/charts/nim-llm:1.2.0".
	ChartRef string

	// Values is the result of helm.ValueRenderer.Render — layers 1-5
	// already merged. Marshaled into the Fleet Bundle as JSON.
	Values map[string]any
}

// OwnerRef captures the metadata needed to construct a
// metav1.OwnerReference inside the adapter without importing the
// Workload CR type into pkg/fleet.
type OwnerRef struct {
	APIVersion string
	Kind       string
	Name       string
	UID        string
	Controller bool
}

// BundleObservedStatus is what FleetBundleEngine.Apply returns so the
// caller can mirror Fleet status into Workload.status without a second
// round-trip to the apiserver.
type BundleObservedStatus struct {
	// PerCluster has one entry per requested target cluster. If Fleet
	// has not yet created a BundleDeployment for a target, FleetState
	// is empty (the caller's MapFleetStateToPhase treats that as Deploying).
	PerCluster []ClusterDeploymentObserved
}

// ClusterDeploymentObserved is the raw Fleet state seen for one target.
// Translated to workload.ClusterPhase by the caller, NOT here, to keep
// pkg/fleet free of workload-domain concepts.
type ClusterDeploymentObserved struct {
	ClusterName string
	// FleetState is BundleDeployment.status.display.state verbatim.
	FleetState string
	// ConnectionError is true when status indicates downstream
	// connectivity loss (typed condition reason — never string-matched).
	ConnectionError bool
}

// FleetSettings carries the per-engine credentials and target git
// repo/branch. Pushed by SettingsReconciler.applySettingsToEngines via
// engine_bus.projectFleet. The bundle engine ignores all fields today
// (it speaks to the local apiserver); the GitRepo engine forwards
// GitRepoURL/GitBranch/GitAuth into pkg/git.EngineSettings on every
// UpdateSettings.
//
// P5-4b will populate these fields from controller.SettingsSnapshot
// (the projectFleet call returns an empty value today; until then the
// engine fails closed with git.ErrNotConfigured on Apply).
type FleetSettings struct {
	GitRepoURL string
	GitBranch  string
	GitAuth    git.GitAuth
}

// --- GitRepo deployment-type. Mirrors BundleDeploymentSpec field-for-field
// (same Components/PullSecret/Owner shape) so the workload-side phase
// aggregation (pkg/workload/phase.go) works unchanged.

// GitRepoDeploymentSpec is the input to FleetGitRepoEngine.Apply.
type GitRepoDeploymentSpec struct {
	WorkloadID     string
	WorkloadNS     string
	TargetClusters []string
	Components     []ComponentBundle
	PullSecretData []byte
	Owner          OwnerRef
}

// GitRepoObservedStatus is the mirrored Fleet state. Reuses
// ClusterDeploymentObserved so workload-side translation is uniform.
type GitRepoObservedStatus struct {
	PerCluster []ClusterDeploymentObserved
}
