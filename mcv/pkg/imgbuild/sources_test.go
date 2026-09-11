package imgbuild

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/redhat-et/GKM/mcv/pkg/cache"
	"github.com/stretchr/testify/assert"
)

// Extra cache trees have to ride inside the existing payload prefix: a second
// builder.Add or Dockerfile COPY would add a layer, and cosign signatures over
// multi-layer images are unreliable.
func TestPrepareBuildContextStagesExtraTreesInSingleLayer(t *testing.T) {
	srcRoot := t.TempDir()
	hashDir := "0123456789abcdef0123456789abcdef"

	cacheDir := filepath.Join(srcRoot, "vllm")
	writeTestFile(t, filepath.Join(cacheDir, "torch_compile_cache", "torch_aot_compile", hashDir, "rank_0_0", "model"))
	tritonDir := filepath.Join(srcRoot, "triton")
	writeTestFile(t, filepath.Join(tritonDir, "__grp__kernel.json"))

	sources, err := cache.DetectSourceTrees([]string{tritonDir})
	assert.NoError(t, err)
	if !assert.Len(t, sources, 1) {
		t.FailNow()
	}

	prep, err := prepareBuildContext("singlelayer", cacheDir, cache.CaptureSpec{Sources: sources})
	assert.NoError(t, err)
	if prep == nil {
		t.FailNow()
	}
	t.Cleanup(func() { CleanupDirs(prep.CacheBuildDir, prep.ManifestBuildDir) })

	assert.FileExists(t, filepath.Join(prep.CacheBuildDir, "torch_compile_cache", "torch_aot_compile", hashDir, "rank_0_0", "model"))
	assert.FileExists(t, filepath.Join(prep.CacheBuildDir, "triton", "__grp__kernel.json"),
		"extra tree must be staged inside the payload prefix")

	img, err := schema2ImageFromBuildContext(prep, "quay.io/example/cache:latest")
	assert.NoError(t, err)
	if prep.TempLayerFile != "" {
		t.Cleanup(func() { os.Remove(prep.TempLayerFile) })
	}

	manifest, err := img.Manifest()
	assert.NoError(t, err)
	assert.Len(t, manifest.Layers, 1, "image must stay single-layered for cosign")

	paths := testLayerPaths(t, img)
	assert.Contains(t, paths, "io.vllm.cache/torch_compile_cache/torch_aot_compile/"+hashDir+"/rank_0_0/model")
	assert.Contains(t, paths, "io.vllm.cache/triton/__grp__kernel.json")
	assert.Contains(t, paths, "io.vllm.manifest/manifest.json")
}

func TestPrepareBuildContextWithoutSourcesIsUnchanged(t *testing.T) {
	srcRoot := t.TempDir()
	cacheDir := filepath.Join(srcRoot, "vllm")
	writeTestFile(t, filepath.Join(cacheDir, "torch_compile_cache", "torch_aot_compile", "0123456789abcdef0123456789abcdef", "rank_0_0", "model"))

	prep, err := prepareBuildContext("nosources", cacheDir)
	assert.NoError(t, err)
	if prep == nil {
		t.FailNow()
	}
	t.Cleanup(func() { CleanupDirs(prep.CacheBuildDir, prep.ManifestBuildDir) })

	entries, err := os.ReadDir(prep.CacheBuildDir)
	assert.NoError(t, err)
	if !assert.Len(t, entries, 1) {
		t.FailNow()
	}
	assert.Equal(t, "torch_compile_cache", entries[0].Name())
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testLayerPaths(t *testing.T, img v1.Image) []string {
	t.Helper()

	layers, err := img.Layers()
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 {
		t.Fatalf("image has %d layers, want 1", len(layers))
	}

	rc, err := layers[0].Compressed()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	gz, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()

	var paths []string
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, header.Name)
	}
	return paths
}
