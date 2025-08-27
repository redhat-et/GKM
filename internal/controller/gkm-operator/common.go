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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
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

	/*
		getCacheNode(ctx context.Context, cacheNamespace string) (*N, error)
		createCacheNode(ctx context.Context, cacheNamespace, cacheName string) error
	*/
	cacheUpdateStatus(ctx context.Context, gkmCache *C, cacheStatus *gkmv1alpha1.GKMCacheStatus, reason string) error

	isBeingDeleted(gkmCache *C) bool
	/*
		validExtractedCache(cacheNamespace string) bool
	*/
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

	r.Logger.V(1).Info("Start reconcileCommonOperator()")

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
		r.Logger.Info("GKMCache Status Controller found no caches")
		return ctrl.Result{Requeue: false}, nil
	}

	// There are GKMCache instances created, so loop through each and reconcile each.
	for _, gkmCache := range (*gkmCacheList).GetItems() {
		r.Logger.V(1).Info("Reconciling",
			"Object", r.CrdCacheStr,
			"Namespace", gkmCache.GetNamespace(),
			"Name", gkmCache.GetNamespace())

		// See if Digest has been set (Webhook validated and image is allowed to be used).
		annotations := gkmCache.GetAnnotations()
		resolvedDigest, digestFound := annotations[utils.GMKCacheAnnotationResolvedDigest]
		if !digestFound {
			// If digest not found, Webhook is still processing, skip over and reconcile on
			// next time in loop.
			r.Logger.Info("Digest NOT Found, Webhook still processing.",
				"Object", r.CrdCacheStr,
				"Namespace", gkmCache.GetNamespace(),
				"Name", gkmCache.GetName())
			continue
		}

		if !reconciler.isBeingDeleted(&gkmCache) {
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

		// Call KubeAPI to Retrieve the list of GKMCacheNodes for this Namespace.
		// Should be one per Node if GKMCache was created in the Namespace.
		opts := []client.ListOption{
			client.InNamespace(gkmCache.GetNamespace()),
		}
		gkmCacheNodeList, err := reconciler.getCacheNodeList(ctx, opts)
		if err != nil {
			// Error returned if unable to call KubeAPI. Don't block Reconcile on one instance,
			// log and go to next GKMCache.
			r.Logger.Error(err, "failed to get GKMCacheNode List", "Namespace", gkmCache.GetNamespace(), "Name", gkmCache.GetName())
			errorHit = true
			continue
		}

		gkmCacheStatus := gkmCache.GetStatus().DeepCopy()
		gkmCacheStatus.Counts = gkmv1alpha1.CacheCounts{}
		gkmCacheStatus.ResolvedDigest = resolvedDigest

		// Loop through each GKMCacheNode (i.e. each Node)
		for _, gkmCacheNode := range (*gkmCacheNodeList).GetItems() {
			nodeStatus := gkmCacheNode.GetStatus()
			if nodeStatus != nil {
				// See if this GKMCache has been added to the GKMCacheNode
				if _, ok := nodeStatus.CacheStatuses[gkmCache.GetName()]; ok {
					// This Cache was found in a GKMCacheNode instance
					gkmCacheStatus.Counts.NodeCnt += nodeStatus.Counts.NodeCnt
					gkmCacheStatus.Counts.NodeInUseCnt += nodeStatus.Counts.NodeInUseCnt
					gkmCacheStatus.Counts.NodeNotInUseCnt += nodeStatus.Counts.NodeNotInUseCnt
					gkmCacheStatus.Counts.NodeErrorCnt += nodeStatus.Counts.NodeErrorCnt
					gkmCacheStatus.Counts.PodRunningCnt += nodeStatus.Counts.PodRunningCnt
					gkmCacheStatus.Counts.PodOutdatedCnt += nodeStatus.Counts.PodOutdatedCnt
				}
			}
		}

		r.Logger.V(1).Info("Processed GKMCache",
			"Namespace", gkmCache.GetNamespace(),
			"CacheName", gkmCache.GetName(),
			"NodeCnt", gkmCacheStatus.Counts.NodeCnt,
			"NodeInUse", gkmCacheStatus.Counts.NodeInUseCnt,
			"NodeNotInUse", gkmCacheStatus.Counts.NodeNotInUseCnt,
			"NodeError", gkmCacheStatus.Counts.NodeErrorCnt,
			"PodRunning", gkmCacheStatus.Counts.PodRunningCnt,
			"PodOutdated", gkmCacheStatus.Counts.PodOutdatedCnt,
		)

		if !reconciler.isBeingDeleted(&gkmCache) {
			reason := "Update Counts"
			// Adjust Condition if need
			if gkmCacheStatus.Counts.NodeErrorCnt != 0 {
				if !gkmv1alpha1.GkmCondError.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondError.Condition())
					reason = "Set Error Condition"
				}
			} else if gkmCacheStatus.Counts.PodOutdatedCnt != 0 {
				if !gkmv1alpha1.GkmCondOutdated.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondOutdated.Condition())
					reason = "Set Outdated Condition"
				}
			} else if gkmCacheStatus.Counts.NodeInUseCnt != 0 {
				if !gkmv1alpha1.GkmCondRunning.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondRunning.Condition())
					reason = "Set Running Condition"
				}
			} else if gkmCacheStatus.Counts.NodeNotInUseCnt != 0 {
				if !gkmv1alpha1.GkmCondExtracted.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondExtracted.Condition())
					reason = "Set Extracted Condition"
				}
			} else {
				if !gkmv1alpha1.GkmCondPending.IsConditionSet(gkmCacheStatus.Conditions) {
					r.setCacheConditions(gkmCacheStatus, gkmv1alpha1.GkmCondPending.Condition())
					reason = "Set Pending Condition"
				}
			}

			if !reflect.DeepEqual(gkmCache.GetStatus(), gkmCacheStatus) {
				gkmCacheStatus.LastUpdated = metav1.Now()

				if err := reconciler.cacheUpdateStatus(ctx, &gkmCache, gkmCacheStatus, reason); err != nil {
					errorHit = true
					continue
				} else {
					// GKMCache Object was updated successfully.
					// Return and Reconcile will be retriggered with the GKMCache Object.
					return ctrl.Result{Requeue: false}, nil
				}
			}
		} else {
			if gkmCacheStatus.Counts.NodeCnt == 0 {
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

// setCacheConditions is a helper function to set conditions on the a GKMCache or ClusterGKMCache object.
func (r *ReconcilerCommonOperator[C, CL, N, NL]) setCacheConditions(gkmCacheStatus *gkmv1alpha1.GKMCacheStatus, condition metav1.Condition) {
	gkmCacheStatus.Conditions = nil
	meta.SetStatusCondition(&gkmCacheStatus.Conditions, condition)
}
