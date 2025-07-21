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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GKMCache is the Schema for the namespace scoped GKMCaches API. Using this
// API allows applications to pre-populate a GPU Kernel Cache in a Pod,
// allowing the application to avoid having to build the kernel on the fly. The
// cache is packaged in an OCI Image, which is referenced in the GKMCache.
//
// The GKMCache.status field can be used to determine if any errors occurred in
// the loading of the GPU Kernel Cache. Because one image can be loaded on
// multiple Kubernetes nodes, GKMCache.status is just a summary, all loaded or
// something failed. GKM creates a GKMCacheNode CR instance for each Kubernetes
// Node for each GKMCache instance. The GKMCacheNode CRD provides load status
// for each GPU Kernel Cache for each GPU on the node.
type GKMCache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GKMCacheSpec   `json:"spec,omitempty"`
	Status GKMCacheStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GKMCacheList contains a list of GKMCache
type GKMCacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GKMCache `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GKMCache{}, &GKMCacheList{})
}
