package cache

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/redhat-et/GKM/mcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

var hashDirRegex = regexp.MustCompile(`^[a-f0-9]{32}$`) // Adjust the regex as needed

const (
	cacheVLLMImagePrefix     = "cache.vllm.image"
	cacheVLLMImageEntryCount = cacheVLLMImagePrefix + "/entry-count"
	cacheVLLMImageCacheSize  = cacheVLLMImagePrefix + "/cache-size-bytes"
	cacheVLLMImageSummary    = cacheVLLMImagePrefix + "/summary"
	cacheVLLMImageFormat     = cacheVLLMImagePrefix + "/format"

	// Cache format constants
	BinaryCacheFormat = "binary"
	CUDABackend       = "cuda"

	// torchAOTCompileDirName is the extra directory vLLM introduces above
	// the per-model hash dir when VLLM_USE_AOT_COMPILE is enabled.
	torchAOTCompileDirName = "torch_aot_compile"
	// megaAOTSaveFormat marks a BinaryCacheMetadata entry as produced by
	// the mega-AOT flow (single bundled "model" blob per rank dir).
	megaAOTSaveFormat = "mega-aot"
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
	VllmHash           string                `json:"vllmHash"`
	CacheFormat        string                `json:"cacheFormat"` // "triton" or "binary"
	TritonCacheEntries []CacheEntry          `json:"triton,omitempty"`
	BinaryCacheEntries []BinaryCacheMetadata `json:"binary,omitempty"`
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
			strings.Contains(path, "fxgraph") ||
			strings.Contains(path, torchAOTCompileDirName) {
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

				// Mega-AOT layout wraps per-model hash dirs under
				// torch_aot_compile/. Recurse one level and treat each
				// child as a hash dir.
				if entry.Name() == torchAOTCompileDirName {
					aotMeta, aotCount := detectMegaAOTEntries(
						filepath.Join(torchCompileCachePath, entry.Name()),
					)
					metadata = append(metadata, aotMeta...)
					count += aotCount
					continue
				}

				count++
				hashDir := filepath.Join(torchCompileCachePath, entry.Name())

				// Try to detect binary cache format first (newer format)
				binaryCacheData, binaryErr := detectBinaryCache(hashDir)
				if binaryErr == nil && len(binaryCacheData) > 0 {
					logging.Debugf("Detected binary cache format for hash: %s", entry.Name())
					vllmMetadata := VLLMCacheMetadata{
						VllmHash:           entry.Name(),
						CacheFormat:        BinaryCacheFormat,
						BinaryCacheEntries: binaryCacheData,
					}
					logging.Debugf("Adding VLLM binary cache metadata: %+v", vllmMetadata)
					metadata = append(metadata, vllmMetadata)
					continue
				}

				// Fall back to triton cache format (older format)
				var tritonCachePath string
				err := filepath.WalkDir(hashDir, func(path string, d fs.DirEntry, err error) error {
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
					logging.Warnf("Neither binary cache nor triton cache found for entry: %s", entry.Name())
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
					CacheFormat:        "triton",
					TritonCacheEntries: tc.Metadata(),
				}

				logging.Debugf("Adding VLLM triton cache metadata: %+v", vllmMetadata)
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

// detectBinaryCache detects binary cache format in a hash directory
// It looks for rank_X_Y directories containing binary cache artifacts
func detectBinaryCache(hashDir string) ([]BinaryCacheMetadata, error) {
	var binaryCaches []BinaryCacheMetadata

	entries, err := os.ReadDir(hashDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read hash directory: %w", err)
	}

	// Look for rank_X_Y directories
	rankDirRegex := regexp.MustCompile(`^rank_\d+_\d+$`)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !rankDirRegex.MatchString(entry.Name()) {
			continue
		}

		rankPath := filepath.Join(hashDir, entry.Name())
		logging.Debugf("Inspecting rank directory: %s", rankPath)

		// Look for prefix directories (e.g., backbone, eagle_head)
		prefixEntries, err := os.ReadDir(rankPath)
		if err != nil {
			logging.Warnf("Failed to read rank directory %s: %v", rankPath, err)
			continue
		}

		for _, prefixEntry := range prefixEntries {
			if !prefixEntry.IsDir() {
				continue
			}

			prefixPath := filepath.Join(rankPath, prefixEntry.Name())
			logging.Debugf("Inspecting prefix directory: %s", prefixPath)

			// Check for binary cache indicators:
			// 1. cache_key_factors.json
			// 2. artifact_compile_range_* files
			cacheKeyPath := filepath.Join(prefixPath, "cache_key_factors.json")
			if _, err := os.Stat(cacheKeyPath); os.IsNotExist(err) {
				logging.Debugf("No cache_key_factors.json in %s, skipping", prefixPath)
				continue
			}

			// Read cache key factors
			var keyFactors CacheKeyFactors
			data, err := os.ReadFile(cacheKeyPath)
			if err != nil {
				logging.Warnf("Failed to read cache_key_factors.json: %v", err)
				continue
			}
			if unmarshalErr := json.Unmarshal(data, &keyFactors); unmarshalErr != nil {
				logging.Warnf("Failed to parse cache_key_factors.json: %v", unmarshalErr)
				continue
			}

			// Count and collect artifact files
			var artifacts []string
			artifactRegex := regexp.MustCompile(`^artifact_compile_range_`)
			prefixFiles, err := os.ReadDir(prefixPath)
			if err != nil {
				logging.Warnf("Failed to read prefix directory %s: %v", prefixPath, err)
				continue
			}

			// Detect actual format by inspecting the first artifact
			cacheSaveFormat := BinaryCacheFormat
			foundFirstArtifact := false

			for _, file := range prefixFiles {
				if artifactRegex.MatchString(file.Name()) {
					artifacts = append(artifacts, file.Name())

					// Detect format from the first artifact: directory = unpacked, file = binary
					if !foundFirstArtifact {
						if file.IsDir() {
							cacheSaveFormat = "unpacked"
						} else {
							cacheSaveFormat = "binary"
						}
						foundFirstArtifact = true
					}
				}
			}

			if len(artifacts) == 0 {
				logging.Debugf("No binary artifacts found in %s, skipping", prefixPath)
				continue
			}

			// Extract target device (could be cuda, rocm, tpu, cpu, etc.)
			targetDevice := ""
			if env, ok := keyFactors.Env["VLLM_TARGET_DEVICE"]; ok {
				if device, ok := env.(string); ok {
					targetDevice = device
				}
			}

			binaryCache := BinaryCacheMetadata{
				Rank:            entry.Name(),
				Prefix:          prefixEntry.Name(),
				ArtifactCount:   len(artifacts),
				ArtifactNames:   artifacts,
				CodeHash:        keyFactors.CodeHash,
				ConfigHash:      keyFactors.ConfigHash,
				CompilerHash:    keyFactors.CompilerHash,
				CacheSaveFormat: cacheSaveFormat,
				TargetDevice:    targetDevice,
				Env:             keyFactors.Env,
			}

			logging.Debugf("Found binary cache: %+v", binaryCache)
			binaryCaches = append(binaryCaches, binaryCache)
		}
	}

	if len(binaryCaches) == 0 {
		return nil, fmt.Errorf("no binary cache detected")
	}

	return binaryCaches, nil
}

// detectMegaAOTEntries walks torch_aot_compile/ and returns metadata for
// each child hash dir that contains a mega-AOT bundle. The second return
// value is the number of hash directories considered (whether or not they
// yielded valid metadata), so the caller can keep its entry count in sync.
func detectMegaAOTEntries(aotDir string) ([]VLLMCacheMetadata, int) {
	entries, err := os.ReadDir(aotDir)
	if err != nil {
		logging.Warnf("Failed to read %s: %v", aotDir, err)
		return nil, 0
	}

	var out []VLLMCacheMetadata
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		count++
		hashDir := filepath.Join(aotDir, entry.Name())
		megaData, megaErr := detectMegaAOTCache(hashDir)
		if megaErr != nil || len(megaData) == 0 {
			logging.Warnf("No mega-AOT artifacts in %s: %v", hashDir, megaErr)
			continue
		}
		logging.Debugf("Detected mega-AOT cache for hash: %s", entry.Name())
		out = append(out, VLLMCacheMetadata{
			VllmHash:           entry.Name(),
			CacheFormat:        BinaryCacheFormat,
			BinaryCacheEntries: megaData,
		})
	}
	return out, count
}

// detectMegaAOTCache detects the mega-AOT bundle layout in a hash directory.
// The layout places one bundled artifact at {hashDir}/rank_X_Y/model, with
// inductor/triton state as a shared sibling at {hashDir}/inductor_cache/.
// Unlike the per-piecewise binary format, no cache_key_factors.json is
// emitted, so hash/env fields are left empty.
func detectMegaAOTCache(hashDir string) ([]BinaryCacheMetadata, error) {
	entries, err := os.ReadDir(hashDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read hash directory: %w", err)
	}

	var out []BinaryCacheMetadata
	rankDirRegex := regexp.MustCompile(`^rank_\d+_\d+$`)
	for _, entry := range entries {
		if !entry.IsDir() || !rankDirRegex.MatchString(entry.Name()) {
			continue
		}
		modelPath := filepath.Join(hashDir, entry.Name(), "model")
		info, err := os.Stat(modelPath)
		if err != nil || info.IsDir() {
			continue
		}
		out = append(out, BinaryCacheMetadata{
			Rank:            entry.Name(),
			ArtifactCount:   1,
			ArtifactNames:   []string{"model"},
			CacheSaveFormat: megaAOTSaveFormat,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no mega-AOT artifacts detected")
	}
	return out, nil
}

func (v *VLLMCache) Name() string { return constants.VLLM }

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
	var summary *Summary
	var err error

	// Check if we have binary cache metadata
	hasBinaryCache := false
	for _, meta := range v.allMetadata {
		if meta.CacheFormat == BinaryCacheFormat && len(meta.BinaryCacheEntries) > 0 {
			hasBinaryCache = true
			break
		}
	}

	if hasBinaryCache {
		logging.Debugf("Building VLLM summary from binary cache metadata")
		summary, err = buildBinaryCacheSummary(v.allMetadata)
		if err != nil {
			logging.WithError(err).Error("failed to build binary cache summary")
			return ""
		}
	} else if v.tritonCache != nil && len(v.tritonCache.allMetadata) > 0 {
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

// buildBinaryCacheSummary builds a summary from binary cache metadata
func buildBinaryCacheSummary(metadata []VLLMCacheMetadata) (*Summary, error) {
	targetMap := make(map[string]SummaryTargetInfo)

	for _, meta := range metadata {
		if meta.CacheFormat != BinaryCacheFormat {
			continue
		}

		for i := range meta.BinaryCacheEntries {
			binaryCache := &meta.BinaryCacheEntries[i]
			// Extract target info from the stored environment variables
			backend := binaryCache.TargetDevice
			if backend == "" {
				backend = CUDABackend // Default if not specified
			}

			// Determine arch and warpSize based on backend and env vars
			arch := "unknown"
			warpSize := 32 // Default for CUDA

			switch backend {
			case "rocm", "hip":
				warpSize = 64 // AMD GPUs use 64-wide wavefronts
				// Try to extract GPU architecture from env
				if env, ok := binaryCache.Env["VLLM_ROCM_CUSTOM_PAGED_ATTN"]; ok && env != nil {
					// ROCm is being used
					arch = "gfx90a" // Common MI250/MI300 arch, could be extracted more precisely
				}
			case "cuda":
				// Try to extract CUDA architecture
				if mainVersion, ok := binaryCache.Env["VLLM_MAIN_CUDA_VERSION"]; ok {
					if version, ok := mainVersion.(string); ok {
						arch = "sm_" + version
					}
				}
			case "tpu":
				warpSize = 128 // TPU uses different parallelism model
			case "cpu":
				warpSize = 1 // CPU doesn't have warp concept
			}

			key := fmt.Sprintf("%s-%s-%d", backend, arch, warpSize)
			if _, exists := targetMap[key]; !exists {
				targetMap[key] = SummaryTargetInfo{
					Backend:  backend,
					Arch:     arch,
					WarpSize: warpSize,
				}
			}
		}
	}

	targets := make([]SummaryTargetInfo, 0, len(targetMap))
	for _, target := range targetMap {
		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets found in binary cache metadata")
	}

	return &Summary{Targets: targets}, nil
}

func (v *VLLMCache) Labels() map[string]string {
	// Determine the cache format from metadata
	// Default to "unpacked" for triton cache format (older unpacked format)
	// or "binary" for new binary format
	cacheFormat := "unpacked"
	if len(v.allMetadata) > 0 && v.allMetadata[0].CacheFormat == BinaryCacheFormat {
		cacheFormat = BinaryCacheFormat
	}

	return map[string]string{
		cacheVLLMImageEntryCount: strconv.Itoa(v.EntryCount()),
		cacheVLLMImageCacheSize:  strconv.FormatInt(v.CacheSizeBytes(), 10),
		cacheVLLMImageSummary:    v.Summary(),
		cacheVLLMImageFormat:     cacheFormat,
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
	return extractCacheAndManifestDirectory(
		r,
		constants.MCVVLLMCacheDir,
		"io.vllm.manifest/",
		constants.ExtractCacheDir,
		constants.ExtractManifestDir,
	)
}
