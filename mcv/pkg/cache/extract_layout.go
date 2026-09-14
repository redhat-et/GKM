package cache

import (
	"path/filepath"
	"strings"

	"github.com/redhat-et/GKM/mcv/pkg/cacheplan"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
)

// ConfigureVLLMExtractLayout sets how vLLM payload paths map under ExtractCacheDir.
// The primary cache root is written under the basename of the recorded VLLM_CACHE_ROOT
// (e.g. /tmp/vllm → vllm/torch_compile_cache/…); extra --source trees stay at the
// top level (e.g. triton/…), matching a host capture-root layout.
func ConfigureVLLMExtractLayout(labels map[string]string) {
	constants.VLLMExtractPrimaryDir = ""
	constants.VLLMExtractExtraSubpaths = nil

	plan, err := cacheplan.Derive(labels)
	if err != nil {
		return
	}
	if plan.CacheType != constants.CacheTypeVLLMTorchCompile {
		return
	}
	constants.VLLMExtractPrimaryDir = filepath.Base(plan.MountDir)
	if constants.VLLMExtractPrimaryDir == "." || constants.VLLMExtractPrimaryDir == "/" {
		constants.VLLMExtractPrimaryDir = ""
		return
	}
	if len(plan.Mounts) <= 1 {
		return
	}
	extras := make(map[string]struct{}, len(plan.Mounts)-1)
	for _, m := range plan.Mounts[1:] {
		extras[m.SubPath] = struct{}{}
	}
	constants.VLLMExtractExtraSubpaths = extras
}

// ResetVLLMExtractLayout clears vLLM extract layout state between operations.
func ResetVLLMExtractLayout() {
	constants.VLLMExtractPrimaryDir = ""
	constants.VLLMExtractExtraSubpaths = nil
}

// vllmPayloadDestRel maps a path relative to io.vllm.cache/ to a path under --dir.
func vllmPayloadDestRel(rel string) string {
	if constants.VLLMExtractPrimaryDir == "" {
		return rel
	}
	top := rel
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		top = rel[:i]
	}
	if constants.VLLMExtractExtraSubpaths != nil {
		if _, extra := constants.VLLMExtractExtraSubpaths[top]; extra {
			return rel
		}
	}
	return filepath.Join(constants.VLLMExtractPrimaryDir, rel)
}
