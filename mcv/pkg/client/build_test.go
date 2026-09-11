package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/redhat-et/GKM/mcv/pkg/cacheplan"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	"github.com/stretchr/testify/assert"
)

const testImage = "quay.io/example/cache:v1"

func TestBuildCacheValidatesBeforeBuilding(t *testing.T) {
	cacheDir := t.TempDir()

	err := BuildCache(BuildOptions{CacheDir: cacheDir})
	assert.ErrorContains(t, err, "image name must be specified")

	err = BuildCache(BuildOptions{ImageName: "invalid image name", CacheDir: cacheDir})
	assert.ErrorContains(t, err, "error validating image name")

	err = BuildCache(BuildOptions{ImageName: testImage})
	assert.ErrorContains(t, err, "cache directory must be specified")

	err = BuildCache(BuildOptions{
		ImageName: testImage,
		CacheDir:  filepath.Join(cacheDir, "missing"),
	})
	assert.ErrorContains(t, err, "does not exist")

	// Unusable sources are reported before any builder is touched, so a bad
	// --source cannot silently produce an image missing the Triton tree.
	err = BuildCache(BuildOptions{
		ImageName: testImage,
		CacheDir:  cacheDir,
		Sources:   []string{filepath.Join(cacheDir, "missing")},
	})
	assert.ErrorContains(t, err, "not readable")

	// Only the builder selection is left, which is deterministic for a bogus name.
	err = BuildCache(BuildOptions{
		ImageName: testImage,
		CacheDir:  cacheDir,
		Builder:   "bogus",
	})
	assert.ErrorContains(t, err, "unsupported builder")
}

func TestInspectCachePlanRejectsInvalidImage(t *testing.T) {
	_, err := InspectCachePlan("invalid-image-name")
	assert.Error(t, err)
}

func TestIsUnsupportedCacheType(t *testing.T) {
	assert.True(t, IsUnsupportedCacheType(&cacheplan.UnsupportedCacheTypeError{CacheType: constants.Triton}))
	assert.False(t, IsUnsupportedCacheType(nil))
}

func TestPlaceExtraTreesMovesTreeToRecordedPath(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "staged", "triton")

	extractDir := filepath.Join(root, "extract")
	writeTestFile(t, filepath.Join(extractDir, "torch_compile_cache", "torch_aot_compile", "model"))
	writeTestFile(t, filepath.Join(extractDir, "triton", "__grp__kernel.json"))

	labels := testLabels(dst)
	assert.NoError(t, placeExtraTrees(labels, extractDir))

	assert.FileExists(t, filepath.Join(dst, "__grp__kernel.json"))
	assert.NoDirExists(t, filepath.Join(extractDir, "triton"))
	assert.DirExists(t, filepath.Join(extractDir, "torch_compile_cache"),
		"the primary tree must stay where it was extracted")
}

func TestPlaceExtraTreesRefusesToOverwriteExistingTree(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "tmp", "triton")
	extractDir := filepath.Join(root, "extract")
	writeTestFile(t, filepath.Join(extractDir, "triton", "__grp__kernel.json"))
	writeTestFile(t, filepath.Join(dst, "stale.json"))

	err := placeExtraTrees(testLabels(dst), extractDir)
	assert.ErrorContains(t, err, "already holds")
	assert.FileExists(t, filepath.Join(extractDir, "triton", "__grp__kernel.json"),
		"source must survive a refused placement")
}

func TestPlaceExtraTreesIgnoresImagesWithoutPlan(t *testing.T) {
	extractDir := t.TempDir()
	writeTestFile(t, filepath.Join(extractDir, "kernel.bin"))

	labels := map[string]string{"cache.triton.image/summary": `{"targets":[]}`}
	assert.NoError(t, placeExtraTrees(labels, extractDir))
}

func testLabels(tritonPath string) map[string]string {
	extras, err := json.Marshal([]cacheplan.Mount{
		{SubPath: "triton", AbsPath: tritonPath, Env: constants.EnvTritonCacheDir, RequiresWritable: true},
	})
	if err != nil {
		panic(err)
	}
	return map[string]string{
		cacheplan.LabelCacheType:    constants.CacheTypeVLLMTorchCompile,
		cacheplan.LabelCacheRootEnv: constants.VLLMCacheRoot + "=/tmp/vllm",
		cacheplan.LabelCacheMounts:  string(extras),
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	assert.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	assert.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
}
