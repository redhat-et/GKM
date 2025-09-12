package preflightcheck

import (
	"encoding/json"
	"errors"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/redhat-et/MCU/mcv/pkg/accelerator"
	"github.com/redhat-et/MCU/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/MCU/mcv/pkg/cache"
	"github.com/redhat-et/MCU/mcv/pkg/config"
	"github.com/redhat-et/MCU/mcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

func CompareCacheSummaryLabelToGPU(img v1.Image, labels map[string]string, devInfo []devices.TritonGPUInfo) (matched, unmatched []devices.TritonGPUInfo, err error) {
	logging.Debug("Starting cache summary label preflight check...")
	if labels == nil {
		configFile, ret := img.ConfigFile()
		if ret != nil {
			return nil, nil, fmt.Errorf("failed to get image config: %w", ret)
		}

		labels = configFile.Config.Labels
		if labels == nil {
			return nil, nil, errors.New("image has no labels")
		}
	}

	summaryStr, ok := labels["cache.triton.image/summary"]
	if !ok {
		if summaryStr, ok = labels["cache.vllm.image/summary"]; !ok {
			return nil, nil, errors.New("image missing cache summary label")
		}
	}

	var summary cache.Summary
	if err = json.Unmarshal([]byte(summaryStr), &summary); err != nil {
		return nil, nil, fmt.Errorf("failed to parse summary label: %w", err)
	}

	for _, gpu := range devInfo {
		isMatch := false
		for _, target := range summary.Targets {
			backendMatches := target.Backend == gpu.Backend
			archMatches := target.Arch == gpu.Arch
			warpMatches := target.WarpSize == gpu.WarpSize

			if backendMatches && archMatches && warpMatches {
				isMatch = true
				break
			}
		}

		if isMatch {
			matched = append(matched, gpu)
		} else {
			unmatched = append(unmatched, gpu)
		}
	}

	if len(matched) == 0 {
		err = fmt.Errorf("no compatible GPU found from summary preflight check")
	}

	return matched, unmatched, err
}

// DetectCacheTypeFromLabels inspects image labels to determine cache type ("triton" or "vllm")
func DetectCacheTypeFromLabels(labels map[string]string) (string, error) {
	if labels == nil {
		return "", fmt.Errorf("no labels provided")
	}
	if _, ok := labels["cache.triton.image/summary"]; ok {
		return constants.Triton, nil
	}
	if _, ok := labels["cache.vllm.image/summary"]; ok {
		return constants.VLLM, nil
	}
	return "", fmt.Errorf("unknown cache type from labels")
}

// CompareCacheManifestToGPU dispatches manifest comparison based on cache type
func CompareCacheManifestToGPU(manifestPath, cacheType string, devInfo []devices.TritonGPUInfo) error {
	if cacheType == "" {
		return fmt.Errorf("cache type is empty")
	}
	switch cacheType {
	case constants.Triton:
		return CompareTritonCacheManifestToGPU(manifestPath, devInfo)
	case constants.VLLM:
		return CompareVLLMCacheManifestToGPU(manifestPath, devInfo)
	default:
		return fmt.Errorf("unsupported cache type: %s", cacheType)
	}
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
