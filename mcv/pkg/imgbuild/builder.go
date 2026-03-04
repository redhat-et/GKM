package imgbuild

import (
	"fmt"

	"github.com/redhat-et/GKM/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
)

const (
	Buildah = "buildah"
	Docker  = "docker"
)

type ImageBuilder interface {
	CreateImage(imgName string, cacheDir string) error
}

var HasApp = utils.HasApp

func New() (ImageBuilder, error) {
	if HasApp(Buildah) {
		logging.Infof("Using buildah to build the image")
		return &buildahBuilder{}, nil
	} else if HasApp(Docker) {
		logging.Infof("Using docker to build the image")
		return &dockerBuilder{}, nil
	}
	return nil, fmt.Errorf("unsupported builder: neither buildah nor docker found")
}

func NewWithBuilder(builder string) (ImageBuilder, error) {
	switch builder {
	case Buildah:
		if HasApp(Buildah) {
			logging.Infof("Using buildah to build the image")
			return &buildahBuilder{}, nil
		}
		return nil, fmt.Errorf("buildah is not available on this system")
	case Docker:
		if HasApp(Docker) {
			logging.Infof("Using docker to build the image")
			return &dockerBuilder{}, nil
		}
		return nil, fmt.Errorf("docker is not available on this system")
	case "":
		return New() // Fallback to auto-detection
	default:
		return nil, fmt.Errorf("unsupported builder: %s", builder)
	}
}
