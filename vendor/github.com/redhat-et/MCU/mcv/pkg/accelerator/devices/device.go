/*
Copyright 2024.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package devices

import (
	"errors"
	"sort"
	"strconv"
	"sync"

	"github.com/redhat-et/MCU/mcv/pkg/config"
	logging "github.com/sirupsen/logrus"
	"golang.org/x/exp/maps"
)

const (
	MOCK DeviceType = iota
	AMD
	NVML
	ROCM
)

var (
	deviceRegistry *Registry
	once           sync.Once
)

type (
	DeviceType        int
	deviceStartupFunc func() Device // Function prototype to startup a new device instance.
	Registry          struct {
		Registry map[string]map[DeviceType]deviceStartupFunc // Static map of supported Devices Startup functions
	}
)

func (d DeviceType) String() string {
	return [...]string{"MOCK", "AMD", "NVML", "ROCM"}[d]
}

type Device interface {
	// Name returns the name of the device
	Name() string
	// DevType returns the type of the device (nvml, ...)
	DevType() DeviceType
	// GetHwType returns the type of hw the device is (gpu, processor)
	HwType() string
	// InitLib the external library loading, if any.
	InitLib() error
	// Init initizalizes and start the metric device
	Init() error
	// Shutdown stops the metric device
	Shutdown() bool
	// GetGPUInfo returns the triton info for a specific GPU
	GetGPUInfo(gpuID int) (TritonGPUInfo, error) // TODO rename
	GetSummary(gpuID int) (DeviceSummary, error)
	// GetAllGPUInfo returns the triton info for a all GPUs on the host
	GetAllGPUInfo() ([]TritonGPUInfo, error) // TODO rename
	GetAllSummaries() ([]DeviceSummary, error)
}

type DeviceSummary struct {
	ID            string
	DriverVersion string
	ProductName   string
}

type GPUFleetSummary struct {
	GPUs []GPUGroup `json:"gpus" yaml:"gpus"`
}

type GPUGroup struct {
	GPUType       string `json:"gpuType" yaml:"gpuType"`
	DriverVersion string `json:"driverVersion" yaml:"driverVersion"`
	IDs           []int  `json:"ids" yaml:"ids"`
}

// Registry gets the default device Registry instance
func GetRegistry() *Registry {
	once.Do(func() {
		deviceRegistry = newRegistry()
		registerDevices(deviceRegistry)
	})
	return deviceRegistry
}

// NewRegistry creates a new instance of Registry without registering devices
func newRegistry() *Registry {
	return &Registry{
		Registry: map[string]map[DeviceType]deviceStartupFunc{},
	}
}

// SetRegistry replaces the global registry instance
// NOTE: All plugins will need to be manually registered
// after this function is called.
func SetRegistry(registry *Registry) {
	deviceRegistry = registry
	registerDevices(deviceRegistry)
}

// Register all available devices in the global registry
func registerDevices(r *Registry) {
	// Call individual device check functions
	amdCheck(r)
	nvmlCheck(r)
	rocmCheck(r)
}

func (r *Registry) MustRegister(a string, d DeviceType, deviceStartup deviceStartupFunc) {
	_, ok := r.Registry[a][d]
	if ok {
		logging.Infof("Device with type %s already exists", d)
		return
	}
	logging.Infof("Adding the device to the registry [%s][%s]", a, d.String())
	r.Registry[a] = map[DeviceType]deviceStartupFunc{
		d: deviceStartup,
	}
}

func (r *Registry) Unregister(d DeviceType) {
	for a := range r.Registry {
		_, exists := r.Registry[a][d]
		if exists {
			delete(r.Registry[a], d)
			return
		}
	}
	logging.Debugf("Device with type %s doesn't exist", d)
}

// GetAllDeviceTypes returns a slice with all the registered devices.
func (r *Registry) GetAllDeviceTypes() []string {
	devices := append([]string{}, maps.Keys(r.Registry)...)
	return devices
}

func addDeviceInterface(registry *Registry, dtype DeviceType, accType string, deviceStartup deviceStartupFunc) error {
	switch accType {
	case config.GPU:
		switch dtype {
		case AMD:
			registry.Unregister(ROCM)
		case ROCM:
			if _, ok := registry.Registry[config.GPU][AMD]; ok {
				return errors.New("AMD already registered. Skipping ROCM")
			}
		}

		logging.Debugf("Try to Register %s", dtype)
		registry.MustRegister(accType, dtype, deviceStartup)

	default:
		logging.Debugf("Try to Register %s", dtype)
		registry.MustRegister(accType, dtype, deviceStartup)
	}

	logging.Debugf("Registered %s", dtype)

	return nil
}

// Startup initializes and returns a new Device according to the given DeviceType [NVML|OTHER].
func Startup(a string) Device {
	// Retrieve the global registry
	registry := GetRegistry()

	for d := range registry.Registry[a] {
		// Attempt to start the device from the registry
		if deviceStartup, ok := registry.Registry[a][d]; ok {
			logging.Infof("Starting up %s", d.String())
			return deviceStartup()
		}
	}

	// The device type is unsupported
	logging.Errorf("unsupported Device")
	return nil
}

// SummarizeGPUs starts the currently-registered GPU device, collects all
// summaries, coalesces them into your desired output shape, and returns it.
func SummarizeGPUs() (*GPUFleetSummary, error) {
	dev := Startup(config.GPU)
	if dev == nil {
		return nil, errors.New("no GPU device available")
	}
	defer dev.Shutdown()

	summaries, err := dev.GetAllSummaries()
	if err != nil {
		return nil, err
	}

	// Group by (ProductName, DriverVersion)
	type key struct {
		product string
		driver  string
	}
	groups := map[key]*GPUGroup{}

	for _, s := range summaries {
		idInt, _ := strconv.Atoi(s.ID) // IDs are strings in DeviceSummary; best-effort parse

		k := key{product: s.ProductName, driver: s.DriverVersion}
		if _, ok := groups[k]; !ok {
			groups[k] = &GPUGroup{
				GPUType:       s.ProductName,
				DriverVersion: s.DriverVersion,
				IDs:           []int{},
			}
		}
		groups[k].IDs = append(groups[k].IDs, idInt)
	}

	// Build deterministic, sorted output
	out := &GPUFleetSummary{GPUs: make([]GPUGroup, 0, len(groups))}
	for _, g := range groups {
		sort.Ints(g.IDs)
		out.GPUs = append(out.GPUs, *g)
	}
	sort.Slice(out.GPUs, func(i, j int) bool {
		if out.GPUs[i].GPUType == out.GPUs[j].GPUType {
			return out.GPUs[i].DriverVersion < out.GPUs[j].DriverVersion
		}
		return out.GPUs[i].GPUType < out.GPUs[j].GPUType
	})

	return out, nil
}
