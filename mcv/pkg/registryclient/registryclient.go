package registryclient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/credentials"
	"github.com/docker/docker/api/types/registry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	logging "github.com/sirupsen/logrus"
	containersauth "go.podman.io/image/v5/pkg/docker/config"
)

// AuthConfig resolves registry credentials for imageRef from:
//  1. Docker CLI config (for parity with Docker Engine behavior), then
//  2. containers/image default auth sources (e.g., containers/auth.json) when
//     DOCKER_CONFIG is not set.
//
// When DOCKER_CONFIG is explicitly set, only that Docker config is consulted so
// callers can isolate auth (tests) or pin a config directory without merging
// host containers credentials. An empty result then lets the daemon / Buildah
// fall back to their own defaults.
func AuthConfig(imageRef string) (*registry.AuthConfig, error) {
	authConfig, err := dockerCLIAuthConfig(imageRef)
	if err != nil {
		return nil, err
	}
	if authConfig != nil {
		return authConfig, nil
	}
	if os.Getenv(config.EnvOverrideConfigDir) != "" {
		return nil, nil
	}

	return containersAuthConfig(imageRef)
}

// dockerConfigDir returns the Docker CLI config directory, honoring DOCKER_CONFIG
// on every call. This avoids github.com/docker/cli's process-wide sync.Once cache
// in config.Dir(), which ignores later DOCKER_CONFIG changes (e.g. in tests).
func dockerConfigDir() string {
	if dir := os.Getenv(config.EnvOverrideConfigDir); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".docker")
}

// dockerCLIAuthConfig loads Docker CLI credentials for the registry hosting imageRef.
// Returns nil when no usable credentials are found so callers can fall back to
// daemon / containers-auth defaults.
func dockerCLIAuthConfig(imageRef string) (*registry.AuthConfig, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference for auth: %w", err)
	}

	cfg, err := config.Load(dockerConfigDir())
	if err != nil {
		logging.Debugf("failed to load docker config: %v", err)
		return nil, nil
	}
	if !cfg.ContainsAuth() {
		cfg.CredentialsStore = credentials.DetectDefaultStore(cfg.CredentialsStore)
	}

	authConfig, err := cfg.GetAuthConfig(ref.Context().RegistryStr())
	if err != nil {
		logging.Debugf("no docker credentials for %s: %v", ref.Context().RegistryStr(), err)
		return nil, nil
	}

	if authConfig.Username == "" && authConfig.Password == "" &&
		authConfig.Auth == "" && authConfig.IdentityToken == "" && authConfig.RegistryToken == "" {
		return nil, nil
	}

	return &registry.AuthConfig{
		Username:      authConfig.Username,
		Password:      authConfig.Password,
		Auth:          authConfig.Auth,
		IdentityToken: authConfig.IdentityToken,
		RegistryToken: authConfig.RegistryToken,
		ServerAddress: authConfig.ServerAddress,
	}, nil
}

func containersAuthConfig(imageRef string) (*registry.AuthConfig, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference for auth: %w", err)
	}
	registryStr := ref.Context().RegistryStr()

	creds, err := containersauth.GetCredentials(nil, registryStr)
	if err != nil {
		if errors.Is(err, containersauth.ErrNotLoggedIn) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read containers credentials for %s: %w", registryStr, err)
	}
	if creds.Username == "" && creds.Password == "" && creds.IdentityToken == "" {
		return nil, nil
	}

	return &registry.AuthConfig{
		Username:      creds.Username,
		Password:      creds.Password,
		IdentityToken: creds.IdentityToken,
		ServerAddress: registryStr,
	}, nil
}

// toAuthnAuthConfig maps Docker/containers registry credentials into the
// go-containerregistry AuthConfig used by remote.
func toAuthnAuthConfig(authConfig *registry.AuthConfig) authn.AuthConfig {
	return authn.AuthConfig{
		Username:      authConfig.Username,
		Password:      authConfig.Password,
		Auth:          authConfig.Auth,
		IdentityToken: authConfig.IdentityToken,
		RegistryToken: authConfig.RegistryToken,
	}
}

// RemoteAuthOptions resolves go-containerregistry remote options with the same
// credential resolution as push/pull (Docker config + containers/auth.json).
func RemoteAuthOptions(imageRef string) ([]remote.Option, error) {
	authConfig, err := AuthConfig(imageRef)
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

// ResolveRegistryDigest resolves an image reference (which may be a mutable tag) to an
// immutable digest reference in the remote registry. Used for verify/pull
// TOCTOU pinning — the image must already exist in the registry.
func ResolveRegistryDigest(imageRef string) (string, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	authOpts, err := RemoteAuthOptions(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to resolve registry credentials: %w", err)
	}

	desc, err := remote.Head(ref, authOpts...)
	if err != nil {
		return "", WrapRegistryHeadError(imageRef, err)
	}

	digestRef := ref.Context().Digest(desc.Digest.String())
	return digestRef.String(), nil
}

// WrapRegistryHeadError maps common registry HEAD failures to clearer messages.
// Classification is heuristic (substring matching on err.Error()); treat wrapped
// messages as hints, not a complete taxonomy of registry transport errors.
func WrapRegistryHeadError(imageRef string, err error) error {
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
