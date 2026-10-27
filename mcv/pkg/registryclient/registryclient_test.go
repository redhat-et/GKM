package registryclient

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthConfigContainersScopedCredentials(t *testing.T) {
	home := t.TempDir()
	runtimeDir := filepath.Join(home, "runtime")
	configDir := filepath.Join(home, ".config")
	containersDir := filepath.Join(configDir, "containers")
	dockerDir := filepath.Join(home, ".docker")
	for _, dir := range []string{runtimeDir, containersDir, dockerDir} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("DOCKER_CONFIG", "")
	require.NoError(t, os.WriteFile(filepath.Join(dockerDir, "config.json"),
		[]byte(`{"auths":{},"credsStore":"mcv-test-disabled"}`), 0o600))

	auths := map[string]map[string]string{}
	for scope, user := range map[string]string{
		"quay.io":             "registry-user",
		"quay.io/myorg":       "namespace-user",
		"quay.io/myorg/cache": "repository-user",
	} {
		auths[scope] = map[string]string{
			"auth": base64.StdEncoding.EncodeToString([]byte(user + ":password")),
		}
	}
	data, err := json.Marshal(map[string]any{"auths": auths})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(containersDir, "auth.json"), data, 0o600))

	for _, tt := range []struct {
		name, image, wantUser string
	}{
		{"repository", "quay.io/myorg/cache:test", "repository-user"},
		{"digest", "quay.io/myorg/cache@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "repository-user"},
		{"namespace", "quay.io/myorg/other:test", "namespace-user"},
		{"nested namespace", "quay.io/myorg/nested/cache:test", "namespace-user"},
		{"registry fallback", "quay.io/other/cache:test", "registry-user"},
		{"repository boundary", "quay.io/myorg/cache-other:test", "namespace-user"},
		{"namespace boundary", "quay.io/myorg-other/cache:test", "registry-user"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := AuthConfig(tt.image)
			require.NoError(t, err)
			require.NotNil(t, creds)
			assert.Equal(t, tt.wantUser, creds.Username)
			assert.Equal(t, "password", creds.Password)
			assert.Equal(t, "quay.io", creds.ServerAddress)
		})
	}
}
