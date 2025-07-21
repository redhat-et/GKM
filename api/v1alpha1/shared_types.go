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

// spec defines the desired state of the GKMCache. The GKMCache describes a GPU
// Kernel Cache that can be deployed by a Pod. The GPU Kernel Cache is packaged
// in an OCI Image which allows the cache to be distributed to Nodes.
type GKMCacheSpec struct {
	// image is a required field and is a valid container image URL used to
	// reference a remote GPU Kernel Cache image. url must not be an empty string,
	// must not exceed 525 characters in length and must be a valid URL.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength:=525
	// +kubebuilder:validation:Pattern=`[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}`
	Image string `json:"image"`
}

// status reflects the observed state of a GKMCache or ClusterGKMCluster
// instance and indicates if the GPU Kernel Cache for a given instance loaded
// successfully or not across all nodes. Use GKMCacheNode or
// ClusterGKMClusterNode instances to determine the status for a given node.
type GKMCacheStatus struct {
	// resolvedDigest contains the digest of the image after it has been verified.
	ResolvedDigest string `json:"resolvedDigest,omitempty"` // Injected by webhook

	// conditions contains the summary state for the GPU Kernel Cache for all the
	// Kubernetes nodes in the cluster.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// totalNodes contains the total number of nodes the GKM Agent is running.
	TotalNodes int `json:"totalNodes"`

	// readyNodes contains the number of nodes the GKM Agent is running that have
	// no failures.
	ReadyNodes int `json:"readyNodes"`

	// failedNodes contains the number of nodes the GKM Agent is running that have
	// failures.
	FailedNodes int `json:"failedNodes"`

	// lastUpdated contains the timestamp of the last time this instance was
	// updated.
	LastUpdated metav1.Time `json:"lastUpdated,omitempty"`
}
