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
	"github.com/google/uuid"
	mcvDevices "github.com/redhat-et/MCU/mcv/pkg/accelerator/devices"
	mcvClient "github.com/redhat-et/MCU/mcv/pkg/client"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/database"
	"github.com/redhat-et/GKM/pkg/utils"
)

// GKMInstance is a generic interface that can either be a gkmv1alpha1.GKMCache or
// a gkmv1alpha1.ClusterGKMCache. This is used to allow both a GKMCache and a ClusterGKMCache
// to be processed by the same code.
type GKMInstance interface {
	GetName() string
	GetNamespace() string
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

	getCacheNode(ctx context.Context, cacheNamespace string) (*N, error)
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

	extractedList, err := database.GetExtractedCacheList(r.Logger)
	if err != nil {
		r.Logger.Error(err, "failed to list Extracted Cache")
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure},
			fmt.Errorf("failed getting list of installed %s for full reconcile: %v",
				r.CrdCacheStr,
				err)
	}

	if (*gkmCacheList).GetItemsLen() == 0 {
		// KubeAPI doesn't have any GKMCache instances
		r.Logger.Info("No GKMCache entries found")
		if len(*extractedList) == 0 {
			// There are no extracted Caches on host, so nothing to do.
			r.Logger.V(1).Info("No extracted cache found, nothing to do")
			return ctrl.Result{Requeue: false}, nil
		}
		// No GKMCache, but there are some Caches that are installed. Check for stranded
		// Cache (Cache still in use) below.
	} else {
		// There are GKMCache instances created, so loop through each and reconcile each.
		for _, gkmCache := range (*gkmCacheList).GetItems() {
			r.Logger.V(1).Info("Reconciling",
				"Object", r.CrdCacheStr,
				"Namespace", gkmCache.GetNamespace(),
				"Name", gkmCache.GetNamespace())

			// Call KubeAPI to Retrieve GKMCacheNode for this GKMCache
			gkmCacheNode, err := reconciler.getCacheNode(ctx, gkmCache.GetNamespace())
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
			if resolvedDigest, digestFound := annotations[utils.GMKCacheAnnotationResolvedDigest]; digestFound {
				r.Logger.V(1).Info("Digest Found",
					"Object", r.CrdCacheStr,
					"Namespace", gkmCache.GetNamespace(),
					"Name", gkmCache.GetName(),
					"Digest", resolvedDigest)

				cnts, ok := nodeCnts[gkmCache.GetNamespace()]
				if !ok {
					cnts = gkmv1alpha1.CacheCounts{}
					cnts.NodeCnt = 1
				}

				key := database.CacheKey{
					Namespace: gkmCache.GetNamespace(),
					Name:      gkmCache.GetName(),
					Digest:    resolvedDigest,
				}

				// If the Cache still exists on host, mark it as viewed so checks aren't rerun
				// in garbage collection.
				if _, cacheFound := (*extractedList)[key]; cacheFound {
					(*extractedList)[key] = false
				}

				// Before extracting and doing work on a given Cache, make sure it is not being deleted.
				if reconciler.isBeingDeleted(&gkmCache) {
					inUse, cacheNodeUpdated, err := r.removeCacheFromCacheNode(
						ctx, reconciler, gkmCacheNode, key.Namespace, key.Name, key.Digest)
					if err != nil {
						errorHit = true
						continue
					} else if inUse {
						// Remember that one on the Cache is still in use, so requeue can be set properly on return.
						stillInUse = true
						cnts.NodeInUseCnt++
					} else if cacheNodeUpdated {
						// KubeAPI was called to update the GKMCacheNode Object. Return and Reconcile
						// will be retriggered with the GKMCacheNode Object update.
						//return ctrl.Result{Requeue: false}, nil
						return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}

					// Update counts for this Namespace.
					nodeCnts[gkmCache.GetNamespace()] = cnts

					// No work done, so process next Cache instance
					continue
				}

				// Check the list of extracted Cache to see if this Digest has been extracted.
				if _, cacheExtracted := (*extractedList)[key]; cacheExtracted {
					r.Logger.V(1).Info("Cache already Extracted",
						"Object", r.CrdCacheStr,
						"Namespace", gkmCache.GetNamespace(),
						"Name", gkmCache.GetName(),
						"Digest", resolvedDigest)

					// Determine if anything changed by updating GKMCacheNode.Status with cache and usage data
					updated, podUseCnt, err := r.checkForCacheUpdateInCacheNode(ctx, reconciler, &gkmCache, gkmCacheNode, resolvedDigest)
					if err != nil {
						errorHit = true
						continue
					} else if updated {
						// Update to GKMCacheNode Object for this Namespace was successful.
						// Return and Reconcile will be retriggered with the GKMCacheNode Object.
						//return ctrl.Result{Requeue: false}, nil
						return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}
					cnts.PodRunningCnt += podUseCnt
				} else {
					// Cache has not been extracted. Check if error occurred in previous extraction attempt.
					nodeStatus := (*gkmCacheNode).GetStatus()
					if nodeStatus != nil {
						if cacheStatus, ok := nodeStatus.CacheStatuses[gkmCache.GetName()][resolvedDigest]; ok {
							if gkmv1alpha1.GkmCondError.IsConditionSet(cacheStatus.Conditions) {
								// Extraction error has occurred, skip this instance.
								cnts.NodeErrorCnt++
								nodeCnts[gkmCache.GetNamespace()] = cnts
								continue
							}
						}
					}

					// addCacheInCacheNode() will be called twice.
					// - First time through the Reconcile loop it will add the GKMCache finalizer to the
					//   GKMCacheNode and return.
					// - Second time through the Reconcile loop, cache is still not extracted which brings
					//   us here again, so Cache is extracted to the host and the current Cache is added to
					//   the list of Caches stored in the GKMCacheNode object.
					if err = r.addCacheInCacheNode(ctx, reconciler, &gkmCache, gkmCacheNode, resolvedDigest); err != nil {
						errorHit = true
						continue
					} else {
						// GKMCacheNode Object was updated successfully.
						// Return and Reconcile will be retriggered with the GKMCacheNode Object.
						//return ctrl.Result{Requeue: false}, nil
						return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}
				}

				// Update counts for this Namespace.
				nodeStatus := (*gkmCacheNode).GetStatus()
				if nodeStatus != nil {
					if cacheStatus, ok := nodeStatus.CacheStatuses[gkmCache.GetName()][resolvedDigest]; ok {
						addCounts(&cnts, cacheStatus.Conditions[0].Type)
						nodeCnts[gkmCache.GetNamespace()] = cnts
					}
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

	// Walked the installed Cache and make sure there are none that are stranded.
	// If the value is true, then it was not processed above.
	for key := range *extractedList {
		if (*extractedList)[key] {
			// GKMCache need to skip extracted ClusterGKMCache and
			// ClusterGKMCache need to skip extracted GKMCache.
			// validExtractedCache() just checks the Namespace for "" and
			// determines if the extracted Cache matches the Object type.
			if valid := reconciler.validExtractedCache(key.Namespace); valid {
				// Call KubeAPI to Retrieve GKMCacheNode for this GKMCache
				gkmCacheNode, err := reconciler.getCacheNode(ctx, key.Namespace)
				if err != nil {
					// Error returned if unable to call KubeAPI or more than one instance returned.
					r.Logger.Error(err, "failed to get GKMCacheNode",
						"Namespace", key.Namespace, "Name", key.Name, "Digest", key.Digest)
					errorHit = true
					// Don't bail on error
				}

				// removeCacheFromCacheNode is coded such that GKMCache and GKMCacheNode may not be present,
				// but delete as much as is detected. So if error was returned on getCacheNode() call or
				// gkmCacheNode is nil, still continue.
				inUse, cacheNodeUpdated, err := r.removeCacheFromCacheNode(
					ctx, reconciler, gkmCacheNode, key.Namespace, key.Name, key.Digest)
				if err != nil {
					errorHit = true
					continue
				} else if inUse {
					stillInUse = true

					// Cache has been extracted, but is not the Resolved Digest. This implies that the GKMCache
					// Image has been updated, but a pod was still using it. Marking it as outdated.
					nodeStatus := (*gkmCacheNode).GetStatus()
					if nodeStatus != nil {
						if cacheStatus, ok := nodeStatus.CacheStatuses[key.Name][key.Digest]; ok {
							if !gkmv1alpha1.GkmCondOutdated.IsConditionSet(cacheStatus.Conditions) {
								r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCondExtracted.Condition())

								if err := reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, nodeStatus, "Update Outdated Condition"); err != nil {
									errorHit = true
									continue
								} else {
									// GKMCacheNode Object was updated successfully.
									// Return and Reconcile will be retriggered with the GKMCacheNode Object.
									//return ctrl.Result{Requeue: false}, nil
									return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
								}
							} else {
								// Updated the PodOutdatedCnt
								cnts, ok := nodeCnts[key.Namespace]
								if !ok {
									cnts = gkmv1alpha1.CacheCounts{}
								}
								cnts.PodOutdatedCnt += len(cacheStatus.Pods)
								nodeCnts[key.Namespace] = cnts
							}
						}
					}
				} else if cacheNodeUpdated {
					// KubeAPI was called to update the GKMCacheNode Object. Return and Reconcile
					// will be retriggered with the GKMCacheNode Object update.
					return ctrl.Result{Requeue: false}, nil
				}
			}
		}
	}

	// Check counts for Updates
	for namespace, cnts := range nodeCnts {
		// Call KubeAPI to Retrieve GKMCacheNode for this Node and Namespace
		gkmCacheNode, err := reconciler.getCacheNode(ctx, namespace)
		if err != nil {
			// Error returned if unable to call KubeAPI or more than one instance returned.
			// Don't block Reconcile on one instance, log and go to next CacheNode.
			r.Logger.Error(err, "KubeAPI call failed to retrieve object to update counts",
				"Object", r.CrdCacheNodeStr,
				"Namespace", namespace)
			errorHit = true
		} else if gkmCacheNode != nil {
			nodeStatus := (*gkmCacheNode).GetStatus().DeepCopy()
			nodeStatus.Counts = cnts

			if !reflect.DeepEqual((*gkmCacheNode).GetStatus().DeepCopy(), nodeStatus) {
				if err := reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, nodeStatus, "Update Counts"); err != nil {
					errorHit = true
				}
			}
		}
	}

	// Return
	if (*gkmCacheList).GetItemsLen() == 0 && !stillInUse {
		// There are no extracted Caches on host, all cleaned up now, so nothing to do.
		return ctrl.Result{Requeue: false}, nil
	} else if errorHit {
		// If an error was encountered during a single GKMCache instance, retry after a pause.
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure}, nil
	} else {
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

	// Record the creation of GKMCacheNode/ClusterGKMCacheNode
	reconciler.cacheNodeRecordEvent(gkmCacheNode, gkmv1alpha1.GkmCacheNodeEventReasonCreated, "", "", "", 0)

	// Build up GKMCacheNode
	return reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, &nodeStatus, "Update GPU list")
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

// addCacheToCacheNode adds a GKMCache status to the GKMCacheNode.Status.CacheStatuses field.
func (r *ReconcilerCommonAgent[C, CL, N]) addCacheInCacheNode(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N],
	gkmCache *C,
	gkmCacheNode *N,
	resolvedDigest string,
) error {
	// Validate Input
	if gkmCache == nil {
		err := fmt.Errorf("cache not set")
		r.Logger.Error(err, "internal error")
		return err
	}
	if gkmCacheNode == nil {
		err := fmt.Errorf("cache node not set")
		r.Logger.Error(err, "internal error")
		return err
	}

	// Add Finalizer to GKMCacheNode if not there. This is a KubeAPI call, so return if finalizer needed to be added.
	changed, err := reconciler.cacheNodeAddFinalizer(ctx, gkmCacheNode, (*gkmCache).GetName())
	if err != nil {
		return err
	} else if changed {
		return nil
	}

	cacheStatus := gkmv1alpha1.CacheStatus{
		LastUpdated: metav1.Now(),
	}

	r.Logger.Info("Cache NOT Extracted, extract now",
		"Namespace", (*gkmCache).GetNamespace(),
		"Name", (*gkmCache).GetName(),
		"digest", resolvedDigest,
		"NoGpu", r.NoGpu)

	// Image has NOT been extracted, call MCV to extract cache from image to host.
	matchedIds, unmatchedIds, err := database.ExtractCache(
		(*gkmCache).GetNamespace(),
		(*gkmCache).GetName(),
		(*gkmCache).GetImage(),
		resolvedDigest,
		r.NoGpu,
		r.Logger,
	)
	if err != nil {
		// Error returned calling MCV to extract the Cache.
		r.Logger.Error(err, "unable to extract cache",
			"Namespace", (*gkmCache).GetNamespace(),
			"Name", (*gkmCache).GetName(),
			"Image", (*gkmCache).GetImage(),
			"Digest", resolvedDigest)

		r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCondError.Condition())
	} else {
		r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCondExtracted.Condition())

		// Stub out the GPU Ids when in TestMode (No GPUs)
		if r.NoGpu {
			matchedIds = append(matchedIds, 0)
			unmatchedIds = append(unmatchedIds, 1, 2)
		}

		cacheStatus.CompGpuList = matchedIds
		cacheStatus.IncompGpuList = unmatchedIds
	}

	// Build up GKMCacheNode.Status.
	nodeStatus := (*gkmCacheNode).GetStatus()
	if nodeStatus != nil {
		if len(nodeStatus.CacheStatuses) == 0 {
			r.Logger.Info("Allocating GKMCacheNode.Status.CacheStatuses",
				"Namespace", (*gkmCache).GetNamespace(),
				"Name", (*gkmCache).GetName(),
				"CacheNodeName", (*gkmCacheNode).GetName(),
				"Digest", resolvedDigest)
			nodeStatus.CacheStatuses = make(map[string]map[string]gkmv1alpha1.CacheStatus)
		}

		nodeStatus.CacheStatuses[(*gkmCache).GetName()] = make(map[string]gkmv1alpha1.CacheStatus)
		nodeStatus.CacheStatuses[(*gkmCache).GetName()][resolvedDigest] = cacheStatus

		return reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, nodeStatus, "Add Cache")
	} else {
		err := fmt.Errorf("cache node not set")
		r.Logger.Error(err, "internal error - addCacheInCacheNode()")
		return err
	}
}

// checkForCacheUpdateInCacheNode reads the Cache file and Usage file and determines if the
// CacheNode Object needs to be update, and if so, call KubeAPI Server to update it.
// This function returns:
//   - bool: Whether or not CacheNode was was updated or not. If true, then Reconcile loop
//     should be exited and restarted.
//   - int: Number of pods on this node that are using the Extracted Cache. Value used to update
//     the associated counts.
//   - error: Non-nil implies an error was encounter when calling KubeAPI Server.
func (r *ReconcilerCommonAgent[C, CL, N]) checkForCacheUpdateInCacheNode(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N],
	gkmCache *C,
	gkmCacheNode *N,
	resolvedDigest string,
) (bool, int, error) {
	podUseCnt := int(0)
	nodeStatus := (*gkmCacheNode).GetStatus().DeepCopy()
	if nodeStatus != nil {
		if cacheStatus, ok := nodeStatus.CacheStatuses[(*gkmCache).GetName()][resolvedDigest]; ok {
			// Read the Cache File
			cacheStatus.VolumeSize = 0
			cacheFile, err := database.GetCacheFile(
				(*gkmCache).GetNamespace(),
				(*gkmCache).GetName(),
				r.Logger)
			if err != nil {
				r.Logger.Error(err, "unable to read cache file, continuing",
					"Namespace", (*gkmCache).GetNamespace(),
					"Name", (*gkmCache).GetName(),
					"Digest", resolvedDigest)
			} else {
				if size, ok := cacheFile.Sizes[resolvedDigest]; ok {
					cacheStatus.VolumeSize = size
				}
			}

			// Read Usage Data
			usage, err := database.GetUsageData(
				(*gkmCache).GetNamespace(),
				(*gkmCache).GetName(),
				resolvedDigest,
				r.Logger)
			if err == nil {
				if !reflect.DeepEqual(cacheStatus.Pods, usage.Pods) {
					r.processPodListChanges(reconciler, gkmCache, gkmCacheNode, cacheStatus.Pods, usage.Pods)

					cacheStatus.Pods = make([]gkmv1alpha1.PodData, len(usage.Pods))
					copy(cacheStatus.Pods, usage.Pods)
				}

				podUseCnt = len(usage.Pods)
				if podUseCnt != 0 {
					// Condition: Running
					if !gkmv1alpha1.GkmCondRunning.IsConditionSet(cacheStatus.Conditions) {
						r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCondRunning.Condition())
					}
				} else {
					// Condition: Extracted
					if !gkmv1alpha1.GkmCondExtracted.IsConditionSet(cacheStatus.Conditions) {
						r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCondExtracted.Condition())
					}
				}
			} else {
				if len(cacheStatus.Pods) != 0 {
					r.processPodListChanges(reconciler, gkmCache, gkmCacheNode, cacheStatus.Pods, []gkmv1alpha1.PodData{})
				}

				cacheStatus.Pods = nil

				// Condition: Extracted
				if !gkmv1alpha1.GkmCondExtracted.IsConditionSet(cacheStatus.Conditions) {
					r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCondExtracted.Condition())
				}
			}

			if !reflect.DeepEqual(nodeStatus.CacheStatuses[(*gkmCache).GetName()][resolvedDigest], cacheStatus) {
				cacheStatus.LastUpdated = metav1.Now()
				nodeStatus.CacheStatuses[(*gkmCache).GetName()][resolvedDigest] = cacheStatus

				if err := reconciler.cacheNodeUpdateStatus(ctx, gkmCacheNode, nodeStatus, "Update CacheStatus"); err != nil {
					return false, podUseCnt, err
				} else {
					// Update to GKMCacheNode Object for this Namespace was successful.
					// Return and Reconcile will be retriggered with the GKMCacheNode Object.
					//return ctrl.Result{Requeue: false}, nil
					return true, podUseCnt, nil
				}
			} else {
				r.Logger.V(1).Info("No Changes GKMCacheNode CacheStatus",
					"Namespace", (*gkmCache).GetNamespace(),
					"Name", (*gkmCache).GetName(),
					"CacheNodeName", (*gkmCacheNode).GetNamespace(),
					"Digest", resolvedDigest)
			}
		} else {
			// GKMCacheNode was probably read before the previous call to KubeAPI Server to
			// add the CacheStatus finished writing. Exit and reenter Reconcile(), which will
			// reread GKMCacheNode.
			r.Logger.Info("GKMCacheNode CacheStatus missing, retry Reconcile",
				"Namespace", (*gkmCache).GetNamespace(),
				"CacheName", (*gkmCache).GetName(),
				"CacheNodeName", (*gkmCacheNode).GetName(),
				"Digest", resolvedDigest)
			return false, podUseCnt, fmt.Errorf("GKMCacheNode CacheStatus missing, retry")
		}
	} else {
		// NodeStatus probably should have existed at this point. Exit and reenter Reconcile(),
		// which will reread GKMCacheNode and hopefully find a NodeStatus.
		r.Logger.Info("GKMCacheNode NodeStatus missing, retry Reconcile",
			"Namespace", (*gkmCache).GetNamespace(),
			"CacheName", (*gkmCache).GetName(),
			"CacheNodeName", (*gkmCacheNode).GetName(),
			"Digest", resolvedDigest)
		return false, podUseCnt, fmt.Errorf("GKMCacheNode NodeStatus missing, retry")
	}

	return false, podUseCnt, nil
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

// Helper function to set conditions on the CacheStatus of a GKMCacheNode or ClusterGKMCacheNode object.
func (r *ReconcilerCommonAgent[C, CL, N]) setCacheNodeConditions(cacheStatus *gkmv1alpha1.CacheStatus, condition metav1.Condition) {
	cacheStatus.Conditions = nil
	meta.SetStatusCondition(&cacheStatus.Conditions, condition)
}

func generateUniqueName(name string) string {
	uuid := uuid.New().String()
	return fmt.Sprintf("%s-%s", name, uuid[:8])
}

func addCounts(cnts *gkmv1alpha1.CacheCounts, condType string) {
	cnts.NodeCnt = 1
	switch condType {
	case string(gkmv1alpha1.GkmCondPending):
		// Temp state, ignore
	case string(gkmv1alpha1.GkmCondExtracted):
		cnts.NodeNotInUseCnt++
	case string(gkmv1alpha1.GkmCondRunning):
		cnts.NodeInUseCnt++
	case string(gkmv1alpha1.GkmCondError):
		cnts.NodeErrorCnt++
	case string(gkmv1alpha1.GkmCondUnloadError):
		cnts.NodeErrorCnt++
	case string(gkmv1alpha1.GkmCondOutdated):
		// PodOutdatedCnt is collected in the Garbage Collection portion of the Reconcile loop.
	}
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

/*
// initCommonAgent is called as the Agent initializes by each the GKMCache
// and ClusterGKMCache Agent. It reads the database files and then calls KubeAPI
// server to retrieve the list of GKMCache or ClusterGKMCache and fixes inconsistencies.
// On a fresh start, there should be no database files and no GKMCache or ClusterGKMCache
// instances. If the Agent pod is restarted, there may be some of each, and they may not
// fully match.
func (r *ReconcilerCommonAgent[C, CL, N]) initCommonAgent(
	ctx context.Context,
	reconciler AgentReconciler[C, CL, N],
) {
	//errorHit := false
	//stillInUse := false
	//nodeCnts := make(map[string]gkmv1alpha1.CacheCounts)

	r.Logger.V(1).Info("Start reconcileCommonAgent()")

	// Get the list of existing GKMCache or ClusterGKMCache objects from KubeAPI Server.
	gkmCacheList, err := reconciler.getCacheList(ctx, []client.ListOption{})
	if err != nil {
		return
	}

	extractedList, err := database.GetExtractedCacheList(r.Logger)
	if err != nil {
		r.Logger.Error(err, "failed to list Extracted Cache")
		return
	}

	if (*gkmCacheList).GetItemsLen() == 0 {
		// KubeAPI doesn't have any GKMCache instances
		r.Logger.Info("No GKMCache entries found")
		if len(*extractedList) == 0 {
			// There are no extracted Caches on host, so nothing to do.
			r.Logger.V(1).Info("No extracted cache found, nothing to do")
			return
		}
		// No GKMCache, but there are some Caches that are installed. Check for stranded
		// Cache (Cache still in use) below.
	} else {
		// There are GKMCache instances created, so loop through each and reconcile each.
		for _, gkmCache := range (*gkmCacheList).GetItems() {
			r.Logger.V(1).Info("Reconciling",
				"Object", r.CrdCacheStr,
				"Namespace", gkmCache.GetNamespace(),
				"Name", gkmCache.GetNamespace())

		}
	}
}
*/
