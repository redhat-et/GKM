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
// The mutating webhook writes both the resolved digest and a
// gkm.io/mutationSig that’s bound to the current AdmissionRequest UID + image
// + digest. The validating webhooks only accept the digest if that signature
// is valid, which guarantees the digest came from the mutator (not the user).
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

	clustergkmcacheLog.V(1).Info("Verifying image signature", "image", cache.Spec.Image)
	digest, err := verifyImageSignature(cctx, cache.Spec.Image)
	if err != nil {
		clustergkmcacheLog.Error(err, "failed to verify image or resolve digest")
		return apierrors.NewBadRequest(fmt.Sprintf(
			"image signature verification failed for '%s': %s",
			cache.Spec.Image, err.Error(),
		))
	}
	resolvedDigest, digestFound := cache.Annotations[utils.GMKCacheAnnotationResolvedDigest]
	if digestFound {
		// Digest hasn't changed so just return
		if digest == resolvedDigest {
			return nil
		}
	}
	cache.Annotations[utils.GMKCacheAnnotationResolvedDigest] = digest

	// Bind a mutation signature to THIS AdmissionRequest UID
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest("unable to read admission request from context")
	}
	secret, err := mutationKeyFromEnv()
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}
	sig, err := signMutation(secret, "", cache.Spec.Image, digest)
	if err != nil {
		return apierrors.NewBadRequest(fmt.Sprintf("failed to sign mutation: %v", err))
	}
	cache.Annotations[utils.GMKCacheAnnotationMutationSig] = sig

	// Audit for convenience (not part of trust)
	cache.Annotations[utils.GMKCacheAnnotationLastMutatedBy] = req.UserInfo.Username

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

	// The validator sees the mutated object.
	// If resolvedDigest is present, it must carry a valid mutationSig for THIS request.
	digest := cache.Annotations[utils.GMKCacheAnnotationResolvedDigest]
	sig := cache.Annotations[utils.GMKCacheAnnotationMutationSig]

	if digest != "" {
		secret, err := mutationKeyFromEnv()
		if err != nil {
			return nil, fmt.Errorf("%s", err.Error())
		}
		if !verifyMutation(secret, "", cache.Spec.Image, digest, sig) {
			return nil, fmt.Errorf("%s present but missing/invalid %s; digest must be set only by the mutating webhook",
				utils.GMKCacheAnnotationResolvedDigest, utils.GMKCacheAnnotationMutationSig)
		}
	}

	// Defense in depth
	// Recompute digest from the image (same logic used by mutator).
	// The mutator adds the gkm.io/resolvedDigest annotation
	// If we just check it exists then the validator will fail.
	// We just recompute the digest and compare it. If it's OK
	// we accept the CR object.
	digest, err := verifyImageSignature(ctx, cache.Spec.Image)
	if err != nil {
		return nil, fmt.Errorf("image signature verification failed: %w", err)
	}

	ann := cache.Annotations["gkm.io/resolvedDigest"]
	if ann == "" || ann != digest {
		return nil, fmt.Errorf("gkm.io/resolvedDigest mismatch - this is not the digest of the verified image")
	}

	return nil, nil
}

// ValidateUpdate implements validation for update events.
func (w *ClusterGKMCache) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	gkmcachelog.Info("Validating Webhook called", "oldObj", oldObj, "newObj", newObj)
	oldCache, ok1 := oldObj.(*ClusterGKMCache)
	newCache, ok2 := newObj.(*ClusterGKMCache)
	if !ok1 || !ok2 {
		return nil, apierrors.NewBadRequest("type assertion to ClusterGKMCache failed")
	}

	oldImg := oldCache.Spec.Image
	newImg := newCache.Spec.Image

	oldDigest := oldCache.Annotations[utils.GMKCacheAnnotationResolvedDigest]
	newDigest := newCache.Annotations[utils.GMKCacheAnnotationResolvedDigest]
	newSig := newCache.Annotations[utils.GMKCacheAnnotationMutationSig]

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
	if newDigest == "" || newSig == "" {
		return nil, fmt.Errorf("%s must be set by mutating webhook when spec.image changes", utils.GMKCacheAnnotationResolvedDigest)
	}

	secret, err := mutationKeyFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%s", err.Error())
	}
	if !verifyMutation(secret, "", newImg, newDigest, newSig) {
		return nil, fmt.Errorf("invalid %s for updated image; digest must be set only by the mutating webhook", utils.GMKCacheAnnotationMutationSig)
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
