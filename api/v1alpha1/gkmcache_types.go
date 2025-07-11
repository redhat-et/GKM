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

// GKMCacheSpec defines the desired state of GKMCache
type GKMCacheSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Name           string `json:"name"`
	Image          string `json:"image"`
	ResolvedDigest string `json:"resolvedDigest,omitempty"` // Injected by webhook
}

type KernelProperties struct {
	TritonVersion string          `json:"tritonVersion"`
	Variant       string          `json:"variant,omitempty"`
	EntryCount    int             `json:"entryCount,omitempty"`
	Summary       []KernelSummary `json:"summary,omitempty"`
}

type KernelSummary struct {
	Backend  string `json:"backend"`
	Arch     string `json:"arch"`
	WarpSize int    `json:"warp_size"`
}

// GKMCacheStatus defines the observed state of GKMCache
type GKMCacheStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	LastSynced metav1.Time        `json:"lastSynced,omitempty"`
	Summary    []KernelSummary    `json:"summary,omitempty"`
	Digest     string             `json:"digest,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GKMCache is the Schema for the gkmcaches API
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
