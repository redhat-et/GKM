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

// GpuStatus defines the status of an individual GPU on a node.
type GpuStatus struct {
	GpuType       string `json:"gpuType,omitempty"`
	DriverVersion string `json:"driverVersion,omitempty"`
	GpuList       []int  `json:"ids,omitempty"`
}

// CacheStatus defines the status of an individual kernel cache for a given digest .on a node
type CacheStatus struct {
	CompGpuList   []int              `json:"compatibleGPUs,omitempty"`
	IncompGpuList []int              `json:"incompatibleGPUs,omitempty"`
	Conditions    []metav1.Condition `json:"conditions,omitempty"`
	LastUpdated   metav1.Time        `json:"lastUpdated"`
	VolumeSize    int64              `json:"volumeSize,omitempty"`
	VolumeIds     []string           `json:"volumeIds,omitempty"`
}

type CacheCounts struct {
	ExtractedCnt   int `json:"extractedCnt"`
	UseCnt         int `json:"useCnt"`
	ErrorCnt       int `json:"errorCnt"`
	PodRunningCnt  int `json:"podRunningCnt"`
	PodOutdatedCnt int `json:"podOutdatedCnt"`
}

// GKMCacheNodeStatus defines the observed state of GKMCacheNode
type GKMCacheNodeStatus struct {
	NodeName      string                            `json:"nodeName"`
	Counts        CacheCounts                       `json:"counts"`
	GpuStatuses   []GpuStatus                       `json:"gpus,omitempty"`
	CacheStatuses map[string]map[string]CacheStatus `json:"caches,omitempty"`
}

// GkmCacheNodeConditionType is used to indicate the status of a GKM Cache
// on a given node.
type GkmCacheNodeConditionType string

const (
	// GkmCacheNodeCondPending indicates that GKM has not yet completed
	// reconciling the GKM Cache on the given node.
	GkmCacheNodeCondPending GkmCacheNodeConditionType = "Pending"

	// GkmCacheNodeCondExtracted indicates that the GKM Cache has been
	// successfully extracted as requested on the given node.
	GkmCacheNodeCondExtracted GkmCacheNodeConditionType = "Extracted"

	// GkmCacheNodeCondRunning indicates that the GKM Cache has been
	// successfully extracted and is being used by a Pod on the given node.
	GkmCacheNodeCondRunning GkmCacheNodeConditionType = "Running"

	// GkmCacheNodeCondOutdated indicates that the GKM Cache has been
	// successfully extracted and is being used by a Pod on the given node
	// but a newer image digest exists.
	GkmCacheNodeCondOutdated GkmCacheNodeConditionType = "Outdated"

	// GkmCacheNodeCondError indicates that an error has occurred on the given
	// node while attempting to apply the configuration described in the CRD.
	GkmCacheNodeCondError GkmCacheNodeConditionType = "Error"

	// GkmCacheNodeCondUnloadError indicates that the GKM Cache was marked
	// for deletion, but removing GK Cache was unsuccessful on the
	// given node.
	GkmCacheNodeCondUnloadError GkmCacheNodeConditionType = "UnloadError"
)

// Condition is a helper method to promote any given GkmCacheNodeConditionType to a
// full metav1.Condition in an opinionated fashion.
func (b GkmCacheNodeConditionType) Condition() metav1.Condition {
	cond := metav1.Condition{}

	switch b {
	case GkmCacheNodeCondPending:
		condType := string(GkmCacheNodeCondPending)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Pending",
			Message: "Not yet complete",
		}
	case GkmCacheNodeCondExtracted:
		condType := string(GkmCacheNodeCondExtracted)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Extracted",
			Message: "The Kernel Cache has been extracted onto the host",
		}
	case GkmCacheNodeCondRunning:
		condType := string(GkmCacheNodeCondRunning)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Running",
			Message: "The Kernel Cache has been extracted and is in use by one or more pods",
		}
	case GkmCacheNodeCondOutdated:
		condType := string(GkmCacheNodeCondOutdated)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Outdated",
			Message: "The Kernel Cache is in use by one or more pods but newer version exists",
		}
	case GkmCacheNodeCondError:
		condType := string(GkmCacheNodeCondError)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Error",
			Message: "An error occurred trying to extract the Kernel Cache",
		}
	case GkmCacheNodeCondUnloadError:
		condType := string(GkmCacheNodeCondUnloadError)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Unload Error",
			Message: "An error occurred trying to remove the extracted Kernel Cache",
		}
	}
	return cond
}

// IsConditionSet loops through the slice of conditions (should only be one) and determines if the input
// GkmCacheNodeConditionType is set.
func (b GkmCacheNodeConditionType) IsConditionSet(conditions []metav1.Condition) bool {
	for _, condition := range conditions {
		if condition.Type == string(b) {
			return true
		}
	}
	return false
}
