package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/redhat-et/GKM/pkg/utils"
)

var (
	gkmcachelog                         = logf.Log.WithName("webhook-ns")
	_           webhook.CustomDefaulter = &GKMCache{}
	_           webhook.CustomValidator = &GKMCache{}
)

type GKMCacheWebhook struct{}

// SetupWebhookWithManager sets up the webhook with the controller-runtime manager
func (w *GKMCache) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&GKMCache{}).
		WithDefaulter(w, admission.DefaulterRemoveUnknownOrOmitableFields).
		WithValidator(w).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-gkm-io-v1alpha1-gkmcache,mutating=true,failurePolicy=fail,sideEffects=None,groups=gkm.io,resources=gkmcaches,verbs=create;update,versions=v1alpha1,name=mgkmcache.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-gkm-io-v1alpha1-gkmcache,mutating=false,failurePolicy=fail,sideEffects=None,groups=gkm.io,resources=gkmcaches,verbs=create;update,versions=v1alpha1,name=vgkmcache.kb.io,admissionReviewVersions=v1

// Default implements the defaulting logic (mutating webhook)
func (w *GKMCache) Default(ctx context.Context, obj runtime.Object) error {
	gkmcachelog.V(1).Info("Mutating Webhook called", "object", obj)

	cache, ok := obj.(*GKMCache)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected GKMCache, got %T", obj))
	}
	gkmcachelog.V(1).Info("Decoded GKMCache object", "name", cache.Name, "namespace", cache.Namespace)

	if cache.Annotations == nil {
		cache.Annotations = map[string]string{}
	}

	if cache.Spec.Image == "" {
		gkmcachelog.Info("spec.image is empty, skipping")
		return nil
	}

	// First check if the image already contains a digest (e.g., from Kyverno mutation)
	var digest string
	if extractedDigest := extractDigestFromImage(cache.Spec.Image); extractedDigest != "" {
		gkmcachelog.Info("Image already contains digest (likely from Kyverno)", "image", cache.Spec.Image, "digest", extractedDigest)
		digest = extractedDigest
	}
	resolvedDigest, digestFound := cache.Annotations[utils.GMKCacheAnnotationResolvedDigest]
	if digestFound {
		// Digest hasn't changed so just return
		if digest == resolvedDigest {
			return nil
		}
	}
	cache.Annotations[utils.GMKCacheAnnotationResolvedDigest] = digest

	gkmcachelog.Info("added/updated resolvedDigest", "image", cache.Spec.Image, "digest", digest)
	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (w *GKMCache) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cache, ok := obj.(*GKMCache)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected GKMCache, got %T", obj))
	}

	if cache.Spec.Image == "" {
		return nil, fmt.Errorf("spec.image must be set")
	}

	if _, exists := cache.Annotations[utils.GMKCacheAnnotationResolvedDigest]; !exists {
		return nil, fmt.Errorf("%s must be set by mutating webhook", utils.GMKCacheAnnotationResolvedDigest)
	}

	if _, exists := cache.Annotations[utils.KyvernoVerifyImagesAnnotation]; !exists {
		return nil, fmt.Errorf("%s must be set by kyverno", utils.KyvernoVerifyImagesAnnotation)
	}

	// Check Kyverno verification status if present
	if err := verifyKyvernoAnnotation(cache.Annotations); err != nil {
		return nil, fmt.Errorf("kyverno verification failed: %w", err)
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (w *GKMCache) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	gkmcachelog.V(1).Info("Validating Webhook called", "oldObj", oldObj, "newObj", newObj)
	oldCache, ok1 := oldObj.(*GKMCache)
	newCache, ok2 := newObj.(*GKMCache)
	if !ok1 || !ok2 {
		return nil, apierrors.NewBadRequest("type assertion to GKMCache failed")
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

	// Image DID change -> the new digest must be present THIS request.
	if newImg == "" {
		return nil, fmt.Errorf("spec.image must be set")
	}
	if newDigest == "" {
		return nil, fmt.Errorf("%s must be set by mutating webhook when spec.image changes", utils.GMKCacheAnnotationResolvedDigest)
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (w *GKMCache) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cache, ok := obj.(*GKMCache)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected GKMCache, got %T", obj))
	}

	clustergkmcacheLog.Info("validating GKMCache delete", "name", cache.Name)

	// Add delete validation logic here if needed.
	return nil, nil
}

// extractDigestFromImage extracts the digest from an image reference if it contains one.
// Returns empty string if the image reference doesn't contain a digest.
// Example: "quay.io/repo/image:tag@sha256:abc123" -> "sha256:abc123"
func extractDigestFromImage(imageRef string) string {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return ""
	}

	// Identifier() returns the digest if present, otherwise the tag
	identifier := ref.Identifier()
	// Check if it's actually a digest (starts with sha256:)
	if len(identifier) > 7 && identifier[:7] == "sha256:" {
		return identifier
	}

	return ""
}

// verifyKyvernoAnnotation checks the kyverno.io/verify-images annotation to ensure
// the image signature was verified by Kyverno and the status is "pass".
// The annotation format is: {"<image>@<digest>":"pass"}
func verifyKyvernoAnnotation(annotations map[string]string) error {
	kyvernoAnnotation, exists := annotations["kyverno.io/verify-images"]
	if !exists {
		return fmt.Errorf("failed to find kyverno.io/verify-images annotation")
	}

	// Parse the JSON annotation
	var verifications map[string]string
	if err := json.Unmarshal([]byte(kyvernoAnnotation), &verifications); err != nil {
		return fmt.Errorf("failed to parse kyverno.io/verify-images annotation: %w", err)
	}

	// Check if any entry has status "pass" and matches our digest
	for _, status := range verifications {
		if status != "pass" {
			return fmt.Errorf("kyverno verification status is not 'pass': %s", status)
		}
	}

	return nil
}
