package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func IsTargetBindMount(target string, log logr.Logger) (bool, error) {
	tmpTarget, err := filepath.EvalSymlinks(target) // resolve symlinks
	if err != nil {
		return false, fmt.Errorf("failed to evaluate symlinks: %w", err)
	}

	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false, fmt.Errorf("failed to open mountinfo: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Split on " - " separator (between optional and required fields)
		parts := strings.Split(line, " - ")
		if len(parts) != 2 {
			continue // malformed line
		}

		fields := strings.Fields(parts[0])
		if len(fields) < 5 {
			continue
		}

		mountPoint := fields[4]
		mountPoint, err = filepath.EvalSymlinks(mountPoint)
		if err != nil {
			continue
		}

		if mountPoint == tmpTarget {
			log.V(1).Info("IsTargetBindMount(): FOUND Mount")
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		log.V(1).Info("IsTargetBindMount():Mount NOT FOUND - Scanner err")
		return false, fmt.Errorf("error reading mountinfo: %w", err)
	}

	log.V(1).Info("IsTargetBindMount(): Mount NOT FOUND")
	return false, nil
}

func IsSourceBindMount(namespace, name string, log logr.Logger) (bool, error) {
	cmd := exec.Command("findmnt")
	cmdOutput := &bytes.Buffer{}
	cmd.Stdout = cmdOutput

	err := cmd.Run()
	if err != nil {
		return false, fmt.Errorf("error executing findmnt: %w", err)
	}

	// Capture and process the output
	output := cmdOutput.String()
	lines := strings.Split(output, "\n")

	// Search for a specific string (e.g., "namespace/name")
	sourcePath := namespace
	sourcePath = filepath.Join(sourcePath, name)
	log.V(1).Info("IsSourceBindMount(): Searching for sourcePath in findmnt output:", "sourcePath", sourcePath)

	found := false
	for _, line := range lines {
		if strings.Contains(line, sourcePath) {
			found = true
			break
		}
	}

	if found {
		log.V(1).Info("IsSourceBindMount(): sourcePath Found", "sourcePath", sourcePath)
	} else {
		log.V(1).Info("IsSourceBindMount(): sourcePath not found", "sourcePath", sourcePath)
	}

	return found, nil
}

// DirSize calculates the total size of a directory and its subdirectories.
func DirSize(path string) (int64, error) {
	var totalSize int64
	err := filepath.Walk(path, func(_ string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	return totalSize, err
}

// IsDirEmpty checks if the directory at the given path is empty.
func IsDirEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("failed to open directory: %w", err)
	}
	defer f.Close()

	_, err = f.Readdirnames(1) // Read at most one entry
	if err == io.EOF {
		return true, nil // Directory is empty
	}
	if err != nil {
		return false, fmt.Errorf("failed to read directory entries: %w", err)
	}
	return false, nil // Directory is not empty
}

func InitializeLogging(logLevel string) logr.Logger {
	var opts zap.Options

	// Setup logging
	switch logLevel {
	case "info":
		opts = zap.Options{
			Development: false,
		}
	case "debug":
		opts = zap.Options{
			Development: true,
		}
	case "trace":
		opts = zap.Options{
			Development: true,
			Level:       zapcore.Level(-2),
		}
	default:
		opts = zap.Options{
			Development: false,
		}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	return ctrl.Log.WithName("gkm-csi")
}
