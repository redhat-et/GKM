package cache

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Cache defines the minimal interface each cache implementation must satisfy
type Cache interface {
	Name() string
	EntryCount() int
	CacheSizeBytes() int64
	Summary() string
	Metadata() []CacheEntry
	Labels() map[string]string
	ManifestTag() string
	CacheTag() string
	SetTmpPath(path string)
}

type CacheEntry interface{}
type Manifest map[string][]CacheEntry
type Labels map[string]string

// DetectCaches runs detection logic and returns all valid cache backends found under a root directory
func DetectCaches(root string) []Cache {
	var caches []Cache

	if triton := DetectTritonCache(root); triton != nil {
		caches = append(caches, triton)
	}

	if vllm := DetectVLLMCache(root); vllm != nil {
		caches = append(caches, vllm)
	}

	return caches
}

// BuildLabels combines label maps from all caches into a single set of image labels
func BuildLabels(caches []Cache) Labels {
	result := make(Labels)
	for _, c := range caches {
		for k, v := range c.Labels() {
			result[k] = v
		}
	}
	return result
}

// BuildManifest collects all cache metadata grouped by backend name
func BuildManifest(caches []Cache) Manifest {
	result := make(Manifest)
	for _, c := range caches {
		result[c.Name()] = c.Metadata()
	}
	return result
}

// WriteManifest marshals the manifest into a JSON file at the given path
func WriteManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}
	return nil
}

// CopyDir performs a native recursive copy of srcDir into dstDir
func CopyDir(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

// getTotalDirSize returns the total size of all non-directory files in a directory
func getTotalDirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// CacheTypes returns a flat list of cache names (e.g., ["triton", "vllm"])
func CacheTypes(caches []Cache) []string {
	names := make([]string, len(caches))
	for i, c := range caches {
		names[i] = c.Name()
	}
	return names
}

// GetTagsFromCaches returns the manifest and cache directory tags for the available cache type
func GetTagsFromCaches(caches []Cache) (manifestTag, cacheTag string, err error) {
	for _, c := range caches {
		if c.Name() == "vllm" || c.Name() == "triton" {
			return c.ManifestTag(), c.CacheTag(), nil
		}
	}
	return "", "", fmt.Errorf("no supported cache type found")
}

// SetCachesBuildDir sets a common tmp/staging path for all cache instances
func SetCachesBuildDir(caches []Cache, path string) {
	if path != "" {
		for _, c := range caches {
			c.SetTmpPath(path)
		}
	}
}
