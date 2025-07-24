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

// ClusterGKMCacheNodeSpec defines the desired state of ClusterGKMCacheNode
type ClusterGKMCacheNodeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of ClusterGKMCacheNode. Edit clustergkmcachenode_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// ClusterGKMCacheNodeStatus defines the observed state of ClusterGKMCacheNode
type ClusterGKMCacheNodeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ClusterGKMCacheNode is the Schema for the clustergkmcachenodes API
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=".status.node"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ClusterGKMCacheNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterGKMCacheNodeSpec   `json:"spec,omitempty"`
	Status ClusterGKMCacheNodeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterGKMCacheNodeList contains a list of ClusterGKMCacheNode
type ClusterGKMCacheNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterGKMCacheNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterGKMCacheNode{}, &ClusterGKMCacheNodeList{})
}
