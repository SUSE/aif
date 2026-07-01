/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AIWorkloadSourceType identifies what is being deployed.
// +kubebuilder:validation:Enum=App;Blueprint
type AIWorkloadSourceType string

const (
	AIWorkloadSourceApp       AIWorkloadSourceType = "App"
	AIWorkloadSourceBlueprint AIWorkloadSourceType = "Blueprint"
)

// AIWorkloadDeployStrategy identifies how the workload is deployed.
// +kubebuilder:validation:Enum=Helm;FleetBundle;GitOps
type AIWorkloadDeployStrategy string

const (
	AIWorkloadDeployHelm        AIWorkloadDeployStrategy = "Helm"
	AIWorkloadDeployFleetBundle AIWorkloadDeployStrategy = "FleetBundle"
	AIWorkloadDeployGitOps      AIWorkloadDeployStrategy = "GitOps"
)

// AIWorkloadPhase is the overall workload phase.
// +kubebuilder:validation:Enum=Pending;Running;Degraded;Failed;Unknown
type AIWorkloadPhase string

const (
	AIWorkloadPhasePending  AIWorkloadPhase = "Pending"
	AIWorkloadPhaseRunning  AIWorkloadPhase = "Running"
	AIWorkloadPhaseDegraded AIWorkloadPhase = "Degraded"
	AIWorkloadPhaseFailed   AIWorkloadPhase = "Failed"
	AIWorkloadPhaseUnknown  AIWorkloadPhase = "Unknown"
)

// AIWorkloadClusterPhase is the per-cluster deployment phase.
// +kubebuilder:validation:Enum=Running;Failed;Pending
type AIWorkloadClusterPhase string

const (
	AIWorkloadClusterPhaseRunning AIWorkloadClusterPhase = "Running"
	AIWorkloadClusterPhaseFailed  AIWorkloadClusterPhase = "Failed"
	AIWorkloadClusterPhasePending AIWorkloadClusterPhase = "Pending"
)

// AppSource contains chart information for App-sourced workloads.
type AppSource struct {
	// ChartRepo is the Rancher ClusterRepo name.
	// +kubebuilder:validation:MinLength=1
	ChartRepo string `json:"chartRepo"`
	// ChartName is the Helm chart name.
	// +kubebuilder:validation:MinLength=1
	ChartName string `json:"chartName"`
	// ChartVersion is the semver chart version.
	// +kubebuilder:validation:MinLength=1
	ChartVersion string `json:"chartVersion"`
	// Release is the Helm release name.
	// +kubebuilder:validation:MinLength=1
	Release string `json:"release"`
	// Vendor selects the secret-injection profile. Defaults to "suse" so
	// existing App-sourced workloads behave identically after CRD upgrade.
	// The UI is expected to populate this from the chart's repo URL (e.g.
	// "nvidia" for charts pulled from helm.ngc.nvidia.com), mirroring how
	// BlueprintComponent.Vendor is filled at Blueprint-build time.
	// +kubebuilder:default=suse
	// +optional
	Vendor ComponentVendor `json:"vendor,omitempty"`
}

// BlueprintSource references a Blueprint CR (Epic 2).
type BlueprintSource struct {
	// Name is the blueprint family slug (label ai-factory.suse.com/blueprint-name).
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Version is the semver version of the blueprint to use.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}

// AIWorkloadSource is a discriminated union describing what is being deployed.
type AIWorkloadSource struct {
	// SourceType is the source discriminator.
	SourceType AIWorkloadSourceType `json:"sourceType"`
	// App is populated when SourceType=App.
	// +optional
	App *AppSource `json:"app,omitempty"`
	// Blueprint is populated when SourceType=Blueprint (Epic 2).
	// +optional
	Blueprint *BlueprintSource `json:"blueprint,omitempty"`
}

// ComponentValueOverride holds per-component Helm value overrides.
type ComponentValueOverride struct {
	// ComponentName matches source.app.chartName (App) or a Blueprint component name.
	// +kubebuilder:validation:MinLength=1
	ComponentName string `json:"componentName"`
	// Values are the Helm values for this component.
	// +optional
	Values *apixv1.JSON `json:"values,omitempty"`
}

// AIWorkloadSpec defines the desired state of AIWorkload.
type AIWorkloadSpec struct {
	// DisplayName is the user-provided workload display name.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	// Source describes what is being deployed.
	Source AIWorkloadSource `json:"source"`
	// TargetNamespace is the namespace on target clusters.
	// +kubebuilder:validation:MinLength=1
	TargetNamespace string `json:"targetNamespace"`
	// TargetClusters are the Rancher cluster IDs to deploy to.
	// +listType=set
	// +optional
	TargetClusters []string `json:"targetClusters,omitempty"`
	// DeployStrategy is the deployment method.
	// +kubebuilder:default=Helm
	// +optional
	DeployStrategy AIWorkloadDeployStrategy `json:"deployStrategy,omitempty"`
	// ComponentValues holds per-component Helm value overrides.
	// +optional
	ComponentValues []ComponentValueOverride `json:"componentValues,omitempty"`
	// FleetBundleNames are the Fleet Bundle CR names for this workload.
	// App-sourced workloads have exactly one entry; Blueprint-sourced workloads
	// have one entry per component.
	// +optional
	FleetBundleNames []string `json:"fleetBundleNames,omitempty"`
}

// AIWorkloadClusterStatus tracks per-cluster deployment outcome.
type AIWorkloadClusterStatus struct {
	// ClusterID is the Rancher cluster ID.
	// +kubebuilder:validation:MinLength=1
	ClusterID string `json:"clusterId"`
	// Phase is the deployment outcome for this cluster.
	// +optional
	Phase AIWorkloadClusterPhase `json:"phase,omitempty"`
	// Message provides additional detail.
	// +optional
	Message string `json:"message,omitempty"`
}

// PullSecretDelivery groups operator-delivered pull-secret names by the
// target namespace they were written into. Blueprint workloads where each
// component opts in to its own BlueprintComponent.TargetNamespace produce
// one entry per distinct namespace; single-namespace blueprints and
// App-sourced workloads produce exactly one entry.
type PullSecretDelivery struct {
	// Namespace is the target namespace the listed secrets were written
	// into on the operator's local cluster (and where Fleet Bundles ship
	// matching copies on each downstream cluster).
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// Names is the set of dockerconfigjson / Opaque Secret names the
	// injectors created in that namespace.
	// +listType=set
	Names []string `json:"names"`
}

// AIWorkloadStatus defines the observed state of AIWorkload.
type AIWorkloadStatus struct {
	// Phase is the overall workload phase derived from clusterStatuses.
	// +optional
	Phase AIWorkloadPhase `json:"phase,omitempty"`
	// ClusterStatuses tracks per-cluster deployment outcome.
	// +optional
	ClusterStatuses []AIWorkloadClusterStatus `json:"clusterStatuses,omitempty"`
	// PullSecretDeliveries groups operator-delivered pull-secret names by
	// the namespace they were written into. Replaces the prior flat
	// PullSecretNames field so blueprints with components in different
	// namespaces (BlueprintComponent.TargetNamespace) deliver correctly to
	// each. Used by reconcilePullSecrets to merge into SAs per namespace,
	// by deliverPullSecrets to emit one Fleet Bundle per (cluster, namespace),
	// and by the finalizer to prune SA references on delete.
	// +optional
	PullSecretDeliveries []PullSecretDelivery `json:"pullSecretDeliveries,omitempty"`
	// Conditions represent the latest observations of the AIWorkload state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the generation observed by the controller.
	// +kubebuilder:validation:Minimum=0
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=aiwl
// +kubebuilder:printcolumn:name="Display Name",type=string,JSONPath=`.spec.displayName`
// +kubebuilder:printcolumn:name="Strategy",type=string,JSONPath=`.spec.deployStrategy`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AIWorkload is the Schema for the aiworkloads API.
type AIWorkload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Spec AIWorkloadSpec `json:"spec,omitempty"`
	// +optional
	Status AIWorkloadStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AIWorkloadList contains a list of AIWorkload.
type AIWorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AIWorkload `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AIWorkload{}, &AIWorkloadList{})
}
