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
	VLLMHOME         = "/home/vllm"
	VLLMCache        = ".cache/vllm"

	MCVTritonCacheDir    = "io.triton.cache/"
	MCVTritonManifestDir = "io.triton.manifest"
	MCVVLLMCacheDir      = "io.vllm.cache"
	MCVVLLMManifestDir   = "io.vllm.manifest"

	EnvTritonCacheDir = "TRITON_CACHE_DIR"
)

// Configurable runtime paths
var (
	TritonCacheDir  string
	ExtractCacheDir string
	MCVManifestDir  string
	VLLMCacheDir    string
	HasTritonCache  bool
	HasVLLMCache    bool
	LogLevels       = []string{"debug", "info", "warning", "error"} // accepted log levels
)

func init() {
	HasTritonCache = false
	HasVLLMCache = false
	ExtractCacheDir = ""
	// Derive user's home directory as the Triton/vLLM caches are stored somewhere here.
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

	if _, err := os.Stat(TritonCacheDir); !os.IsNotExist(err) {
		HasTritonCache = true
	}

	VLLMCacheDir = filepath.Join(home, VLLMCache)
	if _, err := os.Stat(VLLMCacheDir); !os.IsNotExist(err) {
		HasVLLMCache = true
	}

	// Ensure manifest output directory exists
	MCVManifestDir = filepath.Join(MCVBuildDir, ManifestDir)
	if err := os.MkdirAll(MCVManifestDir, 0755); err != nil {
		logging.Warnf("Failed to create manifest directory %s: %v", MCVManifestDir, err)
	}
}
