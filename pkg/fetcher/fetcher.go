package fetcher

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/redhat-et/TKDK/mcv/pkg/constants"
	"github.com/redhat-et/TKDK/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
)

type Fetcher interface {
	FetchImg(imgName string) (v1.Image, error)
}

type fetcher struct {
	local  []Fetcher
	remote Fetcher
}

// Factory function to create a new Fetcher with the specified backend.
func NewFetcher() Fetcher {
	var localFetchers []Fetcher

	if utils.HasApp("docker") {
		if df, err := newDockerFetcher(); err == nil {
			localFetchers = append(localFetchers, df)
		} else {
			logging.Warnf("Failed to init Docker fetcher: %v", err)
		}
	}

	if utils.HasApp("podman") {
		if pf, err := newPodmanFetcher(); err == nil {
			localFetchers = append(localFetchers, pf)
		} else {
			logging.Warnf("Failed to init Podman fetcher: %v", err)
		}
	}

	return &fetcher{local: localFetchers, remote: &remoteFetcher{}}
}

func (f *fetcher) FetchImg(imgName string) (v1.Image, error) {
	// Try to fetch locally first
	for _, localFetcher := range f.local {
		logging.Infof("Trying local fetcher: %T", localFetcher)

		img, _ := localFetcher.FetchImg(imgName)
		if img != nil {
			logging.Infof("Image found locally using %T", localFetcher)
			return img, nil
		}

		// If error or image is nil, log and continue to the next fetcher
		logging.Infof("Failed to fetch image locally using %T:", localFetcher)
	}

	// If local fetch fails, try fetching the image remotely
	img, err := f.remote.FetchImg(imgName)
	if err != nil || img == nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}

	return img, nil
}

func fetchToTempTar(fetchFn func(io.Writer) error) (v1.Image, error) {
	tmpDir := filepath.Join(constants.MCVBuildDir, constants.CacheDir)

	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, err
	}
	logging.Debugf("cache tmp extract dir: %s", tmpDir)

	tarballFilePath := path.Join(tmpDir, "tmp.tar")
	tarballFile, err := os.Create(tarballFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tarball file: %v", err)
	}

	if err := fetchFn(tarballFile); err != nil {
		tarballFile.Close() // Close on error too
		return nil, fmt.Errorf("error writing image to tarball: %w", err)
	}

	// Close explicitly before reading
	if err := tarballFile.Close(); err != nil {
		return nil, fmt.Errorf("error closing tarball file: %w", err)
	}

	logging.Infof("Saved image to tarball: %s", tarballFilePath)

	return loadImageFromTarball(tarballFilePath)
}
