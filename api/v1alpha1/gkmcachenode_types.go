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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced

// GKMCacheNode is the Schema for the gkmcachenodes API
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=".status.nodeName"
// +kubebuilder:printcolumn:name="Extracted",type=string,JSONPath=`.status.counts.extractedCnt`
// +kubebuilder:printcolumn:name="Use",type=string,JSONPath=`.status.counts.useCnt`
// +kubebuilder:printcolumn:name="Error",type=string,JSONPath=`.status.counts.errorCnt`
// +kubebuilder:printcolumn:name="PodRunning",type=string,JSONPath=`.status.counts.podRunningCnt`
// +kubebuilder:printcolumn:name="PodOutdated",type=string,JSONPath=`.status.counts.podOutdatedCnt`
type GKMCacheNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

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

func (cacheNode GKMCacheNode) GetName() string {
	return cacheNode.Name
}

func (cacheNode GKMCacheNode) GetNamespace() string {
	return cacheNode.Namespace
}

func (cacheNode GKMCacheNode) GetAnnotations() map[string]string {
	return cacheNode.Annotations
}

func (cacheNode GKMCacheNode) GetLabels() map[string]string {
	return cacheNode.Labels
}

func (cacheNode GKMCacheNode) GetStatus() *GKMCacheNodeStatus {
	return &cacheNode.Status
}

func (cacheNode GKMCacheNode) GetNodeName() string {
	return cacheNode.Status.NodeName
}

func (cacheNode GKMCacheNode) GetClientObject() client.Object {
	return &cacheNode
}
