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

// AppModuleSpec defines the desired state of AppModule
type AppModuleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Id          string           `json:"id,omitempty"`
	Name        string           `json:"name,omitempty"`
	AppId       string           `json:"appId,omitempty"`
	Language    string           `json:"language,omitempty"`
	Web         string           `json:"web,omitempty"`
	Description string           `json:"description,omitempty"`
	Ref         AppDeploymentRef `json:"ref,omitempty"`
}

type AppDeploymentRef struct {
	AppDeployment map[string]string `json:"appDeployment"`
}

// AppModuleStatus defines the observed state of AppModule
type AppModuleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AppModule is the Schema for the appmodules API
type AppModule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppModuleSpec   `json:"spec,omitempty"`
	Status AppModuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AppModuleList contains a list of AppModule
type AppModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppModule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppModule{}, &AppModuleList{})
}
