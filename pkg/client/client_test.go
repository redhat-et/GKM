package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// check that extracting cache detects the lack of a GPU and continues without error
func TestExtractCacheWithGPUEnabled(t *testing.T) {
	gpu := true
	opts := Options{
		ImageName: "quay.io/gkm/cache-examples:vector-add-cache-cuda",
		EnableGPU: &gpu,
	}

	matchedIDs, unmatchedIDs, err := ExtractCache(opts)
	assert.NoError(t, err, "ExtractCache should not return an error")
	assert.Nil(t, matchedIDs, "Matched IDs should be nil")         // as we are on a no GPU system
	assert.Nil(t, unmatchedIDs, "Unmatched IDs should not be nil") // as we are on a no GPU system
}

// func TestExtractCacheWithBaremetalEnabled(t *testing.T) {
// 	baremetal := true
// 	opts := Options{
// 		ImageName:       "quay.io/gkm/cache-examples:vector-add-cache-cuda",
// 		EnableBaremetal: &baremetal,
// 	}

// 	matchedIDs, unmatchedIDs, err := ExtractCache(opts)
// 	assert.NoError(t, err, "ExtractCache should not return an error")
// 	assert.Nil(t, matchedIDs, "Matched IDs should be nil")
// 	assert.Nil(t, unmatchedIDs, "Unmatched IDs should not be nil")
// }

func TestExtractCacheWithSkipPrecheck(t *testing.T) {
	skipPrecheck := true
	opts := Options{
		ImageName:    "quay.io/gkm/cache-examples:vector-add-cache-cuda",
		SkipPrecheck: &skipPrecheck,
	}

	matchedIDs, unmatchedIDs, err := ExtractCache(opts)
	assert.NoError(t, err, "ExtractCache should not return an error")
	assert.Nil(t, matchedIDs, "Matched IDs should be nil")
	assert.Nil(t, unmatchedIDs, "Unmatched IDs should not be nil")
}

func TestGetSystemGPUInfoWithTimeoutDisabled(t *testing.T) {
	stub := true
	opts := HwOptions{
		EnableStub: &stub,
		Timeout:    0, // Disable timeout
	}

	summary, err := GetSystemGPUInfo(opts)
	assert.NoError(t, err, "GetSystemGPUInfo should not return an error")
	if summary != nil {
		assert.Greater(t, len(summary.GPUs), 0, "There should be at least one GPU detected")
	} else {
		t.Log("No GPUs detected, which is acceptable in some environments")
	}
}

func TestGetSystemGPUInfoWithTimeoutEnabled(t *testing.T) {
	stub := true
	opts := HwOptions{
		EnableStub: &stub,
		Timeout:    5, // Set a timeout of 5 seconds
	}

	summary, err := GetSystemGPUInfo(opts)
	assert.NoError(t, err, "GetSystemGPUInfo should not return an error")
	if summary != nil {
		assert.Greater(t, len(summary.GPUs), 0, "There should be at least one GPU detected")
	} else {
		t.Log("No GPUs detected, which is acceptable in some environments")
	}
}

// This test needs actual GPU hardware to pass
// func TestPreflightCheck(t *testing.T) {
// 	imageName := "quay.io/gkm/cache-examples:vector-add-cache-cuda"

// 	matchedIDs, unmatchedIDs, err := PreflightCheck(imageName)
// 	assert.NoError(t, err, "PreflightCheck should not return an error")
// 	assert.NotNil(t, matchedIDs, "Matched IDs should not be nil")
// 	assert.NotNil(t, unmatchedIDs, "Unmatched IDs should not be nil")
// }

func TestGetSystemGPUInfo(t *testing.T) {
	stub := true
	opts := HwOptions{
		EnableStub: &stub,
		Timeout:    10, // Set a timeout of 10 seconds
	}

	summary, err := GetSystemGPUInfo(opts)
	assert.NoError(t, err, "GetSystemGPUInfo should not return an error")
	if summary != nil {
		assert.Greater(t, len(summary.GPUs), 0, "There should be at least one GPU detected")
	} else {
		t.Log("No GPUs detected, which is acceptable in some environments")
	}
}

func TestInspectCacheImage(t *testing.T) {
	imageName := "quay.io/gkm/cache-examples:vector-add-cache-cuda"

	labels, err := InspectCacheImage(imageName)
	assert.NoError(t, err, "InspectCacheImage should not return an error")
	assert.NotNil(t, labels, "Labels should not be nil")
	assert.Greater(t, len(labels), 0, "Labels should contain at least one entry")
}
