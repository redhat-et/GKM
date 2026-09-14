package cache

import (
	"path/filepath"
	"strings"

	"github.com/redhat-et/GKM/mcv/pkg/cacheplan"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

// ConfigureVLLMExtractLayout sets how vLLM payload paths map under ExtractCacheDir.
// Only the primary compile tree (first segment of cache-mount-subpath, usually
// torch_compile_cache) is placed under basename(VLLM_CACHE_ROOT) (e.g. vllm/).
// Extra --source trees (triton/, …) and any other payload top-level dirs stay
// directly under --dir.
func ConfigureVLLMExtractLayout(labels map[string]string) {
	constants.VLLMExtractPrimaryDir = ""
	constants.VLLMExtractPrimaryTop = ""

	if !cacheplan.IsVLLMCacheImage(labels) {
		return
	}

	var mountDir, subPath string
	plan, err := cacheplan.Derive(labels)
	if err == nil && plan.CacheType == constants.CacheTypeVLLMTorchCompile {
		mountDir = plan.MountDir
		subPath = plan.SubPath
	} else {
		logging.Debugf("vLLM extract layout: derive mount plan failed (%v), using label fallback", err)
		mountDir, _ = cacheplan.RootEnvMountDir(labels[cacheplan.LabelCacheRootEnv])
		subPath = labels[cacheplan.LabelCacheMountSubpath]
	}

	applyVLLMExtractLayout(mountDir, subPath)
}

func applyVLLMExtractLayout(mountDir, mountSubpath string) {
	primaryDir := filepath.Base(filepath.Clean(mountDir))
	if primaryDir == "" || primaryDir == "." || primaryDir == string(filepath.Separator) {
		primaryDir = "vllm"
	}
	constants.VLLMExtractPrimaryDir = primaryDir
	constants.VLLMExtractPrimaryTop = primaryPayloadTop(mountSubpath)
	logging.Infof("Extract layout: primary cache under %s/%s/; other payload tops under --dir/<name>/",
		constants.VLLMExtractPrimaryDir, constants.VLLMExtractPrimaryTop)
}

func primaryPayloadTop(mountSubpath string) string {
	mountSubpath = strings.TrimSpace(mountSubpath)
	if mountSubpath == "" || mountSubpath == "." {
		return constants.TorchCompileDir
	}
	top, _, _ := strings.Cut(mountSubpath, "/")
	if top == "" {
		return constants.TorchCompileDir
	}
	return top
}

// ResetVLLMExtractLayout clears vLLM extract layout state between operations.
func ResetVLLMExtractLayout() {
	constants.VLLMExtractPrimaryDir = ""
	constants.VLLMExtractPrimaryTop = ""
}

// vllmPayloadDestRel maps a path relative to io.vllm.cache/ to a path under --dir.
func vllmPayloadDestRel(rel string) string {
	if constants.VLLMExtractPrimaryDir == "" || constants.VLLMExtractPrimaryTop == "" {
		return rel
	}
	top := rel
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		top = rel[:i]
	}
	if top != constants.VLLMExtractPrimaryTop {
		return rel
	}
	return filepath.Join(constants.VLLMExtractPrimaryDir, rel)
}
