package image

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/redhat-et/GKM/pkg/utils"
)

func (s *ImageServer) initializeFilesystem() error {
	err := os.MkdirAll(s.cacheDir, 0755)
	if err != nil {
		s.log.Error(err, "error creating directory", "directory", s.cacheDir)
		return err
	}
	s.log.V(1).Info("Successfully created directory", "directory", s.cacheDir)
	return nil
}

func (s *ImageServer) ExtractImage(ctx context.Context, cacheImage, namespace, kernelName string) error {
	// Build command to TCV to Extract OCI Image from URL.
	outputDir := s.cacheDir
	if namespace != "" {
		outputDir = filepath.Join(outputDir, namespace)
	}
	if kernelName != "" {
		outputDir = filepath.Join(outputDir, kernelName)
	}

	loadArgs := []string{"-e", "-i", cacheImage, "-d", outputDir}

	if s.noGpu {
		loadArgs = append(loadArgs, "--no-gpu")
	}

	s.log.V(1).Info("extractImage", "cacheImage", cacheImage, "outputDir", outputDir)

	cmd := exec.CommandContext(ctx, utils.TcvBinary, loadArgs...)
	stderr := &bytes.Buffer{}
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", stderr.String(), err)
	}

	return nil
}

func (s *ImageServer) RemoveImage(namespace, kernelName string) error {

	mounted, err := utils.IsSourceBindMount(namespace, kernelName, s.log)
	if err != nil {
		return fmt.Errorf("unable to check if kernel cache is mounted: %w", err)
	}

	if mounted {
		return fmt.Errorf("kernel cache still in use: %w", err)
	}

	// Build command to TCV to Extract OCI Image from URL.
	parentDir := s.cacheDir
	outputDir := s.cacheDir
	if namespace != "" {
		parentDir = filepath.Join(parentDir, namespace)
		outputDir = filepath.Join(outputDir, namespace)
	}
	if kernelName != "" {
		outputDir = filepath.Join(outputDir, kernelName)
	}

	err = os.RemoveAll(outputDir)
	if err != nil {
		return fmt.Errorf("unable to remove kernel cache %s: %w", outputDir, err)
	}
	s.log.V(1).Info("Kernel Cache directory removed", "outputDir", outputDir)

	empty, err := s.IsDirectoryEmpty(parentDir)
	if err != nil {
		return err
	} else if empty {
		s.log.Info("Deleting Namespace directory as well", "parentDir", parentDir)
		err := os.RemoveAll(parentDir)
		if err != nil {
			return fmt.Errorf("unable to remove Namespace directory %s: %w", parentDir, err)
		}
	} else {
		s.log.Info("Namespace directory not empty", "parentDir", parentDir)
	}

	return nil
}

func (s *ImageServer) IsDirectoryEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer func() {
		err := f.Close()
		if err != nil {
			s.log.Error(err, "failed to close directory")
		}
	}()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}
