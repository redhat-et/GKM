package preflightcheck

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/redhat-et/MCU/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/MCU/mcv/pkg/cache"
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
		// Check if this is a binary cache format
		if entry.CacheFormat == "binary" && len(entry.BinaryCacheEntries) > 0 {
			if err := compareBinaryCacheEntriesToGPU(entry.BinaryCacheEntries, devInfo); err != nil {
				return err
			}
		} else if len(entry.TritonCacheEntries) > 0 {
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
	}

	return nil
}

// compareBinaryCacheEntriesToGPU validates binary cache entries against GPU hardware
func compareBinaryCacheEntriesToGPU(entries []cache.BinaryCacheMetadata, devInfo []devices.TritonGPUInfo) error {
	for _, entry := range entries {
		// Extract hardware info from the binary cache metadata
		backend := entry.TargetDevice
		if backend == "" {
			backend = "cuda" // Default if not specified
		}

		// Determine arch and warpSize based on backend and env vars
		arch := "unknown"
		warpSize := 32 // Default for CUDA

		switch backend {
		case "rocm", "hip":
			warpSize = 64 // AMD GPUs use 64-wide wavefronts
			// Try to extract GPU architecture from env
			if env, ok := entry.Env["VLLM_ROCM_CUSTOM_PAGED_ATTN"]; ok && env != nil {
				arch = "gfx90a" // Common MI250/MI300 arch, could be extracted more precisely
			}
		case "cuda":
			// Try to extract CUDA architecture
			if mainVersion, ok := entry.Env["VLLM_MAIN_CUDA_VERSION"]; ok {
				if version, ok := mainVersion.(string); ok {
					arch = "sm_" + version
				}
			}
		case "tpu":
			warpSize = 128 // TPU uses different parallelism model
		case "cpu":
			warpSize = 1 // CPU doesn't have warp concept
		}

		// Check if any GPU matches this binary cache entry
		matched := false
		for _, gpu := range devInfo {
			backendMatches := backend == gpu.Backend
			archMatches := arch == gpu.Arch
			warpMatches := warpSize == gpu.WarpSize

			if backendMatches && archMatches && warpMatches {
				matched = true
				break
			}
		}

		if !matched {
			return fmt.Errorf("binary cache entry (backend=%s, arch=%s, warpSize=%d) does not match any available GPU", backend, arch, warpSize)
		}
	}

	return nil
}
