package constants

import (
	"os"
	"path/filepath"

	logging "github.com/sirupsen/logrus"
)

const (
	TCVTmpDir                = "/tmp/.tcv"
	DockerCacheDirPrefix     = "docker-cache-dir-"
	BuildahCacheDirPrefix    = "buildah-cache-dir-"
	BuildahManifestDirPrefix = "buildah-manifest-dir-"
	PodmanCacheDirPrefix     = "podman-cache-dir-"
	TritonCacheDirName       = "io.triton.cache/"
	ManifestFileName         = "manifest.json"
	ManifestDir              = "manifest"
	DockerfileCacheDir       = "io.triton.cache"
	DockerfileManifestDir    = "io.triton.manifest"
)

var (
	TritonCacheDir string
	TCVManifestDir string
	/* Logging */
	LogLevels = []string{"debug", "info", "warning", "error"} // accepted log levels
)

func init() {
	home := os.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			home = "/tmp" // fallback in worst-case
		}
	}

	if val := os.Getenv("TRITON_CACHE_DIR"); val != "" {
		TritonCacheDir = val
	} else {
		TritonCacheDir = filepath.Join(home, ".triton", "cache")
	}

	TCVManifestDir = filepath.Join(TCVTmpDir, ManifestDir)
	if err := os.MkdirAll(TCVManifestDir, 0755); err != nil {
		logging.Warnf("Failed to create manifest directory %s: %v", TCVManifestDir, err)
	}
}
