package devices

import (
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/accelerator"
	"github.com/jaypipes/ghw/pkg/pci"
	"github.com/jaypipes/pcidb"
	"github.com/redhat-et/MCU/mcv/pkg/config"
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
// If stub mode is enabled, it simulates the presence of an AMD Aldebaran MI200 GPU.
// If no hardware accelerators are found, it returns nil without an error.
func DetectAccelerators() (accInfo *ghw.AcceleratorInfo) {
	if config.IsStubEnabled() {
		logging.Debug("Stub mode configured, simulating accelerator device")
		accInfo = &ghw.AcceleratorInfo{
			Devices: []*accelerator.AcceleratorDevice{
				{
					Address: "0000:00:01.0",
					PCIDevice: &pci.Device{
						Vendor: &pcidb.Vendor{
							Name: "STUBBED AMD",
							ID:   "1002",
						},
						Product: &pcidb.Product{
							Name: "STUBBED AMD",
							ID:   "STUBBED Aldebaran/MI200",
						},
						Driver: "dummy",
						Class: &pcidb.Class{
							Name: "controller",
							ID:   "0300",
						},
					},
				},
				{
					Address: "0000:00:02.0",
					PCIDevice: &pci.Device{
						Vendor: &pcidb.Vendor{
							Name: "STUBBED AMD",
							ID:   "1002",
						},
						Product: &pcidb.Product{
							Name: "STUBBED Product",
							ID:   "STUBBED Aldebaran/MI200",
						},
						Driver: "dummy",
						Class: &pcidb.Class{
							Name: "controller",
							ID:   "0300",
						},
					},
				},
			},
		}
		return accInfo
	}

	acc, err := ghw.Accelerator()
	if err != nil {
		logging.Debugf("no Accelerator detected")
		return nil
	}
	return acc
}
