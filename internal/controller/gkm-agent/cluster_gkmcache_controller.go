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

package gkmAgent

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
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

// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcaches,verbs=get;list;watch
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcachenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcachenodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcachenodes/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update

// ClusterGKMCacheAgentReconciler reconciles a ClusterGKMCache object
type ClusterGKMCacheAgentReconciler struct {
	ReconcilerCommonAgent[gkmv1alpha1.ClusterGKMCache, gkmv1alpha1.ClusterGKMCacheList, gkmv1alpha1.ClusterGKMCacheNode]
}

// ClusterGKMCacheAgentReconciler reconciles/reads each ClusterGKMCache object (read-only) and creates and
// creates/updates/deletes a ClusterGKMCacheNode object to track each ClusterGKMCache on a given Node.
func (r *ClusterGKMCacheAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.Log.WithName("agent-cl")
	r.Logger.V(1).Info("Enter ClusterGKMCache Reconcile", "Name", req)

	return r.reconcileCommonAgent(ctx, r)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGKMCacheAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gkmv1alpha1.ClusterGKMCache{},
			builder.WithPredicates(predicate.And(
				predicate.GenerationChangedPredicate{},
				predicate.ResourceVersionChangedPredicate{},
			)),
		).
		// Trigger reconciliation if the ClusterGKMCacheNode for this node is modified.
		// Own() doesn't work because the ClusterGKMCacheNode is per Namespace and the
		// ClusterGKMCache is not an ownerRef, because there may be multiple ClusterGKMCache
		// that come and go.
		Watches(
			&gkmv1alpha1.ClusterGKMCacheNode{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates((ClusterGkmCacheNodePredicate(r.NodeName))),
		).
		Complete(r)
}

// Only reconcile if a program has been created for a controller's node.
func ClusterGkmCacheNodePredicate(nodeName string) predicate.Funcs {
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

// GetCacheList gets the list of ClusterGKMCache objects from KubeAPI Server.
func (r *ClusterGKMCacheAgentReconciler) getCacheList(
	ctx context.Context,
	opts []client.ListOption,
) (*gkmv1alpha1.ClusterGKMCacheList, error) {
	// Get the list of existing ClusterGKMCache objects
	cacheList := &gkmv1alpha1.ClusterGKMCacheList{}
	if err := r.List(ctx, cacheList, opts...); err != nil {
		r.Logger.Error(err, "failed to list", "Object", r.CrdCacheStr)
		return nil, err
	}

	return cacheList, nil
}

// getCacheNode gets the ClusterGKMCacheNode object from KubeAPI Server for the current
// ClusterGKMCache instance for this node.
func (r *ClusterGKMCacheAgentReconciler) getCacheNode(
	ctx context.Context,
	cacheNamespace string,
) (*gkmv1alpha1.ClusterGKMCacheNode, error) {
	cacheNodeList := &gkmv1alpha1.ClusterGKMCacheNodeList{}

	err := r.List(ctx, cacheNodeList,
		client.InNamespace(cacheNamespace),
		client.MatchingLabels{utils.GKMCacheLabelHostname: r.NodeName},
	)
	if err != nil {
		return nil, err
	}

	switch len(cacheNodeList.Items) {
	case 1:
		r.Logger.V(1).Info("Found ClusterGKMCacheNode", "Name", cacheNodeList.Items[0].Name)
		return &cacheNodeList.Items[0], nil
	case 0:
		// No ClusterGKMCacheNode found, so return nil
		r.Logger.Info("No ClusterGKMCacheNode found")
		return nil, nil
	default:
		// More than one matching ClusterGKMCacheNode found. This should never
		// happen, but if it does, return an error
		r.Logger.Info("Found Multiple ClusterGKMCacheNode, looking for",
			"Namespace", cacheNamespace, "Node", r.NodeName)
		for cacheIndex := range cacheNodeList.Items {
			r.Logger.Info("Found Multiple ClusterGKMCacheNode",
				"Namespace", cacheNodeList.Items[cacheIndex].Namespace,
				"Name", cacheNodeList.Items[cacheIndex].Name)
		}
		return nil, fmt.Errorf("more than one ClusterGKMCacheNode found (%d)", len(cacheNodeList.Items))
	}
}

// createCacheNode create the ClusterGKMCacheNode Object for this Node. This will not have any Status data
// associated with it.
func (r *ClusterGKMCacheAgentReconciler) createCacheNode(ctx context.Context, cacheNamespace, cacheName string) error {
	// Build up GKMCacheNode
	gkmCacheNode := &gkmv1alpha1.ClusterGKMCacheNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:       generateUniqueName(cacheName),
			Finalizers: []string{},
			Labels: map[string]string{
				utils.GKMCacheLabelHostname: r.NodeName,
			},
		},
	}

	r.Logger.Info("Create ClusterGKMCacheNode object",
		"Namespace", gkmCacheNode.Namespace, "CacheNodeName", gkmCacheNode.Name)

	if err := r.Create(ctx, gkmCacheNode); err != nil {
		r.Logger.Error(err, "failed to create ClusterGKMCacheNode object",
			"Namespace", gkmCacheNode.Namespace, "CacheNodeName", gkmCacheNode.Name)
		return err
	}

	return nil
}

// cacheNodeUpdateStatus calls KubeAPI server to updates the Status field for the ClusterGKMCacheNode Object.
func (r *ClusterGKMCacheAgentReconciler) cacheNodeUpdateStatus(
	ctx context.Context,
	gkmCacheNode *gkmv1alpha1.ClusterGKMCacheNode,
	nodeStatus *gkmv1alpha1.GKMCacheNodeStatus,
	reason string,
) error {
	gkmCacheNode.Status = *nodeStatus.DeepCopy()

	r.Logger.Info("Calling KubeAPI to Update ClusaterGKMCacheNode Status",
		"reason", reason,
		"Namespace", gkmCacheNode.Namespace,
		"CacheNodeName", gkmCacheNode.Name,
	)
	if err := r.Status().Update(ctx, gkmCacheNode); err != nil {
		r.Logger.Error(err, "failed to update ClusterGKMCacheNode Status",
			"reason", reason,
			"Namespace", gkmCacheNode.Namespace,
			"CacheNodeName", gkmCacheNode.Name)
		return err
	}

	return nil
}

func (r *ClusterGKMCacheAgentReconciler) isBeingDeleted(gkmCache *gkmv1alpha1.ClusterGKMCache) bool {
	return !(*gkmCache).GetDeletionTimestamp().IsZero()
}

func (r *ClusterGKMCacheAgentReconciler) validExtractedCache(cacheNamespace string) bool {
	if cacheNamespace == "" {
		return true
	} else {
		return false
	}
}

func (r *ClusterGKMCacheAgentReconciler) cacheNodeAddFinalizer(
	ctx context.Context,
	gkmCacheNode *gkmv1alpha1.ClusterGKMCacheNode,
	cacheName string,
) (bool, error) {
	if changed := controllerutil.AddFinalizer(gkmCacheNode, r.getCacheNodeFinalizer(cacheName)); changed {
		r.Logger.Info("Calling KubeAPI to add ClusterGKMCache Finalizer to ClusterGKMCacheNode",
			"Namespace", gkmCacheNode.Namespace,
			"Name", cacheName,
			"CacheNodeName", gkmCacheNode.Name)

		err := r.Update(ctx, gkmCacheNode)
		if err != nil {
			r.Logger.Error(err, "failed to add ClusterGKMCache Finalizer to ClusterGKMCacheNode",
				"Namespace", gkmCacheNode.Namespace,
				"Name", cacheName,
				"CacheNodeName", gkmCacheNode.Name)
			return false, err
		}
		return changed, nil
	}
	return false, nil
}

// getCacheNodeFinalizer returns the finalizer that is added to the ClusterGKMCacheNode object.
func (r *ClusterGKMCacheAgentReconciler) getCacheNodeFinalizer(name string) string {
	return utils.GkmCacheNodeFinalizerPrefix + name + utils.GkmCacheNodeFinalizerSubstring
}

func (r *ClusterGKMCacheAgentReconciler) cacheNodeRemoveFinalizer(
	ctx context.Context,
	gkmCacheNode *gkmv1alpha1.ClusterGKMCacheNode,
	cacheName string,
) (bool, error) {
	if changed := controllerutil.RemoveFinalizer(gkmCacheNode, r.getCacheNodeFinalizer(cacheName)); changed {
		r.Logger.Info("Calling KubeAPI to delete ClusterGKMCache Finalizer from ClusterGKMCacheNode",
			"Namespace", gkmCacheNode.Namespace,
			"Name", cacheName,
			"CacheNodeName", gkmCacheNode.Name)
		err := r.Update(ctx, gkmCacheNode)
		if err != nil {
			r.Logger.Error(err, "failed to delete ClusterGKMCache Finalizer from ClusterGKMCacheNode")
			return false, err
		}
		return changed, nil
	}
	return false, nil
}

func (r *ClusterGKMCacheAgentReconciler) cacheNodeRecordEvent(
	cacheNode *gkmv1alpha1.ClusterGKMCacheNode,
	eventReason gkmv1alpha1.GkmCacheNodeEventReason,
	cacheName, podNamespace, podName string,
	count int,
) {
	var message string
	var eventType string

	switch eventReason {
	case gkmv1alpha1.GkmCacheNodeEventReasonCreated:
		// Record the creation of GKMCacheNode
		eventType = corev1.EventTypeNormal
		message =
			"ClusterGKMCacheNode created on node \"" +
				(*cacheNode).GetNodeName() +
				"\"."
	case gkmv1alpha1.GkmCacheNodeEventReasonCacheUsed:
		eventType = corev1.EventTypeNormal
		message =
			"ClusterGKMCache \"" +
				cacheName +
				"\" used by pod \"" +
				podNamespace + "\\" + podName +
				"\". Use count \"" +
				strconv.Itoa(count) +
				"\"."
	case gkmv1alpha1.GkmCacheNodeEventReasonCacheReleased:
		eventType = corev1.EventTypeNormal
		message =
			"ClusterGKMCache \"" +
				cacheName +
				"\" no longer used by pod \"" +
				podNamespace + "\\" + podName +
				"\". Use count \"" +
				strconv.Itoa(count) +
				"\"."
	case gkmv1alpha1.GkmCacheNodeEventReasonDeleting:
		eventType = corev1.EventTypeWarning
		message =
			"ClusterGKMCache \"" +
				cacheName +
				"\" being deleted but still in use. Use count \"" +
				strconv.Itoa(count) +
				"\"."
	}

	// Record the event
	r.Recorder.Event((*cacheNode).GetClientObject(),
		eventType,
		string(eventReason),
		message)
}
