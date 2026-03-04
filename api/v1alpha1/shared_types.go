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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CacheCounts struct {
	// nodeCnt contains the total number of nodes used in the collection of
	// counts. For GKMCacheNode and ClusterGKMCacheNode, the value will be one.
	// For GKMCache and ClusterGKMCache, the value will be number of nodes GKM is
	// deployed on.
	NodeCnt int `json:"nodeCnt"`

	// nodeInUseCnt contains the total number of nodes that the Kernel Cache has
	// been extracted and that the Kernel Cache is currently being used by one or
	// more pods on that node.
	NodeInUseCnt int `json:"nodeInUseCnt"`

	// nodeNotInUseCnt contains the total number of nodes that the Kernel Cache
	// has been extracted and that the Kernel Cache is not currently being used by
	// a pod on that node.
	NodeNotInUseCnt int `json:"nodeNotInUseCnt"`

	// nodeErrorCnt contains the total number of nodes that the Kernel Cache
	// encounter an error. An error occurs if the OCI Image could not be extracted
	// because of an error in the image, or if the Kernel Cache is not compatible
	// with any of the GPUs detected on the Kubernetes node.
	NodeErrorCnt int `json:"nodeErrorCnt"`

	// podRunningCnt contains the total number of pods that the Kernel Cache is
	// volume mounted.
	PodRunningCnt int `json:"podRunningCnt"`

	// podOutdatedCnt contains the total number of pods that the Kernel Cache is
	// volume mounted, but a newer version of the extracted Kernel Cache has been
	// extracted. This happens when a Kernel Cache is being used, but the
	// associated OCI Image was updated.
	PodOutdatedCnt int `json:"podOutdatedCnt"`
}

type GKMCacheSpec struct {
	// image is a required field and is a valid container image URL used to
	// reference a remote GPU Kernel Cache image. url must not be an empty string,
	// must not exceed 525 characters in length and must be a valid URL.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength:=525
	// +kubebuilder:validation:Pattern=`[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}`
	Image string `json:"image"`

	// storageClassName contains the name of the Kubernetes Storage Class, which
	// will be used for the PersistentVolume and PersistentVolumeClaim the GKM will
	// create in order to store the extract GPU Kernel Cache.
	// +required
	// +kubebuilder:validation:Required
	StorageClassName string `json:"storageClassName"`

	// accessMode is the set of capabilities being requested by the generated PVC.
	// This field is optional. If not provided, it will default to "ReadWriteOnce".
	// This implies that the StorageClass and backing CSI Agent do not support
	// distributing the PVC data to multiple Nodes. GKM will create a PVC per Node
	// and populate each with the extracted GPU Kernel Cache. If the StorageClass
	// and backing CSI Agent do support distributing the PVC data to multiple
	// Nodes, then set this field to "ReadWriteOnce, ReadOnlyMany". GKM will create
	// one PVC for the cluster and leave it up to the StorageClass and backing CSI
	// Agent to distribute the PVC contents to each Node.
	// +kubebuilder:default:={ReadWriteOnce}
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`

	// workloadNamespaces is optional for GKMCache instances, but required for
	// ClusterGKMCache instances. For ClusterGKMCache instances, the cache is
	// cluster scoped, but the workload using the cache must be in a namespace, and
	// the PVC the is mounted in the pods will be in the same namespace. GKM
	// creates this PVC and needs to know what namespace to create it in.
	WorkloadNamespaces []string `json:"workloadNamespaces,omitempty"`
}

type GKMCacheStatus struct {
	// resolvedDigest contains the digest of the image after it has been verified.
	ResolvedDigest string `json:"resolvedDigest,omitempty"` // Injected by webhook

	// pvcOwner is an indication of which process, Agent or Operator, manages the
	// PVC used to store the extracted GPU Kernel Cache. Value of Agent indicates
	// Agent manages the PVC, value of Operator indicates Operator manages the PVC.
	// +kubebuilder:default:=Unknown
	PvcOwner PvcOwner `json:"pvcOwner"`

	// pvcStatus tracks the Persistent Volume Claim that was created for the
	// storage of the extract GPU Kernel Cache. The map is indexed by the namespace
	// the PVC is created .
	PvcStatus map[string]PvcStatus `json:"pvcStatus,omitempty"`

	// conditions contains the summary state for the GPU Kernel Cache for all the
	// Kubernetes nodes in the cluster.
	// DEPRECATED!!
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// counts contains statistics on the deployment of the GPU Kernel Cache for all
	// the Kubernetes nodes in the cluster.
	Counts CacheCounts `json:"counts"`

	// lastUpdated contains the timestamp of the last time the status field for
	// this instance was updated.
	LastUpdated metav1.Time `json:"lastUpdated,omitempty"`
}

type GpuStatus struct {
	// gpuType is the Product Name of the detected GPU.
	GpuType string `json:"gpuType,omitempty"`

	// driverVersion is the version of device driver managing the GPU.
	DriverVersion string `json:"driverVersion,omitempty"`

	// ids is a list of GPU indexes. GPUs of the same type are grouped together and
	// this is the list of indexes associated with the same GPU Type and attributes.
	GpuList []int `json:"ids,omitempty"`
}

type PodData struct {
	// podNamespace is the namespace of the pod the GPU Kernel Cache is mounted in.
	PodNamespace string `json:"podNamespace,omitempty"`

	// podName is the name of the pod the GPU Kernel Cache is mounted in.
	PodName string `json:"podName,omitempty"`

	// volumeId is the Volume Id the CSI Driver received from Kubelet. It
	// identifies the GPU Kernel Cache that is actively Volume Mounted in a pod.
	VolumeId string `json:"volumeId,omitempty"`
}

type CacheStatus struct {
	// compatibleGPUs is the list of GPU ids that the extracted GPU Kernel Cache
	// is compatible with. The ids refer back to the list of GPUs in status.gpus.
	CompGpuList []int `json:"compatibleGPUs,omitempty"`

	// incompatibleGPUs is the list of GPU ids that the extracted GPU Kernel Cache
	// is not compatible with. The ids refer back to the list of GPUs in
	// status.gpus.
	IncompGpuList []int `json:"incompatibleGPUs,omitempty"`

	// conditions contains the summary state for the GPU Kernel Cache on the
	// Kubernetes node referenced by status.nodeName.
	// DEPRECATED!!
	// Conditions []metav1.Condition `json:"conditions,omitempty"`

	// lastUpdated contains the timestamp of the last time this cache instance was
	// updated.
	LastUpdated metav1.Time `json:"lastUpdated"`

	// pvcStatus tracks the Persistent Volume Claim that was created for the
	// storage of the extract GPU Kernel Cache. The map is indexed by the namespace
	// the PVC is created .
	PvcStatus map[string]PvcStatus `json:"pvcStatus,omitempty"`

	// volumeSize is the size of the extracted GPU Kernel Cache in bytes.
	VolumeSize int64 `json:"volumeSize,omitempty"`

	// pods is the list of pods the GPU Kernel Cache that is actively Volume
	// Mounted.
	Pods []PodData `json:"pods,omitempty"`
}

type PvcStatus struct {
	// pvName contains the name of the Persistent Volume that was created for the
	// storage of the extract GPU Kernel Cache.
	PvName string `json:"pvName,omitempty"`

	// pvcName contains the name of the Persistent Volume Claim that was created
	// for the storage of the extract GPU Kernel Cache.
	PvcName string `json:"pvcName,omitempty"`

	// conditions contains the summary state for the GPU Kernel Cache on the
	// Kubernetes node referenced by status.nodeName.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type GKMCacheNodeStatus struct {
	// nodeName is the name of the Kubernetes Node this instance is created.
	NodeName string `json:"nodeName"`

	// resolvedDigest contains the digest of the image after it has been verified.
	// This is a copy of the field in the GKMCache or ClusterGKMCache.
	ResolvedDigest string `json:"resolvedDigest,omitempty"`

	// counts contains statistics on the deployment of the GPU Kernel Cache for the
	// Kubernetes node referenced in nodeName.
	Counts CacheCounts `json:"counts"`

	// gpus is a list of GPUs detected on the Kubernetes Node along with metadata
	// about each GPU.
	GpuStatuses []GpuStatus `json:"gpus,omitempty"`

	// caches is the list of GKMCache or ClusterGKMCache instances that this
	// GKMCacheNode or ClusterGKMCacheNode is keeping status for along with state
	// for each.
	CacheStatuses map[string]map[string]CacheStatus `json:"caches,omitempty"`
}

// PvcOwner describes which module is responsible for managing the PVCs.
// +kubebuilder:validation:Enum=Unknown;Agent;Operator
type PvcOwner string

const (
	// PvcOwnerUnknown means that the owner hasn't been evaluated yet.
	PvcOwnerUnknown PvcOwner = "Unknown"
	// PvcOwnerAgent means that the GKM Agent manages the PVCs. There will be a PVC per Node because
	// the StorageClass does not support ReadOnlyMany.
	PvcOwnerAgent PvcOwner = "Agent"
	// PvcOwnerOperator means that GKM Operator manages the PVCs. There will be one PVC per Cluster
	// because the StorageClass does support ReadOnlyMany.
	PvcOwnerOperator PvcOwner = "Operator"
)

// GkmConditionType is a condition and used to indicate the status of a GKM Cache
// or GKM Cache on a given node.
type GkmConditionType string

const (
	// GkmCondPending indicates that GKM has not yet completed
	// reconciling the GKM Cache on the given node.
	GkmCondPending GkmConditionType = "Pending"

	// GkmCondDownloading indicates that GKM has started a Job to extract the
	// Cache, but the extraction not yet completed.
	GkmCondDownloading GkmConditionType = "Downloading"

	// GkmCondExtracted indicates that the GKM Cache has been
	// successfully extracted as requested on the given node.
	GkmCondExtracted GkmConditionType = "Extracted"

	// GkmCondRunning indicates that the GKM Cache has been
	// successfully extracted and is being used by a Pod on the given node.
	GkmCondRunning GkmConditionType = "Running"

	// GkmCondOutdated indicates that the GKM Cache has been
	// successfully extracted and is being used by a Pod on the given node
	// but a newer image digest exists.
	GkmCondOutdated GkmConditionType = "Outdated"

	// GkmCondError indicates that an error has occurred on the given
	// node while attempting to apply the configuration described in the CRD.
	GkmCondError GkmConditionType = "Error"

	// GkmCondUnloadError indicates that the GKM Cache was marked
	// for deletion, but removing GK Cache was unsuccessful on the
	// given node.
	GkmCondUnloadError GkmConditionType = "UnloadError"
)

// Condition is a helper method to promote any given GkmConditionType to a
// full metav1.Condition in an opinionated fashion.
func (b GkmConditionType) Condition() metav1.Condition {
	cond := metav1.Condition{}

	switch b {
	case GkmCondPending:
		condType := string(GkmCondPending)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Pending",
			Message: "Not yet complete",
		}
	case GkmCondDownloading:
		condType := string(GkmCondDownloading)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Downloading",
			Message: "Job to extract Kernel Cache in progress",
		}
	case GkmCondExtracted:
		condType := string(GkmCondExtracted)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Extracted",
			Message: "The Kernel Cache has been extracted onto the host",
		}
	case GkmCondRunning:
		condType := string(GkmCondRunning)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Running",
			Message: "The Kernel Cache has been extracted and is in use by one or more pods",
		}
	case GkmCondOutdated:
		condType := string(GkmCondOutdated)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Outdated",
			Message: "The Kernel Cache is in use by one or more pods but newer version exists",
		}
	case GkmCondError:
		condType := string(GkmCondError)
		cond = metav1.Condition{
			Type:    condType,
			Status:  metav1.ConditionTrue,
			Reason:  "Error",
			Message: "An error occurred trying to extract the Kernel Cache",
		}
	case GkmCondUnloadError:
		condType := string(GkmCondUnloadError)
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
// GkmConditionType is set.
func (b GkmConditionType) IsConditionSet(conditions []metav1.Condition) bool {
	for _, condition := range conditions {
		if condition.Type == string(b) {
			return true
		}
	}
	return false
}

// IsConditionDownloadSet loops through the slice of conditions (should only be one) and determines
// if the input GkmConditionType is one that indicates the Cache has been downloaded..
func IsConditionDownloadSet(conditions []metav1.Condition) bool {
	for _, condition := range conditions {
		if condition.Type == string(GkmCondExtracted) ||
			condition.Type == string(GkmCondRunning) ||
			condition.Type == string(GkmCondOutdated) {
			return true
		}
	}
	return false
}

// IsConditionDownloadSet loops through the slice of conditions (should only be one) and determines
// if the input GkmConditionType is one that indicates the Cache has been downloaded..
func GetLatestConditionType(conditions []metav1.Condition) metav1.Condition {
	latestCondition := GkmCondPending.Condition()
	for _, condition := range conditions {
		latestCondition = condition
	}
	return latestCondition
}

// Helper function to set conditions on the PvcStatus of a GKMCacheNode or ClusterGKMCacheNode object.
func SetPvcStatusConditions(pvcStatus *PvcStatus, condition metav1.Condition) {
	pvcStatus.Conditions = nil
	meta.SetStatusCondition(&pvcStatus.Conditions, condition)
}

// GkmCacheNodeEventReason is an event reason and used to track the major events of a GKMCacheNode
// or ClusterGKMCacheNode on a given node.
type GkmCacheNodeEventReason string

const (
	// GkmCacheNodeEventReasonCreated indicates that a GKMCacheNode or ClusterGKMCacheNode was created.
	GkmCacheNodeEventReasonCreated GkmCacheNodeEventReason = "Created"

	// GkmCacheNodeEventReasonCacheUsed indicates that a GKMCacheNode or ClusterGKMCacheNode is being used
	// by a given workload pod.
	GkmCacheNodeEventReasonCacheUsed GkmCacheNodeEventReason = "CacheUsed"

	// GkmCacheNodeEventReasonCacheReleased indicates that a GKMCacheNode or ClusterGKMCacheNode is no longer
	// being used by a given workload pod.
	GkmCacheNodeEventReasonCacheReleased GkmCacheNodeEventReason = "CacheRelease"

	// GkmCacheNodeEventReasonDeleting indicates that a GKMCacheNode or ClusterGKMCacheNode is being deleted
	// but the Cache is being used by one or more workload pods.
	GkmCacheNodeEventReasonDeleting GkmCacheNodeEventReason = "Deleting"
)
