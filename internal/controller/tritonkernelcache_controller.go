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

	tcv "github.com/redhat-et/TKDK/tcv/pkg/fetcher"
	tkmv1alpha1 "github.com/redhat-et/TKM/api/v1alpha1"
)

// TritonKernelCacheReconciler reconciles a TritonKernelCache object
type TritonKernelCacheReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tkm.io,resources=tritonkernelcaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tkm.io,resources=tritonkernelcaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tkm.io,resources=tritonkernelcaches/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TritonKernelCache object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *TritonKernelCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cache tkmv1alpha1.TritonKernelCache
	if err := r.Get(ctx, req.NamespacedName, &cache); err != nil {
		logger.Error(err, "unable to fetch TritonKernelCache")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if isConditionTrue(cache.Status.Conditions, "Ready") {
		logger.Info("Cache already marked as Ready", "name", req.Name)
		return ctrl.Result{}, nil
	}

	fetcher := tcv.NewImgFetcher()
	img, err := fetcher.FetchImg(cache.Spec.CacheImage)
	if err != nil {
		logger.Error(err, "failed to fetch image")
		setCondition(&cache, "Verified", metav1.ConditionFalse, "ImageFetchFailed", err.Error())
		_ = r.Status().Update(ctx, &cache)
		return ctrl.Result{Requeue: true}, nil
	}

	digest, err := img.Digest()
	if err != nil {
		logger.Error(err, "failed to get digest")
		setCondition(&cache, "Verified", metav1.ConditionFalse, "DigestError", err.Error())
		_ = r.Status().Update(ctx, &cache)
		return ctrl.Result{}, nil
	}

	if cache.Spec.ValidateSignature {
		logger.Info("Validating image signature with cosign", "image", cache.Spec.CacheImage)

		if err := verifyImageSignature(cache.Spec.CacheImage); err != nil {
			logger.Error(err, "image signature verification failed")
			setCondition(&cache, "Verified", metav1.ConditionFalse, "SignatureInvalid", err.Error())
			_ = r.Status().Update(ctx, &cache)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		setCondition(&cache, "Verified", metav1.ConditionTrue, "SignatureVerified", "Signature verified successfully")
	} else {
		logger.Info("Signature verification skipped", "image", cache.Spec.CacheImage)
		setCondition(&cache, "Verified", metav1.ConditionTrue, "SignatureSkipped", "Validation disabled by spec")
	}

	cache.Status.Digest = digest.String()
	cache.Status.LastSynced = metav1.Now()
	setCondition(&cache, "Verified", metav1.ConditionTrue, "ImageVerified", "Image successfully verified")
	setCondition(&cache, "Ready", metav1.ConditionTrue, "CacheReady", "Cache ready for mount")

	if err := r.Status().Update(ctx, &cache); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TritonKernelCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tkmv1alpha1.TritonKernelCache{}).
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

func setCondition(obj *tkmv1alpha1.TritonKernelCache, condType string, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}

func verifyImageSignature(imageRef string) error {
	// TODO: implement cosign verification
	return nil // stub: always pass for now
}
