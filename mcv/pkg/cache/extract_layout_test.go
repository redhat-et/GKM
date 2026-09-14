package cache

import (
	"testing"

	"github.com/redhat-et/GKM/mcv/pkg/cacheplan"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestVLLMPayloadDestRel(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	constants.VLLMExtractPrimaryDir = "vllm"
	constants.VLLMExtractPrimaryTop = constants.TorchCompileDir

	assert.Equal(t, "vllm/torch_compile_cache/7d44010a2d/x", vllmPayloadDestRel("torch_compile_cache/7d44010a2d/x"))
	assert.Equal(t, "vllm/torch_compile_cache/7d44010a2d/x", vllmPayloadDestRel("/torch_compile_cache/7d44010a2d/x"))
	assert.Equal(t, "triton/KERNEL/hash", vllmPayloadDestRel("triton/KERNEL/hash"))
	assert.Equal(t, "modelinfos/x.json", vllmPayloadDestRel("modelinfos/x.json"))
}

func TestConfigureVLLMExtractLayout(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	labels := map[string]string{
		cacheplan.LabelCacheRootEnv:      "VLLM_CACHE_ROOT=/tmp/vllm",
		cacheplan.LabelCacheMountSubpath: constants.TorchCompileDir + "/torch_aot_compile",
		cacheplan.LabelCacheType:           constants.CacheTypeVLLMTorchCompile,
		cacheplan.LabelFramework:           constants.VLLM,
		"cache.vllm.image/summary":         `{}`,
		cacheplan.LabelCacheMounts:         `[{"subPath":"triton","absPath":"/tmp/triton","env":"TRITON_CACHE_DIR","requiresWritable":true}]`,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Equal(t, "vllm", constants.VLLMExtractPrimaryDir)
	assert.Equal(t, constants.TorchCompileDir, constants.VLLMExtractPrimaryTop)
}

func TestConfigureVLLMExtractLayoutWithoutExtraMountsLabel(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	labels := map[string]string{
		cacheplan.LabelCacheRootEnv:      "VLLM_CACHE_ROOT=/tmp/vllm",
		cacheplan.LabelCacheMountSubpath: constants.TorchCompileDir,
		cacheplan.LabelCacheType:           constants.CacheTypeVLLMTorchCompile,
		cacheplan.LabelFramework:           constants.VLLM,
		"cache.vllm.image/summary":         `{}`,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Equal(t, "vllm", constants.VLLMExtractPrimaryDir)
	assert.Equal(t, constants.TorchCompileDir, constants.VLLMExtractPrimaryTop)
	assert.Equal(t, "triton/x", vllmPayloadDestRel("triton/x"))
}

func TestConfigureVLLMExtractLayoutDeriveFallback(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	// Malformed cache-mounts breaks Derive; layout should still come from root-env + subpath.
	labels := map[string]string{
		cacheplan.LabelCacheRootEnv:      "VLLM_CACHE_ROOT=/tmp/vllm",
		cacheplan.LabelCacheMountSubpath: constants.TorchCompileDir + "/torch_aot_compile",
		"cache.vllm.image/summary":       `{}`,
		cacheplan.LabelCacheMounts:       `{not valid json`,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Equal(t, "vllm", constants.VLLMExtractPrimaryDir)
	assert.Equal(t, constants.TorchCompileDir, constants.VLLMExtractPrimaryTop)
}

func TestConfigureVLLMExtractLayoutDefaultPrimaryDir(t *testing.T) {
	t.Cleanup(ResetVLLMExtractLayout)

	labels := map[string]string{
		"cache.vllm.image/summary": `{}`,
	}
	ConfigureVLLMExtractLayout(labels)
	assert.Equal(t, "vllm", constants.VLLMExtractPrimaryDir)
	assert.Equal(t, constants.TorchCompileDir, constants.VLLMExtractPrimaryTop)
}
