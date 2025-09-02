package main

import (
	"fmt"
	"os"
	"regexp"

	"github.com/containers/buildah"
	"github.com/containers/storage/pkg/unshare"
	"github.com/redhat-et/MCU/mcv/pkg/client"
	"github.com/redhat-et/MCU/mcv/pkg/config"
	"github.com/redhat-et/MCU/mcv/pkg/imgbuild"
	"github.com/redhat-et/MCU/mcv/pkg/logformat"
	"github.com/redhat-et/MCU/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	exitNormal       = 0
	exitExtractError = 1
	exitCreateError  = 2
	exitLogError     = 3
	imageNameRegex   = `^([a-z0-9]+([._-][a-z0-9]+)*(:[0-9]+)?/)?[a-z0-9]+([._-][a-z0-9]+)*(\/[a-z0-9]+([._-][a-z0-9]+)*)*(?::[\w][\w.-]{0,127})?$`
)

func main() {
	initializeLogging()

	if _, err := config.Initialize(config.ConfDir); err != nil {
		logFatal("Error initializing config", err, exitLogError)
	}

	if buildah.InitReexec() {
		return
	}
	unshare.MaybeReexecUsingUserNamespace(false)

	cmd := buildRootCommand()
	if err := cmd.Execute(); err != nil {
		logFatal("Error executing command", err, exitLogError)
	}
}

func initializeLogging() {
	logging.SetReportCaller(true)
	logging.SetFormatter(logformat.Default)
}

func logFatal(message string, err error, exitCode int) {
	logging.Fatalf("%s: %v", message, err)
	os.Exit(exitCode)
}

func buildRootCommand() *cobra.Command {
	var imageName, cacheDirName, logLevel string
	var createFlag, extractFlag, baremetalFlag, noGPUFlag, hwInfoFlag, checkCompatFlag, gpuInfoFlag bool

	cmd := &cobra.Command{
		Use:   "mcv",
		Short: "A GPU Kernel runtime container image management utility",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if err := logformat.ConfigureLogging(logLevel); err != nil {
				logFatal("Error configuring logging", err, exitLogError)
			}
		},
		Run: func(cmd *cobra.Command, args []string) {
			handleRunCommand(imageName, cacheDirName, logLevel, createFlag, extractFlag, baremetalFlag, noGPUFlag, hwInfoFlag, checkCompatFlag, gpuInfoFlag)
		},
	}

	addFlags(cmd, &imageName, &cacheDirName, &logLevel, &createFlag, &extractFlag, &baremetalFlag, &noGPUFlag, &hwInfoFlag, &checkCompatFlag, &gpuInfoFlag)
	return cmd
}

func addFlags(cmd *cobra.Command, imageName, cacheDirName, logLevel *string, createFlag, extractFlag, baremetalFlag, noGPUFlag, hwInfoFlag, checkCompatFlag, gpuInfoFlag *bool) {
	cmd.Flags().StringVarP(imageName, "image", "i", "", "OCI image name")
	cmd.Flags().StringVarP(cacheDirName, "dir", "d", "", "Triton Cache Directory")
	cmd.Flags().StringVarP(logLevel, "log-level", "l", "", "Set the logging verbosity level: debug, info, warning or error")
	cmd.Flags().BoolVarP(createFlag, "create", "c", false, "Create OCI image")
	cmd.Flags().BoolVarP(extractFlag, "extract", "e", false, "Extract a Triton cache from an OCI image")
	cmd.Flags().BoolVarP(baremetalFlag, "baremetal", "b", false, "Run baremetal/detailed preflight checks")
	cmd.Flags().BoolVar(noGPUFlag, "no-gpu", false, "Disable GPU logic for testing")
	cmd.Flags().BoolVar(hwInfoFlag, "hw-info", false, "Display system hardware info")
	cmd.Flags().BoolVar(gpuInfoFlag, "gpu-info", false, "Display GPU info")
	cmd.Flags().BoolVar(checkCompatFlag, "check-compat", false, "Check system GPU compatibility with a given image")
}

func handleRunCommand(imageName, cacheDirName, logLevel string, createFlag, extractFlag, baremetalFlag, noGPUFlag, hwInfoFlag, checkCompatFlag, gpuInfoFlag bool) {
	if hwInfoFlag {
		handleHWInfo()
	}

	if gpuInfoFlag {
		handleGPUInfo()
	}

	if checkCompatFlag {
		handleCheckCompat(imageName)
	}

	configureBaremetalAndGPU(baremetalFlag, noGPUFlag)

	if (createFlag || extractFlag) && imageName == "" {
		logging.Error("--image is required when using --create or --extract")
		os.Exit(exitLogError)
	}

	if createFlag || extractFlag || checkCompatFlag {
		if err := validateImageName(imageName); err != nil {
			logging.Error(err)
			os.Exit(exitLogError)
		}
	}

	if createFlag {
		runCreate(imageName, cacheDirName)
	}

	if extractFlag {
		runExtract(imageName, cacheDirName, logLevel, baremetalFlag)
	}

	if !createFlag && !extractFlag {
		logging.Error("No action specified. Use --create or --extract flag.")
		os.Exit(exitNormal)
	}
}

func validateImageName(imageName string) error {
	if imageName == "" {
		return fmt.Errorf("--image is required")
	}

	matched, err := regexp.MatchString(imageNameRegex, imageName)
	if err != nil {
		return fmt.Errorf("error validating image name: %v", err)
	}

	if !matched {
		return fmt.Errorf("invalid image name: %s. Ensure it is a valid Docker or Quay image URL", imageName)
	}

	return nil
}

func handleHWInfo() {
	xpu, err := client.GetXPUInfo()
	if err != nil {
		logging.Errorf("Error getting system hardware: %v", err)
		os.Exit(exitLogError)
	}
	client.PrintXPUInfo(xpu)
	os.Exit(exitNormal)
}

func handleGPUInfo() {
	summary, err := client.GetSystemGPUInfo()
	if err != nil {
		logging.Errorf("Error getting system hardware: %v", err)
		os.Exit(exitLogError)
	}
	client.PrintGPUSummary(summary)
	os.Exit(exitNormal)
}

func handleCheckCompat(imageName string) {
	if imageName == "" {
		logging.Error("--image is required with --check-compat")
		os.Exit(exitLogError)
	}

	matched, unmatched, err := client.PreflightCheck(imageName)
	if err != nil {
		logging.Errorf("Preflight check failed: %v", err)
	}

	if len(matched) > 0 {
		logging.Debugf("Compatible GPU(s) found (%d):", len(matched))
		logging.Debugf("IDs: %v", matched)
	} else {
		logging.Warn("No compatible GPUs found for the image.")
	}

	if len(unmatched) > 0 {
		logging.Debugf("Incompatible GPU(s) found (%d):", len(unmatched))
		logging.Debugf("IDs: %v", unmatched)
	}

	if err != nil || len(matched) == 0 {
		logging.Warn("Exiting: no compatible GPU(s) detected or error occurred during compatibility check")
		os.Exit(exitExtractError)
	}
	os.Exit(exitNormal)
}

func configureBaremetalAndGPU(baremetalFlag, noGPUFlag bool) {
	config.SetEnabledBaremetal(baremetalFlag)
	logging.Debugf("baremetalFlag %v", baremetalFlag)

	if noGPUFlag {
		logging.Debug("GPU checks disabled: running in no-GPU mode (--no-gpu)")
		config.SetEnabledGPU(false)
		return
	}

	xpuInfo, err := client.GetXPUInfo()
	if err != nil || xpuInfo == nil || xpuInfo.Acc == nil || len(xpuInfo.Acc.Devices) == 0 {
		logging.Warn("No hardware accelerator found. GPU support will be disabled.")
		config.SetEnabledGPU(false)
		return
	}

	logging.Infof("Hardware accelerator(s) detected (%d). GPU support enabled.", len(xpuInfo.Acc.Devices))
	for i, device := range xpuInfo.Acc.Devices {
		if device.PCIDevice != nil {
			logging.Debugf("  Accelerator %d: Vendor=%s, Product=%s", i, device.PCIDevice.Vendor.Name, device.PCIDevice.Product.Name)
		} else {
			logging.Debugf("  Accelerator %d: PCI device info unavailable", i)
		}
	}
	config.SetEnabledGPU(true)
}

func runCreate(imageName, cacheDir string) {
	// Check if the cache directory exists
	if _, err := utils.FilePathExists(cacheDir); err != nil {
		logging.Errorf("Error checking cache file path: %v", err)
		os.Exit(exitCreateError)
	}

	// Initialize the image builder
	builder, _ := imgbuild.New()
	if builder == nil {
		logging.Errorf("Failed to create builder")
		os.Exit(exitCreateError)
	}

	// Create the OCI image
	if err := builder.CreateImage(imageName, cacheDir); err != nil {
		logging.Errorf("Failed to create the OCI image: %v", err)
		os.Exit(exitCreateError)
	}

	logging.Info("OCI image created successfully.")
}

func runExtract(imageName, cacheDir, logLevel string, baremetalFlag bool) {
	gpuEnabled := config.IsGPUEnabled()
	opts := client.Options{
		ImageName:       imageName,
		CacheDir:        cacheDir,
		EnableGPU:       &gpuEnabled,
		LogLevel:        logLevel,
		EnableBaremetal: &baremetalFlag,
	}
	if _, _, err := client.ExtractCache(opts); err != nil {
		logging.Errorf("Error extracting image: %v", err)
		os.Exit(exitExtractError)
	}
}
