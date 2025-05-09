package constants

import "os"

const (
	DockerCacheDirPrefix  = "docker-cache-dir-"
	BuildahCacheDirPrefix = "buildah-cache-dir-"
	PodmanCacheDirPrefix  = "podman-cache-dir-"
	TritonCacheDirName    = "io.triton.cache/"
)

var (
	TritonCacheDir string
	/* Logging */
	LogLevels = []string{"debug", "info", "warning", "error"} // accepted log levels
)

func init() {
	if val := os.Getenv("TRITON_CACHE_DIR"); val != "" {
		TritonCacheDir = val
	} else {
		TritonCacheDir = os.Getenv("HOME") + "/.triton/cache"
	}
}
