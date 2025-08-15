package imgbuild

import (
	"fmt"

	"github.com/redhat-et/MCU/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
)

type ImageBuilder interface {
	CreateImage(imgName string, cacheDir string) error
}

var HasApp = utils.HasApp

func New() (ImageBuilder, error) {
	if HasApp("buildah") {
		logging.Infof("Using buildah to build the image")
		return &buildahBuilder{}, nil
	} else if HasApp("docker") {
		logging.Infof("Using docker to build the image")
		return &dockerBuilder{}, nil
	}
	return nil, fmt.Errorf("unsupported builder: neither buildah nor docker found")
}
