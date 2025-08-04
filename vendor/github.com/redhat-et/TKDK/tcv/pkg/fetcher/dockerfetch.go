package fetcher

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/docker/docker/client"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/redhat-et/TKDK/tcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

type dockerFetcher struct{}

func (d *dockerFetcher) FetchImg(imgName string) (v1.Image, error) {
	// Initialize Docker client
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer apiClient.Close()

	// Use the Docker client to save the image to a tarball
	reader, err := apiClient.ImageSave(context.Background(), []string{imgName})
	if err != nil {
		return nil, fmt.Errorf("failed to save image: %v", err)
	}
	defer reader.Close()

	tmpDir, err := os.MkdirTemp("", constants.DockerCacheDirPrefix)
	if err != nil {
		return nil, err
	}

	// Create a tarball file where the image will be saved
	tarballFilePath := path.Join(tmpDir, "tmp.tar")
	tarballFile, err := os.Create(tarballFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tarball file: %v", err)
	}
	defer tarballFile.Close()

	// Copy the content from the reader to the tarball file
	_, err = io.Copy(tarballFile, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to copy data to tarball file: %v", err)
	}

	logging.Infof("Saved image: %s\n", tarballFile.Name())
	return loadImageFromTarball(tarballFilePath)
}
