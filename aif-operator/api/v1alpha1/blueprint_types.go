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

const (
	BlueprintNameLabel    = "ai-platform.suse.com/blueprint-name"
	BlueprintVersionLabel = "ai-platform.suse.com/blueprint-version"
)

// ComponentVendor selects the secret-injection profile for a Blueprint
// component. "suse" preserves the historical combined-secret + global.imagePullSecrets
// behavior. "nvidia" creates ngc-secret + ngc-api in the target namespace
// and writes both common pull-secret value paths.
// +kubebuilder:validation:Enum=suse;nvidia
type ComponentVendor string

const (
	ComponentVendorSUSE   ComponentVendor = "suse"
	ComponentVendorNvidia ComponentVendor = "nvidia"
)

// BlueprintOrigin identifies where a blueprint came from.
// Named "Origin" (not "Source") to avoid collision with the existing
// BlueprintSource struct in aiworkload_types.go, which is a reference type.
// The user-visible field name remains "source" via the JSON tag.
// +kubebuilder:validation:Enum=SUSE;Nvidia;Custom
type BlueprintOrigin string

const (
	BlueprintOriginSUSE   BlueprintOrigin = "SUSE"
	BlueprintOriginNvidia BlueprintOrigin = "Nvidia"
	BlueprintOriginCustom BlueprintOrigin = "Custom"
)

// BlueprintComponent defines one Helm chart in a Blueprint.
type BlueprintComponent struct {
	// ChartRepo is the Rancher ClusterRepo name.
	// +kubebuilder:validation:MinLength=1
	ChartRepo string `json:"chartRepo"`
	// ChartName is the Helm chart name.
	// +kubebuilder:validation:MinLength=1
	ChartName string `json:"chartName"`
	// ChartVersion is the semver chart version.
	// +kubebuilder:validation:MinLength=1
	ChartVersion string `json:"chartVersion"`
	// Vendor selects the secret-injection profile. Defaults to "suse" so
	// existing blueprints behave identically after CRD upgrade.
	// +kubebuilder:default=suse
	// +optional
	Vendor ComponentVendor `json:"vendor,omitempty"`
	// Values are the Helm values for this component.
	// +optional
	Values *apixv1.JSON `json:"values,omitempty"`
	// TargetNamespace optionally pins this component to a fixed namespace.
	// When empty, the AIWorkload's targetNamespace (from the install wizard) is used.
	// Must be a valid DNS-1123 label (lowercase alphanumerics and '-', starting
	// and ending with an alphanumeric, max 63 chars).
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	TargetNamespace string `json:"targetNamespace,omitempty"`
}

// BlueprintSpec defines the desired state of a Blueprint version.
type BlueprintSpec struct {
	// DisplayName is the human-readable name shared across all versions.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	// Version is the semver version string of this blueprint.
	// +kubebuilder:validation:Pattern=`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[a-zA-Z0-9.-]+)?(\+[a-zA-Z0-9.-]+)?$`
	Version string `json:"version"`
	// Description is an optional human-readable description.
	// +optional
	Description string `json:"description,omitempty"`
	// Source identifies where this blueprint came from (SUSE, Nvidia, or Custom).
	// To leave the source unset, omit the field entirely; the enum does not
	// include the empty string, so setting `source: ""` will fail admission.
	// +optional
	Source BlueprintOrigin `json:"source,omitempty"`
	// Deprecated marks this blueprint version as deprecated.
	// +optional
	Deprecated bool `json:"deprecated,omitempty"`
	// Components are the Helm charts included in this blueprint.
	// +kubebuilder:validation:MinItems=1
	Components []BlueprintComponent `json:"components"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=bp
// +kubebuilder:printcolumn:name="Display Name",type=string,JSONPath=`.spec.displayName`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Blueprint is the Schema for the blueprints API.
type Blueprint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Spec BlueprintSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// BlueprintList contains a list of Blueprint.
type BlueprintList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Blueprint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Blueprint{}, &BlueprintList{})
}
