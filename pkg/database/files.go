package database

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/mount-utils"

	"github.com/redhat-et/GKM/pkg/utils"
)

func IsTargetBindMount(target string, log logr.Logger) (bool, error) {
	// d.mounter.IsLikelyNotMountPoint() doesn't detect bind mounts, so manually search
	// the list of mounts for the Target Path.

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

func IsSourceBindMount(srcPath string, log logr.Logger) (bool, error) {
	cmd := exec.Command("findmnt")
	cmdOutput := &bytes.Buffer{}
	cmd.Stdout = cmdOutput

	err := cmd.Run()
	if err != nil {
		return false, err
		//return false, fmt.Errorf("error executing findmnt: %w", err)
	}

	// Capture and process the output
	output := cmdOutput.String()
	lines := strings.Split(output, "\n")

	found := false
	for _, line := range lines {
		if strings.Contains(line, srcPath) {
			found = true
			break
		}
	}

	if found {
		log.V(1).Info("IsSourceBindMount(): srcPath Found", "srcPath", srcPath)
	} else {
		log.V(1).Info("IsSourceBindMount(): srcPath not found", "srcPath", srcPath)
	}

	return found, nil
}

func BindMount(sourcePath, targetPath string, readOnly bool, mounter mount.Interface, log logr.Logger) error {
	// Perform the bind mount
	options := []string{"bind"}
	if readOnly {
		options = append(options, "ro")
	}

	if err := mounter.Mount(sourcePath, targetPath, "", options); err != nil {
		log.Error(err, "bind mount failed",
			"sourcePath", sourcePath,
			"targetPath", targetPath)
		return err
	}

	return nil
}

func BuildDbDir(basePath, crNamespace, crName, digest string, log logr.Logger) (string, error) {
	// Build Cache Directory string from namespace, name and digest
	//   (e.g., "/var/lib/gkm/caches/<Namespace>/<Name>/<Digest>/...")
	//   (e.g., "/run/gkm/usage/<Namespace>/<Name>/<Digest>/...")
	cachePath := basePath

	if crNamespace != "" {
		cachePath = filepath.Join(cachePath, crNamespace)
	} else {
		cachePath = filepath.Join(cachePath, utils.ClusterScopedSubDir)
	}

	if crName != "" {
		cachePath = filepath.Join(cachePath, crName)
	} else {
		err := fmt.Errorf("custom resource name is required")
		log.Error(err, "unable to extract cache", "namespace", crNamespace, "name", crName)
		return cachePath, err
	}

	// Digest is optional, so ignore if it is not passed in.
	if digest != "" {
		cachePath = filepath.Join(cachePath, digest)
	}

	return cachePath, nil
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
func IsDirEmpty(inputDir, ignoreFile string) bool {
	currDir := filepath.Base(inputDir)

	var fileFound = errors.New("file found")
	err := filepath.WalkDir(inputDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			if d.Name() == ignoreFile {
				// Special file found, so keep walking the contents of the input directory
				return nil
			} else {
				return fileFound
			}
		} else {
			// WalkDir() returns the input directory. Trying to see if that directory is empty, so skip
			if d.Name() != currDir {
				return fileFound
			} else {
				return nil
			}
		}
	})

	if err != nil {
		if errors.Is(err, fileFound) {
			return false // Directory is not empty
		}
	}
	return true // Directory is empty
}
