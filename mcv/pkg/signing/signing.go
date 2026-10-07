package signing

import (
	"context"
	"crypto"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types/registry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/redhat-et/GKM/mcv/pkg/registryclient"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/sign"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/verify"
	"github.com/sigstore/cosign/v3/pkg/cosign"
	logging "github.com/sirupsen/logrus"

	// Register ambient OIDC providers (GitHub Actions, SIGSTORE_ID_TOKEN, etc.)
	// so SignCmd/Fulcio can discover them like the cosign CLI.
	_ "github.com/sigstore/cosign/v3/pkg/providers/all"
)

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
	authConfig, err := registryclient.AuthConfig(imageRef)
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
func Verify(imageRef string, opts *VerifyOptions) error {
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
