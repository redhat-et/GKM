package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

// FilePathExists checks if the given file or directory exists.
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

// HasApp checks if the given app is available in the system PATH.
func HasApp(app string) bool {
	_, err := exec.LookPath(app)
	return err == nil
}

// NormalizeImageTag ensures a mutable image reference has an explicit tag.
// Uses go-containerregistry parsing so host:port registries (e.g. localhost:5000/foo)
// correctly receive ":latest", while digests, already-tagged refs, and short names
// (e.g. "foo" → "foo:latest") keep their original form.
func NormalizeImageTag(imageName string) string {
	if imageName == "" || strings.Contains(imageName, "@") {
		return imageName
	}

	ref, err := name.ParseReference(imageName, name.WeakValidation)
	if err != nil {
		if !strings.Contains(imageName, ":") {
			return imageName + ":latest"
		}
		return imageName
	}
	if _, isDigest := ref.(name.Digest); isDigest {
		return imageName
	}
	tag, ok := ref.(name.Tag)
	if !ok {
		return imageName
	}
	// Input already included an explicit tag.
	if strings.HasSuffix(imageName, ":"+tag.TagStr()) {
		return imageName
	}
	// ParseReference applied the default tag — append it to the original string
	// so short names and host:port refs are preserved.
	return imageName + ":" + tag.TagStr()
}

// CleanupMCVDirs removes the temporary MCV directory using os.RemoveAll.
func CleanupMCVDirs(ctx context.Context, path string) error {
	if path == "" {
		path = constants.MCVBuildDir
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to delete %s: %w", path, err)
	}
	logging.Debugf("Directory %s successfully deleted.", path)
	return nil
}

// SanitizeGroupJSON strips leading paths before ".triton/cache" in __grp__*.json child_paths.
func SanitizeGroupJSON(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	var parsed map[string]map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("failed to parse JSON in %s: %w", filePath, err)
	}

	for key, val := range parsed["child_paths"] {
		if idx := strings.Index(val, ".triton/cache"); idx != -1 {
			parsed["child_paths"][key] = val[idx:]
		}
	}

	return writeFormattedJSON(filePath, parsed)
}

// writeFormattedJSON writes the given data as pretty-formatted JSON to a file.
func writeFormattedJSON(filePath string, data interface{}) error {
	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	if err := os.WriteFile(filePath, formatted, 0644); err != nil {
		return fmt.Errorf("failed to write JSON to %s: %w", filePath, err)
	}
	return nil
}
