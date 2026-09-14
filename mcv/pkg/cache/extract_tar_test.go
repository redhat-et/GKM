package cache

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/redhat-et/GKM/mcv/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestExtractVLLMCacheDirectoryLayout(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	constants.VLLMExtractPrimaryDir = constants.VLLM
	constants.VLLMExtractPrimaryTop = constants.TorchCompileDir

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	assert.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "io.vllm.cache/torch_compile_cache/hash/x",
		Mode: 0644,
		Size: 3,
	}))
	_, err := tw.Write([]byte("abc"))
	assert.NoError(t, err)
	assert.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "io.vllm.cache/triton/KERNEL/hash",
		Mode: 0644,
		Size: 3,
	}))
	_, err = tw.Write([]byte("xyz"))
	assert.NoError(t, err)
	assert.NoError(t, tw.Close())
	assert.NoError(t, gw.Close())

	root := t.TempDir()
	constants.ExtractCacheDir = root
	constants.ExtractManifestDir = filepath.Join(root, "manifest")

	_, _, err = ExtractVLLMCacheDirectory(&buf)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(root, constants.VLLM, "torch_compile_cache", "hash", "x"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "triton", "KERNEL", "hash"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "torch_compile_cache"))
	assert.True(t, os.IsNotExist(err))
}

func TestPayloadRelFromCachePrefix(t *testing.T) {
	rel, ok := payloadRelFromCachePrefix("io.vllm.cache/torch_compile_cache/x", "io.vllm.cache")
	assert.True(t, ok)
	assert.Equal(t, "torch_compile_cache/x", rel)

	rel, ok = payloadRelFromCachePrefix("./io.vllm.cache/triton/y", "./io.vllm.cache")
	assert.True(t, ok)
	assert.Equal(t, "triton/y", rel)
}
