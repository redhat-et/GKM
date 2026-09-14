package cache

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/redhat-et/GKM/mcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

// Cache defines the minimal interface each cache implementation must satisfy
type Cache interface {
	Name() string
	EntryCount() int
	CacheSizeBytes() int64
	Summary() string
	Metadata() []CacheEntry
	Labels() (map[string]string, error)
	ManifestTag() string
	CacheTag() string
	SetTmpPath(path string)
}

type CacheEntry interface{}
type Manifest map[string][]CacheEntry
type Labels map[string]string

// DetectCaches runs detection logic and returns all valid cache backends found under a root directory
func DetectCaches(root string, spec ...CaptureSpec) []Cache {
	var caches []Cache

	if vllm := DetectVLLMCache(root, spec...); vllm != nil {
		caches = append(caches, vllm)
	} else if triton := DetectTritonCache(root); triton != nil {
		caches = append(caches, triton)
	} else if habana := DetectHabanaCache(root); habana != nil {
		caches = append(caches, habana)
	}

	return caches
}

// BuildLabels combines label maps from all caches into a single set of image labels.
// A cache that cannot render its labels (for example extra cache trees whose mount
// metadata does not fit one label) aborts the build rather than contributing a
// partial label set, since an image carrying those trees without their mount
// metadata would restore them nowhere.
func BuildLabels(caches []Cache) (Labels, error) {
	result := make(Labels)
	for _, c := range caches {
		labels, err := c.Labels()
		if err != nil {
			return nil, fmt.Errorf("%s cache: %w", c.Name(), err)
		}
		for k, v := range labels {
			result[k] = v
		}
	}
	return result, nil
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
	return copyDir(srcDir, dstDir, nil)
}

// CopyDirExcludingTopLevel copies srcDir into dstDir but skips immediate
// children of srcDir whose base names appear in skipTop.
func CopyDirExcludingTopLevel(srcDir, dstDir string, skipTop ...string) error {
	skip := make(map[string]struct{}, len(skipTop))
	for _, name := range skipTop {
		skip[name] = struct{}{}
	}
	return copyDir(srcDir, dstDir, skip)
}

func copyDir(srcDir, dstDir string, skipTop map[string]struct{}) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath != "." && skipTop != nil {
			top := relPath
			if i := strings.Index(relPath, string(os.PathSeparator)); i >= 0 {
				top = relPath[:i]
			}
			if _, skip := skipTop[top]; skip {
				if info.IsDir() {
					logging.Debugf("Skipping %s under %s (not part of the packaged cache)", top, srcDir)
					return filepath.SkipDir
				}
				return nil
			}
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

// TotalDirSize returns the total size of all non-directory files under dir.
func TotalDirSize(dir string) (int64, error) {
	return getTotalDirSize(dir)
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

// HasCacheNamed reports whether any detected cache uses the given name constant.
func HasCacheNamed(caches []Cache, name string) bool {
	for _, c := range caches {
		if c.Name() == name {
			return true
		}
	}
	return false
}

// GetTagsFromCaches returns the manifest and cache directory tags for the available cache type
func GetTagsFromCaches(caches []Cache) (manifestTag, cacheTag string, err error) {
	for _, c := range caches {
		if c.Name() == constants.VLLM || c.Name() == constants.Triton || c.Name() == constants.Habana {
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

func ExtractCacheDirectory(r io.Reader, cacheType string) (extractedDirs []string, extractedBytes int64, err error) {
	if cacheType == "" {
		return nil, 0, fmt.Errorf("cache type is empty")
	}
	switch cacheType {
	case constants.Triton:
		return ExtractTritonCacheDirectory(r)
	case constants.VLLM:
		return ExtractVLLMCacheDirectory(r)
	case constants.Habana:
		return ExtractHabanaCacheDirectory(r)
	default:
		return nil, 0, fmt.Errorf("unsupported cache type: %s", cacheType)
	}
}

// Shared extraction logic for Triton/VLLM cache and manifest directories.
// skipPayloadTop, when non-nil, drops payload paths whose first component is a
// key (used to omit vLLM runtime dirs such as modelinfos from older images).
func extractCacheAndManifestDirectory(
	r io.Reader,
	cacheDirPrefix, manifestDirPrefix, extractCacheDir, extractManifestDir string,
	skipPayloadTop map[string]struct{},
) (extractedDirs []string, extractedBytes int64, err error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse layer as tar.gz: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// Ensure top-level output directories exist once
	if err = os.MkdirAll(extractCacheDir, 0755); err != nil {
		return nil, 0, fmt.Errorf("failed to create cache directory: %w", err)
	}
	if err = os.MkdirAll(extractManifestDir, 0755); err != nil {
		return nil, 0, fmt.Errorf("failed to create manifest directory: %w", err)
	}

	for {
		h, ret := tr.Next()
		if ret == io.EOF {
			break
		} else if ret != nil {
			return nil, 0, fmt.Errorf("error reading tar archive: %w", ret)
		}

		// Skip irrelevant files
		if !strings.HasPrefix(h.Name, cacheDirPrefix) &&
			!strings.HasPrefix(h.Name, manifestDirPrefix+"manifest.json") {
			continue
		}

		// Determine output path
		var filePath string
		if strings.HasPrefix(h.Name, cacheDirPrefix) {
			rel := strings.TrimPrefix(h.Name, cacheDirPrefix)
			if rel == "" {
				continue
			}
			if skipPayloadTop != nil {
				top := rel
				if i := strings.IndexByte(rel, '/'); i >= 0 {
					top = rel[:i]
				}
				if _, skip := skipPayloadTop[top]; skip {
					continue
				}
			}
			filePath = filepath.Join(extractCacheDir, rel)

			topDir := filepath.Join(extractCacheDir, filepath.Dir(rel))
			if !stringInSlice(topDir, extractedDirs) {
				extractedDirs = append(extractedDirs, topDir)
			}
		} else if strings.HasPrefix(h.Name, manifestDirPrefix) {
			rel := strings.TrimPrefix(h.Name, manifestDirPrefix)
			filePath = filepath.Join(extractManifestDir, rel)
		}

		// Ensure parent dir exists
		if err = os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return nil, 0, fmt.Errorf("failed to create directory for %s: %w", filePath, err)
		}

		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(filePath, os.FileMode(h.Mode)); err != nil {
				return nil, 0, fmt.Errorf("failed to create directory %s: %w", filePath, err)
			}
		case tar.TypeReg:
			if err = writeFile(filePath, tr, os.FileMode(h.Mode)); err != nil {
				return nil, 0, fmt.Errorf("failed to write file %s: %w", filePath, err)
			}
			if strings.HasPrefix(h.Name, cacheDirPrefix) {
				extractedBytes += h.Size
			}
		default:
			logging.Debugf("Skipping unsupported type: %c in file %s", h.Typeflag, h.Name)
		}
	}

	return extractedDirs, extractedBytes, nil
}

// VLLMNonCacheRootSkipSet returns top-level payload directory names under
// VLLM_CACHE_ROOT that MCV does not restore on extract.
func VLLMNonCacheRootSkipSet() map[string]struct{} {
	return map[string]struct{}{
		constants.VLLMNonCacheRootDirModelInfos:   {},
		constants.VLLMNonCacheRootDirDummyCache: {},
	}
}

func stringInSlice(str string, list []string) bool {
	for _, s := range list {
		if s == str {
			return true
		}
	}
	return false
}

func writeFile(filePath string, tarReader io.Reader, mode os.FileMode) error {
	// Create any parent directories if needed
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directories for %s: %w", filePath, err)
	}

	outFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filePath, err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, tarReader); err != nil {
		return fmt.Errorf("failed to copy content to file %s: %w", filePath, err)
	}

	if err := os.Chmod(filePath, mode); err != nil {
		return fmt.Errorf("failed to set file permissions for %s: %w", filePath, err)
	}

	return nil
}
