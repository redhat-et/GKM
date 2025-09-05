package main

import (
	"fmt"
	"os"

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
	var createFlag, extractFlag, baremetalFlag, noGPUFlag, hwInfoFlag, checkCompatFlag, gpuInfoFlag, stubFlag bool

	cmd := &cobra.Command{
		Use:   "mcv",
		Short: "A GPU Kernel runtime container image management utility",
		Long: `mcv is a utility for managing GPU kernel runtime container images.
It supports creating OCI images from cache directories, extracting caches from images,
and performing hardware compatibility checks.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if err := logformat.ConfigureLogging(logLevel); err != nil {
				logFatal("Error configuring logging", err, exitLogError)
			}
		},
		Run: func(cmd *cobra.Command, args []string) {
			handleRunCommand(imageName, cacheDirName, logLevel, createFlag, extractFlag, baremetalFlag, noGPUFlag, hwInfoFlag, checkCompatFlag, gpuInfoFlag, stubFlag)
		},
	}

	addFlags(cmd, &imageName, &cacheDirName, &logLevel, &createFlag, &extractFlag, &baremetalFlag, &noGPUFlag, &hwInfoFlag, &checkCompatFlag, &gpuInfoFlag, &stubFlag)
	return cmd
}

func addFlags(cmd *cobra.Command, imageName, cacheDirName, logLevel *string, createFlag, extractFlag, baremetalFlag, noGPUFlag, hwInfoFlag, checkCompatFlag, gpuInfoFlag, stubFlag *bool) {
	// Image operations
	cmd.Flags().StringVarP(imageName, "image", "i", "", "OCI image name (required for create, extract, check-compat)")
	cmd.Flags().StringVarP(cacheDirName, "dir", "d", "", "Triton/vLLM cache directory path")

	// Actions (mutually exclusive main operations)
	cmd.Flags().BoolVarP(createFlag, "create", "c", false, "Create OCI image from cache directory")
	cmd.Flags().BoolVarP(extractFlag, "extract", "e", false, "Extract Triton/vLLM cache from OCI image")

	// Information commands
	cmd.Flags().BoolVar(hwInfoFlag, "hw-info", false, "Display detailed system hardware information")
	cmd.Flags().BoolVar(gpuInfoFlag, "gpu-info", false, "Display GPU-specific information")
	cmd.Flags().BoolVar(checkCompatFlag, "check-compat", false, "Check GPU compatibility with specified image")

	// Configuration options
	cmd.Flags().StringVarP(logLevel, "log-level", "l", "info", "Set logging verbosity (debug, info, warning, error)")
	cmd.Flags().BoolVarP(baremetalFlag, "baremetal", "b", false, "Enable detailed baremetal preflight checks")
	cmd.Flags().BoolVar(noGPUFlag, "no-gpu", false, "Disable GPU detection and preflight checks (for testing)")
	cmd.Flags().BoolVar(stubFlag, "stub", false, "Use mock/stub data for hardware info (for testing)")

	// Mark mutually exclusive flags
	cmd.MarkFlagsMutuallyExclusive("create", "extract")
	cmd.MarkFlagsMutuallyExclusive("no-gpu", "hw-info")
	cmd.MarkFlagsMutuallyExclusive("no-gpu", "gpu-info")
	cmd.MarkFlagsMutuallyExclusive("no-gpu", "check-compat")
}

func handleRunCommand(imageName, cacheDirName, logLevel string, createFlag, extractFlag, baremetalFlag, noGPUFlag, hwInfoFlag, checkCompatFlag, gpuInfoFlag, stubFlag bool) {
	// Validate flag combinations
	if err := validateFlagCombinations(createFlag, extractFlag, hwInfoFlag, gpuInfoFlag, checkCompatFlag, imageName, cacheDirName, stubFlag); err != nil {
		logging.Error(err)
		os.Exit(exitLogError)
	}

	configureBoolFlags(baremetalFlag, noGPUFlag, stubFlag)

	if hwInfoFlag {
		handleHWInfo()
		return
	}

	if gpuInfoFlag {
		handleGPUInfo()
		return
	}

	if checkCompatFlag {
		handleCheckCompat(imageName)
		return
	}

	if createFlag {
		runCreate(imageName, cacheDirName)
		return
	}

	if extractFlag {
		runExtract(imageName, cacheDirName, logLevel, baremetalFlag)
		return
	}

	// If no action is specified, show help
	logging.Error("No action specified. Use --help to see available options.")
	os.Exit(exitNormal)
}

func validateFlagCombinations(createFlag, extractFlag, hwInfoFlag, gpuInfoFlag, checkCompatFlag bool, imageName, cacheDirName string, stubFlag bool) error {
	actionCount := 0
	if createFlag {
		actionCount++
	}
	if extractFlag {
		actionCount++
	}
	if hwInfoFlag {
		actionCount++
	}
	if gpuInfoFlag {
		actionCount++
	}
	if checkCompatFlag {
		actionCount++
	}

	if actionCount > 1 {
		return fmt.Errorf("only one action flag can be specified at a time")
	}

	if actionCount == 0 {
		return fmt.Errorf("at least one action must be specified")
	}

	// Image name requirements
	if (createFlag || extractFlag || checkCompatFlag) && imageName == "" {
		return fmt.Errorf("--image is required when using --create, --extract, or --check-compat")
	}

	// Cache directory requirements
	if createFlag && cacheDirName == "" {
		return fmt.Errorf("--dir is required when using --create")
	}

	// Stub flag validation
	if stubFlag && !(hwInfoFlag || gpuInfoFlag) {
		return fmt.Errorf("--stub can only be used with --hw-info or --gpu-info")
	}

	return nil
}

func handleHWInfo() {
	stub := config.IsStubEnabled()
	xpu, err := client.GetXPUInfo(client.HwOptions{EnableStub: &stub})
	if err != nil {
		logging.Errorf("Error getting system hardware: %v", err)
		os.Exit(exitLogError)
	}
	client.PrintXPUInfo(xpu)

	os.Exit(exitNormal)
}

func handleGPUInfo() {
	stub := config.IsStubEnabled()
	summary, err := client.GetSystemGPUInfo(client.HwOptions{EnableStub: &stub})
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

func configureBoolFlags(baremetalFlag, noGPUFlag, stub bool) {
	config.SetEnabledBaremetal(baremetalFlag)
	config.SetEnabledStub(stub)
	config.SetEnabledGPU(!noGPUFlag)

	logging.Debugf("baremetalFlag %v", baremetalFlag)
	logging.Debugf("stub %v", stub)
	logging.Debugf("noGPUFlag %v", noGPUFlag)

	if noGPUFlag {
		logging.Debug("GPU checks disabled: running in no-GPU mode (--no-gpu)")
		return
	}

	xpuInfo, err := client.GetXPUInfo(client.HwOptions{EnableStub: &stub})
	if err != nil || xpuInfo == nil || xpuInfo.Acc == nil || len(xpuInfo.Acc.Devices) == 0 {
		logging.Warn("No hardware accelerator found. GPU mode will be disabled.")
		config.SetEnabledGPU(false)
		return
	}

	logging.Infof("Hardware accelerator(s) detected (%d).", len(xpuInfo.Acc.Devices))
	for i, device := range xpuInfo.Acc.Devices {
		if device.PCIDevice != nil {
			logging.Debugf("  Accelerator %d: Vendor=%s, Product=%s", i, device.PCIDevice.Vendor.Name, device.PCIDevice.Product.Name)
		} else {
			logging.Debugf("  Accelerator %d: PCI device info unavailable", i)
		}
	}
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
