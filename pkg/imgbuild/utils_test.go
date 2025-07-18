package imgbuild

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateDockerfile(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "Dockerfile")

	err := GenerateDockerfile("myorg/myimage:1.0", "cacheLayer", "manifestLayer", outputPath)
	assert.NoError(t, err)

	content, err := os.ReadFile(outputPath)
	assert.NoError(t, err)
	assert.Contains(t, string(content), "FROM scratch")
	assert.Contains(t, string(content), "COPY \"./cacheLayer.")
	assert.Contains(t, string(content), "COPY \"./manifestLayer/manifest.json")
}

func TestCleanupDirs(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "dummy.txt")
	err := os.WriteFile(testFile, []byte("dummy"), 0644)
	assert.NoError(t, err)

	CleanupDirs(tmpDir)

	_, err = os.Stat(tmpDir)
	assert.True(t, os.IsNotExist(err) || err != nil)
}

func TestCleanupWithTimeout(t *testing.T) {
	start := time.Now()
	err := CleanupWithTimeout()
	duration := time.Since(start)

	// should complete quickly unless CleanupMCVDirs is slow
	assert.NoError(t, err)
	assert.Less(t, duration.Milliseconds(), int64(5000))
}
