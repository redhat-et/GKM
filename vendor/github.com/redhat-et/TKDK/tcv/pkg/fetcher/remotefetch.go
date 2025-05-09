package fetcher

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	logging "github.com/sirupsen/logrus"
)

type remoteFetcher struct{}

func (r *remoteFetcher) FetchImg(imgName string) (v1.Image, error) {
	// Parse the image name into a reference (e.g., quay.io/mtahhan/triton-cache)
	ref, err := name.ParseReference(imgName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image name: %w", err)
	}

	logging.Infof("Retrieve remote Img %s!!!!!!!!", imgName)
	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}

	// Print the image details
	logging.Info("Img fetched successfully!!!!!!!!")
	return img, nil
}
