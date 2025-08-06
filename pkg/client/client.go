// Package client provides high-level APIs for extracting Kernel caches
// from OCI images, detecting system GPU and accelerator hardware, and
// running compatibility checks between system GPUs and image metadata.
package client

import (
	"errors"
	"fmt"
	"os"

	"github.com/jaypipes/ghw"
	"github.com/redhat-et/TKDK/mcv/pkg/accelerator"
	"github.com/redhat-et/TKDK/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/TKDK/mcv/pkg/config"
	"github.com/redhat-et/TKDK/mcv/pkg/constants"
	"github.com/redhat-et/TKDK/mcv/pkg/fetcher"
	"github.com/redhat-et/TKDK/mcv/pkg/logformat"
	"github.com/redhat-et/TKDK/mcv/pkg/preflightcheck"
	logging "github.com/sirupsen/logrus"
)

// Options encapsulates configurable settings for cache extraction operations.
type Options struct {
	ImageName       string // The name of the OCI image (e.g., quay.io/user/image:tag)
	CacheDir        string // Path to store the cache; for triton defaults to ~/.triton/cache
	EnableGPU       *bool  // Whether to enable GPU logic (nil = auto-detect, false = disable, true = force)
	LogLevel        string // Logging level: debug, info, warning, error
	EnableBaremetal *bool  // If true, enables full hardware checks including kernel dummy key validation (for baremetal envs only)
	SkipPrecheck    *bool  // If true, skips summary-level preflight GPU compatibility checks
}

// xPU wraps CPU and GPU info
type xPU struct {
	CPU *ghw.CPUInfo
	Acc *ghw.AcceleratorInfo
}

// GetXPUInfo returns combined CPU and accelerator information (e.g., GPUs,
// FPGAs) for the current system using the ghw library. Used for diagnostics
// or --hw-info output.
func GetXPUInfo() (*xPU, error) {
	cpuInfo, accInfo, err := getSystemHW()
	if err != nil {
		return nil, fmt.Errorf("failed to get hardware info: %w", err)
	}
	return &xPU{
		CPU: cpuInfo,
		Acc: accInfo,
	}, nil
}

func getSystemHW() (cpuInfo *ghw.CPUInfo, accInfo *ghw.AcceleratorInfo, err error) {
	cpuInfo, errCPU := ghw.CPU()
	if errCPU != nil {
		logging.Error("failed to get CPU info:", errCPU)
	} else {
		logging.Debug(cpuInfo)
	}

	accInfo, errAcc := ghw.Accelerator()
	if errAcc != nil {
		logging.Error("failed to get accelerator info:", errAcc)
	} else {
		for _, device := range accInfo.Devices {
			logging.Debug(device)
		}
	}

	err = errors.Join(errCPU, errAcc)
	return
}

// PrintXPUInfo logs or prints system CPU and accelerator (GPU) info
// in a human-readable format for CLI users.
func PrintXPUInfo(xpu *xPU) {
	fmt.Println("=== CPU Information ===")
	for _, proc := range xpu.CPU.Processors {
		fmt.Printf("Vendor: %s, Model: %s, Cores: %d, Threads: %d\n",
			proc.Vendor, proc.Model, proc.NumCores, proc.NumThreads)
	}

	fmt.Println("\n=== Accelerator Information ===")
	if xpu.Acc == nil || len(xpu.Acc.Devices) == 0 {
		fmt.Println("No Accelerator detected.")
	} else {
		for i, device := range xpu.Acc.Devices {
			fmt.Printf("Accelerator %d:\n", i)
			fmt.Printf("  Address: %s\n", device.Address)
			if device.PCIDevice != nil {
				fmt.Printf("  Vendor: %s\n", device.PCIDevice.Vendor.Name)
				fmt.Printf("  Product: %s\n", device.PCIDevice.Product.Name)
			} else {
				fmt.Println("  PCI device information unavailable")
			}
		}
	}
}

// ExtractCache pulls and extracts a kernel cache from the specified OCI image.
// It uses the provided options to configure behavior such as GPU checks, logging, and
// output directory. If GPU checks are enabled, it also verifies hardware compatibility.
func ExtractCache(opts Options) error {
	if opts.ImageName == "" {
		return fmt.Errorf("image name must be specified")
	}

	if _, err := config.Initialize(config.ConfDir); err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}

	if err := logformat.ConfigureLogging(opts.LogLevel); err != nil {
		return fmt.Errorf("error configuring logging: %v", err)
	}

	if opts.EnableGPU != nil {
		config.SetEnabledGPU(*opts.EnableGPU)
		if !*opts.EnableGPU {
			logging.Debug("GPU checks disabled via client options")
		}
	}

	if opts.SkipPrecheck != nil {
		config.SetSkipPrecheck(*opts.SkipPrecheck)
		if !*opts.SkipPrecheck {
			logging.Debug("preflight checks disabled via client options")
		}
	}

	if opts.EnableBaremetal != nil {
		config.SetEnabledBaremetal(*opts.EnableBaremetal)
		if !*opts.EnableBaremetal {
			logging.Debug("Baremetal checks disabled via client options")
		}
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = constants.TritonCacheDir // default from init()
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}
	constants.TritonCacheDir = cacheDir

	logging.Infof("Using Triton cache directory: %s", constants.TritonCacheDir)

	return fetcher.New().FetchAndExtractCache(opts.ImageName)
}

// GetSystemGPUInfo returns a list of GPU devices with detailed information
// (e.g., backend, architecture, PTX, etc.).
//
// If GPU support is not explicitly enabled, it auto-detects hardware
// accelerators and enables GPU logic if supported hardware is found.
func GetSystemGPUInfo() ([]devices.TritonGPUInfo, error) {
	if _, err := config.Initialize(config.ConfDir); err != nil {
		return nil, fmt.Errorf("failed to initialize config: %w", err)
	}

	// Auto-detect accelerator hardware if GPU is not already enabled
	if !config.IsGPUEnabled() {
		accInfo, err := ghw.Accelerator()
		if err != nil {
			return nil, fmt.Errorf("failed to detect hardware accelerator: %w", err)
		}

		if accInfo == nil || len(accInfo.Devices) == 0 {
			logging.Warn("No hardware accelerator found. GPU support will be disabled.")
			config.SetEnabledGPU(false)
			return nil, fmt.Errorf("no hardware accelerator present")
		}

		logging.Infof("Hardware accelerator(s) detected (%d). GPU support enabled.", len(accInfo.Devices))
		config.SetEnabledGPU(true)
	}

	// Initialize the GPU accelerator
	acc, err := accelerator.New(config.GPU, true)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GPU accelerator: %w", err)
	}

	// Register the accelerator
	accelerator.GetRegistry().MustRegister(acc)

	// Fetch GPU device information
	info, err := preflightcheck.GetAllGPUInfo(acc)
	if err != nil {
		return nil, fmt.Errorf("failed to get GPU info: %w", err)
	}

	return info, nil
}

// PreflightCheck performs a compatibility check between the system’s detected GPUs
// and the image’s embedded metadata (via summary label). This is a lightweight check
// (label-only) intended to quickly identify supported GPUs for a given image.
//
// Returns slices of matched and unmatched GPUs, along with any error encountered.
func PreflightCheck(imageName string) (matched, unmatched []devices.TritonGPUInfo, err error) {
	// Initialize config
	if _, err = config.Initialize(config.ConfDir); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize config: %w", err)
	}

	// Get device info (handles detection + accelerator setup)
	devInfo, err := GetSystemGPUInfo()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get system GPU info: %w", err)
	}

	// Fetch the image
	img, err := fetcher.NewImgFetcher().FetchImg(imageName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch image: %w", err)
	}

	// Run the compatibility check
	matched, unmatched, err = preflightcheck.CompareTritonSummaryLabelToGPU(img, devInfo)
	if err != nil {
		return matched, unmatched, fmt.Errorf("preflight check failed: %w", err)
	}

	logging.Info("Preflight GPU compatibility check passed.")
	return matched, unmatched, nil
}
