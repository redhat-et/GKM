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
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	mcvClient "github.com/redhat-et/TKDK/mcv/pkg/client"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func (r *GKMCacheReconciler) isBeingDeleted() bool {
	return !r.currCache.GetDeletionTimestamp().IsZero()
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
	r.Logger = ctrl.Log.WithName("gkm-cache")

	r.Logger.Info("Enter GKMCache Reconcile", "Name", req)

	// Get the list of existing GKM Cache objects
	cacheList := &gkmv1alpha1.GKMCacheList{}
	opts := []client.ListOption{}
	if err := r.List(ctx, cacheList, opts...); err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure},
			fmt.Errorf("failed getting list of GKMCache for full reconcile %s : %v",
				req.NamespacedName,
				err)
	}

	installedList, err := database.GetInstalledCacheList(r.Logger)
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure},
			fmt.Errorf("failed getting list of installed GKMCache for full reconcile %s : %v",
				req.NamespacedName,
				err)
	}

	if len(cacheList.Items) == 0 {
		// KubeAPI doesn't have any GKMCache instances
		if len(*installedList) == 0 {
			// There are no extracted Caches on host, so nothing to do.
			r.Logger.Info("GKMCacheController found no caches")
			return ctrl.Result{Requeue: false}, nil
		} else {
			// KubeAPI doesn't have any GKMCache instances but there are extracted
			// caches on the host, so remove any caches found.
			stillInUse := false
			for key, _ := range *installedList {
				r.Logger.Info("Extract Cache stranded, removing now", "key", key)
				inUse, err := database.RemoveCache(
					key.Namespace,
					key.Name,
					key.Digest,
					r.Logger,
				)
				if inUse {
					r.Logger.Info("Extract Cache still in use",
						"namespace", key.Namespace,
						"name", key.Name,
						"digest", key.Digest)
					stillInUse = true
				} else if err != nil {
					r.Logger.Error(err, "GKMCacheController failed to delete cache",
						"namespace", key.Namespace,
						"name", key.Name,
						"digest", key.Digest)
					stillInUse = true
				}
			}
			if stillInUse {
				// There are extracted Caches on host still in use but call GKMCache have been deleted, retry.
				return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentUsagePoll}, nil
			} else {
				// There are no extracted Caches on host, all cleaned up, so nothing to do.
				return ctrl.Result{Requeue: false}, nil
			}
		}
	}

	errorHit := false
	for cacheIndex := range cacheList.Items {
		r.currCache = &cacheList.Items[cacheIndex]
		r.Logger.Info("Reconciling GKMCache", "Namespace", r.currCache.Namespace, "Name", r.currCache.Name)

		// Call KubeAPI to Retrieve GKMCacheNode for this GKMCache
		cacheNode, err := r.getCacheNode(ctx)
		if err != nil {
			// Error returned if unable to call KubeAPI or more than one instance returned.
			// Don't block Reconcile on one instance, log and go to next GKMCache.
			r.Logger.Error(err, "failed to get GKMCacheNode", "Namespace", r.currCache.Namespace, "Name", r.currCache.Name)
			errorHit = true
			continue
		}

		if cacheNode == nil {
			if r.isBeingDeleted() {
				// If the GKMCacheNode doesn't exist and the GKMCache is being deleted,
				// Nothing to do. Just continue with the next GKMCache.
				r.Logger.Info("GKMCacheNode doesn't exist and GKMCache is being deleted",
					"Namespace", r.currCache.Namespace, "Name", r.currCache.Name)
				errorHit = true
				continue
			}
			// Create a new GKMCacheNode object.
			if err = r.createCacheNode(ctx); err != nil {
				r.Logger.Error(err, "error creating GKMCacheNode", "Namespace", r.currCache.Namespace, "Name", r.currCache.Name)
			}

		}
		//r.currCacheNode = cacheNode
		//origCacheNode := r.currCacheNode.DeepCopy()

		// See if Digest has been set (Webhook validated and image is allowed to be used).
		digest, digestFound := r.currCache.Annotations[utils.GMKCacheAnnotationResolvedDigest]
		if digestFound {
			r.Logger.Info("Reconciling: Digest Found",
				"Namespace", r.currCache.Namespace, "Name", r.currCache.Name, "digest", digest)

			// Check the list of installed Cache to see if this Digest has been extracted.
			_, cacheFound := (*installedList)[database.CacheKey{
				Namespace: r.currCache.Namespace,
				Name:      r.currCache.Name,
				Digest:    digest,
			}]
			if cacheFound {
				r.Logger.Info("Reconciling: Cache already Extracted",
					"Namespace", r.currCache.Namespace, "Name", r.currCache.Name, "digest", digest)

				// Image has been extracted. Set to false in our local copy to indicate that
				// is has been processed. Used for garbage collection at the end of reconciling.
				(*installedList)[database.CacheKey{
					Namespace: r.currCache.Namespace,
					Name:      r.currCache.Name,
					Digest:    digest,
				}] = false
			} else {
				r.Logger.Info("Reconciling: Cache NOT Extracted, extract now",
					"Namespace", r.currCache.Namespace, "Name", r.currCache.Name, "digest", digest)

				// Image has NOT been extracted, call MCV to extract cache from image to host.
				if err = database.ExtractCache(
					r.currCache.Namespace,
					r.currCache.Name,
					r.currCache.Spec.Image,
					digest,
					r.NoGpu,
					r.Logger,
				); err != nil {
					// Error returned calling MCV to extract the Cache.
					// Don't block Reconcile on one instance, log and go to next GKMCache.
					r.Logger.Error(err, "unable to extract cache",
						"Namespace", r.currCache.Namespace,
						"Name", r.currCache.Name,
						"Image", r.currCache.Spec.Image,
						"Digest", digest)
					errorHit = true
					continue
				}

				// ToDo: Update GKMCacheNode With Failure
			}

		} else {
			// Webhook has not resolved image URL to a digest, so either Cosign failed
			// or the image is invalid.
			r.Logger.Info("Reconciling: Digest NOT Found, either Cosign failed or the image is invalid.",
				"Namespace", r.currCache.Namespace, "Name", r.currCache.Name)

			// ToDo: Update GKMCacheNode With Failure
		}
	}

	// Walked the installed Cache and make sure there are none that are stranded.
	// If the value is true, then it was not processed above.
	for key, _ := range *installedList {
		if (*installedList)[database.CacheKey{
			Namespace: key.Namespace,
			Name:      key.Name,
			Digest:    key.Digest,
		}] {
			r.Logger.Info("Extract Cache stranded, removing now", "key", key)
			inUse, err := database.RemoveCache(
				key.Namespace,
				key.Name,
				key.Digest,
				r.Logger,
			)
			if inUse {
				r.Logger.Info("Extract Cache still in use",
					"namespace", key.Namespace,
					"name", key.Name,
					"digest", key.Digest)
			} else if err != nil {
				r.Logger.Error(err, "GKMCacheController failed to delete cache",
					"namespace", key.Namespace,
					"name", key.Name,
					"digest", key.Digest)
			}
		}
	}

	/*
		if isConditionTrue(cache.Status.Conditions, "Ready") {
			r.Logger.Info("Cache already marked as Ready", "name", req.Name)
			return ctrl.Result{}, nil
		}

		cache.Status.LastUpdated = metav1.Now()
		setCondition(&cache, "Verified", metav1.ConditionTrue, "ImageVerified", "Image successfully verified")

		if err := r.Status().Update(ctx, &cache); err != nil {
			r.Logger.Error(err, "failed to update status")
			return ctrl.Result{}, err
		}
	*/

	if errorHit {
		// If an error was encountered during a single GKMCache instance, retry.
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentFailure}, nil
	} else {
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryAgentUsagePoll}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *GKMCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gkmv1alpha1.GKMCache{}).
		Complete(r)
}

func isConditionTrue(conds []metav1.Condition, condType string) bool {
	for _, c := range conds {
		if c.Type == condType && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func setCondition(obj *gkmv1alpha1.GKMCache, condType string, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}

// getCacheNode gets the GKMCacheNode object from KubeAPI Server for the current
// GKMCache instance for this node.
func (r *GKMCacheReconciler) getCacheNode(ctx context.Context) (*gkmv1alpha1.GKMCacheNode, error) {

	cacheNodeList := &gkmv1alpha1.GKMCacheNodeList{}

	opts := []client.ListOption{
		client.MatchingLabels{
			utils.GMKCacheLabelHostname: r.NodeName,
			utils.GMKCacheLabelOwnedBy:  r.currCache.GetName(),
		},
	}

	err := r.List(ctx, cacheNodeList, opts...)
	if err != nil {
		return nil, err
	}

	switch len(cacheNodeList.Items) {
	case 1:
		r.Logger.Info("Found GKMCacheNode", "Name", cacheNodeList.Items[0].Name)
		return &cacheNodeList.Items[0], nil
	case 0:
		// No GKMCacheNode found, so return nil
		r.Logger.Info("No GKMCacheNode found")
		return nil, nil
	default:
		// More than one matching GKMCacheNode found. This should never
		// happen, but if it does, return an error
		return nil, fmt.Errorf("more than one GKMCacheNode found (%d)", len(cacheNodeList.Items))
	}
}

/*
func (r *GKMCacheReconciler) initCacheNode() error {
	r.currCacheNode = &gkmv1alpha1.GKMCacheNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:       generateUniqueName(r.currCache.Name),
			Namespace:  r.currCache.Namespace,
			Finalizers: []string{utils.NamespaceGkmCacheFinalizer},
			Labels: map[string]string{
				utils.GMKCacheLabelOwnedBy:  r.currCache.GetName(),
				utils.GMKCacheLabelHostname: r.NodeName,
			},
		},
	}

	r.Logger.Info("Initialized GKMCacheNode object", "CacheName", r.currCache.Name, "CacheNodeName", r.currCacheNode.Name)

	if err := ctrl.SetControllerReference(r.currCache, r.currCacheNode, r.Scheme); err != nil {
		return fmt.Errorf("failed to set GKMCacheNode object owner reference: %v", err)
	}

	return nil
}

func (r *GKMCacheReconciler) createInitialCacheNode() error {
	r.currCacheNode.Status = gkmv1alpha1.GKMCacheNodeStatus{
		Node:          r.NodeName,
		AppLoadStatus: gkmv1alpha1.AppLoadNotLoaded,
		UpdateCount:   0,
		Programs:      []bpfmaniov1alpha1.ClBpfApplicationProgramState{},
		Conditions:    []metav1.Condition{},
	}
	if err := r.initializeNodeProgramList(); err != nil {
		return fmt.Errorf("failed to initialize GKMCacheNode program list. Name: %s, Error: %v", r.currCacheNode.Name, err)
	}
	r.updateBpfAppStateCondition(r, gkmv1alpha1.BpfAppStateCondPending)
	return nil
}
*/

// func (r *GKMCacheReconciler) createCacheNode(ctx context.Context) (ctrl.Result, error) {
func (r *GKMCacheReconciler) createCacheNode(ctx context.Context) error {
	gpus, err := mcvClient.GetSystemGPUInfo()
	if err != nil {
		r.Logger.Error(err, "error retrieving GPU info")
		return err
	}

	output, err := json.MarshalIndent(gpus, "", "  ")
	if err != nil {
		r.Logger.Error(err, "failed to format GPU info")
		return err
	}

	r.Logger.Info("Detected GPU Devices:", "gpus", output)
	return nil

	/*
		// Create a new GKMCacheNode object first, once it's created,
		// initialize the Status sub-resource and then update the status.
		if err := r.initCacheNode(); err != nil {
			r.Logger.Error(err, "failed to initialize GKMCacheNode object")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}
		if err := r.createInitialCacheNode(ctx); err != nil {
			r.Logger.Error(err, "failed to create GKMCacheNode object")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}
		if err := r.initBpfAppStateStatus(); err != nil {
			r.Logger.Error(err, "failed to initialize GKMCacheNode status")
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}
		if _, err := r.updateBpfAppStateStatus(ctx, nil); err != nil {
			r.Logger.Error(err, "failed to update GKMCacheNode status", "Name", r.currentApp.Name)
			return ctrl.Result{Requeue: true, RequeueAfter: retryDurationAgent}, nil
		}
		// We're done creating a new BpfApplicationState object, so we can
		// return and be requeued.
		return ctrl.Result{}, nil
	*/
}
