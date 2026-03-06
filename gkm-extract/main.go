package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

	if err := os.Chmod(cacheDir, 0755); err != nil {
		log.Info("unable to chmod", "err", err)
	}

	// Only one initialization should occur
	initFile := filepath.Join(cacheDir, ".initialized")
	if fileExists(initFile) {
		log.Info("init file already exists", "imageURL", imageURL, "cacheDir", cacheDir, "noGpu", noGpu)
		return nil
	} else {
		if err := createFile(initFile); err != nil {
			log.Info("unable to create init file", "err", err)
		} else {
			log.Info("init file created")
		}
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

		if err := deleteFile(initFile); err != nil {
			log.Info("unable to delete init file", "err", err)
		} else {
			log.Info("deleted init file because of extract error")
		}
		time.Sleep(300 * time.Second)

		return err
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

// fileExists checks if the input filename exists.
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return err == nil
}

func createFile(name string) error {
	// os.O_CREATE: create the file if it does not exist
	// 0644: file permissions (read/write for owner, read for others)
	file, err := os.OpenFile(name, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	// It's important to close the file to free up system resources
	return file.Close()
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
