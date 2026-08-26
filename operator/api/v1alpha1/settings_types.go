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
	// AuthType is the HTTPS authentication method.
	// +kubebuilder:validation:Enum=token;basic
	// +optional
	AuthType string `json:"authType,omitempty"`
	// Username is the HTTPS Git username. It defaults to "token" for token
	// authentication. Basic authentication requires this field or a username
	// key in the referenced credential Secret.
	// +optional
	Username string `json:"username,omitempty"`
	// CredSecretRef references the Git credential secret.
	// +optional
	CredSecretRef *SecretKeyRef `json:"credSecretRef,omitempty"`
	// CABundleSecretRef references a Secret containing the PEM CA bundle used
	// to verify an HTTPS Git server. AIF and the generated Fleet GitRepo consume
	// the same bundle so private Git has one trust configuration.
	// +optional
	CABundleSecretRef *SecretKeyRef `json:"caBundleSecretRef,omitempty"`
}

// ApplicationCollectionSettings configures SUSE Application Collection.
type ApplicationCollectionSettings struct {
	// UserSecretRef references the username secret.
	// +optional
	UserSecretRef *SecretKeyRef `json:"userSecretRef,omitempty"`
	// TokenSecretRef references the access token secret.
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
	// CABundleSecretRef references a Secret containing the PEM CA bundle used
	// to verify the configured chart registry. The Settings controller mirrors
	// this value as "cacerts" in Rancher/Fleet registry auth Secrets.
	// +optional
	CABundleSecretRef *SecretKeyRef `json:"caBundleSecretRef,omitempty"`
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
	// CABundleSecretRef references a Secret containing the PEM CA bundle used
	// to verify the configured chart registry. The Settings controller mirrors
	// this value as "cacerts" in Rancher/Fleet registry auth Secrets.
	// +optional
	CABundleSecretRef *SecretKeyRef `json:"caBundleSecretRef,omitempty"`
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
	// CABundleSecretRef references a Secret containing the PEM CA bundle used
	// to verify a configured mirrored NVIDIA chart registry. The Settings
	// controller mirrors this value as "cacerts" in Rancher/Fleet registry auth
	// Secrets. It is unused by the public NGC chart repositories.
	// +optional
	CABundleSecretRef *SecretKeyRef `json:"caBundleSecretRef,omitempty"`
}

// RegistryEndpointsSettings configures custom chart repositories.
type RegistryEndpointsSettings struct {
	// SUSERegistry overrides the default SUSE Registry chart repository URL.
	// +optional
	SUSERegistry string `json:"suseRegistry,omitempty"`
	// ApplicationCollection overrides the SUSE App Collection chart repository URL.
	// +optional
	ApplicationCollection string `json:"applicationCollection,omitempty"`
	// Nvidia is the OCI URL of a mirrored NVIDIA chart repository for air-gapped installs
	// (e.g. oci://registry.example.com/nvidia). When empty, NVIDIA charts are pulled from the
	// public NGC HTTPS repositories; when set, a single gated OCI ClusterRepo is created at this URL.
	// Note: this is the chart-repo URL only — NVIDIA container images still resolve to nvcr.io and
	// require node-level registry redirection (e.g. containerd hosts.toml) in a true air-gap.
	// +optional
	Nvidia string `json:"nvidia,omitempty"`
}

// AppCatalogSettings configures the static application catalog served to the UI.
type AppCatalogSettings struct {
	// RemoteURL is an optional URL to a remote catalog JSON document. When set,
	// the operator fetches this (and only this) URL on behalf of the UI. When
	// empty, the operator serves its bundled default catalog. Must be an http(s)
	// URL that resolves to a public address; internal/private destinations are
	// rejected.
	// +optional
	RemoteURL string `json:"remoteUrl,omitempty"`
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
	// RegistryEndpoints configures custom chart repositories for air-gap deployments.
	// +optional
	RegistryEndpoints *RegistryEndpointsSettings `json:"registryEndpoints,omitempty"`
	// AppCatalog configures the static application catalog.
	// +optional
	AppCatalog AppCatalogSettings `json:"appCatalog,omitempty"`
	// RancherCatalog configures access to Rancher's catalog API, used to fetch
	// charts from git-backed ClusterRepos.
	// +optional
	RancherCatalog RancherCatalogSettings `json:"rancherCatalog,omitempty"`
}

// RancherCatalogSettings configures the Rancher Steve catalog client used to
// fetch charts from git-backed ClusterRepos (spec.gitRepo), which have no
// HTTP/OCI URL a Fleet HelmOp could pull from. HTTP/OCI ClusterRepos need none
// of this. The Settings controller rebuilds the catalog client whenever these
// fields (or the referenced Secrets) change, so updates take effect without
// restarting the operator.
type RancherCatalogSettings struct {
	// URL is the in-cluster Rancher API endpoint. Defaults to
	// https://rancher.cattle-system.svc when empty.
	// +optional
	URL string `json:"url,omitempty"`
	// TokenSecretRef references a Secret (in the operator namespace) holding a
	// Rancher API token. Required for git-backed ClusterRepos — Rancher's Steve
	// API rejects the operator ServiceAccount token. When unset, git-backed
	// ClusterRepos are not installable.
	// +optional
	TokenSecretRef *SecretKeyRef `json:"tokenSecretRef,omitempty"`
	// CABundleSecretRef references a Secret holding the PEM CA certificate(s)
	// that signed Rancher's serving certificate. When unset, system roots are
	// used (unless InsecureSkipVerify is set).
	// +optional
	CABundleSecretRef *SecretKeyRef `json:"caBundleSecretRef,omitempty"`
	// InsecureSkipVerify skips TLS verification of the Rancher API. Development
	// only — do not use in production/air-gapped installs.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
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
