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

package gkmoperator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/utils"
)

// GKMCacheNodeReconciler reconciles a GKMCacheNode object
type GKMCacheOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Logger logr.Logger

	currCache     *gkmv1alpha1.GKMCache
	currCacheNode *gkmv1alpha1.GKMCacheNode
}

// +kubebuilder:rbac:groups=gkm.io,resources=gkmcaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcaches/finalizers,verbs=update
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcachenodes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GKMCacheNode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *GKMCacheOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	errorHit := false
	r.currCache = nil

	r.Logger = ctrl.Log.WithName("gkm-ns-cache")
	r.Logger.Info("Enter Operator GKMCache Reconcile", "Name", req)

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

	if len(cacheList.Items) == 0 {
		// KubeAPI doesn't have any GKMCache instances, so nothing to do.
		r.Logger.Info("GKMCache Status Controller found no caches")
		return ctrl.Result{Requeue: false}, nil
	}

	// There are GKMCache instances created, so loop through each and reconcile each.
	for cacheIndex := range cacheList.Items {
		r.currCache = &cacheList.Items[cacheIndex]
		r.Logger.Info("Reconciling GKMCache", "Namespace", r.currCache.Namespace, "Name", r.currCache.Name)

		if !r.isCacheBeingDeleted() {
			// Add Finalizer to GKMCache if not there. This is a KubeAPI call, so return if finalizer needed to be added.
			if changed := controllerutil.AddFinalizer(r.currCache, r.getCacheFinalizer()); changed {
				r.Logger.Info("Calling KubeAPI to add finalizer to GKMCache",
					"Namespace", r.currCache.Namespace,
					"CacheNodeName", r.currCache.Name,
				)
				err := r.Update(ctx, r.currCache)
				if err != nil {
					r.Logger.Error(err, "failed to add Finalizer to GKMCache")
					errorHit = true
					continue
				}
				// GKMCache object was updated. Return and change will retrigger a new reconcile.
				return ctrl.Result{Requeue: false}, nil
			}
		}

		// Call KubeAPI to Retrieve the list of GKMCacheNodes for this Namespace.
		// Should be one per Node if GKMCache was created in the Namespace.
		cacheNodeList, err := r.getCacheNodeList(ctx, r.currCache.Namespace)
		if err != nil {
			// Error returned if unable to call KubeAPI. Don't block Reconcile on one instance,
			// log and go to next GKMCache.
			r.Logger.Error(err, "failed to get GKMCacheNode List", "Namespace", r.currCache.Namespace, "Name", r.currCache.Name)
			errorHit = true
			continue
		}

		// Loop through each GKMCacheNode (i.e. each Node)
		totalNodeCnt := 0
		extractedNodeCnt := 0
		runningNodeCnt := 0
		failureNodeCnt := 0
		for cacheNodeIndex := range cacheNodeList.Items {
			r.currCacheNode = &cacheNodeList.Items[cacheNodeIndex]

			// See if this GKMCache has been added to the GKMCacheNode
			outdatedCache := 0
			if _, ok := r.currCacheNode.Status.CacheStatuses[r.currCache.Name]; ok {
				// This Cache was found in a GKMCacheNode instance
				totalNodeCnt++

				// Loop through Digests for a given Cache
				for /*key*/ _, cacheStatus := range r.currCacheNode.Status.CacheStatuses[r.currCache.Name] {
					switch cacheStatus.Conditions[0].Type {
					case string(gkmv1alpha1.GkmCacheNodeCondPending):
						// Temp state, ignore
					case string(gkmv1alpha1.GkmCacheNodeCondExtracted):
						extractedNodeCnt++
					case string(gkmv1alpha1.GkmCacheNodeCondRunning):
						runningNodeCnt++
					case string(gkmv1alpha1.GkmCacheNodeCondOutdated):
						outdatedCache++
					case string(gkmv1alpha1.GkmCacheNodeCondError):
						failureNodeCnt++
					case string(gkmv1alpha1.GkmCacheNodeCondUnloadError):
						failureNodeCnt++
					}
				}
			}
		}

		r.Logger.Info("Processed GKMCache",
			"Namespace", r.currCache.Namespace,
			"CacheName", r.currCache.Name,
			"totalNodeCnt", totalNodeCnt,
			"Extracted", extractedNodeCnt,
			"Running", runningNodeCnt,
			"Failure", failureNodeCnt,
		)

		if r.isCacheBeingDeleted() {
			if totalNodeCnt == 0 {
				// Everything should be cleaned up, so delete the GKMCacheNode specific
				// finalizer from the GKMCache.
				if changed := controllerutil.RemoveFinalizer(r.currCache, r.getCacheFinalizer()); changed {
					r.Logger.Info("Calling KubeAPI to delete GKMCacheNode Finalizer from GKMCache",
						"Namespace", r.currCache.Namespace,
						"CacheNodeName", r.currCache.Name,
					)
					err := r.Update(ctx, r.currCache)
					if err != nil {
						r.Logger.Error(err, "failed to delete GKMCacheNode Finalizer from GKMCache")
						errorHit = true
						continue
					}
					// GKMCache object was updated. Return and change will retrigger a new reconcile.
					return ctrl.Result{Requeue: false}, nil
				}
			} else {
				r.Logger.Info("Deleting GKMCache still in progress",
					"Namespace", r.currCache.Namespace,
					"CacheName", r.currCache.Name,
					"Pending", totalNodeCnt,
				)
			}
		}
	}

	if errorHit {
		// If an error was encountered during a single GKMCache instance, retry after a pause.
		return ctrl.Result{Requeue: true, RequeueAfter: utils.RetryOperatorFailure}, nil
	} else {
		return ctrl.Result{Requeue: false}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *GKMCacheOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gkmv1alpha1.GKMCache{}).
		Watches(&gkmv1alpha1.GKMCacheNode{},
			&handler.EnqueueRequestForObject{},
		).
		Complete(r)
}

func (r *GKMCacheOperatorReconciler) isCacheBeingDeleted() bool {
	return !r.currCache.GetDeletionTimestamp().IsZero()
}

// getCacheFinalizer returns the finalizer that is added to the GKMCache object.
func (r *GKMCacheOperatorReconciler) getCacheFinalizer() string {
	return utils.GkmCacheFinalizer
}

func isConditionTrue(conds []metav1.Condition, condType string) bool {
	for _, c := range conds {
		if c.Type == condType && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *GKMCacheOperatorReconciler) getCacheNodeList(ctx context.Context, cacheNamespace string) (*gkmv1alpha1.GKMCacheNodeList, error) {
	cacheNodeList := &gkmv1alpha1.GKMCacheNodeList{}

	err := r.List(
		ctx,
		cacheNodeList,
		client.InNamespace(cacheNamespace),
	)
	if err != nil {
		return nil, err
	}

	return cacheNodeList, nil
}
