package preflightcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/redhat-et/MCU/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/MCU/mcv/pkg/cache"
	"github.com/redhat-et/MCU/mcv/pkg/config"
	logging "github.com/sirupsen/logrus"
)

func CompareTritonCacheManifestToGPU(manifestPath string, devInfo []devices.TritonGPUInfo) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest file: %w", err)
	}

	var manifest cache.TritonManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	return CompareTritonEntriesToGPU(manifest.Triton, devInfo)
}

func CompareTritonEntriesToGPU(entries []cache.TritonCacheMetadata, devInfo []devices.TritonGPUInfo) error {
	if len(entries) == 0 {
		return errors.New("no cache metadata entries provided")
	}
	if devInfo == nil {
		return errors.New("devInfo is nil")
	}

	var hasMatch bool
	var backendMismatch bool

	for _, entry := range entries {
		dummyKeyMatches := true

		if config.IsBaremetalEnabled() {
			cacheData := &cache.TritonCacheData{
				Hash: entry.Hash,
				Target: cache.Target{
					Backend:  entry.Backend,
					Arch:     entry.Arch,
					WarpSize: entry.WarpSize,
				},
				PtxVersion: &entry.PtxVersion,
				NumStages:  entry.NumStages,
				NumWarps:   entry.NumWarps,
				Debug:      entry.Debug,
			}

			expectedDummyKey, err := cache.ComputeDummyTritonKey(cacheData)
			if err != nil {
				return fmt.Errorf("failed to compute dummy key for entry: %w", err)
			}
			dummyKeyMatches = entry.DummyKey == expectedDummyKey
		}

		for _, gpuInfo := range devInfo {
			backendMatches := entry.Backend == gpuInfo.Backend
			archMatches := entry.Arch == gpuInfo.Arch
			warpMatches := entry.WarpSize == gpuInfo.WarpSize

			ptxMatches := true
			if entry.Backend == "cuda" {
				ptxMatches = entry.PtxVersion == gpuInfo.PTXVersion
			}

			if backendMatches && archMatches && warpMatches && ptxMatches && dummyKeyMatches {
				logging.Infof("Compatible cache found: %s", entry.Hash)
				hasMatch = true
				break
			}

			if !backendMatches {
				backendMismatch = true
			}
		}
	}

	if hasMatch {
		return nil
	}
	if backendMismatch {
		return fmt.Errorf("incompatibility detected: backend mismatch")
	}
	return fmt.Errorf("no compatible GPU found")
}
