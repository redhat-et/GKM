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

package gkmagent

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	mcvDevices "github.com/redhat-et/MCU/mcv/pkg/accelerator/devices"
	mcvClient "github.com/redhat-et/MCU/mcv/pkg/client"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/database"
	"github.com/redhat-et/GKM/pkg/utils"
)

// +kubebuilder:rbac:groups=gkm.io,resources=gkmcaches,verbs=get;list;watch
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcachenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcachenodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcachenodes/finalizers,verbs=update

// GKMCacheReconciler reconciles a GKMCache object
type GKMCacheReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Logger   logr.Logger
	CacheDir string
	NodeName string
	NoGpu    bool

	currCache     *gkmv1alpha1.GKMCache
	currCacheNode *gkmv1alpha1.GKMCacheNode
}

func (r *GKMCacheReconciler) isCacheBeingDeleted() bool {
	return !r.currCache.GetDeletionTimestamp().IsZero()
}

// getCacheNodeFinalizer returns the finalizer that is added to the GKMCacheNode object.
func (r *GKMCacheReconciler) getCacheNodeFinalizer(name string) string {
	return utils.GkmCacheNodeFinalizerPrefix + r.currCache.Name + utils.GkmCacheNodeFinalizerSubstring
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GKMCache object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *GKMCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	errorHit := false
	r.currCache = nil

	r.Logger = ctrl.Log.WithName("gkm-cache")
	r.Logger.V(1).Info("Enter GKMCache Reconcile", "Name", req)

	// Get the list of existing GKM Cache objects
	cacheList := &gkmv1alpha1.GKMCacheList{}
	opts := []client.ListOption{}
	if err := r.List(ctx, cacheList, opts...); err != nil {
		r.Logger.Error(err, "failed to list GKMCache", "Namespace", req.Namespace, "Name", req.Name)
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure},
			fmt.Errorf("failed getting list of GKMCache for full reconcile %s : %v",
				req.NamespacedName,
				err)
	}

	extractedList, err := database.GetExtractedCacheList(r.Logger)
	if err != nil {
		r.Logger.Error(err, "failed to list Extracted Cache", "Namespace", req.Namespace, "Name", req.Name)
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure},
			fmt.Errorf("failed getting list of installed GKMCache for full reconcile %s : %v",
				req.NamespacedName,
				err)
	}

	if len(cacheList.Items) == 0 {
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
		for cacheIndex := range cacheList.Items {
			r.currCache = &cacheList.Items[cacheIndex]
			r.Logger.V(1).Info("Reconciling GKMCache", "Namespace", r.currCache.Namespace, "Name", r.currCache.Name)

			// Call KubeAPI to Retrieve GKMCacheNode for this GKMCache
			cacheNode, err := r.getCacheNode(ctx, r.currCache.Namespace)
			if err != nil {
				// Error returned if unable to call KubeAPI or more than one instance returned.
				// Don't block Reconcile on one instance, log and go to next GKMCache.
				r.Logger.Error(err, "failed to get GKMCacheNode", "Namespace", r.currCache.Namespace, "Name", r.currCache.Name)
				errorHit = true
				continue
			}

			if cacheNode == nil {
				if r.isCacheBeingDeleted() {
					// If the GKMCacheNode doesn't exist and the GKMCache is being deleted,
					// nothing to do. Just continue with the next GKMCache.
					r.Logger.Info("GKMCacheNode doesn't exist and GKMCache is being deleted",
						"Namespace", r.currCache.Namespace, "Name", r.currCache.Name)
					continue
				}

				// Create a new GKMCacheNode object.
				if err = r.createCacheNode(ctx); err != nil {
					errorHit = true
					continue
				} else {
					// Creation of GKMCacheNode Object for this Namespace was successful.
					// Return and Reconcile will be retriggered with the GKMCacheNode Object.
					return ctrl.Result{Requeue: false}, nil
				}
			}
			r.currCacheNode = cacheNode

			// GKMCacheNode takes two steps to complete. The createCacheNode() call creates the
			// Object, but r.currCacheNode.Status is not allowed to be updated in the KubeAPI
			// Create call. So if the NodeName is not set, add the initial r.currCacheNode.Status
			// data, which includes the NodeName and list of detected GPUs.
			if r.currCacheNode.Status.NodeName != r.NodeName {
				// Add initial Status data to GKMCacheNode object.
				if err = r.addGpuToCacheNode(ctx); err != nil {
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
			if resolvedDigest, digestFound := r.currCache.Annotations[utils.GMKCacheAnnotationResolvedDigest]; digestFound {
				r.Logger.V(1).Info("Digest Found",
					"Namespace", r.currCache.Namespace, "Name", r.currCache.Name, "digest", resolvedDigest)

				key := database.CacheKey{
					Namespace: r.currCache.Namespace,
					Name:      r.currCache.Name,
					Digest:    resolvedDigest,
				}

				// Before extracting and doing work on a given Cache, make sure it is not being deleted.
				if r.isCacheBeingDeleted() {
					cacheNodeUpdated, err := r.removeCacheFromCacheNode(ctx, key.Namespace, key.Name, key.Digest)
					if err != nil {
						errorHit = true
						continue
					} else if cacheNodeUpdated {
						// KubeAPI was called to update the GKMCacheNode Object. Return and Reconcile
						// will be retriggered with the GKMCacheNode Object update.
						//return ctrl.Result{Requeue: false}, nil
						return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}

					// If the Cache still exists on host, mark it as viewed so checks aren't rerun
					// in garbage collection.
					if _, cacheFound := (*extractedList)[key]; cacheFound {
						(*extractedList)[key] = false
					}

					// No work done, so process next Cache instance
					continue
				}

				// Check the list of extracted Cache to see if this Digest has been extracted.
				if _, cacheExtracted := (*extractedList)[key]; cacheExtracted {
					r.Logger.V(1).Info("Cache already Extracted",
						"Namespace", r.currCache.Namespace, "Name", r.currCache.Name, "digest", resolvedDigest)

					// Determine if anything changed by updating GKMCacheNode.Status with cache and usage data
					if cacheStatus, ok := r.currCacheNode.Status.CacheStatuses[key.Name][key.Digest]; ok {
						// Read the Cache File
						cacheStatus.VolumeSize = 0
						cacheFile, err := database.GetCacheFile(key.Namespace, key.Name, r.Logger)
						if err != nil {
							r.Logger.Error(err, "unable to read cache file, continuing",
								"Namespace", r.currCache.Namespace, "Name", r.currCache.Name, "digest", resolvedDigest)
						} else {
							if size, ok := cacheFile.Sizes[resolvedDigest]; ok {
								cacheStatus.VolumeSize = size
							}
						}

						// Read Usage Data
						usage, err := database.GetUsageData(key.Namespace, key.Name, key.Digest, r.Logger)
						if err == nil {
							cacheStatus.VolumeIds = usage.VolumeId

							// Condition: Running
							if !gkmv1alpha1.GkmCacheNodeCondRunning.IsConditionSet(cacheStatus.Conditions) {
								r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCacheNodeCondRunning.Condition())
							}
						} else {
							cacheStatus.VolumeIds = nil

							// Condition: Extracted
							if !gkmv1alpha1.GkmCacheNodeCondExtracted.IsConditionSet(cacheStatus.Conditions) {
								r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCacheNodeCondExtracted.Condition())
							}
						}

						/*
							r.Logger.Info("curr",
								"CacheStatuses", r.currCacheNode.Status.CacheStatuses[key.Name][key.Digest],
							)
							r.Logger.Info("copy",
								"CacheStatuses", cacheStatus,
							)
						*/

						if !reflect.DeepEqual(r.currCacheNode.Status.CacheStatuses[key.Name][key.Digest], cacheStatus) {
							cacheStatus.LastUpdated = metav1.Now()
							r.currCacheNode.Status.CacheStatuses[key.Name][key.Digest] = cacheStatus

							r.Logger.Info("Calling KubeAPI to Update GKMCacheNode CacheStatus",
								"Namespace", r.currCacheNode.Namespace,
								"CacheNodeName", r.currCacheNode.Name,
								"CacheName", key.Name,
								"CacheDigest", key.Digest,
							)
							if err := r.Status().Update(ctx, r.currCacheNode); err != nil {
								r.Logger.Error(err, "failed to update GKMCacheNode CacheStatus",
									"Namespace", r.currCacheNode.Namespace,
									"CacheNodeName", r.currCacheNode.Name,
									"CacheName", key.Name,
									"CacheDigest", key.Digest,
								)
								errorHit = true
								continue
							} else {
								// Update to GKMCacheNode Object for this Namespace was successful.
								// Return and Reconcile will be retriggered with the GKMCacheNode Object.
								//return ctrl.Result{Requeue: false}, nil
								return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
							}
						} else {
							r.Logger.V(1).Info("No Changes GKMCacheNode CacheStatus",
								"Namespace", r.currCacheNode.Namespace,
								"CacheNodeName", r.currCacheNode.Name,
								"CacheName", key.Name,
								"CacheDigest", key.Digest,
							)
						}
					} else {
						r.Logger.Info("GKMCacheNode CacheStatus Missing!!!!",
							"Namespace", r.currCacheNode.Namespace,
							"CacheNodeName", r.currCacheNode.Name,
							"CacheName", key.Name,
							"CacheDigest", key.Digest,
						)
					}

					// Image has been extracted and processed and nothing changed on this pass. Set t
					// false in our local copy to indicate that is has been processed. Used for garbage
					// collection at the end of reconciling.
					(*extractedList)[key] = false
				} else {
					// Cache has not been extracted. Check if error occurred in previous extraction attempt.
					if cacheStatus, ok := r.currCacheNode.Status.CacheStatuses[key.Name][key.Digest]; ok {
						if gkmv1alpha1.GkmCacheNodeCondError.IsConditionSet(cacheStatus.Conditions) {
							// Extraction error has occurred, skip this instance.
							continue
						}
					}

					// Add the finalizer, extract the Cache to the host, and then add r.currCache to the
					// list of Caches stored in the GKMCacheNode object.
					if err = r.addCacheInCacheNode(ctx, resolvedDigest); err != nil {
						errorHit = true
						continue
					} else {
						// Adding GKMCache to GKMCacheNode Object for this Namespace was successful.
						// Return and Reconcile will be retriggered with the GKMCacheNode Object.
						//return ctrl.Result{Requeue: false}, nil
						return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentNodeStatusUpdate}, nil
					}
				}

			} else {
				// Webhook has not resolved image URL to a digest, so either Cosign failed
				// or the image is invalid. This should never get here because Webhook should
				// not let GKMCache get created without a valid image.
				r.Logger.Info("Digest NOT Found, either Cosign failed or the image is invalid.",
					"Namespace", r.currCache.Namespace, "Name", r.currCache.Name)

				// ToDo: Update GKMCacheNode With Failure
			}
		}
	}
	r.currCache = nil

	// Walked the installed Cache and make sure there are none that are stranded.
	// If the value is true, then it was not processed above.
	stillInUse := false
	for key := range *extractedList {
		if (*extractedList)[key] {

			// Call KubeAPI to Retrieve GKMCacheNode for this GKMCache
			r.currCacheNode, err = r.getCacheNode(ctx, key.Namespace)
			if err != nil {
				// Error returned if unable to call KubeAPI or more than one instance returned.
				r.Logger.Error(err, "failed to get GKMCacheNode", "Namespace", key.Namespace, "Name", key.Name, "Digest", key.Digest)
			}

			cacheNodeUpdated, err := r.removeCacheFromCacheNode(ctx, key.Namespace, key.Name, key.Digest)
			if err != nil {
				errorHit = true
				continue
			} else if cacheNodeUpdated {
				// KubeAPI was called to update the GKMCacheNode Object. Return and Reconcile
				// will be retriggered with the GKMCacheNode Object update.
				return ctrl.Result{Requeue: false}, nil
			} else {
				stillInUse = true
			}
		}
	}

	// Check Usage Data for Updates

	if len(cacheList.Items) == 0 && !stillInUse {
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

// SetupWithManager sets up the controller with the Manager.
func (r *GKMCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gkmv1alpha1.GKMCache{},
			builder.WithPredicates(predicate.And(
				predicate.GenerationChangedPredicate{},
				predicate.ResourceVersionChangedPredicate{},
			)),
		).
		// Trigger reconciliation if the GKMCacheNode for this node is modified.
		// Own() doesn't work because the GKMCacheNode is per Namespace and the GKMCache
		// is not an ownerRef, because there may be multiple GKMCache that come and go on
		// the Namespace.
		Watches(
			&gkmv1alpha1.GKMCacheNode{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates((GkmCacheNodePredicate(r.NodeName))),
		).
		Complete(r)
}

// Only reconcile if a program has been created for a controller's node.
func GkmCacheNodePredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetLabels()[utils.GKMCacheLabelHostname] == nodeName
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetLabels()[utils.GKMCacheLabelHostname] == nodeName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetLabels()[utils.GKMCacheLabelHostname] == nodeName
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetLabels()[utils.GKMCacheLabelHostname] == nodeName
		},
	}
}

// getCacheNode gets the GKMCacheNode object from KubeAPI Server for the current
// GKMCache instance for this node.
func (r *GKMCacheReconciler) getCacheNode(ctx context.Context, cacheNamespace string) (*gkmv1alpha1.GKMCacheNode, error) {
	cacheNodeList := &gkmv1alpha1.GKMCacheNodeList{}

	err := r.List(ctx, cacheNodeList,
		client.InNamespace(cacheNamespace),
		client.MatchingLabels{utils.GKMCacheLabelHostname: r.NodeName},
	)
	if err != nil {
		return nil, err
	}

	switch len(cacheNodeList.Items) {
	case 1:
		r.Logger.V(1).Info("Found GKMCacheNode", "Name", cacheNodeList.Items[0].Name)
		return &cacheNodeList.Items[0], nil
	case 0:
		// No GKMCacheNode found, so return nil
		r.Logger.Info("No GKMCacheNode found")
		return nil, nil
	default:
		// More than one matching GKMCacheNode found. This should never
		// happen, but if it does, return an error
		r.Logger.Info("Found Multiple GKMCacheNode, looking for",
			"Namespace", cacheNamespace, "Node", r.NodeName)
		for cacheIndex := range cacheNodeList.Items {
			r.Logger.Info("Found Multiple GKMCacheNode",
				"Namespace", cacheNodeList.Items[cacheIndex].Namespace,
				"Name", cacheNodeList.Items[cacheIndex].Name)
		}
		return nil, fmt.Errorf("more than one GKMCacheNode found (%d)", len(cacheNodeList.Items))
	}
}

// createCacheNode create the GKMCacheNode Object for this Node. This will not have any Status data
// associated with it.
func (r *GKMCacheReconciler) createCacheNode(ctx context.Context) error {
	// Validate Input
	if r.currCache == nil {
		err := fmt.Errorf("cache not set")
		r.Logger.Error(err, "internal error")
		return err
	}

	// Build up GKMCacheNode
	r.currCacheNode = &gkmv1alpha1.GKMCacheNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:       utils.GKMCacheNodeNamePrefix + r.currCache.Namespace + "-" + r.NodeName,
			Namespace:  r.currCache.Namespace,
			Finalizers: []string{},
			Labels: map[string]string{
				utils.GKMCacheLabelHostname: r.NodeName,
			},
		},
	}

	r.Logger.Info("Create GKMCacheNode object",
		"Namespace", r.currCacheNode.Namespace, "CacheNodeName", r.currCacheNode.Name)

	if err := r.Create(ctx, r.currCacheNode); err != nil {
		r.Logger.Error(err, "failed to create GKMCacheNode object",
			"Namespace", r.currCacheNode.Namespace, "CacheNodeName", r.currCacheNode.Name)
		return err
	}

	return nil
}

// addGpuToCacheNode calls MCV to collect the set of detected GPUs and adds them to the
// GKMCacheNode.Status.GpuStatuses field. This is only done once right after object creation
// and is not refreshed.
func (r *GKMCacheReconciler) addGpuToCacheNode(ctx context.Context) error {
	// Validate Input
	if r.currCacheNode == nil {
		err := fmt.Errorf("cache node not set")
		r.Logger.Error(err, "internal error")
		return err
	}

	// Retrieve GPU Data
	var gpus *mcvDevices.GPUFleetSummary
	var err error

	// Stub out the GPU Ids when in TestMode (No GPUs)
	if r.NoGpu {
		var tmpGpus mcvDevices.GPUFleetSummary

		tmpGpus.GPUs = append(tmpGpus.GPUs, mcvDevices.GPUGroup{
			GPUType:       "Instinct MI210",
			DriverVersion: "535.43.02",
			IDs:           []int{int(0)},
		})
		tmpGpus.GPUs = append(tmpGpus.GPUs, mcvDevices.GPUGroup{
			GPUType:       "RTX 3090",
			DriverVersion: "2.23.4",
			IDs:           []int{int(1)},
		})
		tmpGpus.GPUs = append(tmpGpus.GPUs, mcvDevices.GPUGroup{
			GPUType:       "RTX 3090",
			DriverVersion: "2.23.4",
			IDs:           []int{int(2)},
		})

		gpus = &tmpGpus
	} else {
		gpus, err = mcvClient.GetSystemGPUInfo()
		if err != nil {
			r.Logger.Error(err, "error retrieving GPU info")
			//return err
		} else {
			r.Logger.Info("Detected GPU Devices:", "gpus", gpus)
		}
	}

	// Add GPU Data to Status
	status := gkmv1alpha1.GKMCacheNodeStatus{
		NodeName: r.NodeName,
	}

	for _, gpu := range gpus.GPUs {
		status.GpuStatuses = append(status.GpuStatuses, gkmv1alpha1.GpuStatus{
			GpuType:       gpu.GPUType,
			DriverVersion: gpu.DriverVersion,
			GpuList:       gpu.IDs,
		})
	}

	// Build up GKMCacheNode
	r.currCacheNode.Status = status

	r.Logger.Info("Update GKMCacheNode GPU list",
		"Namespace", r.currCacheNode.Namespace, "CacheNodeName", r.currCacheNode.Name)
	if err := r.Status().Update(ctx, r.currCacheNode); err != nil {
		r.Logger.Error(err, "failed to update GKMCacheNode GPU list",
			"Namespace", r.currCacheNode.Namespace, "CacheNodeName", r.currCacheNode.Name)
		return err
	}

	return nil
}

// addCacheToCacheNode adds a GKMCache status to the GKMCacheNode.Status.CacheStatuses field.
func (r *GKMCacheReconciler) addCacheInCacheNode(ctx context.Context, digest string) error {
	// Validate Input
	if r.currCache == nil {
		err := fmt.Errorf("cache not set")
		r.Logger.Error(err, "internal error")
		return err
	}
	if r.currCacheNode == nil {
		err := fmt.Errorf("cache node not set")
		r.Logger.Error(err, "internal error")
		return err
	}

	// Add Finalizer to GKMCacheNode if not there. This is a KubeAPI call, so return if finalizer needed to be added.
	if changed := controllerutil.AddFinalizer(r.currCacheNode, r.getCacheNodeFinalizer(r.currCache.Name)); changed {
		r.Logger.Info("Calling KubeAPI to add finalizer to GKMCacheNode",
			"Namespace", r.currCacheNode.Namespace,
			"CacheNodeName", r.currCacheNode.Name,
			"CacheName", r.currCache.Name,
			"CacheDigest", digest,
		)
		err := r.Update(ctx, r.currCacheNode)
		if err != nil {
			r.Logger.Error(err, "failed to add Finalizer to GKMCacheNode")
			return err
		}
		return nil
	}

	cacheStatus := gkmv1alpha1.CacheStatus{
		LastUpdated: metav1.Now(),
	}

	r.Logger.Info("Cache NOT Extracted, extract now",
		"Namespace", r.currCache.Namespace, "Name", r.currCache.Name, "digest", digest)

	// Image has NOT been extracted, call MCV to extract cache from image to host.
	matchedIds, unmatchedIds, err := database.ExtractCache(
		r.currCache.Namespace,
		r.currCache.Name,
		r.currCache.Spec.Image,
		digest,
		r.NoGpu,
		r.Logger,
	)
	if err != nil {
		// Error returned calling MCV to extract the Cache.
		r.Logger.Error(err, "unable to extract cache",
			"Namespace", r.currCache.Namespace,
			"Name", r.currCache.Name,
			"Image", r.currCache.Spec.Image,
			"Digest", digest)

		r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCacheNodeCondError.Condition())
	} else {
		r.setCacheNodeConditions(&cacheStatus, gkmv1alpha1.GkmCacheNodeCondExtracted.Condition())

		// Stub out the GPU Ids when in TestMode (No GPUs)
		if r.NoGpu {
			matchedIds = append(matchedIds, 0)
			unmatchedIds = append(unmatchedIds, 1, 2)
		}

		cacheStatus.CompGpuList = matchedIds
		cacheStatus.IncompGpuList = unmatchedIds
	}

	// Build up GKMCacheNode.Status.
	if len(r.currCacheNode.Status.CacheStatuses) == 0 {
		r.Logger.Info("Allocating GKMCacheNode.Status.CacheStatuses",
			"Namespace", r.currCacheNode.Namespace,
			"CacheNodeName", r.currCacheNode.Name,
			"CacheName", r.currCache.Name,
			"CacheDigest", digest,
		)
		r.currCacheNode.Status.CacheStatuses = make(map[string]map[string]gkmv1alpha1.CacheStatus)
	}

	r.currCacheNode.Status.CacheStatuses[r.currCache.Name] = make(map[string]gkmv1alpha1.CacheStatus)
	r.currCacheNode.Status.CacheStatuses[r.currCache.Name][digest] = cacheStatus

	r.Logger.Info("Calling KubeAPI to update GKMCacheNode with Cache",
		"Namespace", r.currCacheNode.Namespace,
		"CacheNodeName", r.currCacheNode.Name,
		"CacheName", r.currCache.Name,
		"CacheDigest", digest,
	)
	if err := r.Status().Update(ctx, r.currCacheNode); err != nil {
		r.Logger.Error(err, "failed to update GKMCacheNode with Cache",
			"Namespace", r.currCacheNode.Namespace,
			"CacheNodeName", r.currCacheNode.Name,
			"CacheName", r.currCache.Name,
			"CacheDigest", digest,
		)
		return err
	}

	return nil
}

// removeCacheFromCacheNode removes a GKMCache status from the GKMCacheNode.Status.CacheStatuses field.
// This function returns true id KubeAPI was called on the GKMCacheNode. This implies that Reconcile
// needs to be exited and restarted on the next call.
func (r *GKMCacheReconciler) removeCacheFromCacheNode(ctx context.Context, cacheNamespace, cacheName, digest string) (bool, error) {
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
		r.Logger.Info("Not deleted, extract Cache still in use",
			"namespace", cacheNamespace,
			"name", cacheName,
			"digest", digest)
		return false, nil
	} else if err != nil {
		r.Logger.Error(err, "GKMCacheController failed to delete cache",
			"namespace", cacheNamespace,
			"name", cacheName,
			"digest", digest)
		return false, err
	}

	// Extracted Cache deleted from host, remove the Cache data for this Digest from the GKMCacheNode.
	if r.currCacheNode != nil {
		if _, ok := r.currCacheNode.Status.CacheStatuses[cacheName][digest]; ok {
			r.Logger.Info("Deleting CacheStatus via Digest from GKMCacheNode",
				"Namespace", r.currCacheNode.Namespace,
				"CacheNodeName", r.currCacheNode.Name,
				"CacheName", cacheName,
				"CacheDigest", digest,
			)
			delete(r.currCacheNode.Status.CacheStatuses[cacheName], digest)
			updated = true
		}

		// If all the Digests are removed from the given Cache, remove the Cache entry from the GKMCacheNode.
		if _, ok := r.currCacheNode.Status.CacheStatuses[cacheName]; ok {
			if len(r.currCacheNode.Status.CacheStatuses[cacheName]) == 0 {
				r.Logger.Info("Also Deleting CacheStatus via Name from GKMCacheNode",
					"Namespace", r.currCacheNode.Namespace,
					"CacheNodeName", r.currCacheNode.Name,
					"CacheName", cacheName,
					"CacheDigest", digest,
				)
				delete(r.currCacheNode.Status.CacheStatuses, cacheName)
				updated = true
			}
		}

		if updated {
			if err := r.Status().Update(ctx, r.currCacheNode); err != nil {
				r.Logger.Error(err, "failed to update GKMCacheNode with Cache",
					"Namespace", r.currCacheNode.Namespace,
					"CacheNodeName", r.currCacheNode.Name,
					"CacheName", cacheName,
					"CacheDigest", digest,
				)
				return false, err
			}

			return updated, nil
		}

		// Cache was already removed from GKMCacheNode, so delete the GKMCache specific
		// finalizer from the GKMCacheNode.
		if changed := controllerutil.RemoveFinalizer(r.currCacheNode, r.getCacheNodeFinalizer(cacheName)); changed {
			r.Logger.Info("Calling KubeAPI to delete GKMCache Finalizer from GKMCacheNode",
				"Namespace", r.currCacheNode.Namespace,
				"CacheNodeName", r.currCacheNode.Name,
				"CacheName", cacheName,
				"CacheDigest", digest,
			)
			err := r.Update(ctx, r.currCacheNode)
			if err != nil {
				r.Logger.Error(err, "failed to delete GKMCache Finalizer from GKMCacheNode")
				return false, err
			}
			return changed, nil
		}
	}

	return false, nil
}

// Helper function to set conditions on a GKMCacheNode
func (r *GKMCacheReconciler) setCacheNodeConditions(cacheStatus *gkmv1alpha1.CacheStatus, condition metav1.Condition) {
	cacheStatus.Conditions = nil
	meta.SetStatusCondition(&cacheStatus.Conditions, condition)
}
