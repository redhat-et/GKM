package imgbuild

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/registry"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"go.podman.io/storage"
)

const (
	testGitHubIssuer       = "testGitHubIssuer"
	testCertIdentity       = "testCertIdentity"
	testCertIdentityRegexp = "testCertIdentityRegexp"
	testOidcIssuerRegexp   = "testOidcIssuerRegexp"
	testQuayRegistry       = "quay.io"
	testAuthKey            = "auth"
	testAuthsKey           = "auths"
	errCertIdentity        = "certificate-identity"
	errCertOidcIssuer      = "certificate-oidc-issuer"
)

func setIsolatedRegistryAuthEnv(t *testing.T) (homeDir, dockerConfigDir string) {
	t.Helper()
	homeDir = t.TempDir()
	dockerConfigDir = t.TempDir()
	runtimeDir := filepath.Join(homeDir, "runtime")
	configDir := filepath.Join(homeDir, ".config")
	assert.NoError(t, os.MkdirAll(runtimeDir, 0o700))
	assert.NoError(t, os.MkdirAll(configDir, 0o700))
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("DOCKER_CONFIG", dockerConfigDir)
	return homeDir, dockerConfigDir
}

// setIsolatedHomeEnv isolates HOME/XDG paths without setting DOCKER_CONFIG so
// containers/auth.json fallback is exercised.
func setIsolatedHomeEnv(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	runtimeDir := filepath.Join(homeDir, "runtime")
	configDir := filepath.Join(homeDir, ".config")
	assert.NoError(t, os.MkdirAll(runtimeDir, 0o700))
	assert.NoError(t, os.MkdirAll(configDir, 0o700))
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	return homeDir
}

func writeEmptyDockerConfig(t *testing.T, dockerConfigDir string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		testAuthsKey: map[string]any{},
		"credsStore": "mcv-test-disabled",
	})
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(dockerConfigDir, "config.json"), data, 0o600))
}

func writeContainersAuthConfig(t *testing.T, homeDir string, auths map[string]any) {
	t.Helper()
	configDir := filepath.Join(homeDir, ".config", "containers")
	assert.NoError(t, os.MkdirAll(configDir, 0o700))
	data, err := json.Marshal(map[string]any{"auths": auths})
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(configDir, "auth.json"), data, 0o600))
}

func TestCosignSignEmptyImageRef(t *testing.T) {
	err := Sign("", "", true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "image reference is empty")
}

func TestCosignVerifyEmptyImageRef(t *testing.T) {
	err := Verify("", &VerifyOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "image reference is empty")
}

func TestCosignVerifyKeylessIdentityConstraints(t *testing.T) {
	tests := []struct {
		name    string
		opts    VerifyOptions
		wantErr string
	}{
		{
			name:    "only certificate-identity",
			opts:    VerifyOptions{CertIdentity: testCertIdentity},
			wantErr: errCertIdentity,
		},
		{
			name:    "only certificate-oidc-issuer",
			opts:    VerifyOptions{CertOidcIssuer: testGitHubIssuer},
			wantErr: errCertOidcIssuer,
		},
		{
			name:    "only identity regexp",
			opts:    VerifyOptions{CertIdentityRegexp: testCertIdentityRegexp},
			wantErr: errCertIdentity,
		},
		{
			name:    "only issuer regexp",
			opts:    VerifyOptions{CertOidcIssuerRegexp: testOidcIssuerRegexp},
			wantErr: errCertOidcIssuer,
		},
		{
			name: "exact identity and issuer pass pairing validation",
			opts: VerifyOptions{
				CertIdentity:   testCertIdentity,
				CertOidcIssuer: testGitHubIssuer,
			},
		},
		{
			name: "mixed exact identity and issuer regexp pass pairing validation",
			opts: VerifyOptions{
				CertIdentity:         testCertIdentity,
				CertOidcIssuerRegexp: testOidcIssuerRegexp,
			},
		},
		{
			name: "both identity exact and regexp",
			opts: VerifyOptions{
				CertIdentity:       "testCertIdentity",
				CertIdentityRegexp: "testCertIdentityRegexp",
				CertOidcIssuer:     "testGitHubIssuer",
			},
			wantErr: errCertIdentity,
		},
		{
			name: "both issuer exact and regexp",
			opts: VerifyOptions{
				CertIdentity:         testCertIdentity,
				CertOidcIssuer:       "testGitHubIssuer",
				CertOidcIssuerRegexp: testOidcIssuerRegexp,
			},
			wantErr: errCertOidcIssuer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify("localhost:5000/test:latest", &tt.opts)
			assert.Error(t, err)
			if tt.wantErr != "" {
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.NotContains(t, err.Error(), "cosign keyless verify failed")
			} else {
				// Past option validation; failure is from cosign/network/key material
				assert.NotContains(t, err.Error(), "must be set together")
				assert.NotContains(t, err.Error(), "identity constraints requires")
			}
		})
	}
}

func TestCosignSignAndVerifyNetworkFailures(t *testing.T) {
	tests := []struct {
		name string
		opts VerifyOptions
	}{
		{
			name: "Keyless default (any Fulcio identity)",
			opts: VerifyOptions{},
		},
		{
			name: "Keyless with exact identity constraints",
			opts: VerifyOptions{
				CertIdentity:   "https://github.com/org/repo/.github/workflows/ci.yml@refs/heads/main",
				CertOidcIssuer: testGitHubIssuer,
			},
		},
		{
			name: "Keyless with regexp identity constraints",
			opts: VerifyOptions{
				CertIdentityRegexp:   "testCertIdentityRegexp",
				CertOidcIssuerRegexp: testOidcIssuerRegexp,
			},
		},
		{
			name: "Key-based mode",
			opts: VerifyOptions{KeyPath: "/path/to/key"},
		},
		{
			name: "Key-based with ignore tlog",
			opts: VerifyOptions{KeyPath: "/path/to/key", IgnoreTlog: true},
		},
		{
			name: "Keyless with ignore tlog",
			opts: VerifyOptions{IgnoreTlog: true},
		},
	}

	for _, tt := range tests {
		t.Run("Verify/"+tt.name, func(t *testing.T) {
			err := Verify("localhost:5000/test:latest", &tt.opts)
			assert.Error(t, err) // Expected without registry/key material
		})
	}

	t.Run("Sign/Keyless", func(t *testing.T) {
		err := Sign("localhost:5000/test:latest", "", true)
		assert.Error(t, err)
	})

	t.Run("Sign/Key-based", func(t *testing.T) {
		err := Sign("localhost:5000/test:latest", "/path/to/key", true)
		assert.Error(t, err)
	})
}

func TestNormalizeImageTag(t *testing.T) {
	digestRef := "quay.io/gkm/cache-examples@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		in, want string
	}{
		{"example.com/repo/image", "example.com/repo/image:latest"},
		{"example.com/repo/image:v1", "example.com/repo/image:v1"},
		{digestRef, digestRef},
		{"localhost:5000/foo", "localhost:5000/foo:latest"},
		{"localhost:5000/foo:v1", "localhost:5000/foo:v1"},
		{"foo", "foo:latest"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeImageTag(tt.in))
		})
	}
}

func TestResolveRegistryDigest_InvalidReference(t *testing.T) {
	_, err := ResolveRegistryDigest(":::bad")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse image reference")
}

func TestWrapRegistryHeadError(t *testing.T) {
	const imageRef = "example.com/repo:missing"

	tests := []struct {
		name       string
		err        error
		wantSubstr []string
	}{
		{
			name:       "not found",
			err:        errors.New("GET https://example.com/v2/repo/manifests/missing: MANIFEST_UNKNOWN: manifest unknown"),
			wantSubstr: []string{"not found in registry", imageRef},
		},
		{
			name:       "404",
			err:        errors.New("404 Not Found"),
			wantSubstr: []string{"not found in registry"},
		},
		{
			name:       "unauthorized",
			err:        errors.New("401 Unauthorized"),
			wantSubstr: []string{"cannot access image", "check credentials"},
		},
		{
			name:       "denied",
			err:        errors.New("access denied"),
			wantSubstr: []string{"cannot access image", "check credentials"},
		},
		{
			name:       "generic transport failure",
			err:        errors.New("dial tcp [::1]:5000: connect: connection refused"),
			wantSubstr: []string{"failed to resolve image digest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapRegistryHeadError(imageRef, tt.err)
			assert.Error(t, got)
			for _, sub := range tt.wantSubstr {
				assert.Contains(t, got.Error(), sub)
			}
			assert.ErrorIs(t, got, tt.err)
		})
	}

	assert.NoError(t, wrapRegistryHeadError(imageRef, nil))
}

func TestSelectDockerRepoDigest(t *testing.T) {
	const (
		imageRef  = "quay.io/gkm/cache:rocm"
		repo      = "quay.io/gkm/cache"
		digestHex = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		want      = repo + "@" + digestHex
	)

	got, err := selectDockerRepoDigest(imageRef, repo, []string{want})
	assert.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = selectDockerRepoDigest(imageRef, repo, []string{
		"registry.example.com/other@" + digestHex,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no RepoDigests for repository")
	assert.Contains(t, err.Error(), repo)

	_, err = selectDockerRepoDigest(imageRef, repo, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "has no RepoDigests")
}

func TestResolveDigest_DigestPassthrough(t *testing.T) {
	const dig = "quay.io/cmagina/vector-add-cache@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got, err := ResolveDigest(dig)
	assert.NoError(t, err)
	assert.Equal(t, dig, got)
}

func TestManifestDigestFromStorageImage_SkipsImageID(t *testing.T) {
	const imageID = "5409779d74c667a9316c18e56a2379e67e929fdcdaf1ce7ee4fb59f7cb271003"
	const manifest = "sha256:63ef03666c2ec8ec940ba3923df56cfc0611084b3c88a749c551405dbcc0da6e"
	img := &storage.Image{
		ID:      imageID,
		Digest:  "sha256:" + imageID, // storage often records the ID here
		Digests: []digest.Digest{"sha256:" + imageID, digest.Digest(manifest)},
	}
	got, err := manifestDigestFromStorageImage(img)
	assert.NoError(t, err)
	assert.Equal(t, manifest, got)
}

func TestManifestDigestFromStorageImage_OnlyIDErrors(t *testing.T) {
	const imageID = "5409779d74c667a9316c18e56a2379e67e929fdcdaf1ce7ee4fb59f7cb271003"
	img := &storage.Image{
		ID:     imageID,
		Digest: "sha256:" + imageID,
	}
	_, err := manifestDigestFromStorageImage(img)
	assert.Error(t, err)
}

func TestResolveDigest_LocalImage(t *testing.T) {
	// Requires the image from create; skip if absent.
	ref := "quay.io/cmagina/vector-add-cache:rocm"
	got, err := ResolveDigest(ref)
	if err != nil {
		t.Skipf("local image not available: %v", err)
	}
	assert.Contains(t, got, "@sha256:")
	assert.True(t, strings.HasPrefix(got, "quay.io/cmagina/vector-add-cache@"))
	// Must be the manifest digest, not the image config ID.
	assert.NotContains(t, got, "fc3f90e8b2d34bd8b9f5578a47abd8c443fd9ee9a81cc63b40de614e32db626e")
	assert.NotContains(t, got, "5409779d74c667a9316c18e56a2379e67e929fdcdaf1ce7ee4fb59f7cb271003")
}

func TestDockerAndBuildahEmptyImageRef(t *testing.T) {
	d := &dockerBuilder{}
	b := &buildahBuilder{}

	for _, fn := range []struct {
		name string
		call func() error
	}{
		{"docker push", func() error { _, err := d.PushImage(""); return err }},
		{"docker pull", func() error { return d.PullImage("") }},
		{"buildah push", func() error { _, err := b.PushImage(""); return err }},
		{"buildah pull", func() error { return b.PullImage("") }},
	} {
		t.Run(fn.name, func(t *testing.T) {
			err := fn.call()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "image reference is empty")
		})
	}
}

func TestEncodedRegistryAuth_FromDockerConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]any{
		testAuthsKey: map[string]any{
			testQuayRegistry: map[string]string{
				testAuthKey: base64.StdEncoding.EncodeToString([]byte("user:pass")),
			},
		},
	}
	data, err := json.Marshal(cfg)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600))

	t.Setenv("DOCKER_CONFIG", dir)

	encoded, err := encodedRegistryAuth("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.NotEmpty(t, encoded)

	decoded, err := registry.DecodeAuthConfig(encoded)
	assert.NoError(t, err)
	assert.Equal(t, "user", decoded.Username)
	assert.Equal(t, "pass", decoded.Password)

	sysCtx, err := registrySystemContext("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.NotNil(t, sysCtx.DockerAuthConfig)
	assert.Equal(t, "user", sysCtx.DockerAuthConfig.Username)
	assert.Equal(t, "pass", sysCtx.DockerAuthConfig.Password)

	ro, err := cosignRegistryOptions("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.NotNil(t, ro.Keychain, "Sign/Verify must use the same resolved credentials as push/pull")
	auth, err := ro.Keychain.Resolve(nil)
	assert.NoError(t, err)
	got, err := auth.Authorization()
	assert.NoError(t, err)
	assert.Equal(t, "user", got.Username)
	assert.Equal(t, "pass", got.Password)
}

func TestCosignRegistryOptions_NoCredentials(t *testing.T) {
	_, dockerConfigDir := setIsolatedRegistryAuthEnv(t)
	writeEmptyDockerConfig(t, dockerConfigDir)

	ro, err := cosignRegistryOptions("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.Nil(t, ro.Keychain, "no creds → leave Keychain nil so cosign uses DefaultKeychain")
}

func TestRegistryAuthConfig_ContainersAuthFallbackWhenDockerConfigUnset(t *testing.T) {
	homeDir := setIsolatedHomeEnv(t)
	writeContainersAuthConfig(t, homeDir, map[string]any{
		testQuayRegistry: map[string]string{
			testAuthKey: base64.StdEncoding.EncodeToString([]byte("podman-user:podman-pass")),
		},
	})

	authConfig, err := registryAuthConfig("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.NotNil(t, authConfig)
	assert.Equal(t, "podman-user", authConfig.Username)
	assert.Equal(t, "podman-pass", authConfig.Password)

	ro, err := cosignRegistryOptions("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.NotNil(t, ro.Keychain)
	auth, err := ro.Keychain.Resolve(nil)
	assert.NoError(t, err)
	got, err := auth.Authorization()
	assert.NoError(t, err)
	assert.Equal(t, "podman-user", got.Username)
	assert.Equal(t, "podman-pass", got.Password)
}

func TestRegistryAuthConfig_ContainersAuthSkippedWhenDOCKER_CONFIGSet(t *testing.T) {
	homeDir, dockerConfigDir := setIsolatedRegistryAuthEnv(t)
	writeEmptyDockerConfig(t, dockerConfigDir)
	writeContainersAuthConfig(t, homeDir, map[string]any{
		testQuayRegistry: map[string]string{
			testAuthKey: base64.StdEncoding.EncodeToString([]byte("podman-user:podman-pass")),
		},
	})

	authConfig, err := registryAuthConfig("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.Nil(t, authConfig, "DOCKER_CONFIG set → do not merge host containers/auth.json")

	ro, err := cosignRegistryOptions("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.Nil(t, ro.Keychain)
}

func TestCosignRegistryOptions_IdentityToken(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]any{
		testAuthsKey: map[string]any{
			testQuayRegistry: map[string]string{
				"identitytoken": "oidc-token-value",
			},
		},
	}
	data, err := json.Marshal(cfg)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600))

	t.Setenv("DOCKER_CONFIG", dir)

	ro, err := cosignRegistryOptions("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.NotNil(t, ro.Keychain)
	auth, err := ro.Keychain.Resolve(nil)
	assert.NoError(t, err)
	got, err := auth.Authorization()
	assert.NoError(t, err)
	assert.Equal(t, "oidc-token-value", got.IdentityToken)
}

func TestEncodedRegistryAuth_NoCredentials(t *testing.T) {
	_, dockerConfigDir := setIsolatedRegistryAuthEnv(t)
	writeEmptyDockerConfig(t, dockerConfigDir)

	encoded, err := encodedRegistryAuth("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.Empty(t, encoded, "empty credentials must not encode to e30=")

	sysCtx, err := registrySystemContext("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.Nil(t, sysCtx.DockerAuthConfig, "isolated env has no docker or containers credentials")
}

func TestRegistrySystemContext_FromDockerAuthField(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]any{
		testAuthsKey: map[string]any{
			testQuayRegistry: map[string]string{
				testAuthKey: base64.StdEncoding.EncodeToString([]byte("buildah-user:buildah-pass")),
			},
		},
	}
	data, err := json.Marshal(cfg)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600))
	t.Setenv("DOCKER_CONFIG", dir)

	sysCtx, err := registrySystemContext("quay.io/gkm/cache:test")
	assert.NoError(t, err)
	assert.NotNil(t, sysCtx.DockerAuthConfig)
	assert.Equal(t, "buildah-user", sysCtx.DockerAuthConfig.Username)
	assert.Equal(t, "buildah-pass", sysCtx.DockerAuthConfig.Password)
}

func TestBuildahDockerAuthConfig_RegistryToken(t *testing.T) {
	got := buildahDockerAuthConfig(&registry.AuthConfig{
		RegistryToken: "registry-bearer-token",
	})
	assert.NotNil(t, got)
	assert.Equal(t, "registry-bearer-token", got.IdentityToken)
	assert.Empty(t, got.Username)
	assert.Empty(t, got.Password)
}

func TestEncodedRegistryAuth_InvalidRef(t *testing.T) {
	_, err := encodedRegistryAuth(":::not-a-ref")
	assert.Error(t, err)
}

func TestWrapDockerRegistryErr(t *testing.T) {
	opaque := fmt.Errorf("Error response from daemon: API returned a 500 (Internal Server Error) but provided no error-message")
	err := wrapDockerRegistryErr("docker push", opaque)
	assert.ErrorContains(t, err, "docker push failed")
	assert.ErrorContains(t, err, "authentication")
	assert.ErrorContains(t, err, "podman login")

	plain := fmt.Errorf("authentication required")
	err = wrapDockerRegistryErr("docker push", plain)
	assert.EqualError(t, err, "docker push failed: authentication required")
}

func TestKeyPassFunc(t *testing.T) {
	t.Setenv("COSIGN_PASSWORD", "secret")
	pw, err := keyPassFunc(false)
	assert.NoError(t, err)
	assert.Equal(t, []byte("secret"), pw)

	t.Setenv("COSIGN_PASSWORD", "")
	pw, err = keyPassFunc(false)
	assert.NoError(t, err)
	assert.Equal(t, []byte(""), pw)
}
