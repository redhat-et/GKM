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

// ClusterGKMCache is the Schema for the cluster scoped GKMCaches API. Using
// this API allows applications to pre-populate a GPU Kernel Cache in a Pod,
// allowing the application to avoid having to build the kernel on the fly. The
// cache is packaged in an OCI Image, which is referenced in the
// ClusterGKMCache.
//
// The ClusterGKMCache.status field can be used to determine if any error
// occurred in the loading of the GPU Kernel Cache. Because one image can be
// loaded on multiple Kubernetes nodes, ClusterGKMCache.status is just a
// summary, all loaded or something failed. GKM creates a ClusterGKMCacheNode
// CR instance for each Kubernetes Node for each ClusterGKMCache instance. The
// ClusterGKMCacheNode CRD provides load status for each GPU Kernel Cache for
// each GPU on the node.
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`
// +kubebuilder:printcolumn:name="Nodes",type=string,JSONPath=`.status.counts.nodeCnt`
// +kubebuilder:printcolumn:name="Node-In-Use",type=string,JSONPath=`.status.counts.nodeInUseCnt`
// +kubebuilder:printcolumn:name="Node-Not-In-Use",type=string,JSONPath=`.status.counts.nodeNotInUseCnt`
// +kubebuilder:printcolumn:name="Node-Error",type=string,JSONPath=`.status.counts.nodeErrorCnt`
// +kubebuilder:printcolumn:name="Pod-Running",type=string,JSONPath=`.status.counts.podRunningCnt`
// +kubebuilder:printcolumn:name="Pod-Outdated",type=string,JSONPath=`.status.counts.podOutdatedCnt`
// +kubebuilder:printcolumn:name="Last=Updated",type=string,priority=1,JSONPath=".status.lastUpdated"
type ClusterGKMCache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of the ClusterGKMCache instance and describes
	// a GPU Kernel Cache that can be volume mounted in a Pod. The GPU Kernel Cache
	// is packaged in an OCI Image which allows the cache to be distributed to
	// Kubernetes Nodes, where it is extracted to host memory.
	Spec GKMCacheSpec `json:"spec,omitempty"`

	// status reflects the observed state of a ClusterGKMCache instance and
	// indicates if the GPU Kernel Cache for a given instance has been loaded
	// successfully or not and if it has been mounted in any pods across all nodes.
	// Use ClusterGKMClusterNode instances to determine the status for a given node.
	Status GKMCacheStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterGKMCacheList contains a list of ClusterGKMCache
type ClusterGKMCacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterGKMCache `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterGKMCache{}, &ClusterGKMCacheList{})
}

func (cache ClusterGKMCache) GetName() string {
	return cache.Name
}

func (cache ClusterGKMCache) GetNamespace() string {
	return ""
}

func (cache ClusterGKMCache) GetAnnotations() map[string]string {
	return cache.Annotations
}

func (cache ClusterGKMCache) GetLabels() map[string]string {
	return cache.Labels
}

func (cache ClusterGKMCache) GetImage() string {
	return cache.Spec.Image
}

func (cache ClusterGKMCache) GetStatus() *GKMCacheStatus {
	return &cache.Status
}

func (cache ClusterGKMCache) GetClientObject() client.Object {
	return &cache
}

func (cacheList ClusterGKMCacheList) GetItems() []ClusterGKMCache {
	return cacheList.Items
}

func (cacheList ClusterGKMCacheList) GetItemsLen() int {
	return len(cacheList.Items)
}
