package utils

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilePathExists(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "test_file_exists")
	os.WriteFile(tmpFile, []byte("test"), 0644)
	defer os.Remove(tmpFile)

	exists, err := FilePathExists(tmpFile)
	assert.NoError(t, err)
	assert.True(t, exists)

	notExists, err := FilePathExists("/nonexistent/path/123")
	assert.NoError(t, err)
	assert.False(t, notExists)
}

func TestHasApp(t *testing.T) {
	assert.True(t, HasApp("ls"))
	assert.False(t, HasApp("fake_app_that_does_not_exist"))
}

func TestSanitizeGroupJSONAndRestore(t *testing.T) {
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.json")

	originalJSON := `{
  "child_paths": {
    "one": "/home/user/.triton/cache/a",
    "two": "/tmp/.triton/cache/b"
  }
}`

	expectedSanitized := map[string]string{
		"one": ".triton/cache/a",
		"two": ".triton/cache/b",
	}

	err := os.WriteFile(testFile, []byte(originalJSON), 0644)
	assert.NoError(t, err)

	err = SanitizeGroupJSON(testFile)
	assert.NoError(t, err)

	// Read back and verify
	sanitized := map[string]map[string]string{}
	content, err := os.ReadFile(testFile)
	assert.NoError(t, err)
	assert.NoError(t, json.Unmarshal(content, &sanitized))
	assert.Equal(t, expectedSanitized, sanitized["child_paths"])

	// Now restore with fake base path
	basePath := "/mnt/fake"
	err = RestoreFullPathsInGroupJSON(testFile, basePath)
	assert.NoError(t, err)

	restored := map[string]map[string]string{}
	content, err = os.ReadFile(testFile)
	assert.NoError(t, err)
	assert.NoError(t, json.Unmarshal(content, &restored))
	assert.Equal(t, filepath.Join(basePath, "a"), restored["child_paths"]["one"])
	assert.Equal(t, filepath.Join(basePath, "b"), restored["child_paths"]["two"])
}

func TestCleanupMCVDirs(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "mcv_test_cleanup")

	os.MkdirAll(testDir, 0755)

	err := CleanupMCVDirs(context.Background(), testDir)
	assert.NoError(t, err)

	_, statErr := os.Stat(testDir)
	assert.True(t, os.IsNotExist(statErr))
}
