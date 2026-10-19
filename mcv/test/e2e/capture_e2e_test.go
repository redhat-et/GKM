//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMCVCaptureHelp(t *testing.T) {
	output, err := runCaptureCLI(t, findMCVBinary(t), "--help")
	assert.NoError(t, err, "help failed: %s", output)
	for _, flag := range []string{"--source stringArray", "--mount-at string", "--place-extra-trees"} {
		assert.Contains(t, output, flag)
	}
}

// Capture-only options must be rejected before selecting a registry builder or
// resolving a signing digest. Registry handlers have distinct exit codes (4-7).
func TestMCVCaptureFlagsRejectRegistryActions(t *testing.T) {
	binary := findMCVBinary(t)
	options := []struct {
		name string
		args []string
		want string
	}{
		{"source", []string{"--source", "/tmp/triton"}, "--source and --mount-at require --create"},
		{"mount-at", []string{"--mount-at", "/tmp/vllm"}, "--source and --mount-at require --create"},
		{"place-extra-trees", []string{"--place-extra-trees"}, "--place-extra-trees requires --extract"},
	}
	for _, action := range []string{"--push", "--pull", "--sign", "--verify"} {
		for _, option := range options {
			t.Run(strings.TrimPrefix(action, "--")+"/"+option.name, func(t *testing.T) {
				args := append([]string{action, "--image", unreachableImage}, option.args...)
				output, err := runCaptureCLI(t, binary, args...)
				assertCaptureFailure(t, output, err, option.want)
			})
		}
	}
}

func TestMCVCaptureOptionsReachBuildCache(t *testing.T) {
	binary := findMCVBinary(t)
	root := t.TempDir()
	cacheDir := filepath.Join(root, "vllm")
	firstSource := filepath.Join(root, "first", "triton")
	secondSource := filepath.Join(root, "second", "triton")
	for _, dir := range []string{cacheDir, firstSource, secondSource} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// If Cobra retains only the last --source, or runCreate drops Sources, the
	// collision disappears and this deliberately unavailable builder is reached.
	baseArgs := []string{"--create", "--no-gpu", "--image", unreachableImage,
		"--dir", cacheDir, "--builder", "capture-test-unavailable"}
	t.Run("repeated sources retain both values", func(t *testing.T) {
		args := append(append([]string{}, baseArgs...),
			"--source", firstSource, "--source", secondSource)
		output, err := runCaptureCLI(t, binary, args...)
		assertCaptureFailure(t, output, err, "already taken")
		assert.Contains(t, output, secondSource)
		assert.NotContains(t, output, "unsupported builder")
	})
	t.Run("mount path reaches capture validation", func(t *testing.T) {
		args := append(append([]string{}, baseArgs...), "--mount-at", "relative/cache")
		output, err := runCaptureCLI(t, binary, args...)
		assertCaptureFailure(t, output, err, "must be absolute")
		assert.NotContains(t, output, "unsupported builder")
	})
	t.Run("placement requires extract", func(t *testing.T) {
		args := append(append([]string{}, baseArgs...), "--place-extra-trees")
		output, err := runCaptureCLI(t, binary, args...)
		assertCaptureFailure(t, output, err, "--place-extra-trees requires --extract")
	})
}

// Exercise real vLLM metadata and image preparation, stopping at local Docker
// client configuration before a daemon connection can be attempted.
func TestMCVCreateDisablesGPUForCapture(t *testing.T) {
	binary := findMCVBinary(t)
	root := t.TempDir()
	cacheDir := filepath.Join(root, "vllm")
	model := filepath.Join(cacheDir, "torch_compile_cache", "torch_aot_compile",
		strings.Repeat("a", 64), "rank_0_0", "model")
	if err := os.MkdirAll(filepath.Dir(model), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("test mega-AOT cache artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Builder selection only looks up this executable; the Docker SDK does the
	// actual image preparation and would contact DOCKER_HOST afterward.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ENABLE_GPU", "true")
	t.Setenv("DOCKER_HOST", "invalid-docker-host")
	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")

	output, err := runCaptureCLI(t, binary, "--create", "--no-gpu",
		"--image", unreachableImage, "--dir", cacheDir, "--builder", "docker")
	assertCaptureFailure(t, output, err, "failed to create Docker client")
	assert.Contains(t, output, "unable to parse docker host")
	assert.Contains(t, output, "GPU detection disabled (--no-gpu), will use cache metadata for hardware info",
		"the no-GPU flag must be configured before vLLM image labels are prepared")
}

func runCaptureCLI(t *testing.T, binary string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("MCV did not finish local capture validation: args=%v output=%s: %v", args, output, ctx.Err())
	}
	return string(output), err
}

func assertCaptureFailure(t *testing.T, output string, err error, want string) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected a capture error exit: err=%v output=%s", err, output)
	}
	assert.Equal(t, 2, exitErr.ExitCode(), "expected a capture error before another action runs: %s", output)
	assert.Contains(t, output, want)
}
