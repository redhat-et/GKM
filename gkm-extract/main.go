package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	mcvClient "github.com/redhat-et/GKM/mcv/pkg/client"

	"github.com/redhat-et/GKM/pkg/utils"
)

func main() {
	// Process inputs from Environment Variables. These are set in the CSI DaemonSet Yaml by pulling
	// values from the gkm-config ConfigMap Object.

	// Setup logging before anything else so code can log errors.
	logLevel := os.Getenv("GO_LOG")
	log := utils.InitializeLogging(logLevel, "setup", nil)
	log.Info("Logging", "Level", logLevel)

	cacheDir := strings.TrimSpace(os.Getenv("GKM_CACHE_DIR"))
	if cacheDir == "" {
		log.Info("Error: Missing GKM_CACHE_DIR")
		os.Exit(1)
	}

	imageURL := strings.TrimSpace(os.Getenv("GKM_IMAGE_URL"))
	if imageURL == "" {
		log.Info("Error: Missing GKM_IMAGE_URL")
		os.Exit(1)
	}

	noGpu := false
	if os.Getenv("NO_GPU") == "true" {
		noGpu = true
	}

	if err := ExtractCache(cacheDir, imageURL, noGpu, log); err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}

func ExtractCache(cacheDir, imageURL string, noGpu bool, log logr.Logger) (err error) {
	log.Info("extracting cache", "imageURL", imageURL, "cacheDir", cacheDir, "noGpu", noGpu)

	// Create the directory and its parents with standard permissions
	if err := os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		log.Error(err, "unable to make cache directory", "cacheDir", cacheDir)
		return err
	}

	if err := os.Chown(cacheDir, 1000, 1000); err != nil {
		log.Info("unable to chown", "err", err)
	}

	/*
		if err := os.Chmod(cacheDir, 0755); err != nil {
			log.Info("unable to chmod", "err", err)
		}
	*/

	// Acquire an exclusive file lock for this cacheDir to prevent concurrent
	// extractions from multiple controller instances launching parallel Jobs.
	// flock releases automatically when the file descriptor is closed.
	lockPath := filepath.Join(cacheDir, ".extract.lock")
	lf, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		log.Error(err, "unable to open extract lock", "lockPath", lockPath)
		return err
	}
	defer func() { _ = lf.Close() }()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		log.Error(err, "unable to acquire extract lock", "lockPath", lockPath)
		return err
	}

	// Only one initialization should occur per image URL.
	// The init file stores the image URL used for extraction so that a different
	// image triggers re-extraction rather than silently reusing stale cache.
	// The file is written atomically (via a temp file renamed on success) so that
	// a crash mid-extraction does not leave a stale .initialized sentinel.
	initFile := filepath.Join(cacheDir, ".initialized")
	initFileTmp := initFile + ".tmp"
	if data, err := os.ReadFile(initFile); err == nil {
		if strings.TrimSpace(string(data)) == imageURL {
			log.Info("init file already exists", "imageURL", imageURL, "cacheDir", cacheDir, "noGpu", noGpu)
			return nil
		}
		log.Info("image URL changed, clearing cache directory and re-extracting",
			"existing", strings.TrimSpace(string(data)), "new", imageURL)
		if err := clearDirectory(cacheDir); err != nil {
			log.Error(err, "unable to clear cache directory", "cacheDir", cacheDir)
			return err
		}
	}
	if err := os.WriteFile(initFileTmp, []byte(imageURL+"\n"), 0644); err != nil {
		log.Error(err, "unable to create init temp file", "initFileTmp", initFileTmp)
		return err
	}

	// For testing, like in a KIND Cluster, a real GPU may not be available.
	enableGPU := !noGpu
	matchedIds, unmatchedIds, err := mcvClient.ExtractCache(mcvClient.Options{
		ImageName: imageURL,
		CacheDir:  cacheDir,
		EnableGPU: &enableGPU,
		LogLevel:  "info",
	})
	if err != nil {
		log.Error(err, "unable to extract cache", "imageURL", imageURL, "cacheDir", cacheDir, "enableGPU", enableGPU)

		if err := deleteFile(initFileTmp); err != nil {
			log.Info("unable to delete init temp file", "err", err)
		} else {
			log.Info("deleted init temp file because of extract error")
		}
		time.Sleep(300 * time.Second)

		return err
	}
	// Atomically promote the temp init file only after successful extraction.
	if err := os.Rename(initFileTmp, initFile); err != nil {
		log.Info("unable to finalize init file", "err", err)
	} else {
		log.Info("init file created")
	}

	log.Info("Cache Extracted", "matchedIds", matchedIds, "unmatchedIds", unmatchedIds)

	log.Info("Walking Extracted Directory")
	_ = filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			log.Info("  File", "f", d.Name())
		} else {
			log.Info("  Directory", "d", d.Name())
		}
		return nil
	})

	return nil
}

// clearDirectory removes all entries inside dir without removing dir itself.
func clearDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func deleteFile(name string) error {
	// os.O_CREATE: create the file if it does not exist
	// 0644: file permissions (read/write for owner, read for others)
	err := os.Remove(name)
	if err != nil {
		return err
	}
	return nil
}
