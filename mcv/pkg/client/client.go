// Package client provides high-level APIs for capturing kernel caches into OCI
// images and extracting them back out, detecting system GPU and accelerator
// hardware, and running compatibility checks between system GPUs and image
// metadata.
//
// Capture goes through BuildCache, which packs a cache root plus any extra trees
// (for example the Triton JIT cache, which current vLLM builds keep outside
// VLLM_CACHE_ROOT) into one image layer. Serving goes through ExtractCache,
// optionally relocating those extra trees, or InspectCachePlan for callers that
// mount the trees themselves.
package client

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/redhat-et/GKM/mcv/pkg/accelerator"
	"github.com/redhat-et/GKM/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/GKM/mcv/pkg/cache"
	"github.com/redhat-et/GKM/mcv/pkg/cacheplan"
	"github.com/redhat-et/GKM/mcv/pkg/config"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	"github.com/redhat-et/GKM/mcv/pkg/fetcher"
	"github.com/redhat-et/GKM/mcv/pkg/imgbuild"
	"github.com/redhat-et/GKM/mcv/pkg/logformat"
	"github.com/redhat-et/GKM/mcv/pkg/preflightcheck"
	"github.com/redhat-et/GKM/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
)

// Options encapsulates configurable settings for cache extraction operations.
type Options struct {
	ImageName       string // The name of the OCI image (e.g., quay.io/user/image:tag)
	CacheDir        string // Path to store the extracted cache; if not specified, defaults are: ~/.triton/cache (vanilla Triton), ~/.cache/vllm (vLLM)
	EnableGPU       *bool  // Whether to enable GPU logic for preflight checks (nil = auto-detect, false = disable, true = force)
	LogLevel        string // Logging level: debug, info, warning, error
	EnableBaremetal *bool  // If true, enables full hardware checks including kernel dummy key validation (for baremetal envs only)
	SkipPrecheck    *bool  // If true, skips summary-level preflight GPU compatibility checks

	// PlaceExtraTrees relocates cache trees that were captured with
	// cache.CaptureSpec.Sources (for example a Triton JIT cache that lives
	// outside VLLM_CACHE_ROOT) from CacheDir/<subpath> to the absolute path
	// recorded in the image. It is opt-in because it writes outside CacheDir,
	// and it refuses to merge into a non-empty destination.
	PlaceExtraTrees bool
}

// BuildOptions encapsulates settings for capturing a populated cache directory
// into an OCI image.
type BuildOptions struct {
	ImageName string   // Destination image reference (e.g., quay.io/user/image:tag)
	CacheDir  string   // Cache root to capture. For vLLM this is VLLM_CACHE_ROOT, i.e. the directory containing torch_compile_cache, not torch_compile_cache itself.
	Sources   []string // Extra cache trees to capture in the same image layer, e.g. the Triton JIT cache at TRITON_CACHE_DIR. Each is restored at the path given here.
	MountAt   string   // Path the cache root should be mounted at when served. Defaults to CacheDir, which is correct whenever the serving container uses the same layout.
	Builder   string   // "buildah", "docker", or "" to auto-detect
}

type HwOptions struct {
	EnableStub *bool // If true, enables stub mode (Dummy devices); for testing/dev only (false = disable, true = force))
	Timeout    int   // Timeout in seconds for hardware detection operations (0 = disable timeout)
}

func InspectCacheImage(img string) (labels map[string]string, err error) {
	if img == "" {
		return nil, fmt.Errorf("image name must be specified")
	}

	_, err = name.ParseReference(img, name.StrictValidation)
	if err != nil {
		return nil, fmt.Errorf("error validating image name: %v", err)
	}

	return fetcher.NewImgFetcher().InspectImg(img)
}

// BuildCache packages CacheDir together with any extra cache trees listed in
// Sources into a single-layer cache image. All trees share one payload prefix so
// no additional layer is added, and each extra tree records the absolute path it
// has to reappear at, since caches such as the Triton JIT cache embed absolute
// paths and cannot be relocated.
func BuildCache(opts BuildOptions) error { //nolint:gocritic // BuildOptions is a config struct; value semantics keep it symmetric with ExtractCache.
	if opts.ImageName == "" {
		return fmt.Errorf("image name must be specified")
	}
	if _, err := name.ParseReference(opts.ImageName, name.StrictValidation); err != nil {
		return fmt.Errorf("error validating image name: %v", err)
	}
	if opts.CacheDir == "" {
		return fmt.Errorf("cache directory must be specified")
	}
	if ok, err := utils.FilePathExists(opts.CacheDir); err != nil {
		return fmt.Errorf("cannot read cache directory %s: %w", opts.CacheDir, err)
	} else if !ok {
		return fmt.Errorf("cache directory %s does not exist", opts.CacheDir)
	}

	// The captured root is stamped into the mount label, so it has to be the
	// resolved absolute path rather than whatever relative form was passed in.
	root, err := filepath.Abs(opts.CacheDir)
	if err != nil {
		return fmt.Errorf("invalid cache directory %q: %w", opts.CacheDir, err)
	}

	spec, err := cache.NewCaptureSpec(opts.Sources, opts.MountAt, root)
	if err != nil {
		return err
	}

	builder, err := imgbuild.NewWithBuilder(opts.Builder)
	if err != nil {
		return err
	}

	return builder.CreateImage(opts.ImageName, root, spec)
}

// InspectCachePlan decodes an image's mounting labels into the typed plan that
// describes every directory to restore, primary cache root first. Extra trees
// captured from outside the cache root (Triton, Inductor, DeepGEMM) appear as
// additional entries carrying their absolute destination path, the environment
// variable that points the framework at it, and whether the mount must be
// writable. Images with no serving plan, for example bare Triton caches, return
// an error satisfying IsUnsupportedCacheType.
func InspectCachePlan(imageName string) (cacheplan.CachePlan, error) {
	labels, err := InspectCacheImage(imageName)
	if err != nil {
		return cacheplan.CachePlan{}, err
	}
	plan, err := cacheplan.Derive(labels)
	if err != nil {
		return cacheplan.CachePlan{}, err
	}
	return plan, nil
}

// IsUnsupportedCacheType reports whether err means the image describes a cache
// type with no serving plan, as opposed to a malformed one.
func IsUnsupportedCacheType(err error) bool {
	return cacheplan.IsUnsupportedCacheType(err)
}

// It uses the provided options to configure behavior such as GPU checks, logging, and
// output directory. If GPU checks are enabled, it also verifies hardware compatibility.
func ExtractCache(opts Options) (matchedIDs, unmatchedIDs []int, err error) { //nolint:gocritic // Options is a config struct called once per process, and the signature is public API.
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
		if *opts.EnableGPU {
			enable := true
			// double check we have hardware accelerators
			if acc := devices.DetectAccelerators(); acc == nil || len(acc.Devices) == 0 {
				logging.Warn("No accelerators detected, GPU logic disabled.")
				enable = false
			} else {
				logging.Debugf("Detected %d accelerator(s), enabling GPU logic", len(acc.Devices))
			}
			config.SetEnabledGPU(enable)
		} else {
			logging.Debug("GPU support disabled via client options")
			config.SetEnabledGPU(false)
		}
	}

	if !config.IsGPUEnabled() {
		logging.Debug("GPU support is disabled So skipping accelerator detection and disabling preflight check")
		config.SetSkipPrecheck(true) // No GPU, so skip preflight
	}

	if opts.CacheDir != "" {
		cacheDir := opts.CacheDir
		if err = os.MkdirAll(cacheDir, 0755); err != nil {
			return nil, nil, fmt.Errorf("failed to create cache dir: %w", err)
		}
		constants.ExtractCacheDir = cacheDir
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

	if err := fetcher.New().FetchAndExtractCache(opts.ImageName); err != nil {
		return matchedIDs, unmatchedIDs, err
	}

	if opts.PlaceExtraTrees {
		// Relocation is opt-in and writes outside CacheDir, so it is all-or-error:
		// a failure to read the plan or to place any declared tree is surfaced
		// rather than leaving a half-relocated serving layout behind.
		labels, err := InspectCacheImage(opts.ImageName)
		if err != nil {
			return matchedIDs, unmatchedIDs, fmt.Errorf("cannot place extra cache trees without image labels: %w", err)
		}
		if err := placeExtraTrees(labels, constants.ExtractCacheDir); err != nil {
			return matchedIDs, unmatchedIDs, err
		}
	}

	return matchedIDs, unmatchedIDs, nil
}

// placeExtraTrees relocates extra payload subtrees, extracted flat under
// fromRoot, to the absolute paths recorded in the image labels. It is all-or-
// error: every declared tree is verified present and every destination free
// before the first move, so a mismatch fails without stranding some trees in
// CacheDir and placing others at their recorded paths.
func placeExtraTrees(labels map[string]string, fromRoot string) error {
	plan, err := cacheplan.Derive(labels)
	if err != nil {
		if cacheplan.IsUnsupportedCacheType(err) {
			logging.Debugf("Image has no mount plan; extra trees stay under %s", fromRoot)
			return nil
		}
		return err
	}

	extra := plan.Mounts[1:]

	// Validation pass: an image that declares extra trees has to actually carry
	// them, and no destination may already be occupied. A tree already sitting at
	// its recorded path is a no-op, so it is skipped rather than inspected.
	for _, m := range extra {
		src := filepath.Join(fromRoot, m.SubPath)
		if src == m.AbsPath {
			continue
		}
		info, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("declared extra cache tree %q is missing under %s: %w", m.SubPath, fromRoot, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("declared extra cache tree %q under %s is not a directory", m.SubPath, fromRoot)
		}
		if entries, err := os.ReadDir(m.AbsPath); err == nil && len(entries) > 0 {
			return fmt.Errorf("destination %s already holds %d entries; clear it before placing cache tree %s",
				m.AbsPath, len(entries), src)
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect %s: %w", m.AbsPath, err)
		}
	}

	// Move pass: every tree cleared validation above, so a failure here is
	// exceptional and aborts before any later tree is attempted.
	for _, m := range extra {
		src := filepath.Join(fromRoot, m.SubPath)
		if err := placeTree(src, m.AbsPath); err != nil {
			return err
		}
		logging.Infof("Placed extra cache tree at %s (env %s, writable=%t)", m.AbsPath, m.Env, m.RequiresWritable)
	}
	return nil
}

// placeTree moves a cache subtree to dst without merging: an existing non-empty
// destination is reported rather than overwritten, because two trees claiming
// the same path indicate a capture/serving mismatch the operator must resolve.
func placeTree(src, dst string) error {
	if src == dst {
		// Already at its recorded path: treat as successfully placed rather than
		// reading the destination, which would see the source's own contents and
		// report a self-collision.
		return nil
	}
	if entries, err := os.ReadDir(dst); err == nil && len(entries) > 0 {
		return fmt.Errorf("destination %s already holds %d entries; clear it before placing cache tree %s",
			dst, len(entries), src)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect %s: %w", dst, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := cache.CopyDir(src, dst); err != nil {
		return fmt.Errorf("failed to copy cache tree to %s: %w", dst, err)
	}
	return os.RemoveAll(src)
}

// GetSystemGPUInfo returns a summary of GPU devices with information
//
//	gpuType: e.g. nvidia-a100
//	driverVersion: e.g. 535.43.02
//	ids: e.g. [0, 1, 2, 3, 4, 5, 6, 7]
//
// If GPU support is not explicitly enabled, it auto-detects hardware
// accelerators and enables GPU logic if supported hardware is found.
// If no GPUs are found, it returns nil without an error.
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

	config.SetTimeout(opts.Timeout)
	if opts.Timeout > 0 {
		logging.Debugf("Hardware detection timeout set to %d seconds", opts.Timeout)
	} else {
		logging.Debug("Hardware detection timeout disabled")
	}

	// Auto-detect accelerator hardware if GPU is not already enabled
	if accs := devices.DetectAccelerators(); accs == nil || len(accs.Devices) == 0 {
		logging.Info("No accelerators detected, GPU logic disabled.")
		return nil, nil
	} else {
		logging.Infof("Detected %d accelerator(s)", len(accs.Devices))
		logging.Debug("Initializing the accelerator(s)")
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
