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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// BlueprintCatalogMaintainer identifies who maintains a catalog.
type BlueprintCatalogMaintainer struct {
	// +optional
	Name string `json:"name,omitempty"`
	// +optional
	URL string `json:"url,omitempty"`
}

// BlueprintCatalogMember references one blueprint family (by its blueprint-name label)
// offered by a catalog, with per-membership curation. A blueprint listed by
// multiple catalogs belongs to all of them (many-to-many).
type BlueprintCatalogMember struct {
	// Name matches a Blueprint's ai-factory.suse.com/blueprint-name (family) label.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +optional
	Featured bool `json:"featured,omitempty"`
	// +optional
	Category string `json:"category,omitempty"`
}

// BlueprintCatalogSpec is the branding + membership of a blueprint catalog. It is
// UI-consumed data; the operator runs no reconcile logic on it.
type BlueprintCatalogSpec struct {
	// +optional
	DisplayName string `json:"displayName,omitempty"`
	// +optional
	Description string `json:"description,omitempty"`
	// Icon is a URL or data: URI (data: keeps it air-gap friendly).
	// +optional
	Icon string `json:"icon,omitempty"`
	// +optional
	Maintainer *BlueprintCatalogMaintainer `json:"maintainer,omitempty"`
	// +optional
	Categories []string `json:"categories,omitempty"`
	// Blueprints is the catalog's membership list (many-to-many).
	// +optional
	// +listType=map
	// +listMapKey=name
	Blueprints []BlueprintCatalogMember `json:"blueprints,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=bpcatalog
// +kubebuilder:printcolumn:name="Display Name",type=string,JSONPath=`.spec.displayName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// BlueprintCatalog is a branded, membership-bearing collection of blueprints.
type BlueprintCatalog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Spec BlueprintCatalogSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// BlueprintCatalogList contains a list of BlueprintCatalog.
type BlueprintCatalogList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BlueprintCatalog `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BlueprintCatalog{}, &BlueprintCatalogList{})
}
