package preflightcheck

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/redhat-et/MCU/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/MCU/mcv/pkg/cache"
)

// CompareVLLMCacheManifestToGPU compares VLLM manifest entries to GPU info using Triton comparison logic
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

	return nil
}
