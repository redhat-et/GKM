package devices

import (
	"errors"
	"fmt"

	"github.com/jaypipes/ghw"
	logging "github.com/sirupsen/logrus"
)

func GetSystemHW() (cpuInfo *ghw.CPUInfo, accInfo *ghw.AcceleratorInfo, err error) {
	cpuInfo, errCPU := ghw.CPU()
	if errCPU != nil {
		logging.Error("failed to get CPU info:", errCPU)
	} else {
		logging.Debug(cpuInfo)
	}

	accInfo, errAcc := ghw.Accelerator()
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
