package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	gcrremote "github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/redhat-et/GKM/pkg/utils"
)

var (
	gkmcacheLog                         = logf.Log.WithName("webhook-ns")
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

// +kubebuilder:webhook:path=/mutate-gkm-io-v1alpha1-gkmcache,mutating=true,failurePolicy=fail,sideEffects=None,groups=gkm.io,resources=gkmcaches,verbs=create;update,versions=v1alpha1,name=z-mgkmcache.kb.io,admissionReviewVersions=v1,reinvocationPolicy=Never
// +kubebuilder:webhook:path=/validate-gkm-io-v1alpha1-gkmcache,mutating=false,failurePolicy=fail,sideEffects=None,groups=gkm.io,resources=gkmcaches,verbs=create;update,versions=v1alpha1,name=z-vgkmcache.kb.io,admissionReviewVersions=v1

// Default implements the defaulting logic (mutating webhook)
func (w *GKMCache) Default(ctx context.Context, obj runtime.Object) error {
	gkmcacheLog.V(1).Info("Mutating Webhook called", "object", obj)

	cache, ok := obj.(*GKMCache)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected GKMCache, got %T", obj))
	}
	gkmcacheLog.V(1).Info("Decoded GKMCache object", "name", cache.Name, "namespace", cache.Namespace)

	if cache.Annotations == nil {
		cache.Annotations = map[string]string{}
	}

	if cache.Spec.Image == "" {
		gkmcacheLog.Info("spec.image is empty, skipping")
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
			gkmcacheLog.Info("Image already contains digest (likely from Kyverno)", "image", cache.Spec.Image, "digest", extractedDigest)
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
		gkmcacheLog.V(1).Info("Resolving image digest (Kyverno verification disabled)", "image", cache.Spec.Image)
		digest, err = resolveImageDigest(cctx, cache.Spec.Image)
		if err != nil {
			gkmcacheLog.Error(err, "failed to resolve image digest")
			return apierrors.NewBadRequest(fmt.Sprintf(
				"image digest resolution failed for '%s': %s",
				cache.Spec.Image, err.Error(),
			))
		}
	}

	cache.Annotations[utils.GMKCacheAnnotationResolvedDigest] = digest

	gkmcacheLog.Info("added/updated resolvedDigest", "image", cache.Spec.Image, "digest", digest)
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

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (w *GKMCache) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	gkmcacheLog.V(1).Info("Validating Webhook called", "oldObj", oldObj, "newObj", newObj)
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

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (w *GKMCache) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	cache, ok := obj.(*GKMCache)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected GKMCache, got %T", obj))
	}

	gkmcacheLog.Info("validating GKMCache delete", "name", cache.Name)

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

// isKyvernoVerificationEnabled checks if Kyverno verification is enabled.
// It reads from the KYVERNO_VERIFICATION_ENABLED environment variable.
// Defaults to true (enabled) if not set or invalid.
func isKyvernoVerificationEnabled() bool {
	envValue := os.Getenv(utils.EnvKyvernoEnabled)
	if envValue == "" {
		// Default to enabled
		return true
	}
	// Parse the value - accept "true", "1", "yes" as enabled
	// Everything else (including "false", "0", "no") is disabled
	switch strings.ToLower(envValue) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		// Invalid value, default to enabled and log a warning
		gkmcacheLog.Info("Invalid value for KYVERNO_VERIFICATION_ENABLED, defaulting to enabled", "value", envValue)
		return true
	}
}

// resolveImageDigest resolves an image reference to its digest without verifying signatures.
// This is used when Kyverno verification is disabled (development/testing mode).
// It returns the image digest string (sha256:...) if successful.
func resolveImageDigest(ctx context.Context, imageRef string) (string, error) {
	// Parse the image reference (tag or digest).
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("parse image reference: %w", err)
	}

	// Registry access options (authn.DefaultKeychain covers most cases).
	remoteOpts := []gcrremote.Option{
		gcrremote.WithAuthFromKeychain(authn.DefaultKeychain),
		gcrremote.WithContext(ctx),
	}

	// Get the image descriptor to retrieve the digest
	descriptor, err := gcrremote.Get(ref, remoteOpts...)
	if err != nil {
		return "", fmt.Errorf("fetch image descriptor: %w", err)
	}

	return descriptor.Digest.String(), nil
}
