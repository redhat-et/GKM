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

// GKMCacheNode contains the state on a given Kubernetes Node of the set of
// GKMCache instances created in the same Namespace. When one or more GKMCache
// instance are created in a namespace, GKM ensures that one GKMCacheNode
// instance is created per Kubernetes Node. GKMCacheNode cannot be edited by an
// application or user, only by GKM.
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=".status.nodeName"
// +kubebuilder:printcolumn:name="Node-In-Use",type=string,JSONPath=`.status.counts.nodeInUseCnt`
// +kubebuilder:printcolumn:name="Node-Not-In-Use",type=string,JSONPath=`.status.counts.nodeNotInUseCnt`
// +kubebuilder:printcolumn:name="Node-Error",type=string,JSONPath=`.status.counts.nodeErrorCnt`
// +kubebuilder:printcolumn:name="Pod-Running",type=string,JSONPath=`.status.counts.podRunningCnt`
// +kubebuilder:printcolumn:name="Pod-Outdated",type=string,JSONPath=`.status.counts.podOutdatedCnt`
type GKMCacheNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// status reflects the observed state of a GKMCacheNode instance and indicates
	// if each GKMCache instance in this Namespace and on the node referenced by
	// status.nodeName has been loaded successfully or not and if it has been
	// mounted in any pods on the node.
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

func (cacheNodeList GKMCacheNodeList) GetItems() []GKMCacheNode {
	return cacheNodeList.Items
}

func (cacheNodeList GKMCacheNodeList) GetItemsLen() int {
	return len(cacheNodeList.Items)
}
