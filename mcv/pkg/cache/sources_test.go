package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redhat-et/GKM/mcv/pkg/cacheplan"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func writeCacheFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// must wraps a non-nil check that stops the test, since the vendored testify
// build in this repo ships assert without require.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestDetectSourceTrees_ClassifiesKnownKinds(t *testing.T) {
	root := t.TempDir()
	tritonNamed := filepath.Join(root, "tmp", "triton")
	tritonShaped := filepath.Join(root, "weird-jit-dir")
	inductor := filepath.Join(root, "torchinductor_default")
	deepGemm := filepath.Join(root, "deep_gemm")
	flashinfer := filepath.Join(root, ".cache", "flashinfer")

	writeCacheFile(t, filepath.Join(tritonNamed, "cache.db"))
	writeCacheFile(t, filepath.Join(tritonShaped, "__grp__kernel.json"))
	writeCacheFile(t, filepath.Join(inductor, "a", "kernel.so"))
	writeCacheFile(t, filepath.Join(deepGemm, "kernel.cu"))
	writeCacheFile(t, filepath.Join(flashinfer, "fused_kv_cache.so"))

	trees, err := DetectSourceTrees([]string{tritonNamed, tritonShaped, inductor, deepGemm, flashinfer})
	must(t, err)
	if !assert.Len(t, trees, 5) {
		t.FailNow()
	}

	assert.Equal(t, SourceKindTriton, trees[0].Kind)
	assert.Equal(t, constants.EnvTritonCacheDir, trees[0].Env)
	assert.True(t, trees[0].RequiresWritable, "Triton appends to its cache on miss")
	assert.Equal(t, tritonNamed, trees[0].AbsPath, "abs path must be recorded verbatim")

	// Same kind recognized by layout, so an operator-set TRITON_CACHE_DIR with a
	// non-default basename still gets the right env.
	assert.Equal(t, SourceKindTriton, trees[1].Kind)
	assert.Equal(t, constants.EnvTritonCacheDir, trees[1].Env)

	assert.Equal(t, SourceKindInductor, trees[2].Kind)
	assert.Equal(t, constants.EnvTorchInductorCacheDir, trees[2].Env)

	assert.Equal(t, SourceKindDeepGemm, trees[3].Kind)
	assert.Equal(t, constants.EnvDeepGemmCacheDir, trees[3].Env)

	// FLASHINFER_WORKSPACE_BASE points at the workspace base rather than this
	// directory, so no env can be derived safely here.
	assert.Equal(t, SourceKindFlashinfer, trees[4].Kind)
	assert.Empty(t, trees[4].Env)
}

func TestDetectSourceTrees_RejectsUnusablePaths(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "not-a-dir")
	writeCacheFile(t, file)

	_, err := DetectSourceTrees([]string{filepath.Join(root, "missing")})
	assert.ErrorContains(t, err, "not readable")

	_, err = DetectSourceTrees([]string{file})
	assert.ErrorContains(t, err, "not a directory")
}

func TestDetectSourceTrees_RejectsPayloadNameCollisions(t *testing.T) {
	root := t.TempDir()
	writeCacheFile(t, filepath.Join(root, "a", "triton", "k.json"))
	writeCacheFile(t, filepath.Join(root, "b", "triton", "k.json"))
	writeCacheFile(t, filepath.Join(root, "torch_compile_cache", "junk"))

	_, err := DetectSourceTrees([]string{
		filepath.Join(root, "a", "triton"),
		filepath.Join(root, "b", "triton"),
	})
	assert.ErrorContains(t, err, "already taken")

	// Staging a tree named torch_compile_cache would overwrite the primary
	// payload directory, so it has to fail at capture time.
	_, err = DetectSourceTrees([]string{filepath.Join(root, "torch_compile_cache")})
	assert.ErrorContains(t, err, "already taken")
}

func TestVLLMCache_ExtraMountsLabelRoundTrips(t *testing.T) {
	cacheDir := t.TempDir()
	newMegaAOTCache(t, cacheDir, []string{testRank00})

	triton := filepath.Join(t.TempDir(), "triton")
	writeCacheFile(t, filepath.Join(triton, "__grp__kernel.json"))
	sources, err := DetectSourceTrees([]string{triton})
	must(t, err)

	got := DetectVLLMCache(cacheDir, CaptureSpec{Sources: sources})
	assert.NotNil(t, got)
	if got == nil {
		t.FailNow()
	}

	labels, err := got.Labels()
	must(t, err)
	assert.Equal(t, constants.VLLMCacheRoot+"="+cacheDir, labels[kmCacheRootEnv],
		"the mount must land where the cache was captured from, not the KServe default")

	raw, ok := labels[cacheplan.LabelCacheMounts]
	if !assert.True(t, ok, "extra trees must be recorded in the cache-mounts label") {
		t.FailNow()
	}

	var mounts []cacheplan.Mount
	must(t, json.Unmarshal([]byte(raw), &mounts))
	if !assert.Len(t, mounts, 1) {
		t.FailNow()
	}
	assert.Equal(t, SourceKindTriton, mounts[0].SubPath)
	assert.Equal(t, triton, mounts[0].AbsPath)
	assert.Equal(t, constants.EnvTritonCacheDir, mounts[0].Env)
	assert.True(t, mounts[0].RequiresWritable)
	assert.Equal(t, "true", labels[cacheplan.LabelSplitCacheCapture])

	plan, err := cacheplan.Derive(labels)
	must(t, err)
	if !assert.Len(t, plan.Mounts, 2, "primary root plus the Triton tree") {
		t.FailNow()
	}
	for _, key := range []string{kmFramework, kmCacheType, kmCacheMountSubpath, kmCacheRootEnv} {
		assert.NotEmpty(t, labels[key], "label %q must be set", key)
	}
	assert.Equal(t, cacheDir, plan.Mounts[0].AbsPath)
	assert.Equal(t, constants.VLLMCacheRoot, plan.Mounts[0].Env)
	assert.False(t, plan.Mounts[0].RequiresWritable)
	assert.Equal(t, triton, plan.Mounts[1].AbsPath)
	assert.True(t, plan.Mounts[1].RequiresWritable)
}

func TestVLLMCache_MountAtOverridesCapturedRoot(t *testing.T) {
	cacheDir := t.TempDir()
	newMegaAOTCache(t, cacheDir, []string{testRank00})

	got := DetectVLLMCache(cacheDir, CaptureSpec{MountAt: "/tmp/vllm"})
	assert.NotNil(t, got)
	if got == nil {
		t.FailNow()
	}

	labels, err := got.Labels()
	must(t, err)
	assert.Equal(t, constants.VLLMCacheRoot+"=/tmp/vllm", labels[kmCacheRootEnv])
	_, ok := labels[cacheplan.LabelCacheMounts]
	assert.False(t, ok, "no extra trees means no cache-mounts label")
	_, ok = labels[cacheplan.LabelSplitCacheCapture]
	assert.False(t, ok, "no extra trees means no split-cache-capture label")
}

func TestCaptureSpecValidateRejectsUnusableCapture(t *testing.T) {
	root := t.TempDir()

	err := CaptureSpec{MountAt: "relative/path"}.Validate(root)
	assert.ErrorContains(t, err, "must be absolute")

	inside := CaptureSpec{Sources: []SourceTree{{
		PayloadName: SourceKindTriton,
		AbsPath:     filepath.Join(root, "triton"),
	}}}
	assert.ErrorContains(t, inside.Validate(root), "already captured")
	assert.ErrorContains(t, inside.Validate(root), "inside the cache root")

	outside := CaptureSpec{Sources: []SourceTree{{
		PayloadName: SourceKindTriton,
		AbsPath:     filepath.Join(filepath.Dir(root), "sibling-triton"),
	}}}
	assert.NoError(t, outside.Validate(root))

	// A path that merely shares a prefix with the root is not inside it.
	prefixed := CaptureSpec{Sources: []SourceTree{{
		PayloadName: SourceKindTriton,
		AbsPath:     root + "-triton",
	}}}
	assert.NoError(t, prefixed.Validate(root))
}

func TestCaptureSpecValidateRejectsOversizedMountLabel(t *testing.T) {
	// Shipping the payload without the label describing it would restore the
	// trees nowhere, so an unrepresentable label has to abort the capture.
	long := "/tmp/" + strings.Repeat("deep/", 40) + "triton"
	sources := make([]SourceTree, 0, 30)
	for i := range 30 {
		sources = append(sources, SourceTree{
			PayloadName: fmt.Sprintf("triton-%d", i),
			AbsPath:     fmt.Sprintf("%s-%d", long, i),
			Env:         constants.EnvTritonCacheDir,
		})
	}

	_, err := MountsLabel(sources)
	assert.ErrorContains(t, err, "byte limit")
	assert.ErrorContains(t, CaptureSpec{Sources: sources}.Validate(t.TempDir()), "byte limit")
}
