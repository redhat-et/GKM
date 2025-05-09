package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/redhat-et/TKDK/tcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

func FilePathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// HasApp checks if the host has a particular app installed and returns a boolean.
func HasApp(app string) bool {
	path, err := exec.LookPath(app)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false
		}
		return false
	}
	if path == "" {
		return false
	}

	return true
}

func CleanupTmpDirs() error {
	tmpDirPrefixes := []string{
		constants.BuildahCacheDirPrefix,
		constants.DockerCacheDirPrefix,
		constants.PodmanCacheDirPrefix,
	}

	for _, prefix := range tmpDirPrefixes {
		cmd := exec.Command("rm", "-rf", "/tmp/"+prefix)

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to delete /tmp/%s: %w", prefix, err)
		}
	}

	logging.Info("Temporary directories successfully deleted.")
	return nil
}

// SanitizeGroupJSON rewrites child_paths in a __grp__*.json file to remove any leading paths before ".triton/cache"
func SanitizeGroupJSON(filePath string) error {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	var parsed map[string]map[string]string
	if err = json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("failed to parse JSON in %s: %w", filePath, err)
	}

	paths := parsed["child_paths"]
	for key, val := range paths {
		if idx := strings.Index(val, ".triton/cache"); idx != -1 {
			paths[key] = val[idx:] // Strip prefix before .triton/cache
		}
	}

	// Marshal and overwrite file
	updated, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to re-marshal sanitized JSON: %w", err)
	}
	if err := os.WriteFile(filePath, updated, 0644); err != nil {
		return fmt.Errorf("failed to write sanitized JSON to %s: %w", filePath, err)
	}

	return nil
}

// RestoreFullPathsInGroupJSON adds full TritonCacheDir prefix to child_paths in __grp__*.json files
func RestoreFullPathsInGroupJSON(filePath, basePath string) error {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	var parsed map[string]map[string]string
	if err = json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("failed to parse JSON in %s: %w", filePath, err)
	}

	childPaths := parsed["child_paths"]
	for key, val := range childPaths {
		if strings.HasPrefix(val, ".triton/cache") {
			// Make it absolute based on actual TritonCacheDir
			childPaths[key] = filepath.Join(basePath, strings.TrimPrefix(val, ".triton/cache/"))
		}
	}

	updated, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated json: %w", err)
	}
	return os.WriteFile(filePath, updated, 0644)
}
