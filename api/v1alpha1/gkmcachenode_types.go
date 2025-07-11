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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CacheStatus defines the status of an individual kernel cache on a node
type CacheStatus struct {
	KernelCacheRef string             `json:"kernelCacheRef"`
	GpuType        string             `json:"gpuType"`
	DriverVersion  string             `json:"driverVersion"`
	Conditions     []metav1.Condition `json:"conditions,omitempty"`
	LastUpdated    metav1.Time        `json:"lastUpdated,omitempty"`
}

// GKMCacheNodeSpec defines the desired state of GKMCacheNode
type GKMCacheNodeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of GKMCacheNode. Edit gkmcachenode_types.go to remove/update
	NodeName      string        `json:"nodeName"`
	CacheStatuses []CacheStatus `json:"cacheStatuses,omitempty"`
}

// GKMCacheNodeStatus defines the observed state of GKMCacheNode
type GKMCacheNodeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GKMCacheNode is the Schema for the gkmcachenodes API
type GKMCacheNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GKMCacheNodeSpec   `json:"spec,omitempty"`
	Status GKMCacheNodeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GKMCacheNodeList contains a list of GKMCacheNode
type GKMCacheNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GKMCacheNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GKMCacheNode{}, &GKMCacheNodeList{})
}
