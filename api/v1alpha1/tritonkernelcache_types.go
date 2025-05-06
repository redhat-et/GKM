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

// TritonKernelCacheSpec defines the desired state of TritonKernelCache
type TritonKernelCacheSpec struct {
	Name                 string           `json:"name"`
	BinaryImage          string           `json:"binaryImage"`
	IntermediateRepImage string           `json:"intermediateRepImage"`
	KernelProperties     KernelProperties `json:"kernelProperties"`
	ValidateSignature    bool             `json:"validateSignature,omitempty"`
}

type KernelProperties struct {
	TritonVersion string           `json:"tritonVersion"`
	Variant       string           `json:"variant,omitempty"`
	EntryCount    int              `json:"entryCount,omitempty"`
	Metadata      []KernelMetadata `json:"metadata,omitempty"`
}

type KernelMetadata struct {
	Hash     string `json:"hash"`
	Backend  string `json:"backend"`
	Arch     string `json:"arch"`
	WarpSize int    `json:"warp_size"`
	DummyKey string `json:"dummy_key"`
}

// TritonKernelCacheStatus defines the observed state of TritonKernelCache
type TritonKernelCacheStatus struct {
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
	Digest           string             `json:"digest,omitempty"`
	LastSynced       metav1.Time        `json:"lastSynced,omitempty"`
	ObservedMetadata []KernelMetadata   `json:"observedMetadata,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TritonKernelCache is the Schema for the tritonkernelcaches API
type TritonKernelCache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TritonKernelCacheSpec   `json:"spec,omitempty"`
	Status TritonKernelCacheStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TritonKernelCacheList contains a list of TritonKernelCache
type TritonKernelCacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TritonKernelCache `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TritonKernelCache{}, &TritonKernelCacheList{})
}
