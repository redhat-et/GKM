package constants

import (
	"os"
	"path/filepath"

	logging "github.com/sirupsen/logrus"
)

// Core default paths and environment keys
const (
	MCVBuildDir      = "/tmp/.mcv"
	ManifestDir      = "manifest"
	CacheDir         = "cache"
	ManifestFileName = "manifest.json"

	EnvTritonCacheDir = "TRITON_CACHE_DIR"
)

// OCI directory standards
const (
	MCVTritonCacheDir    = "io.triton.cache/"
	MCVTritonManifestDir = "io.triton.manifest"
	MCVMCVVLLMCacheDir   = "io.vllm.cache"
	MCVVLLMManifestDir   = "io.vllm.manifest"
)

// Configurable runtime paths
var (
	TritonCacheDir string
	MCVManifestDir string

	LogLevels = []string{"debug", "info", "warning", "error"} // accepted log levels
)

func init() {
	// Derive user's home directory
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		logging.Warnf("Failed to determine user home dir, falling back to /tmp: %v", err)
		home = "/tmp"
	}

	// Determine Triton cache directory
	if val := os.Getenv(EnvTritonCacheDir); val != "" {
		TritonCacheDir = val
	} else {
		TritonCacheDir = filepath.Join(home, ".triton", "cache")
	}

	// Ensure manifest output directory exists
	MCVManifestDir = filepath.Join(MCVBuildDir, ManifestDir)
	if err := os.MkdirAll(MCVManifestDir, 0755); err != nil {
		logging.Warnf("Failed to create manifest directory %s: %v", MCVManifestDir, err)
	}
}
