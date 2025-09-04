package devices

import "errors"

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

func NewStubbedDeviceCache() *DeviceCache {
	return &DeviceCache{
		Devices: map[string]CachedDevice{
			"gpu": {
				Name:       "STUBBED AMD",
				DeviceType: 1, // DeviceType for GPU, adjust if you have a constant
				HwType:     "gpu",
				TritonInfo: []TritonGPUInfo{
					{
						Name:              "card0",
						UUID:              "daff740f-0000-1000-8062-0165038984ec",
						ComputeCapability: "",
						Arch:              "gfx90a",
						WarpSize:          64,
						MemoryTotalMB:     65520,
						PTXVersion:        0,
						Backend:           "hip",
						ID:                0,
					},
					{
						Name:              "card1",
						UUID:              "acff740f-0000-1000-806b-c6ef57f28db1",
						ComputeCapability: "",
						Arch:              "gfx90a",
						WarpSize:          64,
						MemoryTotalMB:     65520,
						PTXVersion:        0,
						Backend:           "hip",
						ID:                1,
					},
				},
				Summaries: []DeviceSummary{
					{
						ID:            "0",
						DriverVersion: "6.12.10-100.fc40.x86_64",
						ProductName:   "STUBBED Aldebaran/MI200 [Instinct MI210]",
					},
					{
						ID:            "1",
						DriverVersion: "6.12.10-100.fc40.x86_64",
						ProductName:   "STUBBED Aldebaran/MI200 [Instinct MI210]",
					},
				},
			},
		},
	}
}
