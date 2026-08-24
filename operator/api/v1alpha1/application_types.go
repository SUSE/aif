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

// ApplicationChart maps a logical Application to the Helm chart that
// implements it. SourceRef names a cluster-scoped Rancher ClusterRepo; the
// ClusterRepo may be backed by HTTP, OCI, or Git and owns endpoint, trust, and
// authentication concerns.
type ApplicationChart struct {
	// Name is the chart name within the referenced source.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// SourceRef is the name of a Rancher ClusterRepo.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	SourceRef string `json:"sourceRef"`
}

// ApplicationSpec defines the deployable package behind a stable logical
// application identity. Blueprint qualification pins the package version; the
// Application owns the chart/source mapping while the ClusterRepo owns the
// endpoint, trust, and authentication configuration.
type ApplicationSpec struct {
	// Chart identifies the Helm package and its configurable source.
	Chart ApplicationChart `json:"chart"`
	// CredentialProfile selects the runtime secret-injection profile explicitly.
	// It is independent of the chart source URL. Empty values retain the
	// historical SUSE profile for objects created outside API-server defaulting.
	// +kubebuilder:default=suse
	// +optional
	CredentialProfile ComponentVendor `json:"credentialProfile,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=aifapp
// +kubebuilder:printcolumn:name="Chart",type=string,JSONPath=`.spec.chart.name`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.chart.sourceRef`
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.credentialProfile`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Application is the source-independent identity a Blueprint references.
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ApplicationSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// ApplicationList contains a list of Application resources.
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Application{}, &ApplicationList{})
}
