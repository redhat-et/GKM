package cache

import (
	"testing"

	"github.com/redhat-et/GKM/mcv/pkg/cacheplan"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	"github.com/stretchr/testify/assert"
)

const (
	testVLLMCacheRootEnvLabel = constants.VLLMCacheRoot + "=/tmp/vllm"
	testVLLMSummaryStub       = `{}`
)

func TestVLLMPayloadDestRel(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	constants.VLLMExtractPrimaryDir = constants.VLLM
	constants.VLLMExtractPrimaryTop = constants.TorchCompileDir

	assert.Equal(t, constants.VLLM+"/torch_compile_cache/7d44010a2d/x", vllmPayloadDestRel("torch_compile_cache/7d44010a2d/x"))
	assert.Equal(t, constants.VLLM+"/torch_compile_cache/7d44010a2d/x", vllmPayloadDestRel("/torch_compile_cache/7d44010a2d/x"))
	assert.Equal(t, "triton/KERNEL/hash", vllmPayloadDestRel("triton/KERNEL/hash"))
	assert.Equal(t, "modelinfos/x.json", vllmPayloadDestRel("modelinfos/x.json"))
}

func TestConfigureVLLMExtractLayout(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	labels := map[string]string{
		cacheplan.LabelCacheRootEnv:      testVLLMCacheRootEnvLabel,
		cacheplan.LabelCacheMountSubpath: constants.TorchCompileDir + "/torch_aot_compile",
		cacheplan.LabelCacheType:         constants.CacheTypeVLLMTorchCompile,
		cacheplan.LabelFramework:         constants.VLLM,
		cacheplan.LabelVLLMSummary:         testVLLMSummaryStub,
		cacheplan.LabelCacheMounts:       `[{"subPath":"triton","absPath":"/tmp/triton","env":"TRITON_CACHE_DIR","requiresWritable":true}]`,
		cacheplan.LabelSplitCacheCapture: cacheplan.SplitCacheCaptureValue,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Equal(t, constants.VLLM, constants.VLLMExtractPrimaryDir)
	assert.Equal(t, constants.TorchCompileDir, constants.VLLMExtractPrimaryTop)
}

func TestConfigureVLLMExtractLayoutWithoutExtraMountsLabel(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	labels := map[string]string{
		cacheplan.LabelCacheRootEnv:      testVLLMCacheRootEnvLabel,
		cacheplan.LabelCacheMountSubpath: constants.TorchCompileDir,
		cacheplan.LabelCacheType:         constants.CacheTypeVLLMTorchCompile,
		cacheplan.LabelFramework:         constants.VLLM,
		cacheplan.LabelVLLMSummary:         testVLLMSummaryStub,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Empty(t, constants.VLLMExtractPrimaryDir)
	assert.Empty(t, constants.VLLMExtractPrimaryTop)
	assert.Equal(t, "torch_compile_cache/x", vllmPayloadDestRel("torch_compile_cache/x"))
	assert.Equal(t, "triton/x", vllmPayloadDestRel("triton/x"))
}

func TestConfigureVLLMExtractLayoutSplitWithoutSplitLabel(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	// Older split images may have cache-mounts but not split-cache-capture; stay flat.
	labels := map[string]string{
		cacheplan.LabelCacheRootEnv:      testVLLMCacheRootEnvLabel,
		cacheplan.LabelCacheMountSubpath: constants.TorchCompileDir,
		cacheplan.LabelCacheType:         constants.CacheTypeVLLMTorchCompile,
		cacheplan.LabelVLLMSummary:         testVLLMSummaryStub,
		cacheplan.LabelCacheMounts:       `[{"subPath":"triton","absPath":"/tmp/triton","env":"TRITON_CACHE_DIR","requiresWritable":true}]`,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Empty(t, constants.VLLMExtractPrimaryDir)
	assert.Empty(t, constants.VLLMExtractPrimaryTop)
}

func TestConfigureVLLMExtractLayoutDeriveFallback(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	// Malformed cache-mounts breaks Derive; layout should still come from root-env + subpath.
	labels := map[string]string{
		cacheplan.LabelCacheRootEnv:        testVLLMCacheRootEnvLabel,
		cacheplan.LabelCacheMountSubpath:   constants.TorchCompileDir + "/torch_aot_compile",
		cacheplan.LabelVLLMSummary:           testVLLMSummaryStub,
		cacheplan.LabelCacheMounts:         `{not valid json`,
		cacheplan.LabelSplitCacheCapture:   cacheplan.SplitCacheCaptureValue,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Equal(t, constants.VLLM, constants.VLLMExtractPrimaryDir)
	assert.Equal(t, constants.TorchCompileDir, constants.VLLMExtractPrimaryTop)
}

func TestConfigureVLLMExtractLayoutDefaultPrimaryDir(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	labels := map[string]string{
		cacheplan.LabelVLLMSummary:         testVLLMSummaryStub,
		cacheplan.LabelSplitCacheCapture: cacheplan.SplitCacheCaptureValue,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Equal(t, constants.VLLM, constants.VLLMExtractPrimaryDir)
	assert.Equal(t, constants.TorchCompileDir, constants.VLLMExtractPrimaryTop)
}
