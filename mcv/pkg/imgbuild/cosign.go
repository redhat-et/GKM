package imgbuild

import (
	"context"
	"crypto"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/sign"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/verify"
	"github.com/sigstore/cosign/v3/pkg/cosign"
	logging "github.com/sirupsen/logrus"
	"go.podman.io/storage"

	// Register ambient OIDC providers (GitHub Actions, SIGSTORE_ID_TOKEN, etc.)
	// so SignCmd/Fulcio can discover them like the cosign CLI.
	_ "github.com/sigstore/cosign/v3/pkg/providers/all"
)

func remoteAuthOptions(imageRef string) ([]remote.Option, error) {
	authConfig, err := registryAuthConfig(imageRef)
	if err != nil {
		return nil, err
	}
	if authConfig != nil {
		return []remote.Option{
			remote.WithAuth(authn.FromConfig(toAuthnAuthConfig(authConfig))),
		}, nil
	}

	return []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}, nil
}

// toAuthnAuthConfig maps Docker/containers registry credentials into the
// go-containerregistry AuthConfig used by remote and cosign.
func toAuthnAuthConfig(authConfig *registry.AuthConfig) authn.AuthConfig {
	return authn.AuthConfig{
		Username:      authConfig.Username,
		Password:      authConfig.Password,
		Auth:          authConfig.Auth,
		IdentityToken: authConfig.IdentityToken,
		RegistryToken: authConfig.RegistryToken,
	}
}

// fixedAuthKeychain always returns the same authenticator. Cosign Sign/Verify
// use it so private-registry auth matches push/pull and ResolveRegistryDigest
// (Docker config + containers/auth.json), including identity tokens that
// RegistryOptions.AuthConfig cannot express.
type fixedAuthKeychain struct {
	auth authn.Authenticator
}

func (k fixedAuthKeychain) Resolve(_ authn.Resource) (authn.Authenticator, error) {
	return k.auth, nil
}

// cosignRegistryOptions builds cosign RegistryOptions with the same credential
// resolution as push/pull. Empty Keychain leaves cosign on DefaultKeychain.
func cosignRegistryOptions(imageRef string) (options.RegistryOptions, error) {
	ro := options.RegistryOptions{}
	authConfig, err := registryAuthConfig(imageRef)
	if err != nil {
		return ro, err
	}
	if authConfig == nil {
		return ro, nil
	}
	ro.Keychain = fixedAuthKeychain{auth: authn.FromConfig(toAuthnAuthConfig(authConfig))}
	return ro, nil
}

const (
	// DefaultSignTimeout is the timeout for cosign sign (OIDC + upload + tlog).
	DefaultSignTimeout = 3 * time.Minute
	// DefaultVerifyTimeout is the timeout for cosign verify (registry + tlog).
	DefaultVerifyTimeout = 2 * time.Minute
)

// keyPassFunc matches cosign CLI password resolution: COSIGN_PASSWORD first,
// then a terminal prompt when available, otherwise an empty password (for
// passwordless keys in non-interactive environments).
func keyPassFunc(confirm bool) ([]byte, error) {
	if pw, ok := os.LookupEnv("COSIGN_PASSWORD"); ok {
		return []byte(pw), nil
	}
	if cosign.IsTerminal() {
		return cosign.GetPassFromTerm(confirm)
	}
	return []byte{}, nil
}

// Sign signs an OCI image in the registry using cosign, matching the cosign v3
// CLI: one SignCmd with NewBundleFormat (OCI referrers / Sigstore bundle).
// There is no automatic fallback to legacy .sig tags; use the cosign CLI with
// --new-bundle-format=false if legacy tags are required. mcv verify still
// accepts both formats (probe bundles, then legacy), like cosign verify.
//
// Keyless auth is left to SignCmd/Fulcio (ambient providers, then interactive
// or device OIDC).
//
// If keyPath is empty, uses keyless signing (Sigstore OIDC / Fulcio).
// If keyPath is provided, uses key-based signing with the specified private key.
// If yesFlag is true, automatically accepts cosign agreements without prompting.
func Sign(imageRef, keyPath string, yesFlag bool) error {
	if imageRef == "" {
		return fmt.Errorf("image reference is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultSignTimeout)
	defer cancel()

	ro := &options.RootOptions{
		Timeout: DefaultSignTimeout,
	}

	regOpts, err := cosignRegistryOptions(imageRef)
	if err != nil {
		return fmt.Errorf("failed to resolve registry credentials: %w", err)
	}

	// Match cosign v3 CLI default (--new-bundle-format=true).
	signOpts := options.SignOptions{
		Upload:          true,
		TlogUpload:      true,
		NewBundleFormat: true,
		Registry:        regOpts,
	}

	var ko options.KeyOpts
	var signingMethod string

	if keyPath != "" {
		signingMethod = "key-based"
		ko = options.KeyOpts{
			KeyRef:           keyPath,
			PassFunc:         keyPassFunc,
			RekorURL:         options.DefaultRekorURL,
			SkipConfirmation: yesFlag,
		}
	} else {
		signingMethod = "keyless"
		ko = options.KeyOpts{
			FulcioURL:        options.DefaultFulcioURL,
			RekorURL:         options.DefaultRekorURL,
			OIDCIssuer:       options.DefaultOIDCIssuerURL,
			OIDCClientID:     "sigstore",
			SkipConfirmation: yesFlag,
		}
	}

	logging.Infof("Using cosign v3 to sign the image in registry with %s/bundle signing: %s", signingMethod, imageRef)

	if err := sign.SignCmd(ctx, ro, ko, signOpts, []string{imageRef}); err != nil {
		return fmt.Errorf("cosign %s bundle sign failed: %w", signingMethod, err)
	}

	logging.Infof("Successfully signed image (%s/bundle): %s", signingMethod, imageRef)
	return nil
}

// ResolveRegistryDigest resolves an image reference (which may be a mutable tag) to an
// immutable digest reference in the remote registry. Used for verify/pull
// TOCTOU pinning — the image must already exist in the registry.
func ResolveRegistryDigest(imageRef string) (string, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	authOpts, err := remoteAuthOptions(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to resolve registry credentials: %w", err)
	}

	desc, err := remote.Head(ref, authOpts...)
	if err != nil {
		return "", wrapRegistryHeadError(imageRef, err)
	}

	digestRef := ref.Context().Digest(desc.Digest.String())
	return digestRef.String(), nil
}

// wrapRegistryHeadError maps common registry HEAD failures to clearer messages.
// Classification is heuristic (substring matching on err.Error()); treat wrapped
// messages as hints, not a complete taxonomy of registry transport errors.
func wrapRegistryHeadError(imageRef string, err error) error {
	if err == nil {
		return nil
	}
	errLower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errLower, "404"),
		strings.Contains(errLower, "not found"),
		strings.Contains(errLower, "manifest unknown"),
		strings.Contains(errLower, "name unknown"):
		return fmt.Errorf("image %q not found in registry: %w", imageRef, err)
	case strings.Contains(errLower, "401"),
		strings.Contains(errLower, "403"),
		strings.Contains(errLower, "unauthorized"),
		strings.Contains(errLower, "denied"):
		return fmt.Errorf("cannot access image %q in registry (check credentials): %w", imageRef, err)
	default:
		return fmt.Errorf("failed to resolve image digest: %w", err)
	}
}

// ResolveDigest resolves an image to a digest for signing, matching
// cosign CLI behavior:
//   - Digest references are used as-is even if the image is not in the registry
//     (cosign SignedUnknown path — signature tags/bundles can still be uploaded).
//   - Tag references try the registry first, then fall back to a local
//     containers-storage / Docker manifest digest.
func ResolveDigest(imageRef string) (string, error) {
	normalized := NormalizeImageTag(imageRef)
	ref, err := name.ParseReference(normalized)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	if dig, ok := ref.(name.Digest); ok {
		logging.Debugf("Using provided digest for signing (cosign allows signing digests not yet in registry): %s", dig)
		return dig.String(), nil
	}

	remoteDig, err := ResolveRegistryDigest(normalized)
	if err == nil {
		return remoteDig, nil
	}
	logging.Debugf("registry digest resolve failed, trying local image: %v", err)

	localDig, err := resolveLocalImageDigest(ref)
	if err != nil {
		return "", fmt.Errorf("image %q not found in registry or local storage (create/push the image first): %w", imageRef, err)
	}
	logging.Warnf("Signing local digest %s (image not found in registry). "+
		"cosign tree / verify against the registry require the image to be pushed first; "+
		"prefer: mcv --push --sign --image %s", localDig, normalized)
	return localDig, nil
}

// resolveLocalImageDigest returns repo@sha256:… for an image present in local
// containers storage (podman/buildah) or the Docker daemon. Prefers the
// manifest digest, never the image config ID.
func resolveLocalImageDigest(ref name.Reference) (string, error) {
	var errs []error

	dig, err := resolveLocalDigestFromContainersStorage(ref)
	if err == nil {
		return dig, nil
	}
	errs = append(errs, err)

	dig, err = resolveLocalDigestFromDocker(ref)
	if err == nil {
		return dig, nil
	}
	errs = append(errs, err)
	return "", fmt.Errorf("local digest lookup failed: %v", errs)
}

func resolveLocalDigestFromContainersStorage(ref name.Reference) (string, error) {
	storeOpts, err := storage.DefaultStoreOptions()
	if err != nil {
		return "", err
	}
	store, err := storage.GetStore(storeOpts)
	if err != nil {
		return "", err
	}
	defer shutdownStore(store)

	img, err := store.Image(ref.String())
	if err != nil {
		// Try normalized tag form (name.Tag vs bare name).
		img, err = store.Image(NormalizeImageTag(ref.String()))
		if err != nil {
			return "", err
		}
	}

	dgst, err := manifestDigestFromStorageImage(img)
	if err != nil {
		return "", err
	}
	return ref.Context().Digest(dgst).String(), nil
}

// manifestDigestFromStorageImage returns a manifest digest, never the image
// config ID. containers/storage may list the image ID in Digests/Digest.
func manifestDigestFromStorageImage(img *storage.Image) (string, error) {
	if img == nil {
		return "", fmt.Errorf("image is nil")
	}
	idHex := strings.TrimPrefix(img.ID, "sha256:")
	isImageID := func(d string) bool {
		if d == "" {
			return true
		}
		hex := strings.TrimPrefix(d, "sha256:")
		return hex == idHex || d == img.ID
	}

	for _, d := range img.Digests {
		s := d.String()
		if !isImageID(s) {
			return s, nil
		}
	}
	if s := img.Digest.String(); !isImageID(s) {
		return s, nil
	}
	return "", fmt.Errorf("local image %q has no manifest digest (only image id %s)", img.ID, img.ID)
}

func resolveLocalDigestFromDocker(ref name.Reference) (string, error) {
	ctx := context.Background()
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", err
	}
	defer apiClient.Close()

	inspect, err := apiClient.ImageInspect(ctx, ref.String())
	if err != nil {
		inspect, err = apiClient.ImageInspect(ctx, NormalizeImageTag(ref.String()))
		if err != nil {
			return "", err
		}
	}

	repo := ref.Context().Name()
	return selectDockerRepoDigest(ref.String(), repo, inspect.RepoDigests)
}

// selectDockerRepoDigest returns a repo@digest entry matching the requested
// repository. RepoDigests from other registries are ignored to avoid signing
// the wrong manifest when an image was previously pushed elsewhere.
func selectDockerRepoDigest(imageRef, repo string, repoDigests []string) (string, error) {
	for _, rd := range repoDigests {
		if strings.HasPrefix(rd, repo+"@") {
			return rd, nil
		}
	}
	if len(repoDigests) == 0 {
		return "", fmt.Errorf("docker image %q has no RepoDigests", imageRef)
	}
	return "", fmt.Errorf(
		"docker image %q has no RepoDigests for repository %q (found %d for other repositories)",
		imageRef, repo, len(repoDigests),
	)
}

// VerifyOptions configures image signature verification.
type VerifyOptions struct {
	// KeyPath is a public key reference for key-based verification.
	// Empty enables keyless (Sigstore) verification.
	KeyPath string
	// CertIdentity is an optional expected certificate identity subject (keyless only).
	// Cosign flag: --certificate-identity
	// When empty (with CertIdentityRegexp also empty), any Fulcio identity is accepted.
	CertIdentity string
	// CertIdentityRegexp is an optional regexp for the certificate identity (keyless only).
	// Cosign flag: --certificate-identity-regexp
	CertIdentityRegexp string
	// CertOidcIssuer is an optional expected OIDC issuer (keyless only).
	// Cosign flag: --certificate-oidc-issuer
	CertOidcIssuer string
	// CertOidcIssuerRegexp is an optional regexp for the OIDC issuer (keyless only).
	// Cosign flag: --certificate-oidc-issuer-regexp
	CertOidcIssuerRegexp string
	// IgnoreTlog skips transparency-log verification (insecure).
	// Cosign flag: --insecure-ignore-tlog
	// Applies to both keyless and key-based verification.
	IgnoreTlog bool
}

// Verify verifies an OCI image signature in the registry.
// Uses cosign's NewBundleFormat path (CLI default), which verifies OCI Sigstore
// bundles when present and automatically falls back to legacy .sig tags.
//
// If KeyPath is empty, uses keyless verification (Sigstore with transparency log).
// When no certificate identity flags are set, any valid Fulcio-issued identity
// is accepted (convenient for local testing; constrain identity for production).
// If KeyPath is provided, uses key-based verification with the specified public key.
func Verify(imageRef string, opts VerifyOptions) error {
	if imageRef == "" {
		return fmt.Errorf("image reference is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultVerifyTimeout)
	defer cancel()

	certOpts := options.CertVerifyOptions{}
	if opts.KeyPath == "" {
		if opts.CertIdentity != "" && opts.CertIdentityRegexp != "" {
			return fmt.Errorf("--certificate-identity and --certificate-identity-regexp are mutually exclusive")
		}
		if opts.CertOidcIssuer != "" && opts.CertOidcIssuerRegexp != "" {
			return fmt.Errorf("--certificate-oidc-issuer and --certificate-oidc-issuer-regexp are mutually exclusive")
		}

		hasIdentity := opts.CertIdentity != "" || opts.CertIdentityRegexp != ""
		hasIssuer := opts.CertOidcIssuer != "" || opts.CertOidcIssuerRegexp != ""
		if hasIdentity || hasIssuer {
			if !hasIdentity || !hasIssuer {
				return fmt.Errorf("keyless verification with identity constraints requires (--certificate-identity or --certificate-identity-regexp) and (--certificate-oidc-issuer or --certificate-oidc-issuer-regexp)")
			}
			certOpts.CertIdentity = opts.CertIdentity
			certOpts.CertIdentityRegexp = opts.CertIdentityRegexp
			certOpts.CertOidcIssuer = opts.CertOidcIssuer
			certOpts.CertOidcIssuerRegexp = opts.CertOidcIssuerRegexp
		} else {
			// Default keyless path: accept any valid Fulcio-issued identity.
			// Cosign requires identity matchers; .* / .* is an explicit open policy.
			// Prefer --certificate-identity(-regexp) + issuer flags in production.
			logging.Warn("Keyless verify with no identity constraints: accepting any valid Fulcio-issued certificate (set --certificate-identity/--certificate-oidc-issuer for production)")
			certOpts.CertIdentityRegexp = ".*"
			certOpts.CertOidcIssuerRegexp = ".*"
		}
	}

	regOpts, err := cosignRegistryOptions(imageRef)
	if err != nil {
		return fmt.Errorf("failed to resolve registry credentials: %w", err)
	}

	verifyCmd := &verify.VerifyCommand{
		RegistryOptions:   regOpts,
		CertVerifyOptions: certOpts,
		CheckClaims:       true,
		HashAlgorithm:     crypto.SHA256,
		MaxWorkers:        10,
		RekorURL:          options.DefaultRekorURL,
		LocalImage:        false,
		// Cosign CLI default: try OCI bundles, then fall back to legacy .sig tags.
		NewBundleFormat: true,
	}

	var verifyMethod string
	if opts.KeyPath != "" {
		verifyMethod = "key-based"
		verifyCmd.KeyRef = opts.KeyPath
	} else {
		verifyMethod = "keyless"
	}
	verifyCmd.IgnoreTlog = opts.IgnoreTlog
	if opts.IgnoreTlog {
		logging.Warnf("%s verification with --insecure-ignore-tlog: transparency log will not be checked", verifyMethod)
	}

	logging.Infof("Using cosign v3 to verify the image in registry with %s verification: %s", verifyMethod, imageRef)

	if err := verifyCmd.Exec(ctx, []string{imageRef}); err != nil {
		return fmt.Errorf("cosign %s verify failed: %w", verifyMethod, err)
	}

	logging.Infof("Successfully verified image signature: %s", imageRef)
	return nil
}
