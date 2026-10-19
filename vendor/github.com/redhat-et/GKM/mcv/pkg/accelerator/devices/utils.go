package devices

import (
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/redhat-et/GKM/mcv/pkg/config"
	logging "github.com/sirupsen/logrus"
)

func GetProductName(id int) (name string, err error) {
	xpus, errAcc := ghw.Accelerator()
	if errAcc != nil {
		logging.Error("failed to get accelerator info:", errAcc)
	} else {
		for i, device := range xpus.Devices {
			if i == id && device.PCIDevice != nil {
				return device.PCIDevice.Product.Name, nil
			}
		}
	}
	return "", fmt.Errorf("PCI device information unavailable")
}

// DetectAccelerators detects hardware accelerators and enables GPU logic if supported hardware is found.
// If stub mode is enabled, it simulates the accelerator selected by MCV_STUB_PROFILE (default: amd).
// If no hardware accelerators are found, it returns nil without an error.
func DetectAccelerators() (accInfo *ghw.AcceleratorInfo) {
	if config.IsStubEnabled() {
		logging.Debug("Stub mode configured, simulating accelerator device")
		return stubbedAcceleratorInfo(activeStubProfile())
	}

	acc, err := ghw.Accelerator()
	if err != nil {
		logging.Debugf("no Accelerator detected")
		return nil
	}
	return acc
}
