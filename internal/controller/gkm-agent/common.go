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

package gkmAgent

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	mcvDevices "github.com/redhat-et/GKM/mcv/pkg/accelerator/devices"
	mcvClient "github.com/redhat-et/GKM/mcv/pkg/client"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/common"
	"github.com/redhat-et/GKM/pkg/utils"
)

var defaultCacheDir string

func init() {
	initializeCachePath(utils.DefaultCacheDir)
}

// Allow overriding UsageDir location for Testing
var ExportForTestInitializeCachePath = initializeCachePath

func initializeCachePath(value string) {
	defaultCacheDir = value
}

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

type ReconcilerCommonAgent[C GKMInstance, CL GKMInstanceList[C], N GKMNodeInstance] struct {
	client.Client
	Scheme          *runtime.Scheme
	Logger          logr.Logger
	Recorder        record.EventRecorder
	CacheDir        string
	NodeName        string
	NoGpu           bool
	ExtractImage    string
	CrdCacheStr     string // For logging/errors: GKMCache or ClusterGKMCache
	CrdCacheNodeStr string // For logging/errors: GKMCacheNode or ClusterGKMCacheNode
}

// AgentReconciler is an interface that defines the methods needed to reconcile
// a GKMCache or ClusterGKMCache object. The only difference between the two
// object is that a Cluster object does not have a Namespace (which is just "").
type AgentReconciler[C GKMInstance, CL GKMInstanceList[C], N GKMNodeInstance] interface {
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

	getCacheNode(ctx context.Context, cacheNamespace string, cacheName string) (*N, error)
	createCacheNode(ctx context.Context, cacheNamespace, cacheName string) error

	cacheNodeUpdateStatus(ctx context.Context, gkmCacheNode *N, status *gkmv1alpha1.GKMCacheNodeStatus, reason string) error

	isBeingDeleted(gkmCache *C) bool
	validExtractedCache(cacheNamespace string) bool

	cacheNodeAddFinalizer(ctx context.Context, gkmCacheNode *N, cacheName string) (bool, error)
	cacheNodeRemoveFinalizer(ctx context.Context, gkmCacheNode *N, cacheName string) (bool, error)

	cacheNodeRecordEvent(
		gkmCacheNode *N,
		eventReason gkmv1alpha1.GkmCacheNodeEventReason,
		cacheName, podNamespace, podName string,
		count int,
	)
}

// reconcileCommonAgent is the common reconciler loop called by each the GKMCache
// and ClusterGKMCache Agent reconcilers.  It reconciles each GKM Cache in the
// list, making sure an associated GKMCacheNode or ClusterGKMCacheNode is created
// and populated properly, and that the OCI Image in the GKMCache or
// ClusterGKMCache is extracted on the host. The Operator owns the GKMCache
// and ClusterGKMCache Objects, so the Agent reconciler only reads the objects
// and makes sure the intended state is applied. The Agent owns GKMCacheNode and
// ClusterGKMCacheNode Objects, and calls KubeAPI Server to make sure they reflect
// the current state of the GKMCache and ClusterGKMCache Objects on a given node.
func (r *ReconcilerCommonAgent[C, CL, N]) reconcileCommonAgent(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N],
) (ctrl.Result, error) {
	errorHit := false
	stillInUse := false

	// nodeCnts in indexed by GKMCacheName (or ClusterGKMCacheName).
	nodeCnts := make(map[string]gkmv1alpha1.CacheCounts)

	r.Logger.V(1).Info("Start reconcileCommonAgent()")

	// Get the list of existing GKMCache or ClusterGKMCache objects from KubeAPI Server.
	gkmCacheList, err := reconciler.getCacheList(ctx, []client.ListOption{})
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure},
			fmt.Errorf("failed getting list of %s for full reconcile: %v",
				r.CrdCacheStr,
				err)
	}

	if (*gkmCacheList).GetItemsLen() == 0 {
		// KubeAPI doesn't have any GKMCache instances
		r.Logger.Info("No GKMCache entries found")
		return ctrl.Result{Requeue: false}, nil
	} else {
		// There are GKMCache instances created, so loop through each and reconcile each.
		for _, gkmCache := range (*gkmCacheList).GetItems() {
			r.Logger.Info("Reconciling",
				"Object", r.CrdCacheStr,
				"Namespace", gkmCache.GetNamespace(),
				"Name", gkmCache.GetName(),
				"StorageClass", gkmCache.GetStorageClassName(),
				"PvcOwner", gkmCache.GetPvcOwner())

			// Call KubeAPI to Retrieve GKMCacheNode for this GKMCache
			gkmCacheNode, err := reconciler.getCacheNode(ctx, gkmCache.GetNamespace(), gkmCache.GetName())
			if err != nil {
				// Error returned if unable to call KubeAPI or more than one instance returned.
				// Don't block Reconcile on one instance, log and go to next Cache.
				r.Logger.Error(err, "KubeAPI call failed to retrieve object",
					"Object", r.CrdCacheNodeStr,
					"Namespace", gkmCache.GetNamespace(),
					"Name", gkmCache.GetName())
				errorHit = true
				continue
			}

			if gkmCacheNode == nil {
				if reconciler.isBeingDeleted(&gkmCache) {
					// If the GKMCacheNode doesn't exist and the GKMCache is being deleted,
					// nothing to do. Just continue with the next GKMCache.
					r.Logger.Info("Node object doesn't exist and Cache is being deleted",
						"Object", r.CrdCacheNodeStr,
						"Namespace", gkmCache.GetNamespace(),
						"Name", gkmCache.GetNamespace())
					continue
				}

				// Create a new GKMCacheNode object.
				if err = reconciler.createCacheNode(ctx, gkmCache.GetNamespace(), gkmCache.GetName()); err != nil {
					errorHit = true
					continue
				} else {
					// Creation of GKMCacheNode Object for this Namespace was successful.
					// Return and Reconcile will be retriggered with the GKMCacheNode Object.
					return ctrl.Result{Requeue: false}, nil
				}
			}

			// GKMCacheNode and ClusterGKMCacheNode takes two steps to complete. The createCacheNode()
			// call creates the Object, but r.currCacheNode.Status is not allowed to be updated in the
			// KubeAPI Create call. So if the NodeName is not set, add the initial r.currCacheNode.Status
			// data, which includes the NodeName and list of detected GPUs.
			if (*gkmCacheNode).GetNodeName() != r.NodeName {
				// Add initial Status data to GKMCacheNode or ClusterGKMCacheNode object.
				if err = r.addGpuToCacheNode(ctx, reconciler, gkmCacheNode); err != nil {
					errorHit = true
					continue
				} else {
					// Creation of GKMCacheNode Object for this Namespace was successful.
					// Return and Reconcile will be retriggered with the GKMCacheNode Object.
					//return ctrl.Result{Requeue: false}, nil
					return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
				}
			}

			// Save a copy of GKMCacheNode. Use to determine if anything changes in processing.
			//origCacheNode := r.currCacheNode.DeepCopy()

			// See if Digest has been set (Webhook validated and image is allowed to be used).
			annotations := gkmCache.GetAnnotations()
			if resolvedDigest, digestFound := annotations[utils.GKMCacheAnnotationResolvedDigest]; digestFound {
				capacity, capFound := annotations[utils.GKMCacheAnnotationCacheSizeBytes]
				if !capFound {
					capacity = "1Gi"
					r.Logger.Info("Capacity NOT Found, setting to 1GB")
				}

				r.Logger.V(1).Info("Digest and Capacity Found",
					"Object", r.CrdCacheStr,
					"Namespace", gkmCache.GetNamespace(),
					"Name", gkmCache.GetName(),
					"Digest", resolvedDigest,
					"Capacity", capacity,
				)

				// Before extracting and doing work on a given Cache, make sure it is not being deleted.
				if reconciler.isBeingDeleted(&gkmCache) {
					inUse, cacheNodeUpdated, err := r.removeCacheFromCacheNode(
						ctx, reconciler, gkmCacheNode, gkmCache.GetNamespace(), gkmCache.GetName(), resolvedDigest)
					if err != nil {
						errorHit = true
						continue
					} else if inUse {
						// Remember that one on the Cache is still in use, so requeue can be set properly on return.
						stillInUse = true
					} else if cacheNodeUpdated {
						// KubeAPI was called to update the GKMCacheNode Object. Return and Reconcile
						// will be retriggered with the GKMCacheNode Object update.
						//return ctrl.Result{Requeue: false}, nil
						return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}

					// Update counts for this GKMCache (or ClusterGKMCache).
					// BILLY: Make sure counts get updated for deleting Caches
					// nodeCnts[gkmCache.GetName()] = cnts

					// No work done, so process next Cache instance
					continue
				}

				// Check the Condition for this Cache and Digest to see if this Digest has
				// been extracted.
				nodeStatus := (*gkmCacheNode).GetStatus()
				if nodeStatus != nil {
					updated := false
					updateReason := ""

					cnts := gkmv1alpha1.CacheCounts{}
					cnts.NodeCnt = 1

					// Squirrel away the resolvedDigest in the CacheNode for the Webhook to access quickly
					nodeStatus.ResolvedDigest = resolvedDigest

					cacheStatus, cacheStatusExisted := nodeStatus.CacheStatuses[gkmCache.GetName()][resolvedDigest]
					if !cacheStatusExisted {
						r.Logger.Info("CacheStatus does NOT exist, add Finalizer now")
						// This GKMCache/Digest has not been processed yet.
						// Step 1: Make sure there is a GKMCache Finalizer added to the GKMCacheNode
						if cacheNodeUpdated, err := r.addCacheFinalizerToCacheNode(ctx, reconciler, &gkmCache, gkmCacheNode); err != nil {
							errorHit = true
							continue
						} else if cacheNodeUpdated {
							// GKMCacheNode Object was updated successfully.
							// Return and Reconcile will be retriggered with the GKMCacheNode Object.
							//return ctrl.Result{Requeue: false}, nil
							return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
						}
					}

					// If the GKMCache or ClusterGKMCache Owner has not been set, just skip over this
					// Cache and reevaluate on next pass.
					if gkmCache.GetPvcOwner() == gkmv1alpha1.PvcOwnerUnknown ||
						gkmCache.GetPvcOwner() == "" {
						stillInUse = true
						continue
					}
					// If the PVC AccessMode is ReadOnlyMany, then only one PVC per Namespace needs to be
					// created and the storage backend will handle propagating the extracted cache to each
					// node. Since there is only one, the Operator handles the creation. The Agent tracks
					// the state. PVC AccessMode is ReadWriteOnce, the storage backend can not handle
					// propagating the extracted cache so the Agent does it by creating a PVC per Namespace
					// per Node. For GKMCache, it is the Namespace it is created in. For ClusterGKMCache,
					// it is the Namespace of the workload (pod mounting the PVC), which must be provided
					// in the ClusterGKMCache by the user.

					// If the initial read of the Status failed and the initialization work was completed
					// (Finalizer was added) and the PVC Owner is set, now the Agent can continue processing
					// this GKMCache or ClusterGKMCache. Go ahead and allocate the memory need.
					if !cacheStatusExisted {
						r.Logger.Info("CacheStatus does NOT exist, and Finalizer was already added, so initialize CacheStatus now.")

						// Build up GKMCacheNode.Status
						if len(nodeStatus.CacheStatuses) == 0 {
							r.Logger.Info("Allocating GKMCacheNode.Status.CacheStatuses",
								"Namespace", gkmCache.GetNamespace(),
								"Name", gkmCache.GetName(),
								"CacheNodeName", (*gkmCacheNode).GetName(),
								"Digest", resolvedDigest)
							nodeStatus.CacheStatuses = make(map[string]map[string]gkmv1alpha1.CacheStatus)
						}

						nodeStatus.CacheStatuses[gkmCache.GetName()] = make(map[string]gkmv1alpha1.CacheStatus)

						// Build up the first GKMCacheNode.Status.CacheStatuses[name][resolvedDigest]
						cacheStatus = gkmv1alpha1.CacheStatus{}

						// Make sure KubeAPI is called to write this GKMCacheNode or ClusterGKMCacheNode
						// below once some work is done.
						updated = true
						updateReason = "Cache Allocation"
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
						// Get the PVC Status, which is the Per Namespace PV and PVC information.
						if cacheStatus.PvcStatus == nil {
							cacheStatus.PvcStatus = make(map[string]gkmv1alpha1.PvcStatus)
							updated = true
							updateReason = "PvcStatus Allocation"
						}

						pvcStatus, pvcStatusExisted := cacheStatus.PvcStatus[pvcNamespace]
						if !pvcStatusExisted {
							pvcStatus = gkmv1alpha1.PvcStatus{}
							gkmv1alpha1.SetPvcStatusConditions(&pvcStatus, gkmv1alpha1.GkmCondPending.Condition())
							updated = true
							updateReason = "PvcStatus Initialization"
						}

						// Manage PV and PVC
						// If updated is already true, still manage PV and PVCs, because up to this
						// point, it's just been initialization and allocation of structures, no
						// actual work on kube objects.
						if pvcUpdated, pvcReason, err := r.managePVandPVC(
							ctx,
							reconciler,
							&gkmCache,
							gkmCacheNode,
							&pvcStatus,
							pvcNamespace,
							resolvedDigest,
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
								gkmCacheNode,
								&cacheStatus,
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

						if !updated {
							// Update counts for this Namespace.
							updated, updateReason = r.addCounts(ctx, &cnts, pvcNamespace, &pvcStatus)
						}

						if updated {
							// Update the Cache Status copy of the PVC Status before writing the data.
							cacheStatus.PvcStatus[pvcNamespace] = pvcStatus
							break
						}
					} // For each Namespace

					// Update with the collected counts
					nodeStatus.Counts = cnts
					if !updated {
						if !reflect.DeepEqual((*gkmCacheNode).GetStatus().DeepCopy(), nodeStatus) {
							updated = true
							updateReason = "Update Counts"
						}
					}

					if updated {
						// Update the Node Status copy of the Cache Status before writing the data.
						cacheStatus.LastUpdated = metav1.Now()
						nodeStatus.CacheStatuses[gkmCache.GetName()][resolvedDigest] = cacheStatus

						err = reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, nodeStatus, updateReason)
						if err != nil {
							errorHit = true
							continue
						} else {
							// Update to GKMCacheNode Object for this Namespace was successful.
							// Return and Reconcile will be retriggered with the GKMCacheNode Object.
							//return ctrl.Result{Requeue: false}, nil
							return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
						}
					}
					nodeCnts[gkmCache.GetName()] = cnts
				} else {
					r.Logger.Info("Unable to retrieve Status for CacheNode, but Status should exist already",
						"Namespace", gkmCache.GetNamespace(),
						"Name", gkmCache.GetName(),
						"Digest", resolvedDigest)
					continue
				}
			} else {
				// Webhook has not resolved image URL to a digest, so either Cosign failed
				// or the image is invalid. This should never get here because Webhook should
				// not let GKMCache get created without a valid image.
				r.Logger.Info("Digest NOT Found, either Cosign failed or the image is invalid.",
					"Object", r.CrdCacheStr,
					"Namespace", gkmCache.GetNamespace(),
					"Name", gkmCache.GetName())

				// ToDo: Update GKMCacheNode With Failure
			}
		}
	}

	// Return
	if (*gkmCacheList).GetItemsLen() == 0 && !stillInUse {
		// There are no extracted Caches on host, all cleaned up now, so nothing to do.
		r.Logger.V(1).Info("Nothing to do")
		return ctrl.Result{Requeue: false}, nil
	} else if errorHit {
		r.Logger.V(1).Info("Error hit, requeue after 10 sec")
		// If an error was encountered during a single GKMCache instance, retry after a pause.
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure}, nil
	} else {
		r.Logger.V(1).Info("Need to recheck pod, requeue after 5 sec")
		// GKMCache is Reconciled, so wake up to recheck Pod usage periodically.
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentUsagePoll}, nil
	}
}

// addGpuToCacheNode calls MCV to collect the set of detected GPUs and adds them to the
// GKMCacheNode.Status.GpuStatuses field. This is only done once right after object creation
// and is not refreshed.
func (r *ReconcilerCommonAgent[C, CL, N]) addGpuToCacheNode(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N],
	gkmCacheNode *N,
) error {
	// Retrieve GPU Data
	gpus, err := GetGpuList(r.NoGpu, r.Logger)
	if err != nil {
		return err
	}

	// Add GPU Data to Status
	nodeStatus := gkmv1alpha1.GKMCacheNodeStatus{
		NodeName: r.NodeName,
	}

	if gpus != nil {
		for _, gpu := range gpus.GPUs {
			nodeStatus.GpuStatuses = append(nodeStatus.GpuStatuses, gkmv1alpha1.GpuStatus{
				GpuType:       gpu.GPUType,
				DriverVersion: gpu.DriverVersion,
				GpuList:       gpu.IDs,
			})
		}
	} else {
		nodeStatus.GpuStatuses = append(nodeStatus.GpuStatuses, gkmv1alpha1.GpuStatus{
			GpuType: "None Detected",
		})
	}
	nodeStatus.Counts.NodeCnt = 1

	// Build up GKMCacheNode
	err = reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, &nodeStatus, "Update GPU list")
	if err == nil {
		// Record the creation of GKMCacheNode/ClusterGKMCacheNode
		reconciler.cacheNodeRecordEvent(gkmCacheNode, gkmv1alpha1.GkmCacheNodeEventReasonCreated, "", "", "", 0)
	}

	return err
}

func GetGpuList(noGpu bool, log logr.Logger) (*mcvDevices.GPUFleetSummary, error) {
	// Retrieve GPU Data
	var gpus *mcvDevices.GPUFleetSummary
	var err error
	disableTimeout := 0

	// Stub out the GPU Ids when in TestMode (No GPUs)
	if noGpu {
		stub := true
		gpus, err = mcvClient.GetSystemGPUInfo(mcvClient.HwOptions{EnableStub: &stub, Timeout: disableTimeout})
		if err != nil {
			log.Error(err, "error retrieving stubbed GPU info")
			return gpus, err
		} else {
			log.Info("Detected Stubbed GPU Devices:", "gpus", gpus)
		}
	} else {
		stub := false
		gpus, err = mcvClient.GetSystemGPUInfo(mcvClient.HwOptions{EnableStub: &stub, Timeout: disableTimeout})
		if err != nil {
			log.Error(err, "error retrieving GPU info")
			return gpus, err
		} else {
			log.Info("Detected GPU Devices:", "gpus", gpus)
		}
	}

	return gpus, err
}

// addCacheFinalizerToCacheNode determines if a GKMCache Finalizer has been added to the GKMCacheNode.
// If a GKMCache is deleted, this keeps it from being completely deleted until all associated GKMCacheNodes
// have been deleted.
func (r *ReconcilerCommonAgent[C, CL, N]) addCacheFinalizerToCacheNode(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N],
	gkmCache *C,
	gkmCacheNode *N,
) (bool, error) {
	updated := false

	// Validate Input
	if gkmCache == nil {
		err := fmt.Errorf("cache not set")
		r.Logger.Error(err, "internal error")
		return updated, err
	}
	if gkmCacheNode == nil {
		err := fmt.Errorf("cache node not set")
		r.Logger.Error(err, "internal error")
		return updated, err
	}

	// Add Finalizer to GKMCacheNode if not there. This is a KubeAPI call, so return if Finalizer
	// needed to be added.
	updated, err := reconciler.cacheNodeAddFinalizer(ctx, gkmCacheNode, (*gkmCache).GetName())
	if err != nil {
		return updated, err
	}
	return updated, nil
}

// managePVandPVC manages the PV and PVC that the GPU Kernel Cache is extracted to. If PVC does not exist, then
// this function calls KubeAPI to create the PVC. It MAY need to create the PV first. If both are created, this
// function determines if the PVC is in a valid state to receive the extracted GPU Kernel Cache.
func (r *ReconcilerCommonAgent[C, CL, N]) managePVandPVC(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N],
	gkmCache *C,
	gkmCacheNode *N,
	pvcStatus *gkmv1alpha1.PvcStatus,
	pvcNamespace string,
	resolvedDigest string,
	capacity string,
) (bool, string, error) {
	updated := false
	updateReason := ""
	pvCreated := false

	// If the condition on the PVC in the PvcStatus is Pending, then a Job to extract the cache has NOT
	// been launched. Make sure the PV and PVC are in a valid state to handle the extraction.
	if gkmv1alpha1.GkmCondPending.IsConditionSet(pvcStatus.Conditions) {
		if (*gkmCache).GetPvcOwner() == gkmv1alpha1.PvcOwnerAgent {
			// The preferred method for creating a PV is to create the PVC and Kubelet auto-creates the PV.
			// In a KIND cluster, there is not a true CSI driver for storage management, so the PV must be
			// manually created.
			if r.NoGpu {
				found, updatedName, err := common.PvExists(
					ctx,
					r.Client,
					(*gkmCache).GetName(),
					r.NodeName,
					pvcStatus.PvName,
					pvcNamespace,
					resolvedDigest,
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
					//
					// For both GKMCache and ClusterGKMCache, a PV is created per node. Since PVs are
					// not created in any namespaces, make the name unique.
					pvcStatus.PvName = utils.GenerateUniqueName((*gkmCache).GetName())

					accessModes := []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					}

					err := common.CreatePv(
						ctx,
						r.Client,
						r.Scheme,
						(*gkmCacheNode).GetClientObject(),
						(*gkmCache).GetNamespace(),
						(*gkmCache).GetName(),
						r.NodeName,
						pvcStatus.PvName,
						pvcNamespace,
						accessModes,
						(*gkmCache).GetStorageClassName(),
						capacity,
						resolvedDigest,
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
					r.NodeName,
					pvcStatus.PvcName,
					pvcNamespace,
					resolvedDigest,
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
					// For both GKMCache and ClusterGKMCache, a PVC is created per namespace per node. Because
					// they can be in the same namespace on different nodes, make the name unique. If PvName
					// exists from above, use it. If Kubelet created the name, generate a unique one.
					if pvcStatus.PvName != "" {
						pvcStatus.PvcName = pvcStatus.PvName
					} else {
						pvcStatus.PvcName = utils.GenerateUniqueName((*gkmCache).GetName())
					}

					accessModes := []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					}

					err := common.CreatePvc(
						ctx,
						r.Client,
						r.Scheme,
						(*gkmCacheNode).GetClientObject(),
						(*gkmCache).GetNamespace(),
						(*gkmCache).GetName(),
						r.NodeName,
						pvcStatus.PvName,
						pvcStatus.PvcName,
						pvcNamespace,
						accessModes,
						(*gkmCache).GetStorageClassName(),
						capacity,
						resolvedDigest,
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
		// Else the Operator is managing the PVC, handle in Manage Job.
	}

	return updated, updateReason, nil
}

// manageJob determines if the GPU Kernel Cache has been extracted. If not, checks the condition and either
// Launches a Job to extract it, or calls KubeAPI Server to retrieve the list of Jobs that match the labels
// for a given Cache and Digest and determines the state.
func (r *ReconcilerCommonAgent[C, CL, N]) manageJob(
	ctx context.Context,
	gkmCache *C,
	gkmCacheNode *N,
	cacheStatus *gkmv1alpha1.CacheStatus,
	pvcStatus *gkmv1alpha1.PvcStatus,
	jobNamespace string,
	resolvedDigest string,
) (bool, bool, string, error) {
	updated := false
	updateReason := ""
	stillPending := false
	var err error

	if (*gkmCache).GetPvcOwner() == gkmv1alpha1.PvcOwnerAgent {
		// If the condition on the PVC Status is Pending, then a Job to extract the cache has not been
		// launched. Build up and launch the job.
		if gkmv1alpha1.GkmCondPending.IsConditionSet(pvcStatus.Conditions) {
			// Call KubeAPI to create the Job.
			//
			// For both GKMCache and ClusterGKMCache, just use the PVC name, because Jobs are
			// always created per Node per Namespace. Name is already unique.
			jobName := pvcStatus.PvcName

			r.Logger.Info("Cache NOT Extracted, extract now",
				"Namespace", (*gkmCache).GetNamespace(),
				"Job Namespace", jobNamespace,
				"Job Name", jobName,
				"Name", (*gkmCache).GetName(),
				"digest", resolvedDigest,
				"NoGpu", r.NoGpu)

			err = common.LaunchJob(
				ctx,
				r.Client,
				r.Scheme,
				(*gkmCacheNode).GetClientObject(),
				jobNamespace,
				jobName,
				r.NodeName,
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
					"Job Namespace", jobNamespace,
					"Name", (*gkmCache).GetName(),
					"Image", (*gkmCache).GetImage(),
					"Digest", resolvedDigest)
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
					"Job Namespace", jobNamespace,
					"Name", (*gkmCache).GetName(),
					"Digest", resolvedDigest)
				return updated, stillPending, updateReason, nil
			}

			latestJob, err := common.GetLatestJob(
				ctx,
				r.Client,
				jobNamespace,
				pvcStatus.PvcName,
				resolvedDigest,
				r.NodeName,
				r.Logger,
			)
			if err != nil {
				return updated, stillPending, updateReason, err
			}

			r.Logger.Info("Processing Latest Job",
				"Namespace", (*gkmCache).GetNamespace(),
				"Job Namespace", jobNamespace,
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

					r.getImageToGpuList(gkmCache, resolvedDigest, cacheStatus)
				}
			case latestJob.Status.Failed > 0:
				if !gkmv1alpha1.GkmCondError.IsConditionSet(pvcStatus.Conditions) {
					gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondError.Condition())
					updated = true
					updateReason = "Update Condition to Error"
				}
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
	} else {
		// Else the Operator is managing the PVC, track state through the GKMCache or ClusterGKMCache.
		// Check to see if this Cache Status has been updated with the PVC name from the
		// GKMCache yet. the PV may or may not be created by Operator, may be Kubelet, so
		// only need to check for PVC Name.
		if pvcStatus.PvcName == "" {
			// Look through the GKMCache or ClusterGKMCache PVC Status for this namespace to get the
			// current value.
			gkmCacheStatus := (*gkmCache).GetStatus()
			gkmCachePvcStatus, gkmCachePvcStatusExists := gkmCacheStatus.PvcStatus[jobNamespace]
			if gkmCachePvcStatusExists {
				// Check Conditions to determine if Cache already successfully downloaded (there are
				// multiple states that indicate cache downloaded).
				if gkmv1alpha1.IsConditionDownloadSet(gkmCachePvcStatus.Conditions) {
					pvcStatus.PvName = gkmCachePvcStatus.PvName
					pvcStatus.PvcName = gkmCachePvcStatus.PvcName
					updated = true
					updateReason = "Set PVC Name"

					gkmv1alpha1.SetPvcStatusConditions(
						pvcStatus,
						gkmv1alpha1.GetLatestConditionType(gkmCachePvcStatus.Conditions))
				}
			}
		}
	}

	return updated, stillPending, updateReason, err
}

// removeCacheFromCacheNode removes a GKMCache status from the GKMCacheNode.Status.CacheStatuses field.
// This function returns:
//   - bool: inUse implies the Cache is still mounted in a pod.
//   - bool: updated set to true if KubeAPI was called on the GKMCacheNode. This implies that Reconcile
//     needs to be exited and restarted on the next call.
//
// - error: err was encounter during processing.
func (r *ReconcilerCommonAgent[C, CL, N]) removeCacheFromCacheNode(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N],
	gkmCacheNode *N,
	cacheNamespace, cacheName, digest string,
) (bool, bool, error) {
	// Removing the Cache takes two steps:
	// - First, deleted the extracted Cache from the host. This cannot happen if the Cache is still
	//   in use (being used by a pod). If cache exists on host and delete succeeds, remove the Cache
	//   from this digest from the GKMCacheNode and call KubeAPI to apply the change.
	// - Second, delete the GKMCache specific finalizer from the GKMCacheNode.
	//
	// r.currCache may not be set when cleaning up stranded Cache, so don't use in this function.
	// r.currCacheNode should be set, but if it was not found, cleanup cache on the node and skip the
	// GKMCacheNode object updates.
	updated := false

	// Delete Extracted Cache from host.
	r.Logger.Info("Cache being deleted, removing extracted cache from host",
		"namespace", cacheNamespace,
		"name", cacheName,
		"digest", digest)

	inUse := false
	/*
		inUse, err := database.RemoveCache(
			cacheNamespace,
			cacheName,
			digest,
			r.Logger,
		)
		if inUse {
			r.Logger.Info("Not deleted, extracted Cache still in use",
				"namespace", cacheNamespace,
				"name", cacheName,
				"digest", digest)
			return inUse, updated, nil
		} else if err != nil {
			r.Logger.Error(err, "failed to delete extracted cache from host",
				"namespace", cacheNamespace,
				"name", cacheName,
				"digest", digest)
			return inUse, updated, err
		}
	*/

	// Extracted Cache deleted from host, remove the Cache data for this Digest from the GKMCacheNode.
	if gkmCacheNode != nil {
		nodeStatus := (*gkmCacheNode).GetStatus()
		if nodeStatus != nil {
			if _, ok := nodeStatus.CacheStatuses[cacheName][digest]; ok {
				r.Logger.Info("Deleting CacheStatus via Digest",
					"Object", r.CrdCacheNodeStr,
					"Namespace", cacheNamespace,
					"Name", cacheName,
					"CacheNodeName", (*gkmCacheNode).GetName(),
					"Digest", digest)
				delete(nodeStatus.CacheStatuses[cacheName], digest)
				updated = true
			}

			// If all the Digests are removed from the given Cache, remove the Cache entry from the GKMCacheNode.
			if _, ok := nodeStatus.CacheStatuses[cacheName]; ok {
				if len(nodeStatus.CacheStatuses[cacheName]) == 0 {
					r.Logger.Info("Also Deleting CacheStatus via Name from GKMCacheNode",
						"Object", r.CrdCacheNodeStr,
						"Namespace", cacheNamespace,
						"Name", cacheName,
						"CacheNodeName", (*gkmCacheNode).GetName())
					delete(nodeStatus.CacheStatuses, cacheName)
					updated = true
				}
			}

			if updated {
				if err := reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, nodeStatus, "Remove Cache"); err != nil {
					return inUse, false, err
				}

				return inUse, updated, nil
			}

			// Cache was already removed from GKMCacheNode, so delete the GKMCache specific
			// finalizer from the GKMCacheNode.
			changed, err := reconciler.cacheNodeRemoveFinalizer(ctx, gkmCacheNode, cacheName)
			if err != nil {
				r.Logger.Error(err, "failed to delete GKMCache Finalizer from GKMCacheNode")
				return inUse, false, err
			}
			return inUse, changed, nil
		}
	}

	return inUse, updated, nil
}

func (r *ReconcilerCommonAgent[C, CL, N]) addCounts(
	ctx context.Context,
	cnts *gkmv1alpha1.CacheCounts,
	pvcNamespace string,
	pvcStatus *gkmv1alpha1.PvcStatus,
) (bool, string) {
	cnts.NodeCnt = 1
	podUseCnt := 0
	updated := false
	updateReason := ""

	if pvcStatus.PvcName != "" {
		podUseCnt = common.GetPvcUsedByList(
			ctx,
			r.Client,
			r.NodeName,
			pvcNamespace,
			pvcStatus.PvcName,
			r.Logger,
		)
	}

	switch gkmv1alpha1.GetLatestConditionType(pvcStatus.Conditions).Type {
	case string(gkmv1alpha1.GkmCondPending):
		// Temp state, ignore
	case string(gkmv1alpha1.GkmCondExtracted):
		if podUseCnt != 0 {
			updated = true
			updateReason = "Update Condition to Running"
			gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondRunning.Condition())
		} else {
			if cnts.NodeInUseCnt == 0 {
				cnts.NodeNotInUseCnt = 1
			}
		}
	case string(gkmv1alpha1.GkmCondRunning):
		if podUseCnt == 0 {
			updated = true
			updateReason = "Update Condition to Extracted"
			gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondExtracted.Condition())
		} else {
			cnts.NodeInUseCnt = 1
			cnts.NodeNotInUseCnt = 0
			cnts.PodRunningCnt += podUseCnt
		}
	case string(gkmv1alpha1.GkmCondError):
		cnts.NodeErrorCnt++
	case string(gkmv1alpha1.GkmCondUnloadError):
		cnts.NodeErrorCnt++
	case string(gkmv1alpha1.GkmCondOutdated):
		// PodOutdatedCnt is collected in the Garbage Collection portion of the Reconcile loop.
	}

	return updated, updateReason
}

// processPodListChanges is used to walk the list of pods in the usage data and the list
// of pods for a given cache in the Node object. CSI Agent detects pods coming and going and
// adds them to the usage data. The Agent periodically polls the usage data for changes.
// Several pods could come and go in between the polling period, so this function walks
// each list and publishes the changes in Events in the Node Object.
func (r *ReconcilerCommonAgent[C, CL, N]) processPodListChanges(
	reconciler AgentReconciler[C, CL, N],
	gkmCache *C,
	gkmCacheNode *N,
	oldPodList, newPodList []gkmv1alpha1.PodData,
) {
	currPodCnt := len(oldPodList)

	// Look for pods removed from Node Object by walking the OldPodList (which is currently
	// in the Node Object) and see which pods aren't in the NewPodList (the usage data)
	for _, currPod := range oldPodList {
		found := false
		for _, pod := range newPodList {
			if currPod.PodNamespace == pod.PodNamespace &&
				currPod.PodName == pod.PodName {
				found = true
				break
			}
		}
		if !found {
			currPodCnt--
			reconciler.cacheNodeRecordEvent(
				gkmCacheNode,
				gkmv1alpha1.GkmCacheNodeEventReasonCacheReleased,
				(*gkmCache).GetName(),
				currPod.PodNamespace,
				currPod.PodName,
				currPodCnt,
			)
		}
	}

	// Look for pods added to Node Object by walking the NewPodList (the usage data)
	// and see which pods aren't in the OldPodLis t(which is currently in the Node Object)
	for _, currPod := range newPodList {
		found := false
		for _, pod := range oldPodList {
			if currPod.PodNamespace == pod.PodNamespace &&
				currPod.PodName == pod.PodName {
				found = true
				break
			}
		}
		if !found {
			currPodCnt++
			reconciler.cacheNodeRecordEvent(
				gkmCacheNode,
				gkmv1alpha1.GkmCacheNodeEventReasonCacheUsed,
				(*gkmCache).GetName(),
				currPod.PodNamespace,
				currPod.PodName,
				currPodCnt,
			)
		}
	}
}

func (r *ReconcilerCommonAgent[C, CL, N]) getImageToGpuList(
	gkmCache *C,
	resolvedDigest string,
	cacheStatus *gkmv1alpha1.CacheStatus,
) error {
	var matchedIds, unmatchedIds []int

	// Stub out the GPU Ids when in TestMode (No GPUs)
	if r.NoGpu {
		matchedIds = append(matchedIds, 0)
		unmatchedIds = append(unmatchedIds, 1, 2)
	} else {
		// Replace the tag in the Image URL with the Digest. Webhook has verified
		// the image and so pull from the resolved digest.
		updatedImage := utils.ReplaceUrlTag((*gkmCache).GetImage(), resolvedDigest)
		if updatedImage == "" {
			err := fmt.Errorf("unable to update image tag with digest")
			r.Logger.Error(err, "invalid image or digest", "image", (*gkmCache).GetImage(), "digest", resolvedDigest)
			return err
		}

		matchedIds, unmatchedIds, err := mcvClient.PreflightCheck(updatedImage)
		if err != nil {
			r.Logger.Error(err, "unable to image to GPU list",
				"namespace", (*gkmCache).GetNamespace(), "name",
				(*gkmCache).GetName(),
				"image", updatedImage)
			return err
		}
		r.Logger.Info("Image to GPU", "matchedIds", matchedIds, "unmatchedIds", unmatchedIds)
	}

	cacheStatus.CompGpuList = matchedIds
	cacheStatus.IncompGpuList = unmatchedIds

	return nil
}
