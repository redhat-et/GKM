package main

import (
	"testing"
)

func TestValidateFlagCombinations(t *testing.T) {
	tests := []struct {
		name            string
		createFlag      bool
		extractFlag     bool
		gpuInfoFlag     bool
		checkCompatFlag bool
		imageName       string
		cacheDirName    string
		stubFlag        bool
		expectError     bool
	}{
		{
			name:         "Valid create flag with image and dir",
			createFlag:   true,
			imageName:    "quay.io/gkm/cache-examples:vector-add-cache-cuda",
			cacheDirName: "../example/vector-add-cache",
			expectError:  false,
		},
		{
			name:         "Missing image name for create",
			createFlag:   true,
			cacheDirName: "../example/vector-add-cache",
			expectError:  true,
		},
		{
			name:        "Multiple action flags",
			createFlag:  true,
			extractFlag: true,
			imageName:   "quay.io/gkm/cache-examples:vector-add-cache-cuda",
			expectError: true,
		},
		{
			name:         "Invalid image name format",
			createFlag:   true,
			imageName:    "invalid:image_name",
			cacheDirName: "../example/vector-add-cache",
			expectError:  true,
		},
		{
			name:        "Stub flag without gpu-info",
			stubFlag:    true,
			expectError: true,
		},
		{
			name:            "Valid check-compat flag with image",
			checkCompatFlag: true,
			imageName:       "quay.io/gkm/cache-examples:vector-add-cache-cuda",
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFlagCombinations(tt.createFlag, tt.extractFlag, tt.gpuInfoFlag, tt.checkCompatFlag, tt.imageName, tt.cacheDirName, tt.stubFlag)
			if (err != nil) != tt.expectError {
				t.Errorf("Expected error: %v, got: %v", tt.expectError, err)
			}
		})
	}
}
