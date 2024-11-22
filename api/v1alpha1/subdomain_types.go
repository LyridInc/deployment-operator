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

// SubdomainSpec defines the desired state of Subdomain
type SubdomainSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Name         string `json:"name,omitempty"`
	AccountId    string `json:"accountId,omitempty"`
	AppId        string `json:"appId,omitempty"`
	ModuleId     string `json:"moduleId,omitempty"`
	FunctionName string `json:"functionId,omitempty"`
	Tag          string `json:"tag,omitempty"`
	Public       bool   `json:"public,omitempty"`
}

// SubdomainStatus defines the observed state of Subdomain
type SubdomainStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Subdomain is the Schema for the subdomains API
type Subdomain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubdomainSpec   `json:"spec,omitempty"`
	Status SubdomainStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SubdomainList contains a list of Subdomain
type SubdomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subdomain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Subdomain{}, &SubdomainList{})
}
