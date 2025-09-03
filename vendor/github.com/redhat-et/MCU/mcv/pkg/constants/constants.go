package constants

import (
	"os"
	"path/filepath"
	"time"

	logging "github.com/sirupsen/logrus"
)

// Core default paths and environment keys
const (
	VLLM             = "vllm"
	Triton           = "triton"
	MCVBuildDir      = "/tmp/.mcv"
	CacheDir         = "cache"
	ManifestDir      = "manifest"
	ManifestFileName = "manifest.json"
	VLLMHOME         = "/home/vllm"
	VLLMCache        = ".cache/vllm"

	MCVTritonCacheDir    = "io.triton.cache/"
	MCVTritonManifestDir = "io.triton.manifest"
	MCVVLLMCacheDir      = "io.vllm.cache"
	MCVVLLMManifestDir   = "io.vllm.manifest"

	EnvTritonCacheDir    = "TRITON_CACHE_DIR"
	DefaultCacheFilePath = "/tmp/device_cache.json"
	StubbedCacheFile     = "/tmp/device_cache_stub.json"
	CacheTTL             = 10 * time.Minute // Cache Time-To-Live
)

// Configurable runtime paths
var (
	TritonCacheDir     string
	ExtractCacheDir    string
	ExtractManifestDir string
	VLLMCacheDir       string
	HasTritonCache     bool
	HasVLLMCache       bool
	LogLevels          = []string{"debug", "info", "warning", "error"} // accepted log levels
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
	if _, err := os.Stat(TritonCacheDir); err == nil {
		HasTritonCache = true
	}

	VLLMCacheDir = filepath.Join(home, VLLMCache)
	if _, err := os.Stat(VLLMCacheDir); err == nil {
		HasVLLMCache = true
	}
}
