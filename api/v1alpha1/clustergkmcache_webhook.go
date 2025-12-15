package v1alpha1

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/redhat-et/GKM/pkg/utils"
)

var (
	clustergkmcacheLog                         = logf.Log.WithName("webhook-cl")
	_                  webhook.CustomValidator = &ClusterGKMCache{}
	_                  webhook.CustomDefaulter = &ClusterGKMCache{}
)

type ClusterGKMCacheWebhook struct{}

// SetupWebhookWithManager registers the webhook with the controller manager.
func (w *ClusterGKMCache) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&ClusterGKMCache{}).
		WithDefaulter(w, admission.DefaulterRemoveUnknownOrOmitableFields).
		WithValidator(w).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-gkm-io-v1alpha1-clustergkmcache,mutating=true,failurePolicy=fail,sideEffects=None,groups=gkm.io,resources=clustergkmcaches,verbs=create;update,versions=v1alpha1,name=mclustergkmcache.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-gkm-io-v1alpha1-clustergkmcache,mutating=false,failurePolicy=fail,sideEffects=None,groups=gkm.io,resources=clustergkmcaches,verbs=create;update,versions=v1alpha1,name=vclustergkmcache.kb.io,admissionReviewVersions=v1

// Default implements the mutating webhook logic for defaulting.
func (w *ClusterGKMCache) Default(ctx context.Context, obj runtime.Object) error {
	clustergkmcacheLog.V(1).Info("Mutating Webhook called", "object", obj)

	cache, ok := obj.(*ClusterGKMCache)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected ClusterGKMCache, got %T", obj))
	}
	clustergkmcacheLog.V(1).Info("Decoded ClusterGKMCache object", "name", cache.Name)

	if cache.Annotations == nil {
		cache.Annotations = map[string]string{}
	}

	if cache.Spec.Image == "" {
		clustergkmcacheLog.Info("spec.image is empty, skipping")
		return nil
	}

	// Resolve & verify image -> digest
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	kyvernoEnabled := isKyvernoVerificationEnabled()
	var digest string
	var err error
	if kyvernoEnabled {
		// First check if the image already contains a digest (e.g., from Kyverno mutation)
		if extractedDigest := extractDigestFromImage(cache.Spec.Image); extractedDigest != "" {
			clustergkmcacheLog.Info("Image already contains digest (likely from Kyverno)", "image", cache.Spec.Image, "digest", extractedDigest)
			digest = extractedDigest
		}
		resolvedDigest, digestFound := cache.Annotations[utils.GMKCacheAnnotationResolvedDigest]
		if digestFound && digest != "" {
			// Digest hasn't changed so just return
			if digest == resolvedDigest {
				return nil
			}
		}
	} else {
		clustergkmcacheLog.V(1).Info("Resolving image digest (Kyverno verification disabled)", "image", cache.Spec.Image)
		digest, err = resolveImageDigest(cctx, cache.Spec.Image)
		if err != nil {
			clustergkmcacheLog.Error(err, "failed to resolve image digest")
			return apierrors.NewBadRequest(fmt.Sprintf(
				"image digest resolution failed for '%s': %s",
				cache.Spec.Image, err.Error(),
			))
		}
	}
	cache.Annotations[utils.GMKCacheAnnotationResolvedDigest] = digest

	clustergkmcacheLog.Info("added/updated resolvedDigest", "image", cache.Spec.Image, "digest", digest)
	return nil
}

// ValidateCreate implements validation for create events.
func (w *ClusterGKMCache) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cache, ok := obj.(*ClusterGKMCache)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected ClusterGKMCache, got %T", obj))
	}

	if cache.Spec.Image == "" {
		return nil, fmt.Errorf("spec.image must be set")
	}

	if _, exists := cache.Annotations[utils.GMKCacheAnnotationResolvedDigest]; !exists {
		return nil, fmt.Errorf("%s must be set by mutating webhook", utils.GMKCacheAnnotationResolvedDigest)
	}

	if isKyvernoVerificationEnabled() {
		if _, exists := cache.Annotations[utils.KyvernoVerifyImagesAnnotation]; !exists {
			return nil, fmt.Errorf("%s must be set by kyverno", utils.KyvernoVerifyImagesAnnotation)
		}

		// Check Kyverno verification status if present
		if err := verifyKyvernoAnnotation(cache.Annotations); err != nil {
			return nil, fmt.Errorf("kyverno verification failed: %w", err)
		}
	}

	return nil, nil
}

// ValidateUpdate implements validation for update events.
func (w *ClusterGKMCache) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	clustergkmcacheLog.Info("Validating Webhook called", "oldObj", oldObj, "newObj", newObj)
	oldCache, ok1 := oldObj.(*ClusterGKMCache)
	newCache, ok2 := newObj.(*ClusterGKMCache)
	if !ok1 || !ok2 {
		return nil, apierrors.NewBadRequest("type assertion to ClusterGKMCache failed")
	}

	oldImg := oldCache.Spec.Image
	newImg := newCache.Spec.Image

	oldDigest := oldCache.Annotations[utils.GMKCacheAnnotationResolvedDigest]
	newDigest := newCache.Annotations[utils.GMKCacheAnnotationResolvedDigest]

	// If image didn't change, digest must not change.
	if oldImg == newImg {
		if oldDigest != newDigest {
			return nil, fmt.Errorf("%s is immutable when spec.image is unchanged", utils.GMKCacheAnnotationResolvedDigest)
		}
		return nil, nil
	}

	// Image DID change -> the new digest must be present and signed for THIS request.
	if newImg == "" {
		return nil, fmt.Errorf("spec.image must be set")
	}
	if newDigest == "" {
		return nil, fmt.Errorf("%s must be set by mutating webhook when spec.image changes", utils.GMKCacheAnnotationResolvedDigest)
	}

	// Validate Kyverno verification if enabled
	if isKyvernoVerificationEnabled() {
		if _, exists := newCache.Annotations[utils.KyvernoVerifyImagesAnnotation]; !exists {
			return nil, fmt.Errorf("%s must be set by kyverno", utils.KyvernoVerifyImagesAnnotation)
		}

		if err := verifyKyvernoAnnotation(newCache.Annotations); err != nil {
			return nil, fmt.Errorf("kyverno verification failed: %w", err)
		}
	}

	return nil, nil
}

// ValidateDelete implements validation for delete events.
func (w *ClusterGKMCache) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cache, ok := obj.(*ClusterGKMCache)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected ClusterGKMCache, got %T", obj))
	}

	clustergkmcacheLog.Info("validating ClusterGKMCache delete", "name", cache.Name)

	// Add delete validation logic here if needed.
	return nil, nil
}
