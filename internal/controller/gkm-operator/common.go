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

package gkmOperator

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/common"
	"github.com/redhat-et/GKM/pkg/utils"
)

// GKMInstance is a generic interface that can either be a gkmv1alpha1.GKMCache or
// a gkmv1alpha1.ClusterGKMCache. This is used to allow both a GKMCache and a ClusterGKMCache
// to be processed by the same code.
type GKMInstance interface {
	GetName() string
	GetNamespace() string
	GetPodTemplate() *gkmv1alpha1.PodTemplate
	GetStorageClassName() string
	GetAccessMode() []corev1.PersistentVolumeAccessMode
	GetWorkloadNamespaces() []string
	GetPvcOwner() gkmv1alpha1.PvcOwner
	GetAnnotations() map[string]string
	GetLabels() map[string]string
	GetImage() string
	GetStatus() *gkmv1alpha1.GKMCacheStatus
	GetClientObject() client.Object
}

// GKMInstanceList is a generic interface that is a list of type C, which is a list
// of GKMInstance, which is either GKMCache or ClusterGKMCache.
type GKMInstanceList[C any] interface {
	// gkmv1alpha1.GKMCacheList | gkmv1alpha1.ClusterGKMCacheList
	GetItems() []C
	GetItemsLen() int
}

// GKMNodeInstance is a generic interface that can either be a gkmv1alpha1.GKMCacheNode
// or a gkmv1alpha1.ClusterGKMCacheNode. This is used to allow both a GKMCacheNode and a
// ClusterGKMCacheNode to be processed by the same code.
type GKMNodeInstance interface {
	GetName() string
	GetNamespace() string
	GetAnnotations() map[string]string
	GetLabels() map[string]string
	GetStatus() *gkmv1alpha1.GKMCacheNodeStatus
	GetNodeName() string
	GetClientObject() client.Object
}

// GKMNodeInstanceList is a generic interface that is a list of type N, which is a list
// of GKMNodeInstance, which is either GKMCacheNode or ClusterGKMCacheNode.
type GKMNodeInstanceList[N any] interface {
	// gkmv1alpha1.GKMCacheNodeList | gkmv1alpha1.ClusterGKMCacheNodeList
	GetItems() []N
	GetItemsLen() int
}

type ReconcilerCommonOperator[
	C GKMInstance,
	CL GKMInstanceList[C],
	N GKMNodeInstance,
	NL GKMNodeInstanceList[N],
] struct {
	client.Client
	Scheme          *runtime.Scheme
	Logger          logr.Logger
	NoGpu           bool
	ExtractImage    string
	CrdCacheStr     string // For logging/errors: GKMCache or ClusterGKMCache
	CrdCacheNodeStr string // For logging/errors: GKMCacheNode or ClusterGKMCacheNode
}

// OperatorReconciler is an interface that defines the methods needed to reconcile
// a GKMCache or ClusterGKMCache object. The only difference between the two
// object is that a Cluster object does not have a Namespace (which is just "").
type OperatorReconciler[
	C GKMInstance,
	CL GKMInstanceList[C],
	N GKMNodeInstance,
	NL GKMNodeInstanceList[N],
] interface {
	// Reconcile is the main entry point to the reconciler. It will be called by
	// the controller runtime when something happens that the reconciler is
	// interested in. When Reconcile() is invoked, it initializes some state in
	// the given object specific structure, retrieves a list of all Caches of the given
	// type, and then calls reconcileCommon().
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)

	// SetupWithManager registers the reconciler with the manager and defines
	// which kubernetes events will trigger a reconcile.
	SetupWithManager(mgr ctrl.Manager) error

	// GetCacheList calls the Kubernetes API server to retrieve a list of GKMCache or ClusterGKMCache objects.
	getCacheList(ctx context.Context, opts []client.ListOption) (*CL, error)

	// GetCacheNodeList calls the Kubernetes API server to retrieve a list of GKMCacheNode or
	// ClusterGKMCacheNode objects.
	getCacheNodeList(ctx context.Context, opts []client.ListOption) (*NL, error)

	cacheUpdateStatus(ctx context.Context, gkmCache *C, cacheStatus *gkmv1alpha1.GKMCacheStatus, reason string) error

	isBeingDeleted(gkmCache *C) bool

	cacheAddFinalizer(ctx context.Context, gkmCache *C) (bool, error)
	cacheRemoveFinalizer(ctx context.Context, gkmCache *C) (bool, error)
}

// reconcileCommonOperator is the common reconciler loop called by each the GKMCache
// and ClusterGKMCache Operator reconcilers.  It reconciles each GKMCache or
// ClusterGKMCache in the retrieved list, reading all the associated GKMCacheNode or
// ClusterGKMCacheNode objects and consolidating the state of each node in the GKMCache
// or ClusterGKMCache Status field. The Operator owns the GKMCache and ClusterGKMCache
// Objects, so the Operator will call KubeAPI to update the objects when needed.
// Agent reconciler only reads the objects. The Agent owns GKMCacheNode and
// ClusterGKMCacheNode Objects, and calls KubeAPI Server to make sure they reflect
// the current state of the GKMCache and ClusterGKMCache Objects on a given node.
func (r *ReconcilerCommonOperator[C, CL, N, NL]) reconcileCommonOperator(
	ctx context.Context,
	reconciler OperatorReconciler[C, CL, N, NL],
) (ctrl.Result, error) {
	errorHit := false
	stillInUse := false

	r.Logger.V(1).Info("Start reconcileCommonOperator()")

	// Get the list of existing GKMCache or ClusterGKMCache objects from KubeAPI Server.
	gkmCacheList, err := reconciler.getCacheList(ctx, []client.ListOption{})
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryOperatorFailure},
			fmt.Errorf("failed getting list of %s for full reconcile: %v",
				r.CrdCacheStr,
				err)
	}

	if (*gkmCacheList).GetItemsLen() == 0 {
		// KubeAPI doesn't have any GKMCache instances, so nothing to do.
		r.Logger.Info("GKMCache Status Controller found no caches")
		return ctrl.Result{Requeue: false}, nil
	}

	// There are GKMCache instances created, so loop through each and reconcile each.
	for _, gkmCache := range (*gkmCacheList).GetItems() {
		r.Logger.V(1).Info("Reconciling",
			"Object", r.CrdCacheStr,
			"Namespace", gkmCache.GetNamespace(),
			"Name", gkmCache.GetName(),
			"StorageClass", gkmCache.GetStorageClassName(),
			"PvcOwner", gkmCache.GetPvcOwner(),
			"AccessMode", gkmCache.GetAccessMode(),
		)

		// See if Digest has been set (Webhook validated and image is allowed to be used).
		annotations := gkmCache.GetAnnotations()
		resolvedDigest, digestFound := annotations[utils.GKMCacheAnnotationResolvedDigest]
		if !digestFound || resolvedDigest == "" {
			// If digest not found, Webhook is still processing, skip over and reconcile on
			// next time in loop.
			r.Logger.Info("Digest NOT Found, Webhook still processing.",
				"Object", r.CrdCacheStr,
				"Namespace", gkmCache.GetNamespace(),
				"Name", gkmCache.GetName())
			continue
		}
		capacity, capFound := annotations[utils.GKMCacheAnnotationCacheSizeBytes]
		if !capFound {
			capacity = "1Gi"
			r.Logger.Info("Capacity NOT Found, setting to 1GB")
		}

		if !reconciler.isBeingDeleted(&gkmCache) {
			// Add Finalizer to GKMCache or ClusterGKMCache if not there. This is a KubeAPI call,
			// so return if finalizer needed to be added.
			changed, err := reconciler.cacheAddFinalizer(ctx, &gkmCache)
			if err != nil {
				errorHit = true
				continue
			} else if changed {
				// GKMCache object was updated. Return and change will retrigger a new reconcile.
				return ctrl.Result{Requeue: false}, nil
			}
		}

		gkmCacheStatus := gkmCache.GetStatus().DeepCopy()
		gkmCacheStatus.Counts = gkmv1alpha1.CacheCounts{}
		gkmCacheStatus.ResolvedDigest = resolvedDigest

		if gkmCacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerUnknown || gkmCacheStatus.PvcOwner == "" {
			// Initialize the condition to pending.
			r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondPending.Condition())

			gkmCacheStatus.PvcOwner = gkmv1alpha1.PvcOwnerAgent
			accessMode := gkmCache.GetAccessMode()
			for _, mode := range accessMode {
				if mode == corev1.ReadOnlyMany {
					gkmCacheStatus.PvcOwner = gkmv1alpha1.PvcOwnerOperator
				}
			}
			r.Logger.Info("Owner not set, setting now", "Updated Value", gkmCacheStatus.PvcOwner)
		}

		// If the PVC AccessMode is ReadOnlyMany, then only one PVC per Namespace needs to be created
		// and the storage backend will handle propagating the extracted cache to each node. Since
		// there is only one, the Operator handles the creation here.
		if gkmCacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerOperator {
			updated := false
			updateReason := ""

			// Loop through the list of Namespaces. For GKMCache, it's just the namespace
			// GKMCache is created in. For ClusterGKMCache, it's the Workload Namespace list
			// that was provided in ClusterGKMCache.
			namespaceList := gkmCache.GetWorkloadNamespaces()
			if len(namespaceList) == 0 {
				if gkmCache.GetNamespace() == "" {
					r.Logger.Info("No namespaces in ClusterGKMCache Spec.WorkloadNamespaces, so no PVCs created",
						"Namespace", gkmCache.GetNamespace(),
						"Name", gkmCache.GetName(),
					)
				}
			}
			for _, pvcNamespace := range namespaceList {
				// Get the PVC Status, which is the Per Namespace PV and PVC information.
				if gkmCacheStatus.PvcStatus == nil {
					gkmCacheStatus.PvcStatus = make(map[string]gkmv1alpha1.PvcStatus)
					updated = true
					updateReason = "PvcStatus Allocation"
				}

				pvcStatus, pvcStatusExisted := gkmCacheStatus.PvcStatus[pvcNamespace]
				if !pvcStatusExisted {
					pvcStatus = gkmv1alpha1.PvcStatus{}
					gkmv1alpha1.SetPvcStatusConditions(&pvcStatus, gkmv1alpha1.GkmCondPending.Condition())
					updated = true
					updateReason = "PvcStatus Initialization"
				}

				r.Logger.Info("Owner Operator, manage PV/PVC",
					"Namespace", gkmCache.GetNamespace(),
					"Name", gkmCache.GetName(),
					"PVC Namespace", pvcNamespace,
					"Starting PV", pvcStatus.PvName,
					"Starting PVC", pvcStatus.PvcName)

				// Since Operator owns PV/PVC, manage each now.
				// If updated is already true, still manage PV and PVCs, because up to this
				// point, it's just been initialization and allocation of structures, no
				// actual work on kube objects.
				if pvcUpdated, pvcReason, err := r.managePVandPVC(
					ctx,
					reconciler,
					&gkmCache,
					gkmCacheStatus,
					&pvcStatus,
					pvcNamespace,
					capacity,
				); err != nil {
					errorHit = true
					continue
				} else if pvcUpdated {
					updated = true
					updateReason = pvcReason
				}

				if !updated {
					// Launch Job to Extract Cache
					jobUpdated, pending, jobUpdateReason, err := r.manageJob(
						ctx,
						&gkmCache,
						&pvcStatus,
						pvcNamespace,
						resolvedDigest,
					)
					if err != nil {
						errorHit = true
						continue
					}
					if jobUpdated {
						updated = true
						updateReason = jobUpdateReason
					}
					if pending {
						stillInUse = true
					}
				}

				if updated {
					// Update the Cache Status copy of the PVC Status before writing the data below.
					gkmCacheStatus.PvcStatus[pvcNamespace] = pvcStatus
					break
				}
			}

			// Call KubeAPI to update the Status for the GKMCache (or ClusterGKMCache) that was
			// modified above.
			if updated {
				gkmCacheStatus.LastUpdated = metav1.Now()
				err = reconciler.cacheUpdateStatus(ctx, &gkmCache, gkmCacheStatus, updateReason)
				if err != nil {
					errorHit = true
					continue
				} else {
					// GKMCacheNode Object was updated successfully.
					// Return and Reconcile will be retriggered with the GKMCacheNode Object.
					//return ctrl.Result{Requeue: false}, nil
					return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
				}
			}
		}

		// Call KubeAPI to Retrieve the list of GKMCacheNodes for this Namespace.
		// Should be one per Node if GKMCache was created in the Namespace.
		opts := []client.ListOption{
			client.InNamespace(gkmCache.GetNamespace()),
		}
		gkmCacheNodeList, err := reconciler.getCacheNodeList(ctx, opts)
		if err != nil {
			// Error returned if unable to call KubeAPI. Don't block Reconcile on one instance,
			// log and go to next GKMCache.
			r.Logger.Error(err, "failed to get GKMCacheNode List", "Namespace", gkmCache.GetNamespace(), "Name", gkmCache.GetName())
			errorHit = true
			continue
		}

		// Loop through each GKMCacheNode (i.e. each Node)
		for _, gkmCacheNode := range (*gkmCacheNodeList).GetItems() {
			nodeStatus := gkmCacheNode.GetStatus()
			if nodeStatus != nil {
				// See if this GKMCache has been added to the GKMCacheNode
				if _, ok := nodeStatus.CacheStatuses[gkmCache.GetName()]; ok {
					// This Cache was found in a GKMCacheNode instance
					gkmCacheStatus.Counts.NodeCnt += nodeStatus.Counts.NodeCnt
					gkmCacheStatus.Counts.NodeInUseCnt += nodeStatus.Counts.NodeInUseCnt
					gkmCacheStatus.Counts.NodeNotInUseCnt += nodeStatus.Counts.NodeNotInUseCnt
					gkmCacheStatus.Counts.NodeErrorCnt += nodeStatus.Counts.NodeErrorCnt
					gkmCacheStatus.Counts.PodRunningCnt += nodeStatus.Counts.PodRunningCnt
					gkmCacheStatus.Counts.PodOutdatedCnt += nodeStatus.Counts.PodOutdatedCnt
				}
			}
		}

		r.Logger.V(1).Info("Processed GKMCache",
			"Namespace", gkmCache.GetNamespace(),
			"CacheName", gkmCache.GetName(),
			"NodeCnt", gkmCacheStatus.Counts.NodeCnt,
			"NodeInUse", gkmCacheStatus.Counts.NodeInUseCnt,
			"NodeNotInUse", gkmCacheStatus.Counts.NodeNotInUseCnt,
			"NodeError", gkmCacheStatus.Counts.NodeErrorCnt,
			"PodRunning", gkmCacheStatus.Counts.PodRunningCnt,
			"PodOutdated", gkmCacheStatus.Counts.PodOutdatedCnt,
			"Conditions", gkmCacheStatus.Conditions,
		)

		if !reconciler.isBeingDeleted(&gkmCache) {
			reason := "Update Counts"
			// Adjust Condition if need. If Operator owns the PVC extraction, then
			// wait for Pending and Downloading state to clear before adjusting based
			// on GKMCacheNode or ClusterGKMCacheNode counts.
			if gkmCacheStatus.Counts.NodeErrorCnt != 0 {
				if !gkmv1alpha1.GkmCondError.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondError.Condition())
					reason = "Set Error Condition"
				}
			} else if gkmCacheStatus.Counts.PodOutdatedCnt != 0 {
				if !gkmv1alpha1.GkmCondOutdated.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondOutdated.Condition())
					reason = "Set Outdated Condition"
				}
			} else if gkmCacheStatus.Counts.NodeInUseCnt != 0 {
				if !gkmv1alpha1.GkmCondRunning.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondRunning.Condition())
					reason = "Set Running Condition"
				}
			} else if gkmCacheStatus.Counts.NodeNotInUseCnt != 0 {
				if !gkmv1alpha1.GkmCondExtracted.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondExtracted.Condition())
					reason = "Set Extracted Condition"
				}
			}

			if !reflect.DeepEqual(gkmCache.GetStatus(), gkmCacheStatus) {
				gkmCacheStatus.LastUpdated = metav1.Now()

				if err := reconciler.cacheUpdateStatus(ctx, &gkmCache, gkmCacheStatus, reason); err != nil {
					errorHit = true
					continue
				} else {
					// GKMCache Object was updated successfully.
					// Return and Reconcile will be retriggered with the GKMCache Object.
					return ctrl.Result{Requeue: false}, nil
				}
			}
		} else {
			if gkmCacheStatus.Counts.NodeCnt == 0 {
				// Everything should be cleaned up, so delete the GKMCacheNode specific
				// finalizer from the GKMCache.
				changed, err := reconciler.cacheRemoveFinalizer(ctx, &gkmCache)
				if err != nil {
					errorHit = true
					continue
				} else if changed {
					// GKMCache object was updated. Return and change will retrigger a new reconcile.
					return ctrl.Result{Requeue: false}, nil
				}
			} else {
				r.Logger.Info("Deleting GKMCache still in progress",
					"Namespace", gkmCache.GetNamespace(),
					"CacheName", gkmCache.GetName(),
					"Pending", gkmCacheStatus.Counts.NodeCnt,
				)
			}
		}
	}

	if errorHit || stillInUse {
		// If an error was encountered during a single GKMCache instance, or a Job to extract
		// the Cache is still in progress, retry after a pause.
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryOperatorFailure}, nil
	} else {
		return ctrl.Result{Requeue: false}, nil
	}
}

// setCacheConditions is a helper function to set conditions on the a GKMCache or ClusterGKMCache object.
func (r *ReconcilerCommonOperator[C, CL, N, NL]) setCacheConditions(gkmCacheStatus *gkmv1alpha1.GKMCacheStatus, condition metav1.Condition) {
	gkmCacheStatus.Conditions = nil
	meta.SetStatusCondition(&gkmCacheStatus.Conditions, condition)
}

// managePVandPVC manages the PV and PVC that the GPU Kernel Cache is extracted to. If PVC does not exist, then
// this function calls KubeAPI to create the PVC. It MAY need to create the PV first. If both are created, this
// function determines if the PVC is in a valid state to receive the extracted GPU Kernel Cache.
func (r *ReconcilerCommonOperator[C, CL, N, NL]) managePVandPVC(
	ctx context.Context,
	reconciler OperatorReconciler[C, CL, N, NL],
	gkmCache *C,
	cacheStatus *gkmv1alpha1.GKMCacheStatus,
	pvcStatus *gkmv1alpha1.PvcStatus,
	pvcNamespace string,
	capacity string,
) (bool, string, error) {
	updated := false
	updateReason := ""
	pvCreated := false

	// If the condition on the PVC Status is Pending, then a Job to extract the cache has not been
	// launched for this Namespace. Make sure the PV and PVC are in a valid state to handle the extraction.
	if gkmv1alpha1.GkmCondPending.IsConditionSet(pvcStatus.Conditions) {
		r.Logger.Info("Condition is Pending so managing PV/PVC")
		if cacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerOperator {
			// The preferred method for creating a PV is to create the PVC and Kubelet auto-creates the PV.
			// In a KIND cluster, there is not a true CSI driver for storage management, so the PV must be
			// manually created.
			if r.NoGpu {
				found, updatedName, err := common.PvExists(
					ctx,
					r.Client,
					(*gkmCache).GetName(),
					"", // NodeName
					pvcStatus.PvName,
					pvcNamespace,
					cacheStatus.ResolvedDigest,
					r.Logger,
				)
				if err != nil {
					return updated, updateReason, err
				} else if updatedName != "" {
					pvcStatus.PvName = updatedName
					updated = true
					updateReason = "Writing PV Name"
					pvCreated = true
				} else if !found {
					// Call KubeAPI to create the PV.
					pvcStatus.PvName = utils.GenerateUniqueName((*gkmCache).GetName())
					r.Logger.Info("BILLY: Generated PV Name", "Name", pvcStatus.PvName)

					accessModes := []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteMany,
					}

					err := common.CreatePv(
						ctx,
						r.Client,
						r.Scheme,
						(*gkmCache).GetClientObject(),
						(*gkmCache).GetNamespace(),
						(*gkmCache).GetName(),
						"", // NodeName
						pvcStatus.PvName,
						pvcNamespace,
						accessModes,
						(*gkmCache).GetStorageClassName(),
						capacity,
						cacheStatus.ResolvedDigest,
						r.Logger,
					)

					if err != nil {
						return false, updateReason, err
					}

					updated = true
					updateReason = "Create PV"
					pvCreated = true
				}
			}

			// If PV was not written above, then determine if PVC needs to be created.
			if !pvCreated {
				found, updatedName, err := common.PvcExists(
					ctx,
					r.Client,
					(*gkmCache).GetName(),
					"", // NodeName
					pvcStatus.PvcName,
					pvcNamespace,
					cacheStatus.ResolvedDigest,
					r.Logger,
				)
				if err != nil {
					return updated, updateReason, err
				} else if updatedName != "" {
					pvcStatus.PvcName = updatedName
					updated = true
					updateReason = "Writing PVC Name"
				} else if !found {
					// Call KubeAPI to create the PVC.
					//
					// For both GKMCache and ClusterGKMCache, just use the cache name, because PVCs
					// always created in a Namespace. For GKMCache, it's the same namespaces as the
					// GKMCache. For ClusterGKMCache, name is unique at cluster level and will be
					// created in GKMDefaultNamespace.
					pvcStatus.PvcName = (*gkmCache).GetName()

					accessModes := []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteMany,
					}

					err := common.CreatePvc(
						ctx,
						r.Client,
						r.Scheme,
						(*gkmCache).GetClientObject(),
						(*gkmCache).GetNamespace(),
						(*gkmCache).GetName(),
						"",
						pvcStatus.PvName,
						pvcStatus.PvcName,
						pvcNamespace,
						accessModes,
						(*gkmCache).GetStorageClassName(),
						capacity,
						cacheStatus.ResolvedDigest,
						r.Logger,
					)

					if err != nil {
						return updated, updateReason, err
					}

					updated = true
					updateReason = "Create PVC"
				}
			}
		}
	}

	return updated, updateReason, nil
}

// manageJob determines if the GPU Kernel Cache has been extracted. If not, checks the condition and either
// Launches a Job to extract it, or calls KubeAPI Server to retrieve the list of Jobs that match the labels
// for a given Cache and Digest and determines the state.
func (r *ReconcilerCommonOperator[C, CL, N, NL]) manageJob(
	ctx context.Context,
	gkmCache *C,
	pvcStatus *gkmv1alpha1.PvcStatus,
	jobNamespace string,
	resolvedDigest string,
) (bool, bool, string, error) {
	updateReason := ""
	updated := false
	stillPending := false
	var err error

	// If the condition on the PVC Status is Pending, then a Job to extract the cache has not been
	// launched. Build up and launch the job.
	if gkmv1alpha1.GkmCondPending.IsConditionSet(pvcStatus.Conditions) {
		// Call KubeAPI to create the Job.
		//
		// For both GKMCache and ClusterGKMCache, just use the cache name, because Jobs are
		// always created in a Namespace. For GKMCache, it's the same namespaces as the
		// GKMCache. For ClusterGKMCache, name is unique at cluster level and will be
		// created in GKMDefaultNamespace.
		jobName := pvcStatus.PvcName

		r.Logger.Info("Cache NOT Extracted, extract now",
			"Namespace", jobNamespace,
			"Job Namespace", (*gkmCache).GetNamespace(),
			"Job Name", jobName,
			"Name", (*gkmCache).GetName(),
			"digest", resolvedDigest,
			"NoGpu", r.NoGpu,
			"ExtractImage", r.ExtractImage,
		)

		err = common.LaunchJob(
			ctx,
			r.Client,
			r.Scheme,
			(*gkmCache).GetClientObject(),
			jobNamespace,
			jobName,
			"", // NodeName
			(*gkmCache).GetImage(),
			resolvedDigest,
			pvcStatus.PvcName,
			r.NoGpu,
			r.ExtractImage,
			(*gkmCache).GetPodTemplate(),
			r.Logger,
		)

		if err != nil {
			// Error returned launching Job to extract the Cache.
			r.Logger.Error(err, "unable to extract cache",
				"Namespace", (*gkmCache).GetNamespace(),
				"Name", (*gkmCache).GetName(),
				"Image", (*gkmCache).GetImage(),
				"PVC Name", pvcStatus.PvcName,
				"Job Namespace", jobNamespace,
				"Job Name", jobName,
			)
		} else {
			gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondDownloading.Condition())
			updated = true
			updateReason = "Update Condition to Downloading"
		}
	} else {
		// Check Conditions to determine if Cache already successfully downloaded (there are
		// multiple states that indicate cache downloaded)
		if gkmv1alpha1.IsConditionDownloadSet(pvcStatus.Conditions) {
			r.Logger.V(1).Info("Cache already Extracted",
				"Object", r.CrdCacheStr,
				"Namespace", (*gkmCache).GetNamespace(),
				"Name", (*gkmCache).GetName())
			return updated, stillPending, updateReason, nil
		}

		//latestJob, err := r.getLatestJob(ctx, reconciler, gkmCache)
		latestJob, err := common.GetLatestJob(
			ctx,
			r.Client,
			jobNamespace,
			(*gkmCache).GetName(),
			resolvedDigest,
			"", // NodeName
			r.Logger,
		)
		if err != nil {
			return updated, stillPending, updateReason, err
		}

		r.Logger.Info("Processing Latest Job",
			"Namespace", (*gkmCache).GetNamespace(),
			"Name", (*gkmCache).GetName(),
			"Succeeded", latestJob.Status.Succeeded,
			"Failed", latestJob.Status.Failed,
			"Active", latestJob.Status.Active,
			"Ready*", latestJob.Status.Ready,
			"Conditions", pvcStatus.Conditions,
		)

		switch {
		case latestJob.Status.Succeeded > 0:
			if !gkmv1alpha1.IsConditionDownloadSet(pvcStatus.Conditions) {
				gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondExtracted.Condition())
				updated = true
				updateReason = "Update Condition to Extracted"
			}
			/*
				case latestJob.Status.Failed > 0:
					if !gkmv1alpha1.GkmCondError.IsConditionSet(pvcStatus.Conditions) {
						gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondError.Condition())
						updated = true
						logStr = "Update Condition to Error"
					}
			*/
		case latestJob.Status.Ready != nil && *latestJob.Status.Ready > 0:
			if !gkmv1alpha1.GkmCondDownloading.IsConditionSet(pvcStatus.Conditions) {
				gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondDownloading.Condition())
				updated = true
				updateReason = "Update Condition to Downloading"
			}
		default:
			stillPending = true
		}
	}

	return updated, stillPending, updateReason, err
}
