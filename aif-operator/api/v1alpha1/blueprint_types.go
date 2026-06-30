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
	BlueprintNameLabel    = "ai-factory.suse.com/blueprint-name"
	BlueprintVersionLabel = "ai-factory.suse.com/blueprint-version"
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

// ComponentContentType identifies the deployment format of a blueprint component.
// v2 preview - not yet functional in v1.
// +kubebuilder:validation:Enum=Helm;Kustomize;Manifests;Git
type ComponentContentType string

const (
	ComponentContentTypeHelm      ComponentContentType = "Helm"
	ComponentContentTypeKustomize ComponentContentType = "Kustomize"
	ComponentContentTypeManifests ComponentContentType = "Manifests"
	ComponentContentTypeGit       ComponentContentType = "Git"
)

// KustomizeSource points to a Kustomize overlay directory.
// v2 preview - not yet functional in v1.
type KustomizeSource struct {
	// Path to the directory containing kustomization.yaml
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`
	// Optional overlay names to apply
	// +optional
	Overlays []string `json:"overlays,omitempty"`
}

// ManifestSource points to raw Kubernetes YAML manifests.
// v2 preview - not yet functional in v1.
type ManifestSource struct {
	// Path to the directory containing manifest files
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`
	// Optional specific files to include (defaults to all *.yaml/*.yml)
	// +optional
	Files []string `json:"files,omitempty"`
}

// BlueprintGitSource points to content in a Git repository for blueprint components.
// v2 preview - not yet functional in v1.
type BlueprintGitSource struct {
	// Repository URL (https:// or git://)
	// +kubebuilder:validation:MinLength=1
	RepoURL string `json:"repoURL"`
	// Git revision (branch, tag, or commit SHA)
	// +optional
	Revision string `json:"revision,omitempty"`
	// Path within the repository
	// +optional
	Path string `json:"path,omitempty"`
}

// BlueprintInput defines a user-configurable parameter.
// v2 preview - not yet functional in v1.
type BlueprintInput struct {
	// Name of the input (referenced in ValuesFromInputs)
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z][a-zA-Z0-9]*$`
	Name string `json:"name"`

	// UI label
	// +kubebuilder:validation:MinLength=1
	Label string `json:"label"`

	// Description shown in UI
	// +optional
	Description string `json:"description,omitempty"`

	// Input type (string, number, boolean, array, object)
	// +kubebuilder:validation:Enum=string;number;boolean;array;object
	Type string `json:"type"`

	// Whether this input is required
	// +optional
	Required bool `json:"required,omitempty"`

	// Default value (JSON-encoded)
	// +optional
	Default *apixv1.JSON `json:"default,omitempty"`

	// Example value for documentation
	// +optional
	Example string `json:"example,omitempty"`

	// Allowed values (for enum-style inputs)
	// +optional
	Enum []string `json:"enum,omitempty"`
}

// InputMapping maps a blueprint input to a Helm value path.
// v2 preview - not yet functional in v1.
type InputMapping struct {
	// Input name (must match a BlueprintInput.Name)
	// +kubebuilder:validation:MinLength=1
	Input string `json:"input"`

	// JSONPath where to inject the value (e.g., "model.name")
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`

	// Optional transformation (e.g., "toString", "toNumber")
	// +optional
	Transform string `json:"transform,omitempty"`
}

// BlueprintOutput defines a value to extract after deployment.
// v2 preview - not yet functional in v1.
type BlueprintOutput struct {
	// Output name
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// UI label
	// +kubebuilder:validation:MinLength=1
	Label string `json:"label"`

	// Description
	// +optional
	Description string `json:"description,omitempty"`

	// Output type (url, string, number, boolean)
	// +kubebuilder:validation:Enum=url;string;number;boolean
	Type string `json:"type"`

	// How to extract the value
	ValueFrom OutputValueSource `json:"valueFrom"`
}

// OutputValueSource defines where to extract an output value.
// v2 preview - not yet functional in v1.
type OutputValueSource struct {
	// Resource to query
	// +optional
	Resource *ResourceQuery `json:"resource,omitempty"`

	// Static value (alternative to Resource)
	// +optional
	Static *apixv1.JSON `json:"static,omitempty"`
}

// ResourceQuery extracts a value from a deployed Kubernetes resource.
// v2 preview - not yet functional in v1.
type ResourceQuery struct {
	// Resource kind (e.g., "Ingress", "Service")
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Resource name
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Optional namespace (defaults to component's target namespace)
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// JSONPath to extract (e.g., ".spec.rules[0].host")
	// +kubebuilder:validation:MinLength=1
	JSONPath string `json:"jsonPath"`
}

// BlueprintValidation defines pre-flight validation rules.
// v2 preview - not yet functional in v1.
type BlueprintValidation struct {
	// CEL validation rules
	// +optional
	Rules []ValidationRule `json:"rules,omitempty"`
}

// ValidationRule is a single CEL validation expression.
// v2 preview - not yet functional in v1.
type ValidationRule struct {
	// Rule name (for error reporting)
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// CEL expression that must evaluate to true
	// Context: inputs = map of input values
	// Example: "inputs.modelName != ''"
	// +kubebuilder:validation:MinLength=1
	Expression string `json:"expression"`

	// Error message when rule fails
	// +kubebuilder:validation:MinLength=1
	Message string `json:"message"`
}

// RequiredSecret defines a secret that must exist before install.
// v2 preview - not yet functional in v1.
type RequiredSecret struct {
	// Secret name
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Description of what the secret is for
	// +optional
	Description string `json:"description,omitempty"`

	// Required keys in the secret
	// +optional
	Keys []string `json:"keys,omitempty"`
}

// BlueprintRequirements defines cluster prerequisites.
// v2 preview - not yet functional in v1.
type BlueprintRequirements struct {
	// Minimum Kubernetes version
	// +optional
	Kubernetes *KubernetesRequirement `json:"kubernetes,omitempty"`

	// Required cluster capabilities
	// +optional
	Capabilities []CapabilityRequirement `json:"capabilities,omitempty"`
}

// KubernetesRequirement defines Kubernetes version constraints.
// v2 preview - not yet functional in v1.
type KubernetesRequirement struct {
	// Minimum version (semver)
	// +optional
	MinVersion string `json:"minVersion,omitempty"`
}

// CapabilityRequirement defines a required cluster capability.
// v2 preview - not yet functional in v1.
type CapabilityRequirement struct {
	// Capability name (e.g., "nvidia-gpu", "cert-manager")
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Whether this capability is required
	// +optional
	Required bool `json:"required,omitempty"`

	// Description
	// +optional
	Description string `json:"description,omitempty"`
}

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

// BlueprintLifecycle defines install/upgrade/delete policies.
// v2 preview - not yet functional in v1.
type BlueprintLifecycle struct {
	// Install behavior
	// +optional
	Install *LifecycleInstall `json:"install,omitempty"`

	// Upgrade behavior
	// +optional
	Upgrade *LifecycleUpgrade `json:"upgrade,omitempty"`

	// Delete behavior
	// +optional
	Delete *LifecycleDelete `json:"delete,omitempty"`
}

// LifecycleInstall defines install behavior.
// v2 preview - not yet functional in v1.
type LifecycleInstall struct {
	// Install strategy: "ordered" or "parallel"
	// +kubebuilder:validation:Enum=ordered;parallel
	// +optional
	Strategy string `json:"strategy,omitempty"`

	// Whether pre-flight checks are required
	// +optional
	PreflightRequired bool `json:"preflightRequired,omitempty"`
}

// LifecycleUpgrade defines upgrade behavior.
// v2 preview - not yet functional in v1.
type LifecycleUpgrade struct {
	// Upgrade strategy: "safe" or "force"
	// +kubebuilder:validation:Enum=safe;force
	// +optional
	Strategy string `json:"strategy,omitempty"`

	// Whether manual approval is required
	// +optional
	RequiresApproval bool `json:"requiresApproval,omitempty"`
}

// LifecycleDelete defines delete behavior.
// v2 preview - not yet functional in v1.
type LifecycleDelete struct {
	// Resources to retain on delete
	// +optional
	RetainResources []RetainResource `json:"retainResources,omitempty"`
}

// RetainResource defines a resource type to keep on delete.
// v2 preview - not yet functional in v1.
type RetainResource struct {
	// Resource kind
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Reason for retention
	// +optional
	Reason string `json:"reason,omitempty"`
}

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

	// === v2 FIELDS (preview - not yet functional in v1) ===

	// Type is the content type discriminator (defaults to Helm for backward compat).
	// v2 preview - not yet functional in v1.
	// +kubebuilder:default=Helm
	// +kubebuilder:validation:Enum=Helm;Kustomize;Manifests;Git
	// +optional
	Type ComponentContentType `json:"type,omitempty"`

	// Name for this component (used in dependsOn references).
	// Defaults to chartName if omitted.
	// v2 preview - not yet functional in v1.
	// +optional
	Name string `json:"name,omitempty"`

	// Kustomize source (used when Type=Kustomize).
	// v2 preview - not yet functional in v1.
	// +optional
	Kustomize *KustomizeSource `json:"kustomize,omitempty"`

	// Manifests source (used when Type=Manifests).
	// v2 preview - not yet functional in v1.
	// +optional
	Manifests *ManifestSource `json:"manifests,omitempty"`

	// Git source (used when Type=Git).
	// v2 preview - not yet functional in v1.
	// +optional
	Git *BlueprintGitSource `json:"git,omitempty"`

	// DependsOn lists components that must be Ready before this one starts.
	// v2 preview - not yet functional in v1.
	// +optional
	DependsOn []string `json:"dependsOn,omitempty"`

	// ValuesFromInputs maps blueprint inputs to Helm values.
	// v2 preview - not yet functional in v1.
	// +optional
	ValuesFromInputs []InputMapping `json:"valuesFromInputs,omitempty"`

	// CEL validation enforces type-specific field requirements.
	// v2 preview - not yet functional in v1.
	// +kubebuilder:validation:XValidation:rule="self.type == 'Helm' || !has(self.type) ? (self.chartRepo != '' && self.chartName != '') : true",message="chartRepo and chartName required when type=Helm"
	// +kubebuilder:validation:XValidation:rule="self.type == 'Kustomize' ? has(self.kustomize) : true",message="kustomize field required when type=Kustomize"
	// +kubebuilder:validation:XValidation:rule="self.type == 'Manifests' ? has(self.manifests) : true",message="manifests field required when type=Manifests"
	// +kubebuilder:validation:XValidation:rule="self.type == 'Git' ? has(self.git) : true",message="git field required when type=Git"
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
