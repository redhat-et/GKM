package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/redhat-et/GKM/mcv/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestCopyDirExcludingTopLevelSkipsRuntimeDirs(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	writeTestFile(t, filepath.Join(src, constants.TorchCompileDir, "hash", "file"), []byte("x"))
	writeTestFile(t, filepath.Join(src, constants.VLLMNonCacheRootDirModelInfos, "m.json"), []byte("{}"))
	writeTestFile(t, filepath.Join(src, constants.VLLMNonCacheRootDirDummyCache, "d"), []byte("d"))

	err := CopyDirExcludingTopLevel(src, dst,
		constants.VLLMNonCacheRootDirModelInfos,
		constants.VLLMNonCacheRootDirDummyCache,
	)
	assert.NoError(t, err)

	assert.FileExists(t, filepath.Join(dst, constants.TorchCompileDir, "hash", "file"))
	_, err = os.Stat(filepath.Join(dst, constants.VLLMNonCacheRootDirModelInfos, "m.json"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(dst, constants.VLLMNonCacheRootDirDummyCache, "d"))
	assert.True(t, os.IsNotExist(err))
}
