/*
Copyright 2024.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DeploymentEndpointSpec defines the desired state of DeploymentEndpoint
type DeploymentEndpointSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Id          string `json:"id,omitempty"`
	CodeId      string `json:"codeId,omitempty"`
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	VendorId    string `json:"vendorId,omitempty"`
	RegionId    string `json:"regionId,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	Memory      string `json:"memory,omitempty"`
	Timeout     string `json:"timeout,omitempty"`
	Metadata    string `json:"metadata,omitempty"`
	RelatedVega string `json:"relatedVega,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
}

// DeploymentEndpointStatus defines the observed state of DeploymentEndpoint
type DeploymentEndpointStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// DeploymentEndpoint is the Schema for the deploymentendpoints API
type DeploymentEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeploymentEndpointSpec   `json:"spec,omitempty"`
	Status DeploymentEndpointStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DeploymentEndpointList contains a list of DeploymentEndpoint
type DeploymentEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeploymentEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DeploymentEndpoint{}, &DeploymentEndpointList{})
}
