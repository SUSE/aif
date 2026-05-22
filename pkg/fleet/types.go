// Package fleet wraps the rancher/fleet API as a port + adapter so the
// Workload controller can delegate deployStrategy=helm flows to Fleet
// without depending on the Fleet SDK directly.
//
// CLAUDE.md layering rule: this package MUST NOT import api/v1alpha1.
// All domain types live here; CR↔domain translation belongs in the
// consuming package (pkg/workload/conversions.go).
package fleet

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

// FleetSettings is the EngineSettings push target for this package.
// Currently empty: the bundle engine speaks to the local apiserver via
// the injected client.Client (Fleet manager runs in-cluster), so no
// credentials need to land. The struct + UpdateSettings method exist
// for symmetry with helm.EngineSettings / source_collection.EngineSettings
// so future downstream-cluster credential pushes can land without
// changing the engine interface.
type FleetSettings struct{}

// --- GitRepo deployment-type stubs. The parallel GitRepo engine fills
// these in via gitrepo_engine.go; they live here so that story can land
// without editing this file.

// GitRepoDeploymentSpec is the input to FleetGitRepoEngine.Apply.
type GitRepoDeploymentSpec struct{}

// GitRepoObservedStatus is the Fleet GitRepo status mirror.
type GitRepoObservedStatus struct{}
