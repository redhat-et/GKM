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

// GKMNodeInstanceList is a generic interface that is a list of type N, which is a list
// of GKMNodeInstance, which is either GKMCacheNode or ClusterGKMCacheNode.
type GKMNodeInstanceList[N any] interface {
	// gkmv1alpha1.GKMCacheNodeList | gkmv1alpha1.ClusterGKMCacheNodeList
	GetItems() []N
	GetItemsLen() int
}

type ReconcilerCommonAgent[C GKMInstance, CL GKMInstanceList[C], N GKMNodeInstance, NL GKMNodeInstanceList[N]] struct {
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
type AgentReconciler[C GKMInstance, CL GKMInstanceList[C], N GKMNodeInstance, NL GKMNodeInstanceList[N]] interface {
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
	getCacheList(ctx context.Context, opts ...client.ListOption) (*CL, error)

	// GetCacheNodeList calls the Kubernetes API server to retrieve a list of GKMCacheNode or ClusterGKMCacheNode objects.
	getCacheNodeList(ctx context.Context, opts ...client.ListOption) (*NL, error)

	getCacheNode(ctx context.Context, cacheNamespace string, cacheName string) (*N, error)
	createCacheNode(ctx context.Context, cacheNamespace, cacheName string) error

	cacheNodeUpdateStatus(ctx context.Context, gkmCacheNode *N, status *gkmv1alpha1.GKMCacheNodeStatus, reason string) (bool, error)

	deleteCacheNode(ctx context.Context, gkmCacheNode *N) error

	isBeingDeleted(gkmCache *C) bool
	validExtractedCache(cacheNamespace string) bool

	cacheNodeAddFinalizer(ctx context.Context, gkmCacheNode *N, cacheName string) (bool, error)
	hasCacheNodeFinalizer(cacheName string, gkmCacheNode *N) bool
	cacheNodeRemoveFinalizer(ctx context.Context, cacheName string, gkmCacheNode *N) (bool, error)

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
func (r *ReconcilerCommonAgent[C, CL, N, NL]) reconcileCommonAgent(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N, NL],
) (ctrl.Result, error) {
	errorHit := false
	stillInUse := false

	// nodeCnts in indexed by GKMCacheName (or ClusterGKMCacheName).
	nodeCnts := make(map[string]gkmv1alpha1.CacheCounts)

	r.Logger.V(1).Info("Start reconcileCommonAgent()")

	inUseGkmCacheNodeList := make(map[string]bool)

	// Get the list of existing GKMCache or ClusterGKMCache objects from KubeAPI Server.
	gkmCacheList, err := reconciler.getCacheList(ctx)
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure},
			fmt.Errorf("failed getting list of %s for full reconcile: %v",
				r.CrdCacheStr,
				err)
	}

	if (*gkmCacheList).GetItemsLen() == 0 {
		// KubeAPI doesn't have any GKMCache instances
		r.Logger.Info("No GKMCache entries found")
	} else {
		// There are GKMCache instances created, so loop through each and reconcile each.
		for _, gkmCache := range (*gkmCacheList).GetItems() {
			r.Logger.Info("Reconciling",
				"Object", r.CrdCacheStr,
				"Namespace", gkmCache.GetNamespace(),
				"Name", gkmCache.GetName(),
				"StorageClass", gkmCache.GetStorageClassName(),
				"PvcOwner", gkmCache.GetPvcOwner())

			cacheDeleting := reconciler.isBeingDeleted(&gkmCache)

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
				if cacheDeleting {
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
					r.Logger.V(1).Info("Return after CacheNode Create")
					return ctrl.Result{Requeue: false}, nil
				}
			}

			inUseGkmCacheNodeList[(*gkmCacheNode).GetName()] = cacheDeleting

			// GKMCacheNode and ClusterGKMCacheNode takes two steps to complete. The createCacheNode()
			// call creates the Object, but r.currCacheNode.Status is not allowed to be updated in the
			// KubeAPI Create call. So if the NodeName is not set, add the initial r.currCacheNode.Status
			// data, which includes the NodeName and list of detected GPUs.
			if (*gkmCacheNode).GetNodeName() != r.NodeName {
				if cacheDeleting {
					// If the GKMCacheNode hasn't been initialized and the GKMCache is being deleted,
					// nothing to do. Just continue with the next GKMCache.
					r.Logger.Info("Node hasn't been initialized and Cache is being deleted",
						"Object", r.CrdCacheNodeStr,
						"Namespace", gkmCache.GetNamespace(),
						"Name", gkmCache.GetNamespace())
					continue
				}

				// Make sure there is a GKMCache Finalizer added to the GKMCacheNode
				if cacheNodeUpdated, err := r.addCacheFinalizerToCacheNode(ctx, reconciler, &gkmCache, gkmCacheNode); err != nil {
					errorHit = true
					continue
				} else if cacheNodeUpdated {
					r.Logger.Info("Finalizer added")
					// GKMCacheNode Object was updated successfully.
					// Return and Reconcile will be retriggered with the GKMCacheNode Object.
					r.Logger.V(1).Info("Return after Finalizer Added")
					return ctrl.Result{Requeue: false}, nil
					//return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
				}

				// Add initial Status data to GKMCacheNode or ClusterGKMCacheNode object.
				nodeStatus := gkmv1alpha1.GKMCacheNodeStatus{}
				if err := r.addGpuToCacheNode(ctx, reconciler, &nodeStatus); err != nil {
					errorHit = true
					continue
				} else {
					changed, err := reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, &nodeStatus, "Update GPU list")
					if err != nil {
						errorHit = true
						continue
					} else {
						reconciler.cacheNodeRecordEvent(gkmCacheNode, gkmv1alpha1.GkmCacheNodeEventReasonCreated, "", "", "", 0)

						// Update to GKMCacheNode Object for this Namespace was successful.
						// Return and Reconcile will be retriggered with the GKMCacheNode Object.
						r.Logger.V(1).Info("Return after NodeStatus Write", "Reason", "Update GPU list", "changed", changed)
						if changed {
							return ctrl.Result{Requeue: false}, nil
						} else {
							return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
						}
					}
				}
			}

			// See if Digest has been set (Webhook validated and image is allowed to be used).
			annotations := gkmCache.GetAnnotations()
			resolvedDigest, digestFound := annotations[utils.GKMCacheAnnotationResolvedDigest]
			if !digestFound {
				// Webhook has not resolved image URL to a digest, so either Cosign failed
				// or the image is invalid. This should never get here because Webhook should
				// not let GKMCache get created without a valid image.
				r.Logger.Info("Digest NOT Found, either Cosign failed or the image is invalid.",
					"Object", r.CrdCacheStr,
					"Namespace", gkmCache.GetNamespace(),
					"Name", gkmCache.GetName())

				// ToDo: Update GKMCacheNode With Failure
				continue
			}

			capacity, capacityFound := annotations[utils.GKMCacheAnnotationCacheSizeBytes]
			if !capacityFound {
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

			// Check the Condition for this Cache and Digest to see if this Digest has
			// been extracted.
			nodeStatus := (*gkmCacheNode).GetStatus()
			if nodeStatus != nil {
				updated := false
				updateReason := ""
				cacheInUse := false

				cnts := gkmv1alpha1.CacheCounts{}
				cnts.NodeCnt = 1

				// Make sure the GKMCache or ClusterGKMCache Owner has been set, otherwise skip over this
				// Cache and reevaluate on next pass.
				if gkmCache.GetPvcOwner() != gkmv1alpha1.PvcOwnerUnknown && gkmCache.GetPvcOwner() != "" {
					// If the PVC AccessMode is ReadOnlyMany, then only one PVC per Namespace needs to be
					// created and the storage backend will handle propagating the extracted cache to each
					// node. Since there is only one, the Operator handles the creation. The Agent tracks
					// the state. IF PVC AccessMode is ReadWriteOnce, the storage backend can not handle
					// propagating the extracted cache so the Agent does it by creating a PVC per Namespace
					// per Node. For GKMCache, it is the Namespace it is created in. For ClusterGKMCache,
					// it is the Namespace of the workload (pod mounting the PVC), which must be provided
					// in the ClusterGKMCache by the user.

					cacheStatus, cacheStatusExisted := nodeStatus.CacheStatuses[gkmCache.GetName()][resolvedDigest]

					if cacheDeleting && !cacheStatusExisted {
						r.Logger.Info("Cache Status doesn't exist and Cache being deleted",
							"Namespace", gkmCache.GetNamespace(),
							"Name", gkmCache.GetName(),
							"CacheNodeName", (*gkmCacheNode).GetName(),
							"Digest", resolvedDigest)
					} else {
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
							var pvcStatus gkmv1alpha1.PvcStatus
							skipPvcCopy := false

							// CREATE or UPDATE
							if !cacheDeleting {
								// Get the PVC Status, which is the Per Namespace PV and PVC information.
								if cacheStatus.PvcStatus == nil {
									cacheStatus.PvcStatus = make(map[string]gkmv1alpha1.PvcStatus)
									updated = true
									updateReason = "PvcStatus Allocation"
								}

								var pvcStatusExisted bool
								pvcStatus, pvcStatusExisted = cacheStatus.PvcStatus[pvcNamespace]
								if !pvcStatusExisted {
									pvcStatus = gkmv1alpha1.PvcStatus{}
									pvcStatus.PvcOwner = gkmCache.GetPvcOwner()
									gkmv1alpha1.SetPvcStatusConditions(&pvcStatus, gkmv1alpha1.GkmCondPending.Condition())
									updated = true
									updateReason = "PvcStatus Initialization"
								}

								// Manage PV, PVC and Job used for extracted GPU Kernel Cache
								if pvcUpdated, pvcUpdateReason, pending, err := r.managePvcStatusModify(
									ctx,
									reconciler,
									&gkmCache,
									gkmCacheNode,
									&cacheStatus,
									&pvcStatus,
									pvcNamespace,
									resolvedDigest,
									capacity,
								); err != nil {
									errorHit = true
									continue
								} else if pvcUpdated {
									updated = true
									updateReason = pvcUpdateReason
								} else if pending {
									stillInUse = true
									cacheInUse = true
								}
							} else {
								// DELETE
								pvcInUse := false

								// Get the PVC Status, which is the Per Namespace PV and PVC information.
								// If it doesn't exist for this Namespace, then move on to the next Namespace.
								if cacheStatus.PvcStatus == nil {
									continue
								}

								var pvcStatusExisted bool
								pvcStatus, pvcStatusExisted = cacheStatus.PvcStatus[pvcNamespace]
								if !pvcStatusExisted {
									continue
								}

								nodeName := r.NodeName
								// If there are more than one namespace associated with this Digest,
								// use blank NodeName. When the delete looks to see if any Pods are using
								// the PVC, it will not filter on just this node and will properly leave
								// objects in place that are being used.
								if len(cacheStatus.PvcStatus) > 1 {
									nodeName = ""
								}

								// If Owner is Agent, then attempt to delete Job, PVC and PV. Otherwise,
								// there is nothing to do here.
								if gkmCache.GetPvcOwner() == gkmv1alpha1.PvcOwnerAgent {
									var pvcDeleting bool
									if updated, updateReason, pvcInUse, pvcDeleting, err = common.ManagePvcStatusDelete(
										ctx,
										r.Client,
										gkmCache.GetNamespace(),
										gkmCache.GetName(),
										nodeName,
										&pvcStatus,
										gkmv1alpha1.PvcOwnerAgent,
										pvcNamespace,
										resolvedDigest,
										r.Logger,
									); err != nil {
										errorHit = true
										continue
									} else if pvcInUse || pvcDeleting {
										cacheInUse = true
										stillInUse = true
										if !gkmv1alpha1.GkmCondDeleting.IsConditionSet(pvcStatus.Conditions) {
											gkmv1alpha1.SetPvcStatusConditions(&pvcStatus, gkmv1alpha1.GkmCondDeleting.Condition())
											updated = true
											updateReason = "Update Condition to Deleting"
										}
									}
								} else {
									// For Operator managed, determine if still in use
									podUseCnt := common.GetPvcUsedByList(
										ctx,
										r.Client,
										"", // NodeName,
										pvcNamespace,
										pvcStatus.PvcName,
										r.Logger,
									)
									if podUseCnt != 0 {
										pvcInUse = true
										cacheInUse = true
										stillInUse = true
										if !gkmv1alpha1.GkmCondDeleting.IsConditionSet(pvcStatus.Conditions) {
											gkmv1alpha1.SetPvcStatusConditions(&pvcStatus, gkmv1alpha1.GkmCondDeleting.Condition())
											updated = true
											updateReason = "Update Condition to Deleting"
										}
									}
								}

								// If nothing was updated, then this PVC Status can be removed.
								if !updated && !pvcInUse {
									delete(cacheStatus.PvcStatus, pvcNamespace)
									updated = true
									skipPvcCopy = true
									updateReason = "Remove PVC Namespace entry"
								}
							}

							if !updated {
								// Update counts for this Namespace.
								var cntPending bool
								updated, updateReason, cntPending = r.addCounts(
									ctx,
									reconciler,
									gkmCache.GetName(),
									gkmCacheNode,
									&cnts,
									pvcNamespace,
									&pvcStatus,
								)
								if cntPending {
									stillInUse = true
								}
							}

							if updated {
								if !skipPvcCopy {
									// Update the Cache Status copy of the PVC Status before writing the data.
									cacheStatus.PvcStatus[pvcNamespace] = pvcStatus
								}
								break
							}
						} // For each Namespace

						// Update with the collected counts
						if !updated {
							nodeStatus.Counts = cnts
							if !reflect.DeepEqual((*gkmCacheNode).GetStatus(), nodeStatus) {
								updated = true
								updateReason = "Update Counts"
							}
						}

						if updated {
							// Update the Node Status copy of the Cache Status before writing the data.
							cacheStatus.LastUpdated = metav1.Now()
							nodeStatus.CacheStatuses[gkmCache.GetName()][resolvedDigest] = cacheStatus

							changed, err := reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, nodeStatus, updateReason)
							if err != nil {
								errorHit = true
								continue
							} else {
								// Update to GKMCacheNode Object for this Namespace was successful.
								// Return and Reconcile will be retriggered with the GKMCacheNode Object.
								r.Logger.V(1).Info("Return after NodeStatus Write", "Reason", updateReason, "changed", changed)
								if changed {
									return ctrl.Result{Requeue: false}, nil
								} else {
									return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
								}
							}
						}
						nodeCnts[gkmCache.GetName()] = cnts
					}
				} else {
					// Owner (Operator or Agent) not set. Set a flag that work still needs to be done.
					cacheInUse = true
					stillInUse = true
				}

				// If the logic made it this far and GKMCache or ClusterGKMCache is being deleted,
				// Then no more cleanup is needed and Finalizer can be removed.
				if cacheDeleting {
					cacheNodeUpdated, err := r.removeCacheFromCacheNode(
						ctx,
						reconciler,
						gkmCacheNode,
						nodeStatus,
						gkmCache.GetNamespace(),
						gkmCache.GetName(),
						resolvedDigest,
						cacheInUse,
					)
					if err != nil {
						errorHit = true
						continue
					} else if cacheNodeUpdated {
						// KubeAPI was called to update the GKMCacheNode Object. Return and Reconcile
						// will be retriggered with the GKMCacheNode Object update.
						r.Logger.V(1).Info("Return after NodeStatus Write - Delete Cache entry")
						return ctrl.Result{Requeue: false}, nil
						//return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}

					// Update counts for this GKMCache (or ClusterGKMCache).
					// ToDo: Make sure counts get updated for deleting Caches
					// nodeCnts[gkmCache.GetName()] = cnts

					// No work done, so process next Cache instance
					continue
				}

			} else {
				r.Logger.Info("Unable to retrieve Status for CacheNode, but Status should exist already",
					"Namespace", gkmCache.GetNamespace(),
					"Name", gkmCache.GetName(),
					"Digest", resolvedDigest)
				continue
			}
		} // FOR EACH GKMCache or ClusterGKMCache
	}

	// Walk the GKMCacheNode or ClusterGKMCacheNode and determine if any PVCs are stranded.
	// If so, see if the Pod using them is still active. If not, clean them up.
	if strandedInUse, strandedErrFlag := r.manageStrandedPvcs(ctx, reconciler, inUseGkmCacheNodeList); strandedErrFlag {
		errorHit = true
	} else if strandedInUse {
		stillInUse = true
	}

	// Return
	if (*gkmCacheList).GetItemsLen() == 0 && !stillInUse {
		// There are no extracted Caches on host, all cleaned up now, so nothing to do.
		r.Logger.Info("Nothing to do")
		return ctrl.Result{Requeue: false}, nil
	} else if errorHit {
		r.Logger.Info("Error hit, requeue after 10 sec")
		// If an error was encountered during a single GKMCache instance, retry after a pause.
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure}, nil
	} else if stillInUse {
		r.Logger.Info("Processing still pending, requeue after 5 sec")
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentUsagePoll}, nil
	} else {
		r.Logger.Info("Waiting for Pod Event ...")
		return ctrl.Result{Requeue: false}, nil
		//r.Logger.Info("Need to recheck pod, requeue after 5 sec")
		// GKMCache is Reconciled, so wake up to recheck Pod usage periodically.
		//return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentUsagePoll}, nil
	}
}

// addGpuToCacheNode calls MCV to collect the set of detected GPUs and adds them to the
// GKMCacheNode.Status.GpuStatuses field. This is only done once right after object creation
// and is not refreshed.
func (r *ReconcilerCommonAgent[C, CL, N, NL]) addGpuToCacheNode(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N, NL],
	nodeStatus *gkmv1alpha1.GKMCacheNodeStatus,
) error {
	// Retrieve GPU Data
	gpus, err := GetGpuList(r.NoGpu, r.Logger)
	if err != nil {
		return err
	}

	// Add GPU Data to Status
	nodeStatus.NodeName = r.NodeName

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
func (r *ReconcilerCommonAgent[C, CL, N, NL]) addCacheFinalizerToCacheNode(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N, NL],
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

// managePvcStatusModify handles Create and Update calls. If necessary, it will handle the creation
// of a PV, PVC or Job to extract GPU Kernel Cache to the PVC, depending on the state.
func (r *ReconcilerCommonAgent[C, CL, N, NL]) managePvcStatusModify(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N, NL],
	gkmCache *C,
	gkmCacheNode *N,
	cacheStatus *gkmv1alpha1.CacheStatus,
	pvcStatus *gkmv1alpha1.PvcStatus,
	pvcNamespace string,
	resolvedDigest string,
	capacity string,
) (bool, string, bool, error) {
	updated := false
	updateReason := ""
	pending := false
	var err error

	// Manage PV and PVC
	// If updated is already true, still manage PV and PVCs, because up to this
	// point, it's just been initialization and allocation of structures, no
	// actual work on kube objects.
	if updated, updateReason, err = r.managePVandPVC(
		ctx,
		reconciler,
		gkmCache,
		gkmCacheNode,
		pvcStatus,
		pvcNamespace,
		resolvedDigest,
		capacity,
	); err != nil || updated {
		return updated, updateReason, pending, err
	}

	// Launch Job to Extract Cache
	if updated, updateReason, pending, err = r.manageJob(
		ctx,
		gkmCache,
		gkmCacheNode,
		cacheStatus,
		pvcStatus,
		pvcNamespace,
		resolvedDigest,
	); err != nil || updated {
		return updated, updateReason, pending, err
	}

	return updated, updateReason, pending, err
}

// managePVandPVC manages the PV and PVC that the GPU Kernel Cache is extracted to. If PVC does not exist, then
// this function calls KubeAPI to create the PVC. It MAY need to create the PV first. If both are created, this
// function determines if the PVC is in a valid state to receive the extracted GPU Kernel Cache.
func (r *ReconcilerCommonAgent[C, CL, N, NL]) managePVandPVC(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N, NL],
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
				_, found, updatedName, err := common.PvExists(
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
				_ /* pvc */, found, updatedName, err := common.PvcExists(
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
func (r *ReconcilerCommonAgent[C, CL, N, NL]) manageJob(
	ctx context.Context,
	gkmCache *C,
	gkmCacheNode *N,
	cacheStatus *gkmv1alpha1.CacheStatus,
	pvcStatus *gkmv1alpha1.PvcStatus,
	jobNamespace string,
	resolvedDigest string,
) (bool, string, bool, error) {
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
				r.NoGpu,
				r.ExtractImage,
				pvcStatus,
				(*gkmCache).GetPodTemplate(),
				r.Logger,
			)

			if err != nil {
				// Error returned launching Job to extract the Cache.
				r.Logger.Error(err, "unable to extract cache",
					"Namespace", (*gkmCache).GetNamespace(),
					"Job Namespace", jobNamespace,
					"Job Name", jobName,
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

				if gkmv1alpha1.GkmCondDeleting.IsConditionSet(pvcStatus.Conditions) {
					gkmv1alpha1.SetPvcStatusConditions(pvcStatus, gkmv1alpha1.GkmCondRunning.Condition())
					updated = true
					updateReason = "Update Condition to Running"
				}

				return updated, updateReason, stillPending, nil
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
				} else {
					stillPending = true
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

	return updated, updateReason, stillPending, err
}

// removeCacheFromCacheNode removes a GKMCache status from the GKMCacheNode.Status.CacheStatuses field.
// This function returns:
//   - bool: inUse implies the Cache is still mounted in a pod.
//   - bool: updated set to true if KubeAPI was called on the GKMCacheNode. This implies that Reconcile
//     needs to be exited and restarted on the next call.
//
// - error: err was encounter during processing.
func (r *ReconcilerCommonAgent[C, CL, N, NL]) removeCacheFromCacheNode(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N, NL],
	gkmCacheNode *N,
	nodeStatus *gkmv1alpha1.GKMCacheNodeStatus,
	cacheNamespace, cacheName, digest string,
	inUse bool,
) (bool, error) {
	// Removing the Cache takes two steps:
	// - First, remove this digest from the Status.Caches for the GKMCacheNode and call KubeAPI to
	//   apply the change. This step is skipped if the PVC is still in use.
	// - Second, delete the GKMCache specific finalizer from the GKMCacheNode. This allows the GKMCache
	//   to be deleted. This will be done, even if the PVC is still in use.
	updated := false

	// Delete Extracted Cache from host.
	r.Logger.Info("Cache being deleted, remove status cache",
		"namespace", cacheNamespace,
		"name", cacheName,
		"digest", digest,
		"inUse", inUse,
	)

	// Extracted Cache deleted from host, remove the Cache data for this Digest from the GKMCacheNode.
	if gkmCacheNode != nil {
		if !inUse {
			if nodeStatus != nil {
				if cacheStatus, ok := nodeStatus.CacheStatuses[cacheName][digest]; ok {
					if len(cacheStatus.PvcStatus) != 0 {
						r.Logger.Info("PvcStatus in CacheStatus still remain",
							"Object", r.CrdCacheNodeStr,
							"Namespace", cacheNamespace,
							"Name", cacheName,
							"CacheNodeName", (*gkmCacheNode).GetName(),
							"Digest", digest,
							"Num PVC Status", len(cacheStatus.PvcStatus),
						)
						for namespace, pvcStatus := range cacheStatus.PvcStatus {
							r.Logger.Info("Deleting PvcStatus from CacheStatus",
								"Object", r.CrdCacheNodeStr,
								"Namespace", cacheNamespace,
								"Name", cacheName,
								"CacheNodeName", (*gkmCacheNode).GetName(),
								"Digest", digest,
								"Namespace", namespace,
								"PVC", pvcStatus.PvcName,
								"PV", pvcStatus.PvName,
								"Reason", pvcStatus.Conditions[0].Type,
							)
							delete(cacheStatus.PvcStatus, namespace)
						}
					}
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
							"CacheNodeName", cacheName)
						delete(nodeStatus.CacheStatuses, cacheName)
						updated = true
					}
				}

				if updated {
					changed, err := reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, nodeStatus, "Remove Cache")
					if err != nil {
						return false, err
					} else if changed {
						return changed, nil
					} else {
						// If the write doesn't think anything has changed, then keep going.
						updated = false
					}
				}
			}
		}

		// Delete the GKMCache specific finalizer from the GKMCacheNode.
		changed, err := reconciler.cacheNodeRemoveFinalizer(ctx, cacheName, gkmCacheNode)
		if err != nil {
			r.Logger.Error(err, "failed to delete GKMCache Finalizer from GKMCacheNode")
			return false, err
		}
		return changed, nil
	}

	return updated, nil
}

// manageStrandedPvcs walks the GKMCacheNode or ClusterGKMCacheNode and determines if any PVCs are
// stranded (GKMCache or ClusterGKMCache was deleted but Pod was still using).  If so, see if the Pod
// using them is still active. If not, clean them up.
func (r *ReconcilerCommonAgent[C, CL, N, NL]) manageStrandedPvcs(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N, NL],
	inUseGkmCacheNodeList map[string]bool,
) (bool, bool) {
	pending := false
	errorHit := false
	r.Logger.V(1).Info("ENTER manageStrandedPvcs()")

	filters := []client.ListOption{
		client.MatchingLabels{
			utils.GKMCacheLabelHostname: r.NodeName,
		},
	}
	gkmCacheNodeList, err := reconciler.getCacheNodeList(ctx, filters...)
	if err != nil {
		errorHit = true
		return pending, errorHit
	}

	if (*gkmCacheNodeList).GetItemsLen() == 0 {
		// KubeAPI doesn't have any GKMCacheNode instances
		r.Logger.V(1).Info("No GKMCacheNode entries found")
	} else {
		// There are GKMCacheNode instances created, so loop through each and any check if PVCs are stranded.
		for _, gkmCacheNode := range (*gkmCacheNodeList).GetItems() {
			// Check to see if GKMCacheNode was processed above in main loop. If so, skip over.
			if _, ok := inUseGkmCacheNodeList[gkmCacheNode.GetName()]; ok {
				r.Logger.V(1).Info("Skipping Cache Node because Cache still exists",
					"Object", r.CrdCacheStr,
					"Namespace", gkmCacheNode.GetNamespace(),
					"Name", gkmCacheNode.GetName(),
				)
				continue
			}

			r.Logger.Info("Verifying Cache Node",
				"Object", r.CrdCacheStr,
				"Namespace", gkmCacheNode.GetNamespace(),
				"Name", gkmCacheNode.GetName(),
			)
			cnts := gkmv1alpha1.CacheCounts{}
			updated := false
			updateReason := ""
			nodeStatus := gkmCacheNode.GetStatus()
			if nodeStatus != nil {
				if len(nodeStatus.CacheStatuses) == 0 {
					// If there are not CacheStatus entries, GKMCacheNode can be deleted.
					if err = reconciler.deleteCacheNode(ctx, &gkmCacheNode); err != nil {
						r.Logger.Error(err, "failed to delete GKMCacheNode")
						errorHit = true
					}
				} else {
					for cacheName, digestList := range nodeStatus.CacheStatuses {
						for digest, cacheStatus := range digestList {
							nodeName := r.NodeName
							// If there are more than one namespace associated with this Digest,
							// use blank NodeName. When the delete looks to see if any Pods are using
							// the PVC, it will not filter on just this node and will properly leave
							// objects in place that are being used.
							if len(cacheStatus.PvcStatus) > 1 {
								nodeName = ""
							}
							for namespace, pvcStatus := range cacheStatus.PvcStatus {
								if gkmv1alpha1.GkmCondDeleting.IsConditionSet(pvcStatus.Conditions) {
									r.Logger.Info("PVC is in Deleting State",
										"Object", r.CrdCacheNodeStr,
										"Name", cacheName,
										"Digest", digest,
										"Namespace", namespace,
										"PVC", pvcStatus.PvcName,
										"PV", pvcStatus.PvName,
									)
									pvcUpdated, pvcUpdateReason, pvcInUse, pvcDeleting, err := common.ManagePvcStatusDelete(
										ctx,
										r.Client,
										namespace,
										cacheName,
										nodeName,
										&pvcStatus,
										gkmv1alpha1.PvcOwnerAgent,
										namespace,
										digest,
										r.Logger,
									)
									if err != nil {
										errorHit = true
										continue
									}
									if pvcUpdated {
										cacheStatus.PvcStatus[namespace] = pvcStatus
										updated = true
										updateReason = pvcUpdateReason
									}
									if pvcDeleting {
										pending = true
									}

									// If nothing was updated and it's no longer being used, then this PVC Status can be removed.
									if !updated && !pvcInUse && !pvcDeleting {
										delete(cacheStatus.PvcStatus, namespace)
										updated = true
										updateReason = "Remove PVC Namespace entry"
									}
								}

								// Update counts for this Namespace.
								cntUpdated, cntUpdateReason, _ /* cntPending */ := r.addCounts(
									ctx,
									reconciler,
									cacheName,
									&gkmCacheNode,
									&cnts,
									namespace,
									&pvcStatus,
								)
								if cntUpdated && !updated {
									updated = true
									updateReason = cntUpdateReason
								}
							}
							if updated {
								// If all the PVCs were deleted, delete this digest, otherwise updated it.
								if len(cacheStatus.PvcStatus) == 0 {
									r.Logger.Info("Deleting CacheStatus via Digest",
										"Object", r.CrdCacheNodeStr,
										"Name", cacheName,
										"CacheNodeName", gkmCacheNode.GetName(),
										"Digest", digest)
									delete(nodeStatus.CacheStatuses[cacheName], digest)
								} else {
									// Update the Node Status copy of the Cache Status before writing the data.
									cacheStatus.LastUpdated = metav1.Now()
									nodeStatus.CacheStatuses[cacheName][digest] = cacheStatus
								}
							}
						}
						// If all the Digests are removed from the given Cache, remove the Cache entry from the GKMCacheNode.
						if len(nodeStatus.CacheStatuses[cacheName]) == 0 {
							// Delete the GKMCache specific finalizer from the GKMCacheNode.
							updated, err := reconciler.cacheNodeRemoveFinalizer(ctx, cacheName, &gkmCacheNode)
							if err != nil {
								r.Logger.Error(err, "failed to delete GKMCache Finalizer from GKMCacheNode")
								errorHit = true
							} else if updated {
								//pending = true
								return pending, errorHit
							} else {
								r.Logger.Info("Also Deleting CacheStatus via Name from GKMCacheNode",
									"Object", r.CrdCacheNodeStr,
									"Name", cacheName,
									"CacheNodeName", gkmCacheNode.GetName())
								delete(nodeStatus.CacheStatuses, cacheName)
								if !updated {
									updated = true
									updateReason = "Deleting CacheStatus"
								}
							}
						}
					}
					nodeStatus.Counts = cnts
					// Update with the collected counts
					if !updated {
						if !reflect.DeepEqual(gkmCacheNode.GetStatus(), nodeStatus) {
							updated = true
							updateReason = "Update Counts"
						}
					}
					if updated {
						changed, err := reconciler.cacheNodeUpdateStatus(ctx, &gkmCacheNode, nodeStatus, updateReason)
						if err != nil {
							errorHit = true
							continue
						} else if changed {
							// Update to GKMCacheNode Object for this Namespace was successful.
							// Return and Reconcile will be retriggered with the GKMCacheNode Object.
							return pending, errorHit
						}
					}
				}
			}
		}
	}
	return pending, errorHit
}

// addCounts examines a given PVC Status and increments counts based on the state of a given
// PVC and if it is being used by any Pods. This function assumes that the counts were initialized
// prior and this function is called for each PVC Status structure for a given GKMCacheNode or
// ClusterGKMCacheNode.
func (r *ReconcilerCommonAgent[C, CL, N, NL]) addCounts(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N, NL],
	cacheName string,
	gkmCacheNode *N,
	cnts *gkmv1alpha1.CacheCounts,
	pvcNamespace string,
	pvcStatus *gkmv1alpha1.PvcStatus,
) (bool, string, bool) {
	updated := false
	updateReason := ""
	pending := false

	cnts.NodeCnt = 1
	//podCnt := 0
	podUseCnt := 0

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
	case string(gkmv1alpha1.GkmCondDeleting):
		cnts.PodDeletingCnt += podUseCnt
		cnts.NodeInUseCnt = 1
		cnts.NodeNotInUseCnt = 0
		if !reconciler.hasCacheNodeFinalizer(cacheName, gkmCacheNode) {
			cnts.NodeCnt = 0
		}
	case string(gkmv1alpha1.GkmCondError):
		cnts.NodeErrorCnt++
	case string(gkmv1alpha1.GkmCondUnloadError):
		cnts.NodeErrorCnt++
	case string(gkmv1alpha1.GkmCondOutdated):
		// PodOutdatedCnt is collected in the Garbage Collection portion of the Reconcile loop.
	}

	return updated, updateReason, pending
}

func (r *ReconcilerCommonAgent[C, CL, N, NL]) getImageToGpuList(
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
