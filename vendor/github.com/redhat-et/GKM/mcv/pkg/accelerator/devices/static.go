package devices

import (
	"errors"

	"github.com/redhat-et/GKM/mcv/pkg/config"
	logging "github.com/sirupsen/logrus"
)

type StaticDevice struct {
	name       string
	deviceType DeviceType
	hwType     string
	tritonInfo []TritonGPUInfo
	summaries  []DeviceSummary
}

func (d *StaticDevice) Name() string        { return d.name }
func (d *StaticDevice) DevType() DeviceType { return d.deviceType }
func (d *StaticDevice) HwType() string      { return d.hwType }
func (d *StaticDevice) InitLib() error      { return nil }
func (d *StaticDevice) Init() error         { return nil }
func (d *StaticDevice) Shutdown() bool      { return true }
func (d *StaticDevice) GetGPUInfo(gpuID int) (TritonGPUInfo, error) {
	if gpuID < 0 || gpuID >= len(d.tritonInfo) {
		return TritonGPUInfo{}, errors.New("invalid GPU ID")
	}
	return d.tritonInfo[gpuID], nil
}
func (d *StaticDevice) GetSummary(gpuID int) (DeviceSummary, error) {
	if gpuID < 0 || gpuID >= len(d.summaries) {
		return DeviceSummary{}, errors.New("invalid GPU ID")
	}
	return d.summaries[gpuID], nil
}
func (d *StaticDevice) GetAllGPUInfo() ([]TritonGPUInfo, error) {
	return d.tritonInfo, nil
}
func (d *StaticDevice) GetAllSummaries() ([]DeviceSummary, error) {
	return d.summaries, nil
}

// staticCheck registers static devices when stub mode is enabled
func staticCheck(r *Registry) {
	logging.Debugf("Registering static device for stub mode")
	if err := addDeviceInterface(r, 1, config.GPU, staticDeviceStartup); err == nil {
		logging.Debugf("Using static device to obtain GPU info")
	} else {
		logging.Debugf("Error registering static device: %v", err)
	}
}

func staticDeviceStartup() Device {
	cache := NewStubbedDeviceCache()
	convertedDevices := make(map[string]Device)
	for key, cachedDevice := range cache.Devices {
		convertedDevices[key] = &StaticDevice{
			name:       cachedDevice.Name,
			deviceType: cachedDevice.DeviceType,
			hwType:     cachedDevice.HwType,
			tritonInfo: cachedDevice.TritonInfo,
			summaries:  cachedDevice.Summaries,
		}
	}
	saveCache(convertedDevices) // Call saveCache to persist the cache
	// Use the first device from the stubbed cache
	for _, cachedDevice := range cache.Devices {
		return &StaticDevice{
			name:       cachedDevice.Name,
			deviceType: cachedDevice.DeviceType,
			hwType:     cachedDevice.HwType,
			tritonInfo: cachedDevice.TritonInfo,
			summaries:  cachedDevice.Summaries,
		}
	}
	return nil
}

func NewStubbedDeviceCache() *DeviceCache {
	return stubbedDeviceCache(activeStubProfile())
}
