package fetcher

import (
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/redhat-et/TKDK/tcv/pkg/utils"
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
	var localFetcher []Fetcher

	if utils.HasApp("podman") {
		localFetcher = append(localFetcher, &dockerFetcher{})
	}
	if utils.HasApp("docker") {
		localFetcher = append(localFetcher, &podmanFetcher{})
	}

	return &fetcher{local: localFetcher, remote: &remoteFetcher{}}
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
