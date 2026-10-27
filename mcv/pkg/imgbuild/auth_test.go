package imgbuild

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/registry"
	"github.com/stretchr/testify/assert"
)

const (
	testQuayRegistry = "quay.io"
	testAuthKey      = "auth"
	testAuthsKey     = "auths"
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

func writeEmptyDockerConfig(t *testing.T, dockerConfigDir string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		testAuthsKey: map[string]any{},
		"credsStore": "mcv-test-disabled",
	})
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(dockerConfigDir, "config.json"), data, 0o600))
}

func writeDockerConfigWithAuth(t *testing.T, dir string, username, password string) {
	t.Helper()
	cfg := map[string]any{
		testAuthsKey: map[string]any{
			testQuayRegistry: map[string]string{
				testAuthKey: base64.StdEncoding.EncodeToString([]byte(username + ":" + password)),
			},
		},
	}
	data, err := json.Marshal(cfg)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600))
}

func TestEncodedRegistryAuth_FromDockerConfig(t *testing.T) {
	dir := t.TempDir()
	writeDockerConfigWithAuth(t, dir, "user", "pass")

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
	writeDockerConfigWithAuth(t, dir, "buildah-user", "buildah-pass")

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
