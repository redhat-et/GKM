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

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cargohold "github.com/redhat-et/TKDK/cargohold/pkg/fetcher"
	tkmv1alpha1 "github.com/redhat-et/TKM/api/v1alpha1"
)

// TritonKernelCacheClusterReconciler reconciles a TritonKernelCacheCluster object
type TritonKernelCacheClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tkm.io,resources=tritonkernelcacheclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tkm.io,resources=tritonkernelcacheclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tkm.io,resources=tritonkernelcacheclusters/finalizers,verbs=update

// Reconcile is part of the main Kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TritonKernelCacheClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var clusterCache tkmv1alpha1.TritonKernelCacheCluster
	if err := r.Get(ctx, req.NamespacedName, &clusterCache); err != nil {
		logger.Error(err, "unable to fetch TritonKernelCacheCluster")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if isConditionTrue(clusterCache.Status.Conditions, "Ready") {
		logger.Info("Cluster-wide cache already marked as Ready", "name", req.Name)
		return ctrl.Result{}, nil
	}

	fetcher := cargohold.NewImgFetcher()
	img, err := fetcher.FetchImg(clusterCache.Spec.CacheImage)
	if err != nil {
		logger.Error(err, "failed to fetch cluster image")
		setClusterCondition(&clusterCache, "Verified", metav1.ConditionFalse, "ImageFetchFailed", err.Error())
		_ = r.Status().Update(ctx, &clusterCache)
		return ctrl.Result{Requeue: true}, nil
	}

	digest, err := img.Digest()
	if err != nil {
		logger.Error(err, "failed to get digest")
		setClusterCondition(&clusterCache, "Verified", metav1.ConditionFalse, "DigestError", err.Error())
		_ = r.Status().Update(ctx, &clusterCache)
		return ctrl.Result{}, nil
	}

	if clusterCache.Spec.ValidateSignature {
		logger.Info("Validating cluster-wide image signature with cosign", "image", clusterCache.Spec.CacheImage)

		if err := verifyImageSignature(clusterCache.Spec.CacheImage); err != nil {
			logger.Error(err, "cluster image signature verification failed")
			setClusterCondition(&clusterCache, "Verified", metav1.ConditionFalse, "SignatureInvalid", err.Error())
			_ = r.Status().Update(ctx, &clusterCache)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		setClusterCondition(&clusterCache, "Verified", metav1.ConditionTrue, "SignatureVerified", "Cluster image signature verified successfully")
	} else {
		logger.Info("Signature verification skipped", "image", clusterCache.Spec.CacheImage)
		setClusterCondition(&clusterCache, "Verified", metav1.ConditionTrue, "SignatureSkipped", "Validation disabled by spec")
	}

	clusterCache.Status.Digest = digest.String()
	clusterCache.Status.LastSynced = metav1.Now()
	setClusterCondition(&clusterCache, "Ready", metav1.ConditionTrue, "CacheReady", "Cluster-wide cache ready for use")

	if err := r.Status().Update(ctx, &clusterCache); err != nil {
		logger.Error(err, "failed to update cluster cache status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled cluster cache", "name", req.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TritonKernelCacheClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tkmv1alpha1.TritonKernelCacheCluster{}).
		Complete(r)
}

// Helper function to set conditions on the cluster cache
func setClusterCondition(obj *tkmv1alpha1.TritonKernelCacheCluster, condType string, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}
