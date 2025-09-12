// Package client provides high-level APIs for extracting Kernel caches
// from OCI images, detecting system GPU and accelerator hardware, and
// running compatibility checks between system GPUs and image metadata.
package client

import (
	"fmt"
	"os"

	"github.com/jaypipes/ghw"
	"github.com/redhat-et/MCU/mcv/pkg/accelerator"
	"github.com/redhat-et/MCU/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/MCU/mcv/pkg/config"
	"github.com/redhat-et/MCU/mcv/pkg/constants"
	"github.com/redhat-et/MCU/mcv/pkg/fetcher"
	"github.com/redhat-et/MCU/mcv/pkg/logformat"
	"github.com/redhat-et/MCU/mcv/pkg/preflightcheck"
	logging "github.com/sirupsen/logrus"
)

// Options encapsulates configurable settings for cache extraction operations.
type Options struct {
	ImageName       string // The name of the OCI image (e.g., quay.io/user/image:tag)
	CacheDir        string // Path to store the cache; for triton defaults to ~/.triton/cache
	EnableGPU       *bool  // Whether to enable GPU logic for preflight checks (nil = auto-detect, false = disable, true = force)
	LogLevel        string // Logging level: debug, info, warning, error
	EnableBaremetal *bool  // If true, enables full hardware checks including kernel dummy key validation (for baremetal envs only)
	SkipPrecheck    *bool  // If true, skips summary-level preflight GPU compatibility checks
}

type HwOptions struct {
	EnableStub *bool // If true, enables stub mode (Dummy devices); for testing/dev only (false = disable, true = force))
}

// xPU wraps CPU and GPU info
type xPU struct {
	CPU *ghw.CPUInfo
	Acc *ghw.AcceleratorInfo
}

// GetXPUInfo returns combined CPU and accelerator information (e.g., GPUs,
// FPGAs) for the current system using the ghw library. Used for diagnostics
// or --hw-info output.
func GetXPUInfo(opts HwOptions) (*xPU, error) {
	if !config.IsInitialized() {
		if _, err := config.Initialize(config.ConfDir); err != nil {
			return nil, fmt.Errorf("failed to initialize config: %w", err)
		}
	}

	if opts.EnableStub != nil {
		config.SetEnabledStub(*opts.EnableStub)
		if *opts.EnableStub {
			logging.Debug("Stub Mode enabled via client options")
		} else {
			logging.Debug("Stub Mode disabled via client options")
		}
	}
	cpuInfo, accInfo, err := devices.GetSystemHW()
	if err != nil {
		return nil, fmt.Errorf("failed to get hardware info: %w", err)
	}
	return &xPU{
		CPU: cpuInfo,
		Acc: accInfo,
	}, nil
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
func ExtractCache(opts Options) (matchedIDs, unmatchedIDs []int, err error) {
	if opts.ImageName == "" {
		return nil, nil, fmt.Errorf("image name must be specified")
	}

	if !config.IsInitialized() {
		if _, err = config.Initialize(config.ConfDir); err != nil {
			return nil, nil, fmt.Errorf("failed to initialize config: %w", err)
		}
	}

	if err = logformat.ConfigureLogging(opts.LogLevel); err != nil {
		return nil, nil, fmt.Errorf("error configuring logging: %v", err)
	}

	if opts.SkipPrecheck != nil {
		config.SetSkipPrecheck(*opts.SkipPrecheck)
		if *opts.SkipPrecheck {
			logging.Debug("preflight checks disabled via client options")
		}
	}

	if opts.EnableBaremetal != nil {
		config.SetEnabledBaremetal(*opts.EnableBaremetal)
		if !*opts.EnableBaremetal {
			logging.Debug("Baremetal checks disabled via client options")
		}
	}

	if opts.EnableGPU != nil {
		config.SetEnabledGPU(*opts.EnableGPU)
		if *opts.EnableGPU {
			logging.Debug("GPU support enabled via client options")
		} else {
			logging.Debug("GPU support disabled via client options")
		}
	}

	if config.IsGPUEnabled() {
		logging.Debug("GPU support is enabled")

		// Auto-detect accelerator hardware if GPU is not already enabled
		if _, err = devices.DetectAccelerators(); err != nil {
			logging.Warn("No accelerators detected, GPU logic disabled.")
		}
	} else {
		logging.Debug("GPU support is disabled So skipping accelerator detection and disabling preflight check")
		config.SetSkipPrecheck(true) // No GPU, so skip preflight
	}

	// If caller asked to skip preflight, do not run it here or downstream.
	// Otherwise, run it ONCE here, and then set SkipPrecheck=true so downstream won’t repeat it.
	shouldRunPreflight := config.IsGPUEnabled() && !config.IsSkipPrecheckEnabled()
	if shouldRunPreflight {
		matchedIDs, unmatchedIDs, err = PreflightCheck(opts.ImageName)
		if err != nil {
			return nil, nil, fmt.Errorf("preflight check failed: %w", err)
		}
		// Prevent duplicate preflight inside extract
		config.SetSkipPrecheck(true)
		logging.WithFields(logging.Fields{
			"matched":   matchedIDs,
			"unmatched": unmatchedIDs,
		}).Info("Preflight completed")
	} else if config.IsSkipPrecheckEnabled() {
		logging.Debug("Skipping preflight (requested by options)")
	} else if !config.IsSkipPrecheckEnabled() {
		logging.Debug("Skipping preflight (GPU disabled)")
	}

	if opts.CacheDir != "" {
		cacheDir := opts.CacheDir
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return nil, nil, fmt.Errorf("failed to create cache dir: %w", err)
		}
		constants.ExtractCacheDir = cacheDir
	}

	return nil, nil, fetcher.New().FetchAndExtractCache(opts.ImageName)
}

// GetSystemGPUInfo returns a summary of GPU devices with information
//
//	gpuType: e.g. nvidia-a100
//	driverVersion: e.g. 535.43.02
//	ids: e.g. [0, 1, 2, 3, 4, 5, 6, 7]
//
// If GPU support is not explicitly enabled, it auto-detects hardware
// accelerators and enables GPU logic if supported hardware is found.
func GetSystemGPUInfo(opts HwOptions) (*devices.GPUFleetSummary, error) {
	if !config.IsInitialized() {
		if _, err := config.Initialize(config.ConfDir); err != nil {
			return nil, fmt.Errorf("failed to initialize config: %w", err)
		}
	}

	if opts.EnableStub != nil {
		config.SetEnabledStub(*opts.EnableStub)
		if *opts.EnableStub {
			logging.Debug("Stub Mode enabled via client options")
		} else {
			logging.Debug("Stub Mode disabled via client options")
		}
	}

	// Auto-detect accelerator hardware if GPU is not already enabled
	if _, err := devices.DetectAccelerators(); err != nil {
		return nil, err
	}

	logging.Debug("Initializing the accelerator")
	// Initialize the GPU accelerator
	acc, err := accelerator.New(config.GPU, true)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GPU accelerator: %w", err)
	}

	if acc == nil || acc.Device() == nil {
		return nil, fmt.Errorf("accelerator initialization returned nil")
	}

	// Register the accelerator
	accelerator.GetAcceleratorRegistry().RegisterAccelerator(acc)

	// Fetch GPU device information
	summary, err := accelerator.SummarizeGPUs()
	if err != nil {
		return nil, fmt.Errorf("failed to get GPU info: %w", err)
	}
	return summary, nil
}

// PrintGPUSummary prints the fleet summary in a human-friendly form.
func PrintGPUSummary(summary *devices.GPUFleetSummary) {
	if summary == nil || len(summary.GPUs) == 0 {
		fmt.Println("No GPUs found.")
		return
	}

	fmt.Println("GPU Fleet:")
	for _, g := range summary.GPUs {
		fmt.Printf("  - GPU Type: %s\n", g.GPUType)
		fmt.Printf("    Driver Version: %s\n", g.DriverVersion)
		fmt.Printf("    IDs: %v\n", g.IDs)
	}
}

// PreflightCheck performs a compatibility check between the system’s detected GPUs
// and the image’s embedded metadata (via summary label). This is a lightweight check
// (label-only) intended to quickly identify supported GPUs for a given image.
//
// Returns slices of matched and unmatched GPUs, along with any error encountered.
func PreflightCheck(imageName string) (matchedIDs, unmatchedIDs []int, err error) {
	if !config.IsInitialized() {
		if _, err = config.Initialize(config.ConfDir); err != nil {
			return nil, nil, fmt.Errorf("failed to initialize config: %w", err)
		}
	}

	// Initialize the GPU accelerator
	acc, err := accelerator.New(config.GPU, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize GPU accelerator: %w", err)
	}

	// Register the accelerator
	accelerator.GetAcceleratorRegistry().RegisterAccelerator(acc)
	// Get device info (handles detection + accelerator setup)
	devInfo, err := preflightcheck.GetAllGPUInfo(acc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get system GPU info: %w", err)
	}

	// Fetch the image
	img, err := fetcher.NewImgFetcher().FetchImg(imageName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch image: %w", err)
	}

	// Run the compatibility check
	matched, unmatched, err := preflightcheck.CompareCacheSummaryLabelToGPU(img, nil, devInfo)
	if err != nil {
		return nil, nil, fmt.Errorf("preflight check failed: %w", err)
	}

	// Convert matched/unmatched TritonGPUInfo slices into GPU IDs
	matchedIDs = extractGPUIDs(matched)
	unmatchedIDs = extractGPUIDs(unmatched)

	logging.Info("Preflight GPU compatibility check passed.")
	return matchedIDs, unmatchedIDs, nil
}

func extractGPUIDs(infos []devices.TritonGPUInfo) []int {
	ids := make([]int, 0, len(infos))
	for _, ti := range infos {
		ids = append(ids, ti.ID)
	}
	return ids
}
