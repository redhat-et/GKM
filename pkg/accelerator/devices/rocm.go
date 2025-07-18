package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/redhat-et/TKDK/mcv/pkg/config"
	"github.com/redhat-et/TKDK/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
)

const rocmHwType = config.GPU

var (
	rocmAccImpl = gpuROCm{}
	rocmType    DeviceType
)

type gpuROCm struct {
	devices map[int]GPUDevice // GPU identifiers mapped to device info
}

type ROCMGPUInfo struct {
	GPUInfo map[int]*ROCMCardInfo
	DrvInfo *ROCMSystemInfo
}

type ROCMCardInfo struct {
	UniqueID           string `json:"Unique ID"`
	SerialNumber       string `json:"Serial Number"`
	VRAMTotalMemory    string `json:"VRAM Total Memory (B)"`
	VRAMUsedMemory     string `json:"VRAM Total Used Memory (B)"`
	VISVRAMTotalMemory string `json:"VIS_VRAM Total Memory (B)"`
	VISVRAMUsedMemory  string `json:"VIS_VRAM Total Used Memory (B)"`
	GTTTotalMemory     string `json:"GTT Total Memory (B)"`
	GTTUsedMemory      string `json:"GTT Total Used Memory (B)"`
	CardSeries         string `json:"Card Series"`
	CardModel          string `json:"Card Model"`
	CardVendor         string `json:"Card Vendor"`
	CardSKU            string `json:"Card SKU"`
	SubsystemID        string `json:"Subsystem ID"`
	DeviceRev          string `json:"Device Rev"`
	NodeID             string `json:"Node ID"`
	GUID               string `json:"GUID"`
	GFXVersion         string `json:"GFX Version"`
}

type ROCMSystemInfo struct {
	System struct {
		DriverVersion string `json:"Driver version"`
	} `json:"system"`
}

func rocmCheck(r *Registry) {
	if err := initROCmLib(); err != nil {
		logging.Debugf("Error initializing ROCm: %v", err)
		return
	}
	rocmType = ROCM
	if err := addDeviceInterface(r, rocmType, rocmHwType, rocmDeviceStartup); err == nil {
		logging.Infof("Using %s to obtain GPU info", rocmAccImpl.Name())
	} else {
		logging.Infof("Error registering rocm-smi: %v", err)
	}
}

func rocmDeviceStartup() Device {
	a := rocmAccImpl
	if err := a.InitLib(); err != nil {
		logging.Errorf("Error initializing %s: %v", rocmType.String(), err)
		return nil
	}
	if err := a.Init(); err != nil {
		logging.Errorf("Failed to init device: %v", err)
		return nil
	}
	logging.Infof("Using %s to obtain GPU info", rocmType.String())
	return &a
}

func initROCmLib() error {
	if utils.HasApp("rocm-smi") {
		return nil
	}
	return errors.New("couldn't find rocm-smi")
}

func (r *gpuROCm) InitLib() error {
	return initROCmLib()
}

func (r *gpuROCm) Name() string {
	return rocmType.String()
}

func (r *gpuROCm) DevType() DeviceType {
	return rocmType
}

func (r *gpuROCm) HwType() string {
	return rocmHwType
}

// Init initializes and starts the GPU info collection using a **single `rocm-smi` command**
func (r *gpuROCm) Init() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gpuInfoList, err := getAllROCmGPUInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get GPU information: %v", err)
	}

	// Populate the devices map
	r.devices = make(map[int]GPUDevice, len(gpuInfoList.GPUInfo))
	for gpuID, info := range gpuInfoList.GPUInfo {
		memTotal, _ := strconv.ParseUint(info.VRAMTotalMemory, 10, 64)
		name := "card" + strconv.Itoa(gpuID)
		r.devices[gpuID] = GPUDevice{
			ID: gpuID,
			TritonInfo: TritonGPUInfo{
				Name:              name,
				UUID:              info.UniqueID,
				ComputeCapability: "",
				Arch:              info.GFXVersion,
				WarpSize:          64,
				MemoryTotalMB:     memTotal / (1024 * 1024),
				Backend:           "hip",
			},
		}
	}

	return nil
}

// Shutdown stops the GPU metric collector
func (r *gpuROCm) Shutdown() bool {
	return true
}

func getAllROCmGPUInfo(ctx context.Context) (*ROCMGPUInfo, error) {
	gpus, err := getROCmGPUInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get GPU info")
	}
	system, err := getROCmSystemInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get system info")
	}

	return &ROCMGPUInfo{
		GPUInfo: gpus,
		DrvInfo: system,
	}, nil
}

// Fetches all GPUs' info in **one single rocm-smi call**
func getROCmGPUInfo(ctx context.Context) (map[int]*ROCMCardInfo, error) {
	cmd := exec.CommandContext(ctx, "rocm-smi", "--json", "--showproductname", "--showuniqueid", "--showserial", "--showmeminfo", "all")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute rocm-smi: %v", err)
	}

	var gpuInfo map[string]*ROCMCardInfo
	if err = json.Unmarshal(output, &gpuInfo); err != nil {
		return nil, fmt.Errorf("failed to parse rocm-smi output: %v", err)
	}

	prettyJSON, err := json.MarshalIndent(gpuInfo, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to pretty print JSON: %v", err)
	}

	logging.Debugf("ROCM JSON output:\n%s", string(prettyJSON))

	// Convert map keys from "GPUX" to int keys
	parsedGPUs := make(map[int]*ROCMCardInfo)
	for key, gpu := range gpuInfo {
		var gpuID int
		_, err := fmt.Sscanf(key, "card%d", &gpuID)
		if err == nil {
			parsedGPUs[gpuID] = gpu
		}
	}

	return parsedGPUs, nil
}

// Fetches all GPUs' info in **one single rocm-smi call**
func getROCmSystemInfo(ctx context.Context) (*ROCMSystemInfo, error) {
	cmd := exec.CommandContext(ctx, "rocm-smi", "--json", "--showdriverversion")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute rocm-smi: %v", err)
	}

	var systemInfo ROCMSystemInfo
	if err = json.Unmarshal(output, &systemInfo); err != nil {
		return nil, fmt.Errorf("failed to parse rocm-smi output: %v", err)
	}

	prettyJSON, err := json.MarshalIndent(systemInfo, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to pretty print JSON: %v", err)
	}

	logging.Debugf("ROCM JSON output:\n%s", string(prettyJSON))

	return &systemInfo, nil
}

// GetAllGPUInfo returns a list of GPU info for all devices
func (r *gpuROCm) GetAllGPUInfo() ([]TritonGPUInfo, error) {
	var allTritonInfo []TritonGPUInfo
	for gpuID, dev := range r.devices {
		allTritonInfo = append(allTritonInfo, dev.TritonInfo)
		logging.Debugf("GPU %d: %+v", gpuID, dev.TritonInfo)
	}
	return allTritonInfo, nil
}

// GetGPUInfo retrieves the stored GPU info for a specific device ID.
func (r *gpuROCm) GetGPUInfo(gpuID int) (TritonGPUInfo, error) {
	dev, exists := r.devices[gpuID]
	if !exists {
		return TritonGPUInfo{}, fmt.Errorf("GPU device %d not found", gpuID)
	}
	return dev.TritonInfo, nil
}
