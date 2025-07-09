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

	tkmv1alpha1 "github.com/redhat-et/TKM/api/v1alpha1"
)

// TKMCacheNodeReconciler reconciles a TKMCacheNode object
type TKMCacheNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tkm.io,resources=tkmcachenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tkm.io,resources=tkmcachenodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tkm.io,resources=tkmcachenodes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TKMCacheNode object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *TKMCacheNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var nodeStatus tkmv1alpha1.TKMCacheNode
	if err := r.Get(ctx, req.NamespacedName, &nodeStatus); err != nil {
		logger.Error(err, "unable to fetch TKMCacheNodeStatus")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if isConditionTrue(nodeStatus.Status.Conditions, "Ready") {
		logger.Info("Node cache already marked as Ready", "name", req.Name)
		return ctrl.Result{}, nil
	}

	for i, cache := range nodeStatus.Spec.CacheStatuses {
		gpuType, driverVersion, err := detectGPU()
		if err != nil {
			logger.Error(err, "failed to detect GPU")
			setNodeCondition(&nodeStatus, "Compatible", metav1.ConditionFalse, "GPUDetectError", err.Error())
			_ = r.Status().Update(ctx, &nodeStatus)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		if !isCompatible(cache.GpuType, gpuType, cache.DriverVersion, driverVersion) {
			logger.Info("GPU incompatibility detected", "node", req.Name, "gpuType", gpuType, "driverVersion", driverVersion)
			setNodeCondition(&nodeStatus, "Compatible", metav1.ConditionFalse, "IncompatibleGPU", "Cache incompatible with node GPU")
			_ = r.Status().Update(ctx, &nodeStatus)
			return ctrl.Result{}, nil
		}

		// Update the GPU type and driver version
		nodeStatus.Spec.CacheStatuses[i].GpuType = gpuType
		nodeStatus.Spec.CacheStatuses[i].DriverVersion = driverVersion
	}
	setNodeCondition(&nodeStatus, "Ready", metav1.ConditionTrue, "CacheReady", "Node cache is compatible and ready")
	if err := r.Status().Update(ctx, &nodeStatus); err != nil {
		logger.Error(err, "failed to update node status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled node status", "node", req.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TKMCacheNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tkmv1alpha1.TKMCacheNode{}).
		Complete(r)
}

// Helper function to detect GPU information
func detectGPU() (string, string, error) {
	// TODO: reuse tcv to do GPU detection
	return "nvidia", "470.57.02", nil // Stub: Replace with actual GPU detection
}

// Check compatibility between cache GPU requirements and detected GPU
func isCompatible(requiredGPU, detectedGPU, requiredDriver, detectedDriver string) bool {
	return requiredGPU == detectedGPU && requiredDriver == detectedDriver
}

// Helper function to set conditions on the node status
func setNodeCondition(obj *tkmv1alpha1.TKMCacheNode, condType string, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}
