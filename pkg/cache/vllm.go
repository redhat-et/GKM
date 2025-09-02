package cache

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/redhat-et/MCU/mcv/pkg/constants"
	"github.com/redhat-et/MCU/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
)

var hashDirRegex = regexp.MustCompile(`^[a-f0-9]{32}$`) // Adjust the regex as needed

const (
	cacheVLLMImagePrefix     = "cache.vllm.image"
	cacheVLLMImageEntryCount = cacheVLLMImagePrefix + "/entry-count"
	cacheVLLMImageCacheSize  = cacheVLLMImagePrefix + "/cache-size-bytes"
	cacheVLLMImageSummary    = cacheVLLMImagePrefix + "/summary"
)

// VLLMCache represents a VLLM-style compile cache (e.g., torch_inductor or fxgraph)
type VLLMCache struct {
	rootPath    string
	tmpPath     string
	count       int
	tritonCache *TritonCache
	allMetadata []VLLMCacheMetadata
}

type VLLMCacheMetadata struct {
	VllmHash           string       `json:"vllmHash"`
	TritonCacheEntries []CacheEntry `json:"triton"`
}

// DetectVLLMCache walks the given root directory to detect whether VLLM-style cache artifacts exist
func DetectVLLMCache(cacheDir string) *VLLMCache {
	found := false
	var torchCompileCachePath string
	metadata := []VLLMCacheMetadata{}
	var tc *TritonCache

	err := filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.Contains(path, "vendor") || strings.HasPrefix(d.Name(), ".")) {
			return fs.SkipDir
		}

		name := d.Name()
		if strings.HasSuffix(name, "vllm_compile_cache.py") ||
			strings.Contains(path, "inductor_cache") ||
			strings.Contains(path, "fxgraph") {
			found = true
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		logging.WithError(err).Warnf("Error walking vllm cache directory: %s", cacheDir)
		return nil
	}

	var count int
	if found {
		torchCompileCachePath = filepath.Join(cacheDir, "torch_compile_cache")
		if _, err := os.Stat(torchCompileCachePath); os.IsNotExist(err) {
			logging.Warnf("Torch compile cache path does not exist: %s", torchCompileCachePath)
			return nil
		}
		entries, err := os.ReadDir(torchCompileCachePath)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				count++
				var tritonCachePath string
				err := filepath.WalkDir(filepath.Join(torchCompileCachePath, entry.Name()), func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() && d.Name() == "triton_cache" {
						tritonCachePath = path
						return filepath.SkipDir
					}
					return nil
				})
				if err != nil || tritonCachePath == "" {
					logging.Warnf("Triton cache path not found for entry: %s", entry.Name())
					continue
				}

				// Check if tritonCachePath exists
				if _, err := os.Stat(tritonCachePath); os.IsNotExist(err) {
					logging.Warnf("Triton cache path does not exist: %s", tritonCachePath)
					continue
				}

				logging.Debugf("Inspecting potential Triton cache at: %s", tritonCachePath)
				_tc := DetectTritonCache(tritonCachePath)
				if _tc == nil {
					logging.Warnf("Failed to detect Triton cache at: %s", tritonCachePath)
					continue
				}
				tc = _tc
				vllmMetadata := VLLMCacheMetadata{
					VllmHash:           entry.Name(),
					TritonCacheEntries: tc.Metadata(),
				}

				logging.Debugf("Adding VLLM metadata: %+v", vllmMetadata)
				metadata = append(metadata, vllmMetadata)
			}
		}
	}

	if found {
		return &VLLMCache{
			rootPath:    cacheDir,
			tritonCache: tc,
			count:       count,
			allMetadata: metadata,
		}
	}
	return nil
}

func (v *VLLMCache) Name() string { return "vllm" }

func (v *VLLMCache) EntryCount() int {
	return v.count
}

func (v *VLLMCache) CacheSizeBytes() int64 {
	size, _ := getTotalDirSize(v.rootPath)
	return size
}

func (v *VLLMCache) Summary() string {
	// The summary should include the target hardware summary from the Triton cache
	// as well as any relevant VLLM-specific details (if applicable)
	// For now, we only include the Triton summary if available
	var summary *Summary
	var err error

	if v.tritonCache != nil && len(v.tritonCache.allMetadata) > 0 {
		logging.Debugf("Building VLLM summary from Triton metadata")
		tempSummary, tempErr := BuildTritonSummary(v.tritonCache.allMetadata)
		if tempErr != nil {
			logging.WithError(tempErr).Error("failed to build vLLM summary")
			return ""
		}
		summary = tempSummary
	}

	jsonData, err := json.Marshal(summary)
	if err != nil {
		logging.WithError(err).Error("failed to marshal vLLM summary")
		return ""
	}

	logging.Debugf("VLLM Summary: %s", string(jsonData))

	return string(jsonData)
}

func (v *VLLMCache) Labels() map[string]string {
	return map[string]string{
		cacheVLLMImageEntryCount: strconv.Itoa(v.EntryCount()),
		cacheVLLMImageCacheSize:  strconv.FormatInt(v.CacheSizeBytes(), 10),
		cacheVLLMImageSummary:    v.Summary(),
	}
}

func (v *VLLMCache) Metadata() []CacheEntry {
	entries := make([]CacheEntry, 0, len(v.allMetadata))
	for _, meta := range v.allMetadata {
		entries = append(entries, meta)
	}
	return entries
}

func (v *VLLMCache) ManifestTag() string {
	return fmt.Sprintf("./%s", constants.MCVVLLMManifestDir)
}

func (v *VLLMCache) CacheTag() string {
	return fmt.Sprintf("./%s", constants.MCVVLLMCacheDir)
}

func (v *VLLMCache) SetTmpPath(path string) {
	if path != "" {
		v.tmpPath = path
	}
}

// Extracts the vllm cache and manifest in a given reader for tar.gz.
// This is only used for *compat* variant.
func ExtractVLLMCacheDirectory(r io.Reader) ([]string, error) {
	var extractedDirs []string
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layer as tar.gz: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// Ensure top-level output directories exist once
	if err = os.MkdirAll(constants.ExtractCacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}
	if err = os.MkdirAll(constants.ExtractManifestDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create manifest directory: %w", err)
	}

	for {
		h, ret := tr.Next()
		if ret == io.EOF {
			break
		} else if ret != nil {
			return nil, fmt.Errorf("error reading tar archive: %w", ret)
		}

		// Skip irrelevant files
		if !strings.HasPrefix(h.Name, constants.MCVVLLMCacheDir) &&
			!strings.HasPrefix(h.Name, "io.vllm.manifest/manifest.json") {
			continue
		}

		// Determine output path
		var filePath string
		if strings.HasPrefix(h.Name, constants.MCVVLLMCacheDir) {
			rel := strings.TrimPrefix(h.Name, constants.MCVVLLMCacheDir)
			if rel == "" {
				continue
			}
			filePath = filepath.Join(constants.ExtractCacheDir, rel)

			topDir := filepath.Join(constants.ExtractCacheDir, filepath.Dir(rel))
			if !stringInSlice(topDir, extractedDirs) {
				extractedDirs = append(extractedDirs, topDir)
			}
		} else if strings.HasPrefix(h.Name, "io.vllm.manifest/") {
			rel := strings.TrimPrefix(h.Name, "io.vllm.manifest/")
			filePath = filepath.Join(constants.ExtractManifestDir, rel)
		}

		// Ensure parent dir exists
		if err = os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", filePath, err)
		}

		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(filePath, os.FileMode(h.Mode)); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", filePath, err)
			}
		case tar.TypeReg:
			if err = writeFile(filePath, tr, os.FileMode(h.Mode)); err != nil {
				return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
			}
		default:
			logging.Debugf("Skipping unsupported type: %c in file %s", h.Typeflag, h.Name)
		}
	}

	// Fix up cache JSONs
	err = filepath.Walk(constants.ExtractCacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasPrefix(info.Name(), "__grp__") && strings.HasSuffix(info.Name(), ".json") {
			if err := utils.RestoreFullPathsInGroupJSON(path, constants.ExtractCacheDir); err != nil {
				logging.Warnf("failed to restore full paths in %s: %v", path, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error restoring full paths in cache JSON files: %w", err)
	}

	return extractedDirs, nil
}
