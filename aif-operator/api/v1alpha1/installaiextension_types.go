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

// ExtensionSourceKind indicates the source type for the UI extension assets.
type ExtensionSourceKind string

const (
	ExtensionSourceKindHelm ExtensionSourceKind = "Helm"
	ExtensionSourceKindGit  ExtensionSourceKind = "Git"
)

// InstallAIExtensionPhase represents the current installation phase.
type InstallAIExtensionPhase string

const (
	InstallAIExtensionPhasePending    InstallAIExtensionPhase = "Pending"
	InstallAIExtensionPhaseInstalling InstallAIExtensionPhase = "Installing"
	InstallAIExtensionPhaseInstalled  InstallAIExtensionPhase = "Installed"
	InstallAIExtensionPhaseFailed     InstallAIExtensionPhase = "Failed"
)

// HelmSource configures the Helm chart-based extension deployment model.
// The controller installs the Helm chart, which creates a Deployment + Service
// serving the extension assets. The Helm release name is derived from the last
// path segment of ChartURL.
type HelmSource struct {
	// ChartURL is the Helm chart repository URL (oci:// or https://).
	// The Helm release name is derived from the last path segment of this URL.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^(oci://|https://).+`
	ChartURL string `json:"chartURL"`

	// Version is the chart version to install.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// Values are optional Helm values overrides passed to the chart installation.
	// +optional
	Values map[string]apixv1.JSON `json:"values,omitempty"`
}

// GitSource configures the git-based extension serving model.
// The controller creates a ClusterRepo pointing to this git repository and branch.
type GitSource struct {
	// Repo is the git repository URL containing the extension's index.yaml.
	// +kubebuilder:validation:MinLength=1
	Repo string `json:"repo"`

	// Branch is the git branch serving the extension assets (typically "gh-pages").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:default=gh-pages
	Branch string `json:"branch"`
}

// ExtensionSource is a discriminated union indicating how UI extension assets are served.
// +kubebuilder:validation:XValidation:rule="self.kind == 'Helm' ? has(self.helm) : true",message="helm is required when kind is Helm"
// +kubebuilder:validation:XValidation:rule="self.kind == 'Helm' ? !has(self.git) : true",message="git must not be set when kind is Helm"
// +kubebuilder:validation:XValidation:rule="self.kind == 'Git' ? has(self.git) : true",message="git is required when kind is Git"
// +kubebuilder:validation:XValidation:rule="self.kind == 'Git' ? !has(self.helm) : true",message="helm must not be set when kind is Git"
type ExtensionSource struct {
	// Kind selects the source type for the UI extension assets.
	// "Helm" installs a Helm chart that deploys a container serving extension assets and creates a URL-based ClusterRepo.
	// "Git" creates a ClusterRepo pointing to a git repository branch.
	// +kubebuilder:validation:Enum=Helm;Git
	Kind ExtensionSourceKind `json:"kind"`

	// Helm is populated when Kind=Helm.
	// +optional
	Helm *HelmSource `json:"helm,omitempty"`

	// Git is populated when Kind=Git.
	// +optional
	Git *GitSource `json:"git,omitempty"`
}

// ExtensionConfig identifies the UI extension managed by the controller.
// For Helm sources, a UIPlugin CR with this name is created by the operator.
// For Git sources, the UIPlugin is installed via Helm from the git-hosted chart repo.
type ExtensionConfig struct {
	// Name is the UIPlugin resource name to verify after chart installation.
	// This must match the UIPlugin name created by the Helm chart.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Version is the expected extension version in the UIPlugin spec.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}

// InstallAIExtensionSpec defines the desired state of InstallAIExtension.
type InstallAIExtensionSpec struct {
	// Source configures how the UI extension assets are served and
	// how the ClusterRepo is created.
	Source ExtensionSource `json:"source"`

	// Extension identifies the UIPlugin for post-install verification.
	Extension ExtensionConfig `json:"extension"`
}

// InstallAIExtensionStatus defines the observed state of InstallAIExtension.
type InstallAIExtensionStatus struct {
	// Phase is the current installation phase.
	// +kubebuilder:validation:Enum=Pending;Installing;Installed;Failed
	// +optional
	Phase InstallAIExtensionPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the InstallAIExtension state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// HelmReleaseName is the name of the installed Helm release, derived from the chart URL.
	// +optional
	HelmReleaseName string `json:"helmReleaseName,omitempty"`

	// HelmReleaseRevision is the Helm release revision number.
	// +optional
	HelmReleaseRevision int32 `json:"helmReleaseRevision,omitempty"`

	// ActiveExtensionName is the extension name last successfully reconciled.
	// Used to detect name changes and clean up orphaned resources.
	// +optional
	ActiveExtensionName string `json:"activeExtensionName,omitempty"`

	// ActiveSourceKind is the source kind last successfully reconciled.
	// Used to detect source switches and clean up old source's resources.
	// +optional
	ActiveSourceKind ExtensionSourceKind `json:"activeSourceKind,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=aifext
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.source.kind`
// +kubebuilder:printcolumn:name="Extension",type=string,JSONPath=`.spec.extension.name`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.extension.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// InstallAIExtension is the Schema for the installaiextensions API
type InstallAIExtension struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of InstallAIExtension
	// +required
	Spec InstallAIExtensionSpec `json:"spec"`

	// status defines the observed state of InstallAIExtension
	// +optional
	Status InstallAIExtensionStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// InstallAIExtensionList contains a list of InstallAIExtension
type InstallAIExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstallAIExtension `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InstallAIExtension{}, &InstallAIExtensionList{})
}
