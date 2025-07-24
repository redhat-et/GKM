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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
)

// ClusterGKMCacheReconciler reconciles a ClusterGKMCache object
type ClusterGKMCacheReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcaches,verbs=get;list;watch
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcachenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcachenodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gkm.io,resources=clustergkmcachenodes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterGKMCache object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *ClusterGKMCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	/*
		var clusterCache gkmv1alpha1.ClusterGKMCache
		if err := r.Get(ctx, req.NamespacedName, &clusterCache); err != nil {
			logger.Error(err, "unable to fetch ClusterGKMCache")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		if isConditionTrue(clusterCache.Status.Conditions, "Ready") {
			logger.Info("Cluster-wide cache already marked as Ready", "name", req.Name)
			return ctrl.Result{}, nil
		}

		clusterCache.Status.LastUpdated = metav1.Now()
		setClusterCondition(&clusterCache, "Ready", metav1.ConditionTrue, "CacheReady", "Cluster-wide cache ready for use")

		if err := r.Status().Update(ctx, &clusterCache); err != nil {
			logger.Error(err, "failed to update cluster cache status")
			return ctrl.Result{}, err
		}
	*/
	logger.Info("Successfully reconciled cluster cache", "name", req.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGKMCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gkmv1alpha1.ClusterGKMCache{}).
		Complete(r)
}

// Helper function to set conditions on the cluster cache
func setClusterCondition(obj *gkmv1alpha1.ClusterGKMCache, condType string, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}
