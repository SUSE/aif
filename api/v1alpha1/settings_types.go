package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FleetAuthType defines the authentication method for Fleet GitOps
type FleetAuthType string

const (
	FleetAuthTypeSSH   FleetAuthType = "ssh"
	FleetAuthTypeToken FleetAuthType = "token"
	FleetAuthTypeBasic FleetAuthType = "basic"
)

// SettingsSpec defines the desired state of Settings
type SettingsSpec struct {
	// ApplicationCollection configures SUSE Application Collection integration
	// +optional
	ApplicationCollection *ApplicationCollectionConfig `json:"applicationCollection,omitempty"`

	// SUSERegistry configures SUSE Registry integration
	// +optional
	SUSERegistry *SUSERegistryConfig `json:"suseRegistry,omitempty"`

	// Fleet configures Fleet GitOps integration
	// +optional
	Fleet *FleetConfig `json:"fleet,omitempty"`

	// RegistryEndpoints overrides upstream defaults for air-gap deployments
	// +optional
	RegistryEndpoints *RegistryEndpointsSpec `json:"registryEndpoints,omitempty"`

	// ImageRewrite controls Helm-values prefix substitution at deploy time
	// +optional
	ImageRewrite *ImageRewriteSpec `json:"imageRewrite,omitempty"`

	// CatalogDiscovery controls how the SUSE Application Collection is discovered
	// +optional
	CatalogDiscovery *CatalogDiscoverySpec `json:"catalogDiscovery,omitempty"`

	// BlueprintClassification overrides annotation-based vendor-chart wrapping decisions
	// +optional
	BlueprintClassification *BlueprintClassificationSpec `json:"blueprintClassification,omitempty"`
}

// ApplicationCollectionConfig configures SUSE Application Collection integration
type ApplicationCollectionConfig struct {
	// UserSecretRef references the username secret
	// +optional
	UserSecretRef *corev1.SecretKeySelector `json:"userSecretRef,omitempty"`

	// TokenSecretRef references the token secret
	// +optional
	TokenSecretRef *corev1.SecretKeySelector `json:"tokenSecretRef,omitempty"`

	// Categories is a filter list of categories to include
	// +optional
	Categories []string `json:"categories,omitempty"`
}

// SUSERegistryConfig configures SUSE Registry integration
type SUSERegistryConfig struct {
	// UserSecretRef references the username secret
	// +optional
	UserSecretRef *corev1.SecretKeySelector `json:"userSecretRef,omitempty"`

	// TokenSecretRef references the token secret
	// +optional
	TokenSecretRef *corev1.SecretKeySelector `json:"tokenSecretRef,omitempty"`

	// RefreshIntervalMinutes is the NIM index refresh cadence
	// +kubebuilder:default=10
	// +optional
	RefreshIntervalMinutes *int32 `json:"refreshIntervalMinutes,omitempty"`
}

// FleetConfig configures Fleet GitOps integration
type FleetConfig struct {
	// RepoURL is the Git repository URL
	// +optional
	RepoURL string `json:"repoURL,omitempty"`

	// Branch is the Git branch
	// +kubebuilder:default=main
	// +optional
	Branch string `json:"branch,omitempty"`

	// AuthType is the authentication method
	// +kubebuilder:validation:Enum=ssh;token;basic
	// +optional
	AuthType FleetAuthType `json:"authType,omitempty"`

	// CredSecretRef references the Git credential secret
	// +optional
	CredSecretRef *corev1.SecretKeySelector `json:"credSecretRef,omitempty"`
}

// RegistryEndpointsSpec overrides upstream defaults for air-gap deployments
type RegistryEndpointsSpec struct {
	// SUSERegistry is the hostname for SUSE Registry
	// Default: "registry.suse.com"
	// +optional
	SUSERegistry string `json:"suseRegistry,omitempty"`

	// ApplicationCollection is the OCI hostname for SUSE Application Collection chart pulls
	// Default: "dp.apps.rancher.io"
	// +optional
	ApplicationCollection string `json:"applicationCollection,omitempty"`

	// ApplicationCollectionAPI is the HTTP API URL for SUSE App Collection metadata
	// Default: "https://api.apps.rancher.io"
	// +optional
	ApplicationCollectionAPI string `json:"applicationCollectionAPI,omitempty"`
}

// ImageRewriteSpec controls Helm-values prefix substitution
type ImageRewriteSpec struct {
	// Enabled applies rewrite rules during Helm values merge
	// Default: false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Rules apply in order; first match per field wins
	// +optional
	Rules []ImageRewriteRule `json:"rules,omitempty"`
}

// ImageRewriteRule defines a single image prefix rewrite rule
type ImageRewriteRule struct {
	// Match is the prefix to match on image.repository / image.registry fields
	// +kubebuilder:validation:MinLength=1
	Match string `json:"match"`

	// Replace is the substitution prefix
	// +kubebuilder:validation:MinLength=1
	Replace string `json:"replace"`
}

// CatalogDiscoverySpec controls SUSE Application Collection discovery
type CatalogDiscoverySpec struct {
	// ApplicationCollectionMode selects discovery strategy
	// +kubebuilder:validation:Enum=api;registry-fallback;disabled
	// +kubebuilder:default=api
	// +optional
	ApplicationCollectionMode string `json:"applicationCollectionMode,omitempty"`
}

// BlueprintClassificationSpec overrides annotation-based vendor-chart wrapping decisions
type BlueprintClassificationSpec struct {
	// ForceReferenceBlueprint lists charts to wrap as AIF Blueprints regardless of annotation
	// +optional
	ForceReferenceBlueprint []ChartRef `json:"forceReferenceBlueprint,omitempty"`

	// ForceBuildingBlock lists charts to skip wrapping regardless of annotation
	// +optional
	ForceBuildingBlock []ChartRef `json:"forceBuildingBlock,omitempty"`
}

// ChartRef references a Helm chart
type ChartRef struct {
	// Repo is the OCI repository
	// +kubebuilder:validation:MinLength=1
	Repo string `json:"repo"`

	// Chart is the chart name
	// +kubebuilder:validation:MinLength=1
	Chart string `json:"chart"`
}

// SettingsStatus defines the observed state of Settings
type SettingsStatus struct {
	// LastApplied is when settings were last applied to engines
	// +optional
	LastApplied metav1.Time `json:"lastApplied,omitempty"`

	// Conditions represent the latest available observations of the Settings state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=aifset
// +kubebuilder:printcolumn:name="Last Applied",type=date,JSONPath=`.status.lastApplied`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Settings is the Schema for the settings API
type Settings struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SettingsSpec   `json:"spec,omitempty"`
	Status SettingsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SettingsList contains a list of Settings
type SettingsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Settings `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Settings{}, &SettingsList{})
}
