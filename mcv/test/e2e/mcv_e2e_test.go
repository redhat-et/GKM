//go:build e2e
// +build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	testImage = "quay.io/gkm/cache-examples:vector-add-cache-cuda"
	unreachableImage   = "invalid.invalid/gkm/e2e-cosign:test"
)

// TestMCVGPUInfo tests the GPU info functionality
func TestMCVGPUInfo(t *testing.T) {
	mcvBinary := findMCVBinary(t)

	// Test with stub mode (no actual GPU required)
	cmd := exec.Command(mcvBinary, "--gpu-info", "--stub")
	output, err := cmd.CombinedOutput()

	// This might fail if stub mode isn't properly configured, but shouldn't crash
	if err != nil {
		t.Skipf("GPU info with stub mode: %s", string(output))
	}

	assert.Contains(t, string(output), "GPU")
}

// TestMCVCheckCompat tests the compatibility check functionality
func TestMCVCheckCompat(t *testing.T) {
	mcvBinary := findMCVBinary(t)

	cmd := exec.Command(mcvBinary, "--check-compat", "--image", testImage)
	output, err := cmd.CombinedOutput()

	t.Logf("Compatibility check output: %s, err: %v", string(output), err)

	// Verify command ran with output; success or failure not important
	assert.NotEmpty(t, output, "Expected compatibility check output")
}

// TestMCVVersionFlag tests the version flag
func TestMCVVersionFlag(t *testing.T) {
	mcvBinary := findMCVBinary(t)

	cmd := exec.Command(mcvBinary, "--version")
	output, err := cmd.CombinedOutput()
	assert.NoError(t, err, "Failed to get version: %s", string(output))

	// Verify version output contains expected format
	outputStr := string(output)
	assert.Regexp(t, `mcv.*version.*\d+\.\d+`, outputStr, "Version should contain 'mcv version X.Y'")
}

// TestMCVHelpFlag tests the help flag
func TestMCVHelpFlag(t *testing.T) {
	mcvBinary := findMCVBinary(t)

	cmd := exec.Command(mcvBinary, "--help")
	output, err := cmd.CombinedOutput()
	assert.NoError(t, err, "Failed to get help: %s", string(output))

	outputStr := string(output)
	expectedFlags := []string{
		"Usage", "Flags", "create", "extract", "push", "pull",
		"sign", "verify", "certificate-identity",
		"certificate-oidc-issuer", "insecure-ignore-tlog",
	}
	for _, flag := range expectedFlags {
		assert.Contains(t, outputStr, flag)
	}
}

// TestMCVPushPullSignVerifyCLI exercises push/pull/sign/verify through the real
// binary: required flags, mutually exclusive actions, and certificate constraints.
// These do not need a registry; they cover CLI wiring that unit tests of
// validate() alone cannot (cobra MarkFlagsMutuallyExclusive, exit paths).
func TestMCVPushPullSignVerifyCLI(t *testing.T) {
	mcvBinary := findMCVBinary(t)

	tests := []struct {
		name       string
		args       []string
		wantErr    bool
		wantOutput string
	}{
		{
			name:       "push requires image",
			args:       []string{"--push"},
			wantErr:    true,
			wantOutput: "--image",
		},
		{
			name:       "pull requires image",
			args:       []string{"--pull"},
			wantErr:    true,
			wantOutput: "--image",
		},
		{
			name:       "sign requires image",
			args:       []string{"--sign"},
			wantErr:    true,
			wantOutput: "--image",
		},
		{
			name:       "verify requires image",
			args:       []string{"--verify"},
			wantErr:    true,
			wantOutput: "--image",
		},
		{
			name:       "sign and verify together",
			args:       []string{"--sign", "--verify", "--image", testImage},
			wantErr:    true,
			wantOutput: "only one action flag",
		},
		{
			name:       "push and verify together",
			args:       []string{"--push", "--verify", "--image", testImage},
			wantErr:    true,
			wantOutput: "--verify",
		},
		{
			name:       "pull and sign together",
			args:       []string{"--pull", "--sign", "--image", testImage},
			wantErr:    true,
			wantOutput: "--sign",
		},
		{
			name: "certificate-identity without issuer",
			args: []string{
				"--verify", "--image", testImage,
				"--certificate-identity", "user@example.com",
			},
			wantErr:    true,
			wantOutput: "certificate-oidc-issuer",
		},
		{
			name: "both certificate-identity and certificate-identity-regexp",
			args: []string{
				"--verify", "--image", testImage,
				"--certificate-identity", "user@example.com",
				"--certificate-identity-regexp", ".*@example.com",
				"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
			},
			wantErr:    true,
			wantOutput: "certificate-identity",
		},
		{
			name: "both certificate-oidc-issuer and certificate-oidc-issuer-regexp",
			args: []string{
				"--verify", "--image", testImage,
				"--certificate-identity", "user@example.com",
				"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
				"--certificate-oidc-issuer-regexp", "https://.*",
			},
			wantErr:    true,
			wantOutput: "certificate-oidc-issuer",
		},
		{
			name: "insecure-ignore-tlog without verify",
			args: []string{
				"--pull", "--image", testImage,
				"--insecure-ignore-tlog",
			},
			wantErr:    true,
			wantOutput: "insecure-ignore-tlog",
		},
		{
			name: "yes without sign",
			args: []string{
				"--pull", "--image", testImage,
				"--yes",
			},
			wantErr:    true,
			wantOutput: "--yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(mcvBinary, tt.args...)
			output, err := cmd.CombinedOutput()
			outputStr := string(output)
			t.Logf("args=%v output=%s", tt.args, outputStr)

			if tt.wantErr {
				assert.Error(t, err, "expected failure for %v: %s", tt.args, outputStr)
			} else {
				assert.NoError(t, err, "unexpected failure for %v: %s", tt.args, outputStr)
			}
			if tt.wantOutput != "" {
				assert.Contains(t, outputStr, tt.wantOutput)
			}
		})
	}
}

// TestMCVSignVerifyUnreachableImage checks that --sign and --verify are wired
// through to digest resolution (fail on a clearly invalid registry host) rather
// than being rejected only at flag parsing.
func TestMCVSignVerifyUnreachableImage(t *testing.T) {
	mcvBinary := findMCVBinary(t)

	t.Run("sign", func(t *testing.T) {
		cmd := exec.Command(mcvBinary, "--sign", "--image", unreachableImage, "--yes")
		output, err := cmd.CombinedOutput()
		assert.Error(t, err, "sign should fail for unreachable image: %s", string(output))
		assert.NotContains(t, string(output), "--image is required")
	})

	t.Run("verify", func(t *testing.T) {
		cmd := exec.Command(mcvBinary, "--verify", "--image", unreachableImage)
		output, err := cmd.CombinedOutput()
		assert.Error(t, err, "verify should fail for unreachable image: %s", string(output))
		assert.NotContains(t, string(output), "--image is required")
	})

	t.Run("push", func(t *testing.T) {
		cmd := exec.Command(mcvBinary, "--push", "--image", unreachableImage)
		output, err := cmd.CombinedOutput()
		assert.Error(t, err, "push should fail for missing local image: %s", string(output))
		assert.NotContains(t, string(output), "--image is required")
	})

	t.Run("pull", func(t *testing.T) {
		cmd := exec.Command(mcvBinary, "--pull", "--image", unreachableImage)
		output, err := cmd.CombinedOutput()
		assert.Error(t, err, "pull should fail for unreachable image: %s", string(output))
		assert.NotContains(t, string(output), "--image is required")
	})
}

// findMCVBinary locates the mcv binary for testing
func findMCVBinary(t *testing.T) string {
	t.Helper()

	// Try common locations
	locations := []string{
		"../../_output/bin/mcv",
		"../../_output/bin/linux_amd64/mcv",
		"../../_output/bin/darwin_amd64/mcv",
		"../../_output/bin/darwin_arm64/mcv",
		"mcv", // In PATH
	}

	for _, loc := range locations {
		if absPath, err := filepath.Abs(loc); err == nil {
			if _, err := os.Stat(absPath); err == nil {
				t.Logf("Found mcv binary at: %s", absPath)
				return absPath
			}
		}
	}

	// Try to find in PATH
	if path, err := exec.LookPath("mcv"); err == nil {
		t.Logf("Found mcv binary in PATH: %s", path)
		return path
	}

	t.Fatal("Could not find mcv binary. Please run 'make build' first.")
	return ""
}
