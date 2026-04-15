package preflightcheck

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/redhat-et/GKM/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/GKM/mcv/pkg/cache"
)

// CompareVLLMCacheManifestToGPU compares VLLM manifest entries to GPU info
// Handles both triton cache (legacy) and binary cache (new) formats
func CompareVLLMCacheManifestToGPU(manifestPath string, devInfo []devices.TritonGPUInfo) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read VLLM manifest file: %w", err)
	}

	var manifest cache.VLLMManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("failed to parse VLLM manifest JSON: %w", err)
	}

	for _, entry := range manifest.VLLM {
		// Check cache format and validate accordingly
		switch entry.CacheFormat {
		case "binary":
			if len(entry.BinaryCacheEntries) > 0 {
				if err := compareBinaryCacheEntriesToGPU(entry.BinaryCacheEntries, devInfo); err != nil {
					return err
				}
			}
		case "aot_compile":
			if len(entry.AOTCompileEntries) > 0 {
				if err := compareAOTCompileCacheEntriesToGPU(entry.AOTCompileEntries, devInfo); err != nil {
					return err
				}
			}
		case "triton":
			if len(entry.TritonCacheEntries) > 0 {
				// Handle triton cache format (legacy)
				convertedEntries := make([]cache.TritonCacheMetadata, len(entry.TritonCacheEntries))
				for i, e := range entry.TritonCacheEntries {
					if metadata, ok := e.(cache.TritonCacheMetadata); ok {
						convertedEntries[i] = metadata
					} else {
						return fmt.Errorf("failed to assert type cache.TritonCacheMetadata for entry: %v", e)
					}
				}
				if err := CompareTritonEntriesToGPU(convertedEntries, devInfo); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unknown cache format: %s", entry.CacheFormat)
		}
	}

	return nil
}

// compareAOTCompileCacheEntriesToGPU validates AOT compile cache entries against GPU hardware
// AOT compile caches have limited metadata, so this primarily relies on the summary-based check
func compareAOTCompileCacheEntriesToGPU(entries []cache.AOTCompileCacheMetadata, devInfo []devices.TritonGPUInfo) error {
	// AOT compile cache entries don't contain cache_key_factors.json with env vars,
	// so we can't extract detailed hardware requirements from the manifest.
	// The summary label (created during image build) contains the actual GPU info
	// and is checked by CompareCacheSummaryLabelToGPU.
	//
	// Here we just verify the entries exist and log for debugging.
	if len(entries) == 0 {
		return fmt.Errorf("no AOT compile cache entries found")
	}

	// Log the AOT cache entries for debugging
	for _, entry := range entries {
		fmt.Printf("AOT compile cache: hash=%s, rank=%s, size=%d bytes\n",
			entry.Hash, entry.Rank, entry.FileSize)
	}

	// Actual hardware compatibility is validated via the summary label
	return nil
}

// compareBinaryCacheEntriesToGPU validates binary cache entries against GPU hardware
// Note: Binary cache metadata doesn't directly contain compute capability.
// The Summary label (built during image creation using actual GPU detection) is the
// primary source of truth for hardware compatibility. This function provides a basic
// backend-level check.
func compareBinaryCacheEntriesToGPU(entries []cache.BinaryCacheMetadata, devInfo []devices.TritonGPUInfo) error {
	for i := range entries {
		entry := &entries[i]
		// Extract backend from the binary cache metadata
		backend := entry.TargetDevice
		if backend == "" {
			backend = cache.CUDABackend // Default if not specified
		}

		// Basic warp size validation based on backend
		expectedWarpSize := 32 // Default for CUDA
		switch backend {
		case "rocm", "hip":
			expectedWarpSize = 64 // AMD GPUs use 64-wide wavefronts
		case "cuda":
			expectedWarpSize = 32 // NVIDIA GPUs use 32-wide warps
		case "tpu":
			expectedWarpSize = 128 // TPU uses different parallelism model
		case "cpu":
			expectedWarpSize = 1 // CPU doesn't have warp concept
		}

		// Check if any GPU matches the backend and warp size
		matched := false
		for _, gpu := range devInfo {
			backendMatches := backend == gpu.Backend
			warpMatches := expectedWarpSize == gpu.WarpSize

			if backendMatches && warpMatches {
				matched = true
				// For detailed arch compatibility, rely on Summary label check
				fmt.Printf("Binary cache entry matches GPU: backend=%s, warpSize=%d\n",
					backend, expectedWarpSize)
				break
			}
		}

		if !matched {
			return fmt.Errorf("binary cache entry (backend=%s, warpSize=%d) does not match any available GPU. Use Summary label for precise arch validation", backend, expectedWarpSize)
		}
	}

	return nil
}
