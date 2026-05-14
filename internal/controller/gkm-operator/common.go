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
	"k8s.io/apimachinery/pkg/api/errors"
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
	KindCluster     bool
	ExtractLogLevel string
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

	cacheUpdateStatus(ctx context.Context, gkmCache *C, cacheStatus *gkmv1alpha1.GKMCacheStatus, reason string) (bool, error)

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

	// This is a Map indexed by the Cache Namespace and Name. If a GKMCache or
	// ClusterGKMCache instance is deleted while the associated Serving PVC is
	// still being used by a Pod, then that PVC is "stranded" and needs to be
	// cleaned up. This map tracks what caches were processed in the main loop
	// so that when walking PVCs the code can determine which PVCs are stranded
	// and need to be checked if they are still in use and can be deleted.
	inUseGkmCacheList := make(map[string]map[string]bool)

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
		r.Logger.V(1).Info("GKMCache Status Controller found no caches")
	} else {
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

			cacheDeleting := reconciler.isBeingDeleted(&gkmCache)
			if _, ok := inUseGkmCacheList[gkmCache.GetNamespace()]; !ok {
				inUseGkmCacheList[gkmCache.GetNamespace()] = make(map[string]bool)
			}
			inUseGkmCacheList[gkmCache.GetNamespace()][gkmCache.GetName()] = cacheDeleting

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

			if !cacheDeleting {
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

			gkmCacheStatus := gkmCache.GetStatus()
			gkmCacheStatus.Counts = gkmv1alpha1.CacheCounts{}
			gkmCacheStatus.ResolvedDigest = resolvedDigest

			// The PvcOwner is the controller that creates and manages the PV/PVC/Job.
			// * If AccessMode is ReadOnlyMany (ROX), then only one PV/PVC/Job is needed for
			//   the Cluster so the Operator creates and manages the resources. The Job downloads
			//   and extracts the content from the OCI Image once to the PVC and the Storage
			//   backend is responsible to distributing the content to each Node. Not all
			//   Clusters have a Storage instance that supports this, so RWO must also be supported.
			// * If AccessMode is ReadWriteOnce (RWO), then:
			//   * Agent on each Node creates and manages a PV/PVC/Job per Node. The Job downloads
			//     and extracts the content from the OCI Image on each Node. These are the download
			//     PV/PVC/Job instances.
			//   * Once the download has occurred, the Operator creates one Serving PV/PVC that
			//     refeneces the path used in the download PVC. This Serving PVC is what is given to
			//     the ISVC to mount in the workload. Note, no Serving Job is needed.
			if gkmCacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerUnknown || gkmCacheStatus.PvcOwner == "" {
				// Initialize the condition to pending.
				r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondPending.Condition())

				gkmCacheStatus.PvcOwner = determineOwner(gkmCache.GetAccessMode())
				r.Logger.Info("Owner not set, setting now", "Updated Value", gkmCacheStatus.PvcOwner)
			}

			updated := false
			updateReason := ""

			// pvcInUse is used to indicate on a delete of the GKMCache or ClusterGKMCache that a
			// PVC is still in use. The must go ahead and delete the GKMCache or ClusterGKMCache
			// and will use manageStrandedPvcs() to clean up the PV and PVCs once they are no longer
			// being used by a pod.
			pvcInUse := false

			// pvcDeleting is used to indicate that KubeAPI has been called to delete the PVC but
			// the delete is still being processed.
			pvcDeleting := false

			// Map index by Namespace. Contains the collection of counts per Namespace
			// so the Operator created Serving PVC can provide a summary State of the
			// Download PVCs from each Node. Per Namespace is needed for the ClusterGKMCache,
			// because there is a PVC per namespace. Collected for the GKMCache just to
			// simplify code.
			namespaceCnts := make(map[string]*gkmv1alpha1.CacheCounts)

			// If Agent managed, then collect the counts now. For Operator managed,
			// delay the work to collect until there is no more work to do.
			if gkmCacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerAgent {
				if err := r.collectNodeCounts(
					ctx,
					reconciler,
					&gkmCache,
					gkmCacheStatus,
					namespaceCnts,
				); err != nil {
					errorHit = true
					continue
				}
			}

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
				var pvcStatus gkmv1alpha1.PvcStatus
				skipPvcCopy := false

				namespaceExists, namespaceDeleting, err := r.namespaceExists(ctx, pvcNamespace)
				if err != nil {
					errorHit = true
					continue
				}

				// CREATE or UPDATE
				if !cacheDeleting && !namespaceDeleting {

					// Get the PVC Status, which is the Per Namespace PV and PVC information.
					if gkmCacheStatus.PvcStatus == nil {
						gkmCacheStatus.PvcStatus = make(map[string]gkmv1alpha1.PvcStatus)
						updated = true
						updateReason = "PvcStatus Allocation"
					}

					var pvcStatusExisted bool
					pvcStatus, pvcStatusExisted = gkmCacheStatus.PvcStatus[pvcNamespace]
					if !pvcStatusExisted {
						pvcStatus = gkmv1alpha1.PvcStatus{}
						gkmv1alpha1.SetPvcStatusConditions(&pvcStatus, gkmv1alpha1.GkmCondPending.Condition())
						pvcStatus.PvcOwner = gkmv1alpha1.PvcOwnerOperator
						updated = true
						updateReason = "PvcStatus Initialization"
					}

					// Manage PV, PVC and Job used for extracted GPU Kernel Cache
					if pvcUpdated, pvcUpdateReason, pending, err := r.managePvcStatusModify(
						ctx,
						reconciler,
						&gkmCache,
						gkmCacheStatus,
						&pvcStatus,
						pvcNamespace,
						resolvedDigest,
						capacity,
						namespaceExists,
						namespaceCnts,
					); err != nil {
						errorHit = true
						continue
					} else if pvcUpdated {
						updated = true
						updateReason = pvcUpdateReason
					} else if pending {
						stillInUse = true
					}
				} else {
					// DELETE

					// Get the PVC Status, which is the Per Namespace PV and PVC information.
					// If it doesn't exist for this Namespace, then move on to the next Namespace.
					if gkmCacheStatus.PvcStatus == nil {
						continue
					}

					var pvcStatusExisted bool
					pvcStatus, pvcStatusExisted = gkmCacheStatus.PvcStatus[pvcNamespace]
					if !pvcStatusExisted {
						continue
					}

					if updated, updateReason, pvcInUse, pvcDeleting, err = common.ManagePvcStatusDelete(
						ctx,
						r.Client,
						gkmCache.GetNamespace(),
						gkmCache.GetName(),
						"", // NodeName
						&pvcStatus,
						gkmv1alpha1.PvcOwnerOperator,
						pvcNamespace,
						resolvedDigest,
						r.Logger,
					); err != nil {
						errorHit = true
						continue
					} else if pvcInUse || pvcDeleting {
						stillInUse = true
						if !gkmv1alpha1.GkmCondDeleting.IsConditionSet(pvcStatus.Conditions) {
							gkmv1alpha1.SetPvcStatusConditions(&pvcStatus, gkmv1alpha1.GkmCondDeleting.Condition())
							updated = true
							updateReason = "Update Condition to Deleting"
						}
					}

					// If nothing was updated, then this PVC Status can be removed.
					if !updated && !pvcInUse && !pvcDeleting {
						delete(gkmCacheStatus.PvcStatus, pvcNamespace)
						updated = true
						skipPvcCopy = true
						updateReason = "Remove PVC Namespace entry"
					}
				}

				if updated {
					if !skipPvcCopy {
						// Update the Cache Status copy of the PVC Status before writing the data below.
						gkmCacheStatus.PvcStatus[pvcNamespace] = pvcStatus
					}
					break
				}
			} // For each Namespace

			// Call KubeAPI to update the Status for the GKMCache (or ClusterGKMCache) that was
			// modified above.
			if updated {
				gkmCacheStatus.LastUpdated = metav1.Now()
				changed, err := reconciler.cacheUpdateStatus(ctx, &gkmCache, gkmCacheStatus, updateReason)
				if err != nil {
					errorHit = true
					continue
				} else {
					// GKMCache Object was updated successfully.
					// Return and Reconcile will be retriggered with the GKMCache Object.
					r.Logger.V(1).Info("Return after CacheStatus Write", "Reason", updateReason, "changed", changed)
					if changed {
						return ctrl.Result{Requeue: false}, nil
					} else {
						return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}
				}
			}

			// If Agent managed, the counts were collected above, so collect for
			// Operator managed now.
			if gkmCacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerOperator {
				if err := r.collectNodeCounts(
					ctx,
					reconciler,
					&gkmCache,
					gkmCacheStatus,
					namespaceCnts,
				); err != nil {
					errorHit = true
					continue
				}
			}

			// Adjust the Cache Condition if need. This is a summary of all the Nodes.
			if gkmCacheStatus.Counts.NodeErrorCnt != 0 {
				if !gkmv1alpha1.GkmCondError.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondError.Condition())
					updated = true
					updateReason = "Set Error Cache Condition"
				}
			} else if gkmCacheStatus.Counts.PodOutdatedCnt != 0 {
				if !gkmv1alpha1.GkmCondOutdated.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondOutdated.Condition())
					updated = true
					updateReason = "Set Outdated Cache Condition"
				}
			} else if gkmCacheStatus.Counts.NodeInUseCnt != 0 {
				if !gkmv1alpha1.GkmCondRunning.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondRunning.Condition())
					updated = true
					updateReason = "Set Running Cache Condition"
				}
			} else if gkmCacheStatus.Counts.NodeNotInUseCnt != 0 {
				if !gkmv1alpha1.GkmCondExtracted.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondExtracted.Condition())
					updated = true
					updateReason = "Set Extracted Cache Condition"
				}
			}

			if updated || !reflect.DeepEqual(gkmCache.GetStatus(), gkmCacheStatus) {
				gkmCacheStatus.LastUpdated = metav1.Now()

				if changed, err := reconciler.cacheUpdateStatus(ctx, &gkmCache, gkmCacheStatus, updateReason); err != nil {
					errorHit = true
					continue
				} else {
					// GKMCache Object was updated successfully.
					// Return and Reconcile will be retriggered with the GKMCache Object.
					r.Logger.V(1).Info("Return after CacheStatus Write", "Reason", updateReason, "changed", changed)
					if changed {
						return ctrl.Result{Requeue: false}, nil
					} else {
						return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}
				}
			}

			if cacheDeleting {
				if gkmCacheStatus.Counts.NodeCnt == 0 && !pvcDeleting {
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
					stillInUse = true
				}
			}
		} // FOR EACH GKMCache
	}

	// Walk the GKMCacheNode or ClusterGKMCacheNode and determine if any PVCs are stranded.
	// If so, see if the Pod using them is still active. If not, clean them up.
	if strandedInUse, strandedErrFlag := r.manageStrandedPvcs(ctx, reconciler, inUseGkmCacheList); strandedErrFlag {
		errorHit = true
	} else if strandedInUse {
		stillInUse = true
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

// managePvcStatusModify handles Create and Update calls. If necessary, it will handle the creation
// of a PV, PVC or Job to extract GPU Kernel Cache to the PVC, depending on the state.
func (r *ReconcilerCommonOperator[C, CL, N, NL]) managePvcStatusModify(
	ctx context.Context,
	reconciler OperatorReconciler[C, CL, N, NL],
	gkmCache *C,
	gkmCacheStatus *gkmv1alpha1.GKMCacheStatus,
	pvcStatus *gkmv1alpha1.PvcStatus,
	pvcNamespace string,
	resolvedDigest string,
	capacity string,
	namespaceExists bool,
	namespaceCnts map[string]*gkmv1alpha1.CacheCounts,
) (bool, string, bool, error) {
	updated := false
	updateReason := ""
	pending := false
	var err error

	// Since Operator owns PV/PVC, manage each now.
	// If updated is already true, still manage PV and PVCs, because up to this
	// point, it's just been initialization and allocation of structures, no
	// actual work on kube objects.
	if updated, updateReason, pending, err := r.managePVandPVC(
		ctx,
		reconciler,
		gkmCache,
		gkmCacheStatus,
		pvcStatus,
		pvcNamespace,
		capacity,
		namespaceExists,
		namespaceCnts,
	); err != nil || updated || pending {
		return updated, updateReason, pending, err
	}

	// Launch Job to Extract Cache
	if gkmCacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerOperator {
		updated, updateReason, pending, err = r.manageJob(
			ctx,
			gkmCache,
			pvcStatus,
			pvcNamespace,
			resolvedDigest,
		)
	}

	return updated, updateReason, pending, err
}

// managePVandPVC manages the PV and PVC that the GPU Kernel Cache is extracted to. If PVC does not exist, then
// this function calls KubeAPI to create the PVC. It MAY need to create the PV first. If both are created, this
// function determines if the PVC is in a valid state to receive the extracted GPU Kernel Cache.
func (r *ReconcilerCommonOperator[C, CL, N, NL]) managePVandPVC(
	ctx context.Context,
	reconciler OperatorReconciler[C, CL, N, NL],
	gkmCache *C,
	gkmCacheStatus *gkmv1alpha1.GKMCacheStatus,
	pvcStatus *gkmv1alpha1.PvcStatus,
	pvcNamespace string,
	capacity string,
	namespaceExists bool,
	namespaceCnts map[string]*gkmv1alpha1.CacheCounts,
) (bool, string, bool, error) {
	updated := false
	updateReason := ""
	pending := false

	pvCreated := false

	if gkmv1alpha1.GkmCondNoNamespace.IsConditionSet(pvcStatus.Conditions) {
		if namespaceExists {
			r.Logger.Info("Clearing Condition from No Namespace to Pending")
			gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondPending.Condition())
			updated = true
			updateReason = "Update Condition to Pending from No Namespace"
		}
	}

	// If the condition on the PVC Status is Pending, then a Job to extract the cache has not been
	// launched for this Namespace. Make sure the PV and PVC are in a valid state to handle the extraction.
	if gkmv1alpha1.GkmCondPending.IsConditionSet(pvcStatus.Conditions) {
		r.Logger.Info("Condition is Pending so managing PV/PVC")

		if !namespaceExists {
			gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondNoNamespace.Condition())
			updated = true
			updateReason = "Update Condition to No Namespace"
			return updated, updateReason, pending, nil
		}
		// Hold off creating the Serving PV/PVC until at least one Download PVC has completed
		if gkmCacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerAgent &&
			gkmCacheStatus.Counts.NodeInUseCnt == 0 && gkmCacheStatus.Counts.NodeNotInUseCnt == 0 {
			pending = true
			return updated, updateReason, pending, nil
		}

		// The preferred method for creating a PV is to create the PVC and Kubelet auto-creates the PV.
		// In a KIND cluster, there is not a true CSI driver for storage management, so the PV must be
		// manually created.
		if r.KindCluster {
			_, found, updatedName, err := common.PvExists(
				ctx,
				r.Client,
				(*gkmCache).GetName(),
				"", // NodeName
				pvcStatus.PvName,
				pvcNamespace,
				gkmCacheStatus.ResolvedDigest,
				r.Logger,
			)
			if err != nil {
				return updated, updateReason, pending, err
			} else if updatedName != "" {
				pvcStatus.PvName = updatedName
				updated = true
				updateReason = "Writing PV Name"
				pvCreated = true
			} else if !found {
				// Call KubeAPI to create the PV.
				pvcStatus.PvName = utils.GenerateUniqueName((*gkmCache).GetName())

				accessModes := []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
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
					gkmCacheStatus.ResolvedDigest,
					r.Logger,
				)

				if err != nil {
					return false, updateReason, pending, err
				}

				updated = true
				updateReason = "Create PV"
				pvCreated = true
			}
		}

		// If PV was not written above, then determine if PVC needs to be created.
		if !pvCreated {
			_ /* pvc */, found, updatedName, err := common.PvcExists(
				ctx,
				r.Client,
				(*gkmCache).GetName(),
				"", // NodeName
				pvcStatus.PvcName,
				pvcNamespace,
				gkmCacheStatus.ResolvedDigest,
				r.Logger,
			)
			if err != nil {
				return updated, updateReason, pending, err
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
					corev1.ReadWriteOnce,
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
					gkmCacheStatus.ResolvedDigest,
					r.Logger,
				)

				if err != nil {
					return updated, updateReason, pending, err
				}

				updated = true
				updateReason = "Create PVC"

				// This is the Serving PVC so OCI Image was extracted via the Download PVC, so set
				// condition to extracted.
				if gkmCacheStatus.PvcOwner == gkmv1alpha1.PvcOwnerAgent &&
					!gkmv1alpha1.IsConditionDownloadSet(pvcStatus.Conditions) {
					gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondExtracted.Condition())
				}
			}
		}
	} else {
		if cnts, exists := namespaceCnts[pvcNamespace]; exists {
			// Adjust the Cache Condition if need. This is a summary of all the Nodes.
			if cnts.NodeErrorCnt != 0 {
				if !gkmv1alpha1.GkmCondError.IsConditionSet(pvcStatus.Conditions) {
					gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondExtracted.Condition())
					updated = true
					updateReason = "Set Error PVC Condition"
				}
			} else if cnts.PodOutdatedCnt != 0 {
				if !gkmv1alpha1.GkmCondOutdated.IsConditionSet(pvcStatus.Conditions) {
					gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondOutdated.Condition())
					updated = true
					updateReason = "Set Outdated PVC Condition"
				}
			} else if cnts.NodeInUseCnt != 0 {
				if !gkmv1alpha1.GkmCondRunning.IsConditionSet(pvcStatus.Conditions) {
					gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondRunning.Condition())
					updated = true
					updateReason = "Set Running PVC Condition"
				}
			} else if cnts.NodeNotInUseCnt != 0 {
				if !gkmv1alpha1.GkmCondExtracted.IsConditionSet(pvcStatus.Conditions) {
					gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondExtracted.Condition())
					updated = true
					updateReason = "Set Extracted PVC Condition"
				}
			}
		}
	}

	return updated, updateReason, pending, nil
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
) (bool, string, bool, error) {
	updated := false
	updateReason := ""
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
			"KIND", r.KindCluster,
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
			r.NoGpu,
			r.KindCluster,
			r.ExtractImage,
			pvcStatus,
			(*gkmCache).GetPodTemplate(),
			r.Logger,
			r.ExtractLogLevel,
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
				"Name", (*gkmCache).GetName(),
				"Job Namespace", jobNamespace,
				"Digest", resolvedDigest)
			return updated, updateReason, stillPending, nil
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
		if err != nil || latestJob == nil {
			r.Logger.Info("Unable to get Latest Job",
				"Namespace", (*gkmCache).GetNamespace(),
				"Name", (*gkmCache).GetName(),
				"Image", (*gkmCache).GetImage(),
				"PVC Name", pvcStatus.PvcName,
				"Job Namespace", jobNamespace,
				"Job Name", pvcStatus.JobName,
				"err", err,
			)
			if latestJob == nil {
				stillPending = true
			}
			return updated, updateReason, stillPending, err
		}

		r.Logger.Info("Processing Latest Job",
			"Namespace", (*gkmCache).GetNamespace(),
			"Job Namespace", jobNamespace,
			"Name", (*gkmCache).GetName(),
			"Latest Job Name", latestJob.Name,
			"Succeeded", latestJob.Status.Succeeded,
			"Failed", latestJob.Status.Failed,
			"Active", latestJob.Status.Active,
			"Ready*", latestJob.Status.Ready,
			"Conditions", pvcStatus.Conditions,
		)

		// Job Name is not saved on Create because the an additional hash
		// is add to requested name. So wait to store the Job name until after
		// a query.
		if pvcStatus.JobName != latestJob.Name {
			pvcStatus.JobName = latestJob.Name
			updated = true
			updateReason = "Set Job Name"
		}

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

	return updated, updateReason, stillPending, err
}

func (r *ReconcilerCommonOperator[C, CL, N, NL]) collectNodeCounts(
	ctx context.Context,
	reconciler OperatorReconciler[C, CL, N, NL],
	gkmCache *C,
	gkmCacheStatus *gkmv1alpha1.GKMCacheStatus,
	namespaceCnts map[string]*gkmv1alpha1.CacheCounts,
) error {
	// Call KubeAPI to Retrieve the list of GKMCacheNodes for a Namespace or
	// ClusterGKMCacheNodes. Should be one per Node.
	opts := []client.ListOption{
		client.InNamespace((*gkmCache).GetNamespace()),
	}
	gkmCacheNodeList, err := reconciler.getCacheNodeList(ctx, opts)
	if err != nil {
		// Error returned if unable to call KubeAPI. Don't block Reconcile on one instance,
		// log and go to next GKMCache.
		r.Logger.Error(err, "failed to get GKMCacheNode List",
			"Namespace", (*gkmCache).GetNamespace(),
			"Name", (*gkmCache).GetName())
		return err
	}

	// Loop through each GKMCacheNode (i.e. each Node)
	for _, gkmCacheNode := range (*gkmCacheNodeList).GetItems() {
		nodeStatus := gkmCacheNode.GetStatus()
		if nodeStatus != nil {
			// See if this GKMCache has been added to the GKMCacheNode
			if _, ok := nodeStatus.CacheStatuses[(*gkmCache).GetName()]; ok {
				// This Cache was found in a GKMCacheNode instance so collect a summary
				// of the counts for all Namespaces (cluster scoped may have more than
				// one namespace).
				gkmCacheStatus.Counts.NodeCnt += nodeStatus.Counts.NodeCnt
				gkmCacheStatus.Counts.NodeInUseCnt += nodeStatus.Counts.NodeInUseCnt
				gkmCacheStatus.Counts.NodeNotInUseCnt += nodeStatus.Counts.NodeNotInUseCnt
				gkmCacheStatus.Counts.NodeErrorCnt += nodeStatus.Counts.NodeErrorCnt
				gkmCacheStatus.Counts.PodRunningCnt += nodeStatus.Counts.PodRunningCnt
				gkmCacheStatus.Counts.PodDeletingCnt += nodeStatus.Counts.PodDeletingCnt
				gkmCacheStatus.Counts.PodOutdatedCnt += nodeStatus.Counts.PodOutdatedCnt

				for cacheName, digestList := range nodeStatus.CacheStatuses {
					for digest, cacheStatus := range digestList {
						for namespace, pvcStatus := range cacheStatus.PvcStatus {
							r.Logger.Info("Looping through namespaces",
								"Name", cacheName,
								"Namespace", namespace,
								"Digest", digest,
								"pvcStatus", pvcStatus)

							// Check if namespace exists
							cnts, exists := namespaceCnts[namespace]

							// Allocate if missing
							if !exists {
								cnts = &gkmv1alpha1.CacheCounts{}
								namespaceCnts[namespace] = cnts
							}

							switch gkmv1alpha1.GetLatestConditionType(pvcStatus.Conditions).Type {
							case string(gkmv1alpha1.GkmCondPending):
								// Temp state, ignore
							case string(gkmv1alpha1.GkmCondExtracted):
								cnts.NodeNotInUseCnt++
							case string(gkmv1alpha1.GkmCondRunning):
								cnts.NodeInUseCnt++
							case string(gkmv1alpha1.GkmCondDeleting):
								cnts.PodDeletingCnt++
							case string(gkmv1alpha1.GkmCondError):
								cnts.NodeErrorCnt++
							case string(gkmv1alpha1.GkmCondUnloadError):
								cnts.NodeErrorCnt++
							case string(gkmv1alpha1.GkmCondOutdated):
								cnts.PodOutdatedCnt++
							}
						}
					}
				}
			}
		}
	}

	r.Logger.V(1).Info("Processed GKMCache",
		"Namespace", (*gkmCache).GetNamespace(),
		"CacheName", (*gkmCache).GetName(),
		"NodeCnt", gkmCacheStatus.Counts.NodeCnt,
		"NodeInUse", gkmCacheStatus.Counts.NodeInUseCnt,
		"NodeNotInUse", gkmCacheStatus.Counts.NodeNotInUseCnt,
		"NodeError", gkmCacheStatus.Counts.NodeErrorCnt,
		"PodRunning", gkmCacheStatus.Counts.PodRunningCnt,
		"PodDeleting", gkmCacheStatus.Counts.PodDeletingCnt,
		"PodOutdated", gkmCacheStatus.Counts.PodOutdatedCnt,
		"Conditions", gkmCacheStatus.Conditions,
	)

	return nil
}

// manageStrandedPvcs walks the GKMCacheNode or ClusterGKMCacheNode and determines if any PVCs are
// stranded (GKMCache or ClusterGKMCache was deleted but Pod was still using PVC). If so, see if the
// Pod using them is still active. If not, clean them up.
func (r *ReconcilerCommonOperator[C, CL, N, NL]) manageStrandedPvcs(
	ctx context.Context,
	reconciler OperatorReconciler[C, CL, N, NL],
	inUseGkmCacheList map[string]map[string]bool,
) (bool, bool) {
	pending := false
	errorHit := false

	r.Logger.V(1).Info("ENTER manageStrandedPvcs()")

	pvcList, err := common.GetGkmPvcList(ctx, r.Client, r.Logger)
	if err != nil {
		errorHit = true
		return pending, errorHit
	}

	for _, pvc := range pvcList {
		// Retrieve the Cache Name from the labels in the PVC.
		labels := pvc.GetLabels()

		gkmCacheNamespace, found := labels[utils.PvcLabelCacheNamespace]
		if !found {
			continue
		} else {
			// If the Cache Namespace is empty, then this is a ClusterGKMCache created
			// PVC, make sure the Object is the same. Else handles the inverse.
			if gkmCacheNamespace == "" && r.CrdCacheStr != utils.CrdClusterGKMCache {
				continue
			} else if gkmCacheNamespace != "" && r.CrdCacheStr != utils.CrdGKMCache {
				continue
			}
		}

		gkmCacheName, found := labels[utils.PvcLabelCache]
		if !found {
			continue
		}
		pvName, found := labels[utils.PvcLabelPvName]
		if !found {
			continue
		}
		nodeName, found := labels[utils.PvcLabelNode]
		if !found {
			continue
		}
		digest, found := labels[utils.PvcLabelDigest]
		if !found {
			continue
		}

		if cl, ok := inUseGkmCacheList[gkmCacheNamespace]; ok {
			if _, ok := cl[gkmCacheName]; ok {
				r.Logger.V(1).Info("For PVC Skipping Cache because Cache still exists",
					"Object", r.CrdCacheStr,
					"Namespace", gkmCacheNamespace,
					"Name", gkmCacheName,
				)
				continue
			}
		}

		pvcStatus := gkmv1alpha1.PvcStatus{
			PvName:   pvName,
			PvcName:  pvc.Name,
			PvcOwner: gkmv1alpha1.PvcOwnerOperator,
		}

		pvcUpdated, pvcUpdateReason, pvcInUse, pvcDeleting, err := common.ManagePvcStatusDelete(
			ctx,
			r.Client,
			gkmCacheNamespace,
			gkmCacheName,
			nodeName,
			&pvcStatus,
			gkmv1alpha1.PvcOwnerOperator,
			pvc.GetNamespace(), // pvcNamespace
			digest,
			r.Logger,
		)
		if err != nil {
			errorHit = true
			continue
		}
		// This PVC Status is associated with the GKMCacheNode or ClusterGKMCacheNode.
		// Operator cannot update it. The CacheNode is being used to store the PV and PVC
		// names. So if something was updated (PVC or PV was deleted), just mark pending so
		// this code checks again to see if anything needs to be deleted.
		if pvcUpdated || pvcDeleting {
			pending = true
			r.Logger.Info("PVC still In Use or was Updated",
				"Object", r.CrdCacheNodeStr,
				"Name", gkmCacheName,
				"Digest", digest,
				"Namespace", gkmCacheNamespace,
				"PVC Namespace", pvc.GetNamespace(),
				"PVC Name", pvc.GetName(),
				"Still In Use", pvcInUse,
				"Deleting", pvcDeleting,
				"Updated", pvcUpdated,
				"Reason", pvcUpdateReason,
			)
		}
	}

	// Process PVs. Only need to look for PVs in a Phase of Failed (no PVC).
	// If they have a PVC, they will be in a Phase od Bonded.
	pvList, err := common.GetGkmPvFailedList(ctx, r.Client, r.Logger)
	if err != nil {
		errorHit = true
		return pending, errorHit
	}

	for _, pv := range pvList {
		// Retrieve the Cache Name from the labels in the PV.
		labels := pv.GetLabels()

		gkmCacheNamespace, found := labels[utils.PvLabelCacheNamespace]
		if !found {
			continue
		} else {
			// If the Cache Namespace is empty, then this is a ClusterGKMCache created
			// PV, make sure the Object is the same. Else handles the inverse.
			if gkmCacheNamespace == "" && r.CrdCacheStr != utils.CrdClusterGKMCache {
				continue
			} else if gkmCacheNamespace != "" && r.CrdCacheStr != utils.CrdGKMCache {
				continue
			}
		}

		gkmCacheName, found := labels[utils.PvLabelCache]
		if !found {
			continue
		}
		nodeName, found := labels[utils.PvLabelNode]
		if !found {
			continue
		}
		digest, found := labels[utils.PvLabelDigest]
		if !found {
			continue
		}
		pvcNamespace, found := labels[utils.PvLabelPvcNamespace]
		if !found {
			continue
		}

		if cl, ok := inUseGkmCacheList[gkmCacheNamespace]; ok {
			if _, ok := cl[gkmCacheName]; ok {
				r.Logger.V(1).Info("For PV Skipping Cache because Cache still exists",
					"Object", r.CrdCacheStr,
					"Namespace", gkmCacheNamespace,
					"Name", gkmCacheName,
				)
				continue
			}
		}

		pvcStatus := gkmv1alpha1.PvcStatus{
			PvName:   pv.Name,
			PvcOwner: gkmv1alpha1.PvcOwnerOperator,
		}

		pvUpdated, pvUpdateReason, pvInUse, pvDeleting, err := common.ManagePvcStatusDelete(
			ctx,
			r.Client,
			gkmCacheNamespace,
			gkmCacheName,
			nodeName,
			&pvcStatus,
			gkmv1alpha1.PvcOwnerOperator,
			pvcNamespace,
			digest,
			r.Logger,
		)
		if err != nil {
			errorHit = true
			continue
		}
		// This PV Status is associated with the GKMCacheNode or ClusterGKMCacheNode.
		// Operator cannot update it. The CacheNode is being used to store the PV and PVC
		// names. So if something was updated (PVC or PV was deleted), just mark pending so
		// this code checks again to see if anything needs to be deleted.
		if pvUpdated || pvDeleting {
			pending = true
			r.Logger.Info("PV still In Use or was Updated",
				"Object", r.CrdCacheNodeStr,
				"Name", gkmCacheName,
				"Digest", digest,
				"Namespace", gkmCacheNamespace,
				"PVC Namespace", pvcNamespace,
				"PV Name", pv.GetName(),
				"Still In Use", pvInUse,
				"Deleting", pvDeleting,
				"Updated", pvUpdated,
				"Reason", pvUpdateReason,
			)
		}
	}

	r.Logger.V(1).Info("EXIT manageStrandedPvcs()")

	return pending, errorHit
}

func (r *ReconcilerCommonOperator[C, CL, N, NL]) namespaceExists(ctx context.Context, name string) (bool, bool, error) {
	namespaceExists := false
	namespaceDeleting := false

	ns := &corev1.Namespace{}

	err := r.Client.Get(ctx, client.ObjectKey{Name: name}, ns)
	if err != nil {
		if errors.IsNotFound(err) {
			return namespaceExists, namespaceDeleting, nil
		}
		return namespaceExists, namespaceDeleting, err
	}

	namespaceExists = true
	namespaceDeleting = !(*ns).GetDeletionTimestamp().IsZero()

	return namespaceExists, namespaceDeleting, nil
}

// determineOwner walks the list of AccessMode values and if any of the values are
// ReadOnlyMany then the owner is PvcOwnerOperator, otherwise PvcOwnerAgent.
func determineOwner(accessMode []corev1.PersistentVolumeAccessMode) gkmv1alpha1.PvcOwner {
	pvcOwner := gkmv1alpha1.PvcOwnerAgent
	for _, mode := range accessMode {
		if mode == corev1.ReadOnlyMany {
			pvcOwner = gkmv1alpha1.PvcOwnerOperator
		}
	}
	return pvcOwner
}
