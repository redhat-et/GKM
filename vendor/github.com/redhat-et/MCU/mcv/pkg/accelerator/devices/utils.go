package devices

import (
	"errors"
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/accelerator"
	"github.com/jaypipes/ghw/pkg/pci"
	"github.com/jaypipes/pcidb"
	"github.com/redhat-et/MCU/mcv/pkg/config"
	logging "github.com/sirupsen/logrus"
)

func GetSystemHW() (cpuInfo *ghw.CPUInfo, accInfo *ghw.AcceleratorInfo, err error) {
	cpuInfo, errCPU := ghw.CPU()
	if errCPU != nil {
		logging.Error("failed to get CPU info:", errCPU)
	} else {
		logging.Debug(cpuInfo)
	}

	accInfo, errAcc := DetectAccelerators()
	if errAcc != nil {
		logging.Error("failed to get accelerator info:", errAcc)
	} else {
		for _, device := range accInfo.Devices {
			logging.Debug(device)
		}
	}

	err = errors.Join(errCPU, errAcc)
	return
}

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
func DetectAccelerators() (accInfo *ghw.AcceleratorInfo, err error) {
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
		return accInfo, nil
	}

	acc, err := ghw.Accelerator()
	if err != nil {
		return nil, fmt.Errorf("failed to detect hardware accelerator: %w", err)
	}
	return acc, nil
}
