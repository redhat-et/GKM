package devices

import (
	"encoding/json"
	"testing"
)

// Test data captured from amd-smi on Strix Halo (Radeon 8060S Graphics)
const amdStaticJSON = `[
    {
        "gpu": 0,
        "asic": {
            "market_name": "Strix Halo [Radeon Graphics / Radeon 8050S Graphics / Radeon 8060S Graphics]",
            "vendor_id": "0x1002",
            "vendor_name": "Advanced Micro Devices Inc. [AMD/ATI]",
            "subvendor_id": "0x103c",
            "device_id": "0x1586",
            "subsystem_id": "0x8d1d",
            "rev_id": "0xd1",
            "asic_serial": "N/A",
            "oam_id": "N/A",
            "num_compute_units": 40,
            "target_graphics_version": "gfx1151"
        },
        "bus": {
            "bdf": "0000:c4:00.0",
            "max_pcie_width": "N/A",
            "max_pcie_speed": "N/A",
            "pcie_interface_version": "N/A",
            "slot_type": "N/A"
        },
        "vbios": {
            "name": "AMD STRIX_HALO_GENERIC",
            "build_date": "2024/06/17 02:08",
            "part_number": "113-STRXLGEN-001",
            "version": "023.011.000.039.000001"
        },
        "limit": {
            "max_power": {
                "value": 0,
                "unit": "W"
            },
            "min_power": {
                "value": 0,
                "unit": "W"
            },
            "socket_power": {
                "value": 0,
                "unit": "W"
            },
            "slowdown_edge_temperature": "N/A",
            "slowdown_hotspot_temperature": "N/A",
            "slowdown_vram_temperature": "N/A",
            "shutdown_edge_temperature": "N/A",
            "shutdown_hotspot_temperature": "N/A",
            "shutdown_vram_temperature": "N/A"
        },
        "driver": {
            "name": "amdgpu",
            "version": "6.17.1-300.fc43.x86_64"
        },
        "board": {
            "model_number": "N/A",
            "product_serial": "N/A",
            "fru_id": "N/A",
            "product_name": "Strix Halo [Radeon Graphics / Radeon 8050S Graphics / Radeon 8060S Graphics]",
            "manufacturer_name": "Advanced Micro Devices, Inc. [AMD/ATI]"
        },
        "ras": {
            "eeprom_version": "N/A",
            "parity_schema": "N/A",
            "single_bit_schema": "N/A",
            "double_bit_schema": "N/A",
            "poison_schema": "N/A",
            "ecc_block_state": "N/A"
        },
        "partition": {
            "compute_partition": "N/A",
            "memory_partition": "N/A",
            "partition_id": 0
        },
        "soc_pstate": "N/A",
        "xgmi_plpd": "N/A",
        "process_isolation": "Disabled",
        "numa": {
            "node": 0,
            "affinity": -1
        },
        "vram": {
            "type": "UNKNOWN",
            "vendor": "N/A",
            "size": {
                "value": 32768,
                "unit": "MB"
            },
            "bit_width": 256
        },
        "cache_info": [
            {
                "cache": 0,
                "cache_properties": [
                    "DATA_CACHE",
                    "SIMD_CACHE"
                ],
                "cache_size": {
                    "value": 32,
                    "unit": "KB"
                },
                "cache_level": 1,
                "max_num_cu_shared": 10,
                "num_cache_instance": 64
            },
            {
                "cache": 1,
                "cache_properties": [
                    "INST_CACHE",
                    "SIMD_CACHE"
                ],
                "cache_size": {
                    "value": 32,
                    "unit": "KB"
                },
                "cache_level": 1,
                "max_num_cu_shared": 2,
                "num_cache_instance": 20
            },
            {
                "cache": 2,
                "cache_properties": [
                    "DATA_CACHE",
                    "SIMD_CACHE"
                ],
                "cache_size": {
                    "value": 2048,
                    "unit": "KB"
                },
                "cache_level": 2,
                "max_num_cu_shared": 40,
                "num_cache_instance": 1
            },
            {
                "cache": 3,
                "cache_properties": [
                    "DATA_CACHE",
                    "SIMD_CACHE"
                ],
                "cache_size": {
                    "value": 32768,
                    "unit": "KB"
                },
                "cache_level": 3,
                "max_num_cu_shared": 40,
                "num_cache_instance": 1
            }
        ]
    }
]`

const amdListJSON = `[
    {
        "gpu": 0,
        "bdf": "0000:c4:00.0",
        "uuid": "00ff1586-0000-1000-8000-000000000000",
        "kfd_id": 42905,
        "node_id": 1,
        "partition_id": 0
    }
]`

// TestAMDStaticJSONParsing tests parsing of amd-smi static --json output
func TestAMDStaticJSONParsing(t *testing.T) {
	// Test wrapper struct parsing (new format)
	var wrapper struct {
		GPUData []*AMDCardInfo `json:"gpu_data"`
	}

	err := json.Unmarshal([]byte(amdStaticJSON), &wrapper)
	if err == nil {
		t.Logf("Successfully parsed with wrapper format: %d GPUs found", len(wrapper.GPUData))
		if len(wrapper.GPUData) > 0 {
			t.Logf("GPU 0: %s", wrapper.GPUData[0].ASIC.MarketName)
		}
	} else {
		t.Logf("Wrapper format failed (expected for ROCm 6.3.1): %v", err)

		// Try compat mode (direct array)
		err = json.Unmarshal([]byte(amdStaticJSON), &wrapper.GPUData)
		if err != nil {
			t.Fatalf("Compat mode parsing failed: %v", err)
		}
		t.Logf("Successfully parsed with compat mode: %d GPUs found", len(wrapper.GPUData))
		if len(wrapper.GPUData) > 0 {
			t.Logf("GPU 0: %s", wrapper.GPUData[0].ASIC.MarketName)
			t.Logf("  Device ID: %s", wrapper.GPUData[0].ASIC.DeviceID)
			t.Logf("  Compute Units: %d", wrapper.GPUData[0].ASIC.NumComputeUnits)
			t.Logf("  Target GFX: %s", wrapper.GPUData[0].ASIC.TargetGraphicsVersion)
			t.Logf("  VRAM: %d %s", wrapper.GPUData[0].VRAM.Size.Value, wrapper.GPUData[0].VRAM.Size.Unit)
		}
	}

	// Verify parsing into map structure (as used by getAMDGPUInfo)
	parsedGPUs := make(map[int]*AMDCardInfo)
	for _, gpu := range wrapper.GPUData {
		parsedGPUs[gpu.GPU] = gpu
	}

	if len(parsedGPUs) != 1 {
		t.Fatalf("Expected 1 GPU in map, got %d", len(parsedGPUs))
	}

	gpu0, exists := parsedGPUs[0]
	if !exists {
		t.Fatal("GPU 0 not found in parsed map")
	}

	// Verify key fields
	if gpu0.GPU != 0 {
		t.Errorf("Expected GPU ID 0, got %d", gpu0.GPU)
	}

	if gpu0.ASIC.DeviceID != "0x1586" {
		t.Errorf("Expected Device ID 0x1586, got %s", gpu0.ASIC.DeviceID)
	}

	if gpu0.ASIC.NumComputeUnits != 40 {
		t.Errorf("Expected 40 compute units, got %d", gpu0.ASIC.NumComputeUnits)
	}

	expectedVRAM := uint64(32768) // MB
	actualVRAM := calculateMemoryMB(gpu0.VRAM.Size.Value, gpu0.VRAM.Size.Unit)
	if actualVRAM != expectedVRAM {
		t.Errorf("Expected VRAM %d MB, got %d MB", expectedVRAM, actualVRAM)
	}
}

// TestAMDListJSONParsing tests parsing of amd-smi list --json output
func TestAMDListJSONParsing(t *testing.T) {
	var listInfo []*AMDListInfo
	err := json.Unmarshal([]byte(amdListJSON), &listInfo)
	if err != nil {
		t.Fatalf("Failed to parse list JSON: %v", err)
	}

	if len(listInfo) != 1 {
		t.Fatalf("Expected 1 GPU in list, got %d", len(listInfo))
	}

	parsedGPUs := make(map[int]*AMDListInfo)
	for _, gpu := range listInfo {
		parsedGPUs[gpu.GPU] = gpu
	}

	gpu0, exists := parsedGPUs[0]
	if !exists {
		t.Fatal("GPU 0 not found in parsed list map")
	}

	// Verify key fields
	if gpu0.GPU != 0 {
		t.Errorf("Expected GPU ID 0, got %d", gpu0.GPU)
	}

	if gpu0.BDF != "0000:c4:00.0" {
		t.Errorf("Expected BDF 0000:c4:00.0, got %s", gpu0.BDF)
	}

	expectedUUID := "00ff1586-0000-1000-8000-000000000000"
	if gpu0.UniqueID != expectedUUID {
		t.Errorf("Expected UUID %s, got %s", expectedUUID, gpu0.UniqueID)
	}

	if gpu0.KFDID != 42905 {
		t.Errorf("Expected KFD ID 42905, got %d", gpu0.KFDID)
	}

	t.Logf("GPU 0: BDF=%s, UUID=%s, KFD_ID=%d", gpu0.BDF, gpu0.UniqueID, gpu0.KFDID)
}

// TestGFXArchitectureTranslation tests the GPU to GFX architecture mapping
func TestGFXArchitectureTranslation(t *testing.T) {
	testCases := []struct {
		productName string
		expectedArch string
	}{
		{"Instinct MI210", "gfx90a"},
		{"Instinct MI300", "gfx90c"},
		{"Strix Halo [Radeon Graphics / Radeon 8050S Graphics / Radeon 8060S Graphics]", "Unknown architecture for this GPU"},
		{"RDNA 2 something", "gfx1030"},
	}

	for _, tc := range testCases {
		arch := TranslateGPUToArch(tc.productName)
		if arch != tc.expectedArch {
			t.Errorf("For %s: expected %s, got %s", tc.productName, tc.expectedArch, arch)
		}
	}
}
