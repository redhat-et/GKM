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
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ClusterGKMCacheNode contains the state on a given Kubernetes Node of the set
// of ClusterGKMCache instances. When one or more ClusterGKMCache instance are
// created, GKM ensures that one ClusterGKMCacheNode instance is created per
// Kubernetes Node. Cluster GKMCacheNode cannot be edited by an application or
// user, only by GKM.
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=".status.nodeName"
// +kubebuilder:printcolumn:name="Node-In-Use",type=string,JSONPath=`.status.counts.nodeInUseCnt`
// +kubebuilder:printcolumn:name="Node-Not-In-Use",type=string,JSONPath=`.status.counts.nodeNotInUseCnt`
// +kubebuilder:printcolumn:name="Node-Error",type=string,JSONPath=`.status.counts.nodeErrorCnt`
// +kubebuilder:printcolumn:name="Pod-Running",type=string,JSONPath=`.status.counts.podRunningCnt`
// +kubebuilder:printcolumn:name="Pod-Outdated",type=string,JSONPath=`.status.counts.podOutdatedCnt`
type ClusterGKMCacheNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// status reflects the observed state of a ClusterGKMCacheNode instance and
	// indicates if each ClusterGKMCache instance on the node referenced by
	// status.nodeName has been loaded successfully or not and if it has been
	// mounted in any pods on the node.
	Status GKMCacheNodeStatus `json:"status,omitempty"`
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

func (cacheNode ClusterGKMCacheNode) GetName() string {
	return cacheNode.Name
}

func (cacheNode ClusterGKMCacheNode) GetNamespace() string {
	return ""
}

func (cacheNode ClusterGKMCacheNode) GetAnnotations() map[string]string {
	return cacheNode.Annotations
}

func (cacheNode ClusterGKMCacheNode) GetLabels() map[string]string {
	return cacheNode.Labels
}

func (cacheNode ClusterGKMCacheNode) GetStatus() *GKMCacheNodeStatus {
	return &cacheNode.Status
}

func (cacheNode ClusterGKMCacheNode) GetNodeName() string {
	return cacheNode.Status.NodeName
}

func (cacheNode ClusterGKMCacheNode) GetClientObject() client.Object {
	return &cacheNode
}

func (cacheNodeList ClusterGKMCacheNodeList) GetItems() []ClusterGKMCacheNode {
	return cacheNodeList.Items
}

func (cacheNodeList ClusterGKMCacheNodeList) GetItemsLen() int {
	return len(cacheNodeList.Items)
}
