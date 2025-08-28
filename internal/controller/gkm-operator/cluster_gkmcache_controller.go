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

//lint:file-ignore U1000 Linter claims functions unused, but are required for generic

package gkmOperator

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/utils"
)

// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcaches/finalizers,verbs=update
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcachenodes,verbs=get;list;watch

// ClusterGKMCacheOperatorReconciler reconciles a ClusterGKMCache Status object
type ClusterGKMCacheOperatorReconciler struct {
	ReconcilerCommonOperator[
		gkmv1alpha1.ClusterGKMCache,
		gkmv1alpha1.ClusterGKMCacheList,
		gkmv1alpha1.ClusterGKMCacheNode,
		gkmv1alpha1.ClusterGKMCacheNodeList,
	]
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state. Reconcile()
// calls reconcileCommonOperator(), which performs common reconciliation for GKMCache
// and ClusterGKMCache object. It reconciles each GKMCache or ClusterGKMCache in the
// retrieved list, reading all the associated GKMCacheNode or ClusterGKMCacheNode objects
// and consolidating the state of each node in the GKMCache or ClusterGKMCache
// Status field. The Operator owns the GKMCache and ClusterGKMCache Objects, so
// the Operator will call KubeAPI to update the objects when needed.
// Agent reconciler only reads the objects. The Agent owns GKMCacheNode and
// ClusterGKMCacheNode Objects, and calls KubeAPI Server to make sure they reflect
// the current state of the GKMCache and ClusterGKMCache Objects on a given node.
func (r *ClusterGKMCacheOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("oper-cl")
	r.Logger.V(1).Info("Enter ClusterGKMCache Operator Reconcile", "Name", req)

	return r.reconcileCommonOperator(ctx, r)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGKMCacheOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gkmv1alpha1.ClusterGKMCache{}).
		Watches(&gkmv1alpha1.ClusterGKMCacheNode{},
			&handler.EnqueueRequestForObject{},
		).
		Complete(r)
}

// GetCacheList gets the list of GKMCache objects from KubeAPI Server.
func (r *ClusterGKMCacheOperatorReconciler) getCacheList(
	ctx context.Context,
	opts []client.ListOption,
) (*gkmv1alpha1.ClusterGKMCacheList, error) {
	// Get the list of existing GKMCache objects
	cacheList := &gkmv1alpha1.ClusterGKMCacheList{}
	if err := r.List(ctx, cacheList, opts...); err != nil {
		r.Logger.Error(err, "failed to list", "Object", r.CrdCacheStr)
		return nil, err
	}

	return cacheList, nil
}

// GetCacheNodeList gets the list of GKMCacheNode objects from KubeAPI Server.
func (r *ClusterGKMCacheOperatorReconciler) getCacheNodeList(
	ctx context.Context,
	opts []client.ListOption,
) (*gkmv1alpha1.ClusterGKMCacheNodeList, error) {
	// Get the list of existing GKMCache objects
	cacheList := &gkmv1alpha1.ClusterGKMCacheNodeList{}
	if err := r.List(ctx, cacheList, opts...); err != nil {
		r.Logger.Error(err, "failed to list", "Object", r.CrdCacheNodeStr)
		return nil, err
	}

	return cacheList, nil
}

// cacheUpdateStatus calls KubeAPI server to updates the Status field for the ClusterGKMCache Object.
func (r *ClusterGKMCacheOperatorReconciler) cacheUpdateStatus(
	ctx context.Context,
	gkmCache *gkmv1alpha1.ClusterGKMCache,
	cacheStatus *gkmv1alpha1.GKMCacheStatus,
	reason string,
) error {
	gkmCache.Status = *cacheStatus.DeepCopy()

	r.Logger.Info("Calling KubeAPI to Update ClusterGKMCache Status",
		"reason", reason,
		"CacheName", gkmCache.Name,
	)
	if err := r.Status().Update(ctx, gkmCache); err != nil {
		r.Logger.Info("failed to update ClusterGKMCache Status",
			"err", err,
			"reason", reason,
			"CacheName", gkmCache.Name)
		return err
	}

	return nil
}

func (r *ClusterGKMCacheOperatorReconciler) isBeingDeleted(gkmCache *gkmv1alpha1.ClusterGKMCache) bool {
	return !gkmCache.GetDeletionTimestamp().IsZero()
}

/*

func isConditionTrue(conds []metav1.Condition, condType string) bool {
	for _, c := range conds {
		if c.Type == condType && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *ClusterGKMCacheOperatorReconciler) getCacheNodeList(ctx context.Context, cacheNamespace string) (*gkmv1alpha1.GKMCacheNodeList, error) {
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
*/

func (r *ClusterGKMCacheOperatorReconciler) cacheAddFinalizer(ctx context.Context, gkmCache *gkmv1alpha1.ClusterGKMCache) (bool, error) {
	if changed := controllerutil.AddFinalizer(gkmCache, r.getCacheFinalizer()); changed {
		r.Logger.Info("Calling KubeAPI to add Finalizer to ClusterGKMCache",
			"CacheNodeName", gkmCache.Name)

		err := r.Update(ctx, gkmCache)
		if err != nil {
			r.Logger.Error(err, "failed to add Finalizer to ClusterGKMCache",
				"CacheNodeName", gkmCache.Name)
			return false, err
		}
		return changed, nil
	}
	return false, nil
}

// getCacheFinalizer returns the finalizer that is added to the GKMCache object.
func (r *ClusterGKMCacheOperatorReconciler) getCacheFinalizer() string {
	return utils.ClusterGkmCacheFinalizer
}

func (r *ClusterGKMCacheOperatorReconciler) cacheRemoveFinalizer(
	ctx context.Context,
	gkmCache *gkmv1alpha1.ClusterGKMCache,
) (bool, error) {
	if changed := controllerutil.RemoveFinalizer(gkmCache, r.getCacheFinalizer()); changed {
		r.Logger.Info("Calling KubeAPI to delete  Finalizer from ClusterGKMCache",
			"CacheNodeName", gkmCache.Name)
		err := r.Update(ctx, gkmCache)
		if err != nil {
			r.Logger.Error(err, "failed to delete Finalizer from ClusterGKMCache")
			return false, err
		}
		return changed, nil
	}
	return false, nil
}
