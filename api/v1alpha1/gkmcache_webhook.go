package v1alpha1

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
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
	"github.com/sigstore/cosign/v2/pkg/cosign"
	ociremote "github.com/sigstore/cosign/v2/pkg/oci/remote"
	rekorclient "github.com/sigstore/rekor/pkg/generated/client"

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
// The mutating webhook writes both the resolved digest and a
// gkm.io/mutationSig that’s bound to the current AdmissionRequest UID + image
// + digest. The validating webhooks only accept the digest if that signature
// is valid, which guarantees the digest came from the mutator (not the user).
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

	// Resolve & verify image -> digest
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gkmcachelog.V(1).Info("Verifying image signature", "image", cache.Spec.Image)
	digest, err := verifyImageSignature(cctx, cache.Spec.Image)
	if err != nil {
		gkmcachelog.Error(err, "failed to verify image or resolve digest")
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

// verifyImageSignature verifies that at least one attached signature for imageRef
// is valid according to Sigstore's trust roots (Fulcio, Rekor, CT/SCT).
// It returns the verified image digest string (sha256:...) if successful.
func verifyImageSignature(ctx context.Context, imageRef string) (string, error) {
	// Parse the image reference (tag or digest).
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("parse image reference: %w", err)
	}

	// Rekor public instance client (HTTPS).
	rc := rekorclient.NewHTTPClientWithConfig(nil,
		rekorclient.DefaultTransportConfig().
			WithHost("rekor.sigstore.dev").
			WithBasePath("/").
			WithSchemes([]string{"https"}),
	)

	// Load Sigstore trust material (Fulcio, CT, Rekor) via TUF:
	trusted, err := cosign.TrustedRoot()
	if err != nil {
		return "", fmt.Errorf("load Sigstore trust roots: %w", err)
	}

	// Registry access options (authn.DefaultKeychain covers most cases).
	regOpts := []ociremote.Option{
		ociremote.WithRemoteOptions(
			gcrremote.WithAuthFromKeychain(authn.DefaultKeychain),
		),
	}

	// Pull the signed entity and its signatures.
	se, err := ociremote.SignedEntity(ref, regOpts...)
	if err != nil {
		return "", fmt.Errorf("fetch signed entity: %w", err)
	}

	h, err := se.Digest() // v1.Hash used by VerifyImageSignature
	if err != nil {
		return "", fmt.Errorf("compute digest: %w", err)
	}
	digest := h.String()

	sigs, err := se.Signatures()
	if err != nil {
		return "", fmt.Errorf("get signatures: %w", err)
	}

	list, err := sigs.Get()
	if err != nil {
		return "", fmt.Errorf("list signatures: %w", err)
	}
	if len(list) == 0 {
		return "", fmt.Errorf("no signatures found for %s", ref.Name())
	}

	// ---- Keyless/OIDC verification options ----
	co := &cosign.CheckOpts{
		RegistryClientOpts: regOpts,
		RekorClient:        rc,
		TrustedMaterial:    trusted,
		// ClaimVerifier verifies payload claims (incl. digest), and lets you
		// optionally enforce annotations if you set co.Annotations.
		ClaimVerifier: cosign.SimpleClaimVerifier,

		// (TODO) CONSTRAIN IDENTITY for GitHub Actions OIDC:
		// If you know the expected issuer/subject, set them here to restrict
		// which certificates are accepted. Otherwise, else leave this slice empty
		// to accept any trusted Fulcio-issued identity.
		//
		// For GitHub Actions:
		// Identities: []cosign.Identity{{
		// 	Issuer:  "https://token.actions.githubusercontent.com",
		// 	Subject: "https://github.com/OWNER/REPO/.github/workflows/WORKFLOW.yml@refs/heads/main",
		// }},
	}

	var verifyErrs []error
	for _, sig := range list {
		if _, err := cosign.VerifyImageSignature(ctx, sig, h, co); err != nil {
			verifyErrs = append(verifyErrs, err)
			continue
		}
		// First successful verification is enough.
		return digest, nil
	}

	// If we got here, nothing verified.
	if len(verifyErrs) > 0 {
		return "", fmt.Errorf("no valid signatures; last error: %w", verifyErrs[len(verifyErrs)-1])
	}
	return "", errors.New("no valid signatures")
}

func mutationKeyFromEnv() (string, error) {
	k := os.Getenv("MUTATION_SIGNING_KEY")
	if k == "" {
		return "", fmt.Errorf("MUTATION_SIGNING_KEY env var not set")
	}
	return k, nil
}

// HMAC(secret, requestUID|image|digest), base64-encoded
func signMutation(secret, requestUID, image, digest string) (string, error) {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(requestUID))
	mac.Write([]byte("|"))
	mac.Write([]byte(image))
	mac.Write([]byte("|"))
	mac.Write([]byte(digest))
	sum := mac.Sum(nil)
	return base64.StdEncoding.EncodeToString(sum), nil
}

func verifyMutation(secret, requestUID, image, digest, sigB64 string) bool {
	if sigB64 == "" {
		return false
	}
	wantSig, _ := signMutation(secret, requestUID, image, digest)
	want, _ := base64.StdEncoding.DecodeString(wantSig)
	got, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	return hmac.Equal(want, got)
}
