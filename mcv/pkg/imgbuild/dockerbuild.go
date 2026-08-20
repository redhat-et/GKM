/*
Copyright Red Hat Inc.

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
package imgbuild

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	// Import hash algorithms to register them with crypto package
	_ "crypto/sha256" // Registers SHA256
	_ "crypto/sha512" // Registers SHA384 and SHA512

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/credentials"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	digest "github.com/opencontainers/go-digest"
	logging "github.com/sirupsen/logrus"
	containersauth "go.podman.io/image/v5/pkg/docker/config"
	itypes "go.podman.io/image/v5/types"
)

type dockerBuilder struct{}

// CreateImage loads a compat cache image into the Docker daemon using Docker
// Schema 2 media types throughout. BuildKit's docker build path can produce an
// OCI manifest with Docker layer types, which breaks docker save and kind load.
func (d *dockerBuilder) CreateImage(imageName, cacheDir string) error {
	prep, err := prepareBuildContext("docker", cacheDir)
	if err != nil {
		return err
	}
	defer CleanupDirs(prep.CacheBuildDir, prep.ManifestBuildDir)

	imageWithTag := NormalizeImageTag(imageName)
	tag, err := name.NewTag(imageWithTag)
	if err != nil {
		return fmt.Errorf("invalid image reference %q: %w", imageWithTag, err)
	}

	img, err := schema2ImageFromBuildContext(prep, imageName)
	if err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}
	if prep.TempLayerFile != "" {
		defer os.Remove(prep.TempLayerFile)
	}

	ctx := context.Background()
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer apiClient.Close()

	if err := loadImageIntoDocker(ctx, apiClient, tag, img); err != nil {
		return err
	}
	logging.Info("Docker image built successfully")

	if err := CleanupWithTimeout(); err != nil {
		return fmt.Errorf("cleanup error: %w", err)
	}
	return nil
}

func loadImageIntoDocker(ctx context.Context, apiClient *client.Client, tag name.Tag, img v1.Image) error {
	pr, pw := io.Pipe()
	defer pr.Close()
	go func() {
		pw.CloseWithError(tarball.Write(tag, img, pw))
	}()

	resp, err := apiClient.ImageLoad(ctx, pr)
	if err != nil {
		return fmt.Errorf("failed to load image into Docker: %w", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("failed to read Docker load response: %w", err)
	}
	return nil
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

// registryAuthConfig resolves registry credentials for imageRef from:
//  1. Docker CLI config (for parity with Docker Engine behavior), then
//  2. containers/image default auth sources (e.g., containers/auth.json) when
//     DOCKER_CONFIG is not set.
//
// When DOCKER_CONFIG is explicitly set, only that Docker config is consulted so
// callers can isolate auth (tests) or pin a config directory without merging
// host containers credentials. An empty result then lets the daemon / Buildah
// fall back to their own defaults.
func registryAuthConfig(imageRef string) (*registry.AuthConfig, error) {
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

// encodedRegistryAuth returns base64-encoded registry credentials for the Docker Engine API.
// When no usable credentials are found, it returns an empty string so the daemon can fall
// back to its own credential stores (important for Podman docker-compat + containers/auth.json).
// Encoding an empty AuthConfig as "e30=" would override that lookup with anonymous auth.
func encodedRegistryAuth(imageRef string) (string, error) {
	authConfig, err := registryAuthConfig(imageRef)
	if err != nil {
		return "", err
	}
	if authConfig == nil {
		return "", nil
	}

	encoded, err := registry.EncodeAuthConfig(*authConfig)
	if err != nil {
		return "", fmt.Errorf("failed to encode registry credentials: %w", err)
	}
	return encoded, nil
}

// buildahDockerAuthConfig maps registry.AuthConfig into containers/image's
// DockerAuthConfig. Decodes the base64 Auth field and maps RegistryToken to
// IdentityToken when needed (containers/image only supports username/password
// and identity tokens).
func buildahDockerAuthConfig(authConfig *registry.AuthConfig) *itypes.DockerAuthConfig {
	if authConfig == nil {
		return nil
	}

	username := authConfig.Username
	password := authConfig.Password
	identityToken := authConfig.IdentityToken
	if identityToken == "" && authConfig.RegistryToken != "" {
		identityToken = authConfig.RegistryToken
	}
	if username == "" && password == "" && authConfig.Auth != "" {
		u, p, err := decodeDockerAuthField(authConfig.Auth)
		if err != nil {
			logging.Debugf("failed to decode docker auth field: %v", err)
		} else {
			username, password = u, p
		}
	}
	if username == "" && password == "" && identityToken == "" {
		return nil
	}

	return &itypes.DockerAuthConfig{
		Username:      username,
		Password:      password,
		IdentityToken: identityToken,
	}
}

func decodeDockerAuthField(auth string) (username, password string, err error) {
	decoded, err := base64.StdEncoding.DecodeString(auth)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode auth field: %w", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("auth field is not username:password")
	}
	return parts[0], parts[1], nil
}

// registrySystemContext builds a containers/image SystemContext for Buildah push/pull.
// When credentials are available for the image registry, they are attached so
// Buildah and Docker Engine auth behavior stay aligned.
func registrySystemContext(imageRef string) (*itypes.SystemContext, error) {
	sysCtx := &itypes.SystemContext{}

	authConfig, err := registryAuthConfig(imageRef)
	if err != nil {
		return nil, err
	}
	sysCtx.DockerAuthConfig = buildahDockerAuthConfig(authConfig)
	return sysCtx, nil
}

// wrapDockerRegistryErr clarifies opaque Podman compat-API errors (HTTP 500 with
// {error,errorDetail} instead of Docker's {message}), which often mean auth is missing.
// Classification is heuristic (substring matching on err.Error()); treat wrapped
// messages as hints, not a complete taxonomy of registry/daemon failures.
func wrapDockerRegistryErr(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "provided no error-message") {
		return fmt.Errorf("%s failed: %w (registry may require authentication; try: docker login or podman login)", op, err)
	}
	return fmt.Errorf("%s failed: %w", op, err)
}

func extractPushedDigest(raw []byte, imageRef string) (string, error) {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference for digest extraction: %w", err)
	}

	// Only accept manifest push markers (aux with Tag, or "tag: digest:" status lines).
	// Layer progress messages may include bare sha256 values that must not be signed.
	var manifestAuxDigest, statusDigest string
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var msg struct {
			Status string           `json:"status"`
			Aux    *json.RawMessage `json:"aux"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("failed to decode docker push output: %w", err)
		}
		if msg.Aux != nil {
			if d := digestFromManifestPushAux(*msg.Aux); d != "" {
				manifestAuxDigest = d
			}
		}
		if d := digestFromPushStatus(msg.Status); d != "" {
			statusDigest = d
		}
	}

	streamDigest := manifestAuxDigest
	if streamDigest == "" {
		streamDigest = statusDigest
	}
	if streamDigest == "" {
		return "", nil
	}
	return ref.Context().Digest(streamDigest).String(), nil
}

// confirmPushedDigest resolves the tag in the registry and reconciles it with any
// digest parsed from the push stream. Registry HEAD is authoritative when available.
func confirmPushedDigest(imageRef, streamDigestRef string) (string, error) {
	streamDigest := digestFromReference(streamDigestRef)

	registryDigestRef, err := ResolveRegistryDigest(imageRef)
	if err != nil {
		if streamDigest != "" {
			logging.Warnf(
				"push reported digest %s but registry HEAD failed: %v; using stream-reported digest",
				streamDigestRef, err,
			)
			return streamDigestRef, nil
		}
		logging.Warnf(
			"push completed for %s but digest could not be resolved (no digest in push stream and registry HEAD failed: %v)",
			imageRef, err,
		)
		return "", nil
	}

	if streamDigest == "" {
		return registryDigestRef, nil
	}

	registryDigest := digestFromReference(registryDigestRef)
	if registryDigest == "" {
		return registryDigestRef, nil
	}
	if streamDigest != registryDigest {
		return "", fmt.Errorf(
			"push stream digest %s does not match registry manifest %s",
			streamDigestRef, registryDigestRef,
		)
	}
	return registryDigestRef, nil
}

func digestFromReference(digestRef string) string {
	if digestRef == "" {
		return ""
	}
	ref, err := name.ParseReference(digestRef)
	if err != nil {
		return ""
	}
	dig, ok := ref.(name.Digest)
	if !ok {
		return ""
	}
	return dig.DigestStr()
}

// digestFromManifestPushAux reads Digest/digest from a Docker PushResult-style aux
// blob. Requires Tag/tag so layer-only aux payloads are ignored.
func digestFromManifestPushAux(raw json.RawMessage) string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	if pushAuxStringField(fields, "Tag", "tag") == "" {
		return ""
	}
	for _, key := range []string{"Digest", "digest"} {
		v, ok := fields[key]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		if d := normalizeManifestDigest(s); d != "" {
			return d
		}
	}
	return ""
}

func pushAuxStringField(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := fields[key]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if ok && s != "" {
			return s
		}
	}
	return ""
}

// digestFromPushStatus parses Docker/Podman manifest push status lines such as:
//
//	"latest: digest: sha256:abc… size: 1234"
func digestFromPushStatus(status string) string {
	const marker = ": digest:"
	idx := strings.Index(strings.ToLower(status), marker)
	if idx < 0 {
		return ""
	}
	tagPart := strings.TrimSpace(status[:idx])
	if tagPart == "" {
		return ""
	}
	rest := strings.TrimSpace(status[idx+len(marker):])
	if rest == "" {
		return ""
	}
	// Take the first token (digest); ignore trailing "size: N".
	digestToken := strings.Fields(rest)[0]
	return normalizeManifestDigest(digestToken)
}

var (
	// supportedDigestAlgorithms lists OCI digest algorithms to try when parsing
	// manifest digests. Add new algorithms here as they become available in the
	// OCI digest package without changing validation logic elsewhere.
	supportedDigestAlgorithms = []digest.Algorithm{
		digest.SHA256,
		digest.SHA384,
		digest.SHA512,
	}
)

// normalizeManifestDigest normalizes and validates manifest digests using the
// OCI digest package. Supports any algorithm defined in supportedDigestAlgorithms.
// Accepts both "algorithm:hex" and bare hex formats (algorithm inferred from length).
// Handles case-insensitive algorithm prefixes. Returns normalized "algorithm:hex"
// or empty string if invalid.
func normalizeManifestDigest(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	// If it contains ":", try to parse as "algorithm:hex" format
	if idx := strings.Index(s, ":"); idx > 0 {
		// Normalize both algorithm and hex to lowercase (OCI digest spec requires lowercase)
		normalized := strings.ToLower(s)
		d, err := digest.Parse(normalized)
		if err == nil && isSupportedAlgorithm(d.Algorithm()) {
			return d.String()
		}
		// Fall through to try bare hex in case the ":" is part of invalid format
	}

	// Bare hex string - try each supported algorithm to find a match by length
	hexStr := s

	// Quick validation: must be valid hex characters
	for _, c := range hexStr {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return ""
		}
	}

	// Try to construct a valid digest using each supported algorithm
	hexLower := strings.ToLower(hexStr)
	for _, alg := range supportedDigestAlgorithms {
		if !alg.Available() {
			continue
		}
		// Check if hex length matches this algorithm's expected size
		// Size() returns bytes, hex is 2 chars per byte
		expectedHexLen := alg.Size() * 2
		if len(hexStr) == expectedHexLen {
			candidate := alg.String() + ":" + hexLower
			d, err := digest.Parse(candidate)
			if err == nil {
				return d.String()
			}
		}
	}

	return ""
}

// isSupportedAlgorithm checks if an algorithm is in our supported list.
func isSupportedAlgorithm(alg digest.Algorithm) bool {
	for _, supported := range supportedDigestAlgorithms {
		if alg == supported {
			return true
		}
	}
	return false
}

// PushImage pushes a local image to a remote registry using docker.
// Returns a digest-pinned reference when the push output includes one.
func (d *dockerBuilder) PushImage(imageRef string) (string, error) {
	if imageRef == "" {
		return "", fmt.Errorf("image reference is empty")
	}

	imageRef = NormalizeImageTag(imageRef)
	logging.Infof("Pushing image to registry using docker: %s", imageRef)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultPushPullTimeout)
	defer cancel()

	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("failed to create docker client: %w", err)
	}
	defer apiClient.Close()

	registryAuth, err := encodedRegistryAuth(imageRef)
	if err != nil {
		return "", err
	}

	pushOptions := image.PushOptions{RegistryAuth: registryAuth}
	responseBody, err := apiClient.ImagePush(ctx, imageRef, pushOptions)
	if err != nil {
		return "", wrapDockerRegistryErr("docker push", err)
	}
	defer responseBody.Close()

	var captured bytes.Buffer
	stream := io.TeeReader(responseBody, &captured)
	if displayErr := jsonmessage.DisplayJSONMessagesStream(stream, os.Stdout, 0, false, nil); displayErr != nil {
		return "", wrapDockerRegistryErr("docker push", displayErr)
	}

	streamDigestRef, err := extractPushedDigest(captured.Bytes(), imageRef)
	if err != nil {
		return "", err
	}
	digestRef, err := confirmPushedDigest(imageRef, streamDigestRef)
	if err != nil {
		return "", fmt.Errorf("failed to confirm pushed image digest: %w", err)
	}
	if digestRef == "" {
		logging.Warnf("Successfully pushed image %s but digest is unknown; use --sign with --push only after digest can be resolved", imageRef)
	} else {
		logging.Infof("Successfully pushed image: %s (digest %s)", imageRef, digestFromReference(digestRef))
	}
	return digestRef, nil
}

// PullImage pulls an image from a remote registry using docker
func (d *dockerBuilder) PullImage(imageRef string) error {
	if imageRef == "" {
		return fmt.Errorf("image reference is empty")
	}

	imageRef = NormalizeImageTag(imageRef)
	logging.Infof("Pulling image from registry using docker: %s", imageRef)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultPushPullTimeout)
	defer cancel()

	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer apiClient.Close()

	registryAuth, err := encodedRegistryAuth(imageRef)
	if err != nil {
		return err
	}

	pullOptions := image.PullOptions{RegistryAuth: registryAuth}
	responseBody, err := apiClient.ImagePull(ctx, imageRef, pullOptions)
	if err != nil {
		return wrapDockerRegistryErr("docker pull", err)
	}
	defer responseBody.Close()

	if err := jsonmessage.DisplayJSONMessagesStream(responseBody, os.Stdout, 0, false, nil); err != nil {
		return wrapDockerRegistryErr("docker pull", err)
	}

	logging.Infof("Successfully pulled image: %s", imageRef)
	return nil
}
