package preflightcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/redhat-et/TKDK/mcv/pkg/accelerator"
	"github.com/redhat-et/TKDK/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/TKDK/mcv/pkg/cache"
	"github.com/redhat-et/TKDK/mcv/pkg/config"
	logging "github.com/sirupsen/logrus"
)

func CompareTritonCacheManifestToGPU(manifestPath string, devInfo []devices.TritonGPUInfo) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest file: %w", err)
	}

	var entries []cache.TritonImageData
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	return CompareTritonEntriesToGPU(entries, devInfo)
}

func CompareTritonEntriesToGPU(entries []cache.TritonImageData, devInfo []devices.TritonGPUInfo) error {
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

func CompareTritonSummaryLabelToGPU(img v1.Image, devInfo []devices.TritonGPUInfo) error {
	configFile, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("failed to get image config: %w", err)
	}

	labels := configFile.Config.Labels
	if labels == nil {
		return errors.New("image has no labels")
	}

	summaryStr, ok := labels["cache.triton.image/summary"]
	if !ok {
		return errors.New("image missing cache summary label")
	}

	var summary cache.TritonSummary
	if err := json.Unmarshal([]byte(summaryStr), &summary); err != nil {
		return fmt.Errorf("failed to parse summary label: %w", err)
	}

	for _, target := range summary.Targets {
		for _, gpu := range devInfo {
			if target.Backend == gpu.Backend &&
				target.Arch == gpu.Arch &&
				target.WarpSize == gpu.WarpSize {
				logging.Debugf("Summary preflight match found: %+v", target)
				return nil
			}
		}
	}

	return fmt.Errorf("no compatible GPU found from summary preflight check")
}

func GetAllGPUInfo(acc accelerator.Accelerator) ([]devices.TritonGPUInfo, error) {
	if acc == nil {
		return nil, fmt.Errorf("accelerator is nil")
	}
	if !config.IsGPUEnabled() {
		return nil, nil
	}
	gpu := accelerator.GetActiveAcceleratorByType(config.GPU)
	if gpu == nil {
		return nil, fmt.Errorf("no active GPU accelerator found")
	}
	device := gpu.Device()
	return device.GetAllGPUInfo()
}
