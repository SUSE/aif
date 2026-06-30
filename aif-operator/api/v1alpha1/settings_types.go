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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretKeyRef is a reference to a key within a Kubernetes Secret.
type SecretKeyRef struct {
	// Name is the Secret name.
	Name string `json:"name"`
	// Key is the key within the Secret.
	Key string `json:"key"`
}

// FleetSettings configures Fleet GitOps integration.
type FleetSettings struct {
	// RepoURL is the Git repository URL.
	// +optional
	RepoURL string `json:"repoURL,omitempty"`
	// Branch is the Git branch to track.
	// +kubebuilder:default=main
	// +optional
	Branch string `json:"branch,omitempty"`
	// AuthType is the authentication method.
	// +kubebuilder:validation:Enum=ssh;token;basic
	// +optional
	AuthType string `json:"authType,omitempty"`
	// CredSecretRef references the Git credential secret.
	// +optional
	CredSecretRef *SecretKeyRef `json:"credSecretRef,omitempty"`
}

// ApplicationCollectionSettings configures SUSE Application Collection.
type ApplicationCollectionSettings struct {
	// UserSecretRef references the username secret.
	// +optional
	UserSecretRef *SecretKeyRef `json:"userSecretRef,omitempty"`
	// TokenSecretRef references the access token secret.
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
	// Categories filters catalog entries by category.
	// +optional
	Categories []string `json:"categories,omitempty"`
}

// SUSERegistrySettings configures SUSE Registry integration.
type SUSERegistrySettings struct {
	// UserSecretRef references the username secret.
	// +optional
	UserSecretRef *SecretKeyRef `json:"userSecretRef,omitempty"`
	// TokenSecretRef references the access token secret.
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
	// RefreshIntervalMinutes is the NIM index refresh cadence.
	// +kubebuilder:default=10
	// +optional
	RefreshIntervalMinutes int32 `json:"refreshIntervalMinutes,omitempty"`
}

// NvidiaSettings configures NVIDIA NGC integration.
type NvidiaSettings struct {
	// UserSecretRef references the username secret (NGC username; conventionally "$oauthtoken").
	// +optional
	UserSecretRef *SecretKeyRef `json:"userSecretRef,omitempty"`
	// TokenSecretRef references the NGC API key secret.
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
}

// RegistryEndpointsSettings overrides upstream registry hosts for air-gap deployments.
type RegistryEndpointsSettings struct {
	// SUSERegistry overrides the default SUSE Registry hostname.
	// +optional
	SUSERegistry string `json:"suseRegistry,omitempty"`
	// ApplicationCollection overrides the SUSE App Collection OCI hostname.
	// +optional
	ApplicationCollection string `json:"applicationCollection,omitempty"`
	// ApplicationCollectionAPI overrides the SUSE App Collection HTTP API URL.
	// +optional
	ApplicationCollectionAPI string `json:"applicationCollectionAPI,omitempty"`
	// Nvidia is the OCI URL of a mirrored NVIDIA chart repository for air-gapped installs
	// (e.g. oci://registry.example.com/nvidia). When empty, NVIDIA charts are pulled from the
	// public NGC HTTPS repositories; when set, a single gated OCI ClusterRepo is created at this URL.
	// Note: this is the chart-repo URL only — NVIDIA container images still resolve to nvcr.io and
	// require node-level registry redirection (e.g. containerd hosts.toml) in a true air-gap.
	// +optional
	Nvidia string `json:"nvidia,omitempty"`
}

// CatalogDiscoverySettings controls how the SUSE Application Collection is discovered.
type CatalogDiscoverySettings struct {
	// ApplicationCollectionMode selects the discovery strategy.
	// +kubebuilder:validation:Enum=api;registry-fallback;disabled
	// +kubebuilder:default=api
	// +optional
	ApplicationCollectionMode string `json:"applicationCollectionMode,omitempty"`
}

// ImageRewriteRule defines a single image prefix rewrite rule.
type ImageRewriteRule struct {
	// Match is the prefix to match.
	// +kubebuilder:validation:MinLength=1
	Match string `json:"match"`
	// Replace is the substitution prefix.
	// +kubebuilder:validation:MinLength=1
	Replace string `json:"replace"`
}

// ImageRewriteSettings controls Helm-values prefix substitution at deploy time.
type ImageRewriteSettings struct {
	// Enabled applies rewrite rules during Helm values merge.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Rules apply in order; first match per field wins.
	// +optional
	Rules []ImageRewriteRule `json:"rules,omitempty"`
}

// SettingsSpec defines the desired state of Settings.
type SettingsSpec struct {
	// Fleet configures Fleet GitOps integration.
	// +optional
	Fleet FleetSettings `json:"fleet,omitempty"`
	// ApplicationCollection configures SUSE Application Collection.
	// +optional
	ApplicationCollection ApplicationCollectionSettings `json:"applicationCollection,omitempty"`
	// SUSERegistry configures SUSE Registry integration.
	// +optional
	SUSERegistry SUSERegistrySettings `json:"suseRegistry,omitempty"`
	// Nvidia configures NVIDIA NGC integration.
	// +optional
	Nvidia NvidiaSettings `json:"nvidia,omitempty"`
	// RegistryEndpoints overrides upstream registry defaults for air-gap deployments.
	// +optional
	RegistryEndpoints *RegistryEndpointsSettings `json:"registryEndpoints,omitempty"`
	// CatalogDiscovery controls how the SUSE Application Collection is discovered.
	// +optional
	CatalogDiscovery CatalogDiscoverySettings `json:"catalogDiscovery,omitempty"`
	// ImageRewrite controls Helm-values prefix substitution at deploy time.
	// +optional
	ImageRewrite ImageRewriteSettings `json:"imageRewrite,omitempty"`
}

// SettingsStatus defines the observed state of Settings.
type SettingsStatus struct {
	// LastApplied is when settings were last applied.
	// +optional
	LastApplied *metav1.Time `json:"lastApplied,omitempty"`
	// Conditions represent the latest observations of the Settings state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the generation observed by the controller.
	// +kubebuilder:validation:Minimum=0
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=aiplmset
// +kubebuilder:printcolumn:name="Last Applied",type=date,JSONPath=`.status.lastApplied`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Settings is the Schema for the settings API.
type Settings struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SettingsSpec   `json:"spec,omitempty"`
	Status SettingsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SettingsList contains a list of Settings.
type SettingsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Settings `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Settings{}, &SettingsList{})
}
