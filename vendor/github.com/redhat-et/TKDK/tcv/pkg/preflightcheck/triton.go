package preflightcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/redhat-et/TKDK/tcv/pkg/accelerator"
	"github.com/redhat-et/TKDK/tcv/pkg/accelerator/devices"
	"github.com/redhat-et/TKDK/tcv/pkg/config"
	logging "github.com/sirupsen/logrus"
)

func GetTritonCacheJSONData(filePath string) (*TritonCacheData, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		logging.Errorf("Failed to read file %s: %v", filePath, err)
		return nil, fmt.Errorf("failed to read file %s: %v", filePath, err)
	}

	var data TritonCacheData
	if err = json.Unmarshal(content, &data); err != nil {
		logging.Errorf("Failed to parse JSON in file %s: %v", filePath, err)
		return nil, fmt.Errorf("failed to parse JSON in file %s: %v", filePath, err)
	}

	// Check if the "hash" field is present and valid
	if data.Hash == "" {
		logging.Debugf("File %s does not contain the required 'hash' field", filePath)
		// DO NOT return an error.
		return nil, nil
	}

	prettyJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		logging.Errorf("Failed to pretty print JSON in file %s: %v", filePath, err)
		return nil, fmt.Errorf("failed to pretty print JSON in file %s: %v", filePath, err)
	}

	logging.Debugf("Cache JSON output:\n%s", string(prettyJSON))
	return &data, nil
}

func CompareTritonCacheToGPU(cacheData *TritonCacheData, acc accelerator.Accelerator) error {
	if cacheData == nil {
		return errors.New("cache data is nil")
	}
	if acc == nil {
		return errors.New("no Accelerator detected")
	}

	var devInfo []devices.TritonGPUInfo
	if config.IsGPUEnabled() {
		if gpu := accelerator.GetActiveAcceleratorByType(config.GPU); gpu != nil {
			d := gpu.Device()
			if tritonDevInfo, err := d.GetAllGPUInfo(); err == nil {
				devInfo = tritonDevInfo
			} else {
				return fmt.Errorf("couldn't retrieve Accelerator info: %w", err)
			}
		}
	}

	var hasMatch bool

	for _, gpuInfo := range devInfo {
		backendMatches := cacheData.Target.Backend == gpuInfo.Backend
		archMatches := ConvertArchToString(cacheData.Target.Arch) == gpuInfo.Arch
		warpMatches := cacheData.Target.WarpSize == gpuInfo.WarpSize
		ptxMatches := true

		if gpuInfo.Backend == "cuda" && cacheData.PtxVersion != nil {
			ptxMatches = *cacheData.PtxVersion == gpuInfo.PTXVersion
			if !ptxMatches {
				logging.Debugf("PTX version mismatch - cache=%d, gpu=%d", *cacheData.PtxVersion, gpuInfo.PTXVersion)
			}
		}

		if backendMatches && archMatches && warpMatches && ptxMatches {
			hasMatch = true
			break
		}

		if !backendMatches || !archMatches || !warpMatches {
			logging.Debugf(
				"Mismatch - backendMatches=%v, archMatches=%v", backendMatches, archMatches)
			logging.Debugf("Backend - cache=%v, gpu=%v", cacheData.Target.Backend, gpuInfo.Backend)
			logging.Debugf("Arch - cache=%v, gpu=%v", ConvertArchToString(cacheData.Target.Arch), gpuInfo.Arch)
			logging.Debugf("WarpSize - cache=%v, gpu=%v", cacheData.Target.WarpSize, gpuInfo.WarpSize)
			break
		}
	}

	if hasMatch {
		return nil
	}

	return fmt.Errorf("incompatibility detected")
}

// checkFirstKeyHash checks if the first key in the JSON file is "Hash": "hashvalue"
func checkFirstKeyHash(filePath string) (bool, error) {
	logging.Debugf("checkFirstKeyHash:%v", filePath)

	// Read the JSON file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}

	// Unmarshal into a generic map
	var jsonData TritonCacheData
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return false, err
	}

	// Check if the "hash" field is present and valid
	if jsonData.Hash == "" {
		// DO NOT return an error.
		return false, nil
	}

	return true, nil
}

func FindAllTritonCacheJSON(rootDir string) ([]string, error) {
	var files []string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && filepath.Ext(path) == ".json" {
			match, err := checkFirstKeyHash(path)
			if err != nil {
				log.Printf("Error checking file %s: %v\n", path, err)
				return nil
			}
			if match {
				files = append(files, path)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no valid Triton cache JSON files found in %s", rootDir)
	}

	return files, nil
}

func ConvertArchToString(arch any) string {
	switch v := arch.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	default:
		logging.Warnf("Unexpected arch type: %T", v)
		return ""
	}
}

func CompareTritonCacheManifestToGPU(manifestPath string, devInfo []devices.TritonGPUInfo) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest file: %w", err)
	}

	var entries []TritonImageData
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	return CompareTritonEntriesToGPU(entries, devInfo)
}

func CompareTritonEntriesToGPU(entries []TritonImageData, devInfo []devices.TritonGPUInfo) error {
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
			cacheData := &TritonCacheData{
				Hash: entry.Hash,
				Target: Target{
					Backend:  entry.Backend,
					Arch:     entry.Arch,
					WarpSize: entry.WarpSize,
				},
				PtxVersion: &entry.PtxVersion,
				NumStages:  entry.NumStages,
				NumWarps:   entry.NumWarps,
				Debug:      entry.Debug,
			}

			expectedDummyKey, err := ComputeDummyTritonKey(cacheData)
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

	var summary TritonSummary
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
