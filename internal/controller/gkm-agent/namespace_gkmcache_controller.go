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

package gkmagent

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/utils"
)

// +kubebuilder:rbac:groups=gkm.io,resources=gkmcaches,verbs=get;list;watch
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcachenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcachenodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gkm.io,resources=gkmcachenodes/finalizers,verbs=update

// GKMCacheReconciler reconciles a GKMCache object
type GKMCacheReconciler struct {
	ReconcilerCommon[gkmv1alpha1.GKMCache, gkmv1alpha1.GKMCacheList, gkmv1alpha1.GKMCacheNode]
}

// GKMCacheReconciler reconciles/reads each GKMCache object (read-only) and creates and
// creates/updates/deletes a GKMCacheNode object to track each GKMCache on a given Node.
func (r *GKMCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("gkm-ns-cache")
	r.Logger.V(1).Info("Enter GKMCache Reconcile", "Name", req)

	return r.reconcileCommon(ctx, r)
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
		// Own() doesn't work because the GKMCacheNode is per Namespace and the
		// GKMCache is not an ownerRef, because there may be multiple GKMCache
		// that come and go on the Namespace.
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

// GetCacheList gets the list of GKMCache objects from KubeAPI Server.
func (r *GKMCacheReconciler) getCacheList(
	ctx context.Context,
	opts []client.ListOption,
) (*gkmv1alpha1.GKMCacheList, error) {
	// Get the list of existing GKMCache objects
	cacheList := &gkmv1alpha1.GKMCacheList{}
	if err := r.List(ctx, cacheList, opts...); err != nil {
		r.Logger.Error(err, "failed to list", "Object", r.CrdCacheStr)
		return nil, err
	}

	return cacheList, nil
}

// getCacheNode gets the GKMCacheNode object from KubeAPI Server for the current
// GKMCache instance for this node.
func (r *GKMCacheReconciler) getCacheNode(
	ctx context.Context,
	cacheNamespace string,
) (*gkmv1alpha1.GKMCacheNode, error) {
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
func (r *GKMCacheReconciler) createCacheNode(ctx context.Context, cacheNamespace, cacheName string) error {
	// Build up GKMCacheNode
	gkmCacheNode := &gkmv1alpha1.GKMCacheNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:       generateUniqueName(cacheName),
			Namespace:  cacheNamespace,
			Finalizers: []string{},
			Labels: map[string]string{
				utils.GKMCacheLabelHostname: r.NodeName,
			},
		},
	}

	r.Logger.Info("Create GKMCacheNode object",
		"Namespace", gkmCacheNode.Namespace, "CacheNodeName", gkmCacheNode.Name)

	if err := r.Create(ctx, gkmCacheNode); err != nil {
		r.Logger.Error(err, "failed to create GKMCacheNode object",
			"Namespace", gkmCacheNode.Namespace, "CacheNodeName", gkmCacheNode.Name)
		return err
	}

	return nil
}

// cacheNodeUpdateStatus calls KubeAPI server to updates the Status field for the GKMCacheNode Object.
func (r *GKMCacheReconciler) cacheNodeUpdateStatus(
	ctx context.Context,
	gkmCacheNode *gkmv1alpha1.GKMCacheNode,
	nodeStatus *gkmv1alpha1.GKMCacheNodeStatus,
	reason string,
) error {
	gkmCacheNode.Status = *nodeStatus.DeepCopy()

	r.Logger.Info("Calling KubeAPI to Update GKMCacheNode Status",
		"reason", reason,
		"Namespace", gkmCacheNode.Namespace,
		"CacheNodeName", gkmCacheNode.Name,
	)
	if err := r.Status().Update(ctx, gkmCacheNode); err != nil {
		r.Logger.Error(err, "failed to update GKMCacheNode Status",
			"reason", reason,
			"Namespace", gkmCacheNode.Namespace,
			"CacheNodeName", gkmCacheNode.Name)
		return err
	}

	return nil
}

func (r *GKMCacheReconciler) isBeingDeleted(gkmCache *gkmv1alpha1.GKMCache) bool {
	return !(*gkmCache).GetDeletionTimestamp().IsZero()
}

func (r *GKMCacheReconciler) validExtractedCache(cacheNamespace string) bool {
	if cacheNamespace == "" {
		return false
	} else {
		return true
	}
}

func (r *GKMCacheReconciler) cacheNodeAddFinalizer(ctx context.Context, gkmCacheNode *gkmv1alpha1.GKMCacheNode, cacheName string) (bool, error) {
	if changed := controllerutil.AddFinalizer(gkmCacheNode, r.getCacheNodeFinalizer(cacheName)); changed {
		r.Logger.Info("Calling KubeAPI to add GKMCache Finalizer to GKMCacheNode",
			"Namespace", gkmCacheNode.Namespace,
			"Name", cacheName,
			"CacheNodeName", gkmCacheNode.Name)

		err := r.Update(ctx, gkmCacheNode)
		if err != nil {
			r.Logger.Error(err, "failed to add GKMCache Finalizer to GKMCacheNode",
				"Namespace", gkmCacheNode.Namespace,
				"Name", cacheName,
				"CacheNodeName", gkmCacheNode.Name)
			return false, err
		}
		return changed, nil
	}
	return false, nil
}

// getCacheNodeFinalizer returns the finalizer that is added to the GKMCacheNode object.
func (r *GKMCacheReconciler) getCacheNodeFinalizer(name string) string {
	return utils.GkmCacheNodeFinalizerPrefix + name + utils.GkmCacheNodeFinalizerSubstring
}

func (r *GKMCacheReconciler) cacheNodeRemoveFinalizer(
	ctx context.Context,
	gkmCacheNode *gkmv1alpha1.GKMCacheNode,
	cacheName string,
) (bool, error) {
	if changed := controllerutil.RemoveFinalizer(gkmCacheNode, r.getCacheNodeFinalizer(cacheName)); changed {
		r.Logger.Info("Calling KubeAPI to delete GKMCache Finalizer from GKMCacheNode",
			"Namespace", gkmCacheNode.Namespace,
			"Name", cacheName,
			"CacheNodeName", gkmCacheNode.Name)
		err := r.Update(ctx, gkmCacheNode)
		if err != nil {
			r.Logger.Error(err, "failed to delete GKMCache Finalizer from GKMCacheNode")
			return false, err
		}
		return changed, nil
	}
	return false, nil
}
