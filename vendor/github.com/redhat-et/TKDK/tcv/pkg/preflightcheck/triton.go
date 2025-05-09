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

// Define the struct matching the JSON structure
type TritonCacheData struct {
	Hash                      string     `json:"hash"`
	Target                    Target     `json:"target"`
	Name                      string     `json:"name"`
	NumWarps                  int        `json:"num_warps"`
	NumCtas                   int        `json:"num_ctas,omitempty"`
	NumStages                 int        `json:"num_stages"`
	ClusterDims               []int      `json:"cluster_dims"`
	EnableFpFusion            bool       `json:"enable_fp_fusion"`
	SupportedFp8Dtypes        []string   `json:"supported_fp8_dtypes,omitempty"`
	DeprecatedFp8Dtypes       []string   `json:"deprecated_fp8_dtypes,omitempty"`
	DefaultDotInputPrecision  string     `json:"default_dot_input_precision"`
	AllowedDotInputPrecisions []string   `json:"allowed_dot_input_precisions"`
	MaxNumImpreciseAccDefault int        `json:"max_num_imprecise_acc_default"`
	ExternLibs                [][]string `json:"extern_libs,omitempty"`
	Debug                     bool       `json:"debug"`
	BackendName               string     `json:"backend_name"`
	SanitizeOverflow          bool       `json:"sanitize_overflow"`
	Shared                    int        `json:"shared"`
	Arch                      string     `json:"arch"`
	WarpSize                  int        `json:"warp_size"`

	// Optional/Backend-specific fields
	PtxVersion              *int    `json:"ptx_version,omitempty"`  // CUDA-only
	MaxNReg                 *int    `json:"maxnreg,omitempty"`      // CUDA
	WavesPerEU              *int    `json:"waves_per_eu,omitempty"` // ROCm-only
	LaunchCooperativeGrid   *bool   `json:"launch_cooperative_grid,omitempty"`
	MatrixInstrNonKDim      *int    `json:"matrix_instr_nonkdim,omitempty"`
	KPack                   *int    `json:"kpack,omitempty"`
	AllowFlushDenorm        *bool   `json:"allow_flush_denorm,omitempty"`
	InstructionSchedVariant *string `json:"instruction_sched_variant,omitempty"`
}

type TritonImageData struct {
	Hash       string `json:"hash"`
	DummyKey   string `json:"dummy_key,omitempty"`
	PtxVersion int    `json:"ptx_version,omitempty"`
	NumStages  int    `json:"num_stages,omitempty"`
	NumWarps   int    `json:"num_warps,omitempty"`
	Debug      bool   `json:"debug,omitempty"`
	Target
}

type Target struct {
	Backend  string `json:"backend"`
	Arch     any    `json:"arch"`
	WarpSize int    `json:"warp_size"`
}

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

func CompareTritonCacheImageToGPU(img v1.Image, acc accelerator.Accelerator) error {
	if img == nil {
		return errors.New("image is nil")
	}
	if acc == nil {
		return errors.New("accelerator is nil")
	}

	configFile, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("failed to get image config: %v", err)
	}

	labels := configFile.Config.Labels
	if labels == nil {
		return errors.New("image has no labels")
	}
	logging.Debugf("Image Labels: %+v", labels)

	metadata, ok := labels["cache.triton.image/metadata"]
	if !ok {
		return errors.New("image missing cache metadata label")
	}
	logging.Debugf("Raw metadata label: %s", metadata)

	var metadataList []TritonImageData
	if err = json.Unmarshal([]byte(metadata), &metadataList); err != nil {
		return fmt.Errorf("failed to parse metadata label: %v", err)
	}
	logging.Debugf("Parsed %d cache entries from image metadata", len(metadataList))
	for i, e := range metadataList {
		logging.Debugf("Parsed metadata[%d]: backend=%s arch=%s warp=%d", i, e.Backend, e.Arch, e.WarpSize)
	}

	var devInfo []devices.TritonGPUInfo
	if config.IsGPUEnabled() {
		if gpu := accelerator.GetActiveAcceleratorByType(config.GPU); gpu != nil {
			d := gpu.Device()
			if tritonDevInfo, err := d.GetAllGPUInfo(); err == nil {
				devInfo = tritonDevInfo
			} else {
				return fmt.Errorf("couldn't retrieve GPU info: %w", err)
			}
		}
	}

	var hasMatch bool
	var backendMismatch bool

	for _, entry := range metadataList {
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
				return fmt.Errorf("failed to compute dummy key for image entry: %w", err)
			}

			j, _ := json.MarshalIndent(expectedDummyKey, "", "  ")
			logging.Debugf("Dummy Key Components:%s", string(j))

			dummyKeyMatches = entry.DummyKey == expectedDummyKey
			if !dummyKeyMatches {
				logging.Debugf("Dummy key mismatch (baremetal): image=%s, expected=%s", entry.DummyKey, expectedDummyKey)
			}
		}

		for _, gpuInfo := range devInfo {
			logging.Debugf("Checking entry: backend=%s arch=%s warp=%d",
				entry.Backend, entry.Arch, entry.WarpSize)
			logging.Debugf("Against GPU: backend=%s arch=%s warp=%d",
				gpuInfo.Backend, gpuInfo.Arch, gpuInfo.WarpSize)
			backendMatches := entry.Backend == gpuInfo.Backend
			archMatches := entry.Arch == gpuInfo.Arch
			warpMatches := entry.WarpSize == gpuInfo.WarpSize

			ptxMatches := true
			if entry.Backend == "cuda" {
				ptxMatches = entry.PtxVersion == gpuInfo.PTXVersion
				if !ptxMatches {
					logging.Debugf("PTX version mismatch - image=%d, gpu=%d", entry.PtxVersion, gpuInfo.PTXVersion)
				}
			}

			if backendMatches && archMatches && warpMatches && ptxMatches && dummyKeyMatches {
				logging.Debugf("Cache match found: hash=%s", entry.Hash)
				hasMatch = true
				break
			}

			if !backendMatches {
				backendMismatch = true
				logging.Debugf("Backend mismatch - img=%s, gpu=%s", entry.Backend, gpuInfo.Backend)
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
