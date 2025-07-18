package cache

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/redhat-et/TKDK/mcv/pkg/constants"
)

// VLLMCache represents a VLLM-style compile cache (e.g., torch_inductor or fxgraph)
type VLLMCache struct {
	rootPath string
	tmpPath  string
}

// DetectVLLMCache walks the given root directory to detect whether VLLM-style cache artifacts exist
func DetectVLLMCache(root string) *VLLMCache {
	found := false

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.Contains(path, "vendor") || strings.HasPrefix(d.Name(), ".")) {
			return fs.SkipDir
		}
		name := d.Name()

		if strings.Contains(path, "torch_compile_cache") ||
			strings.HasSuffix(name, "vllm_compile_cache.py") ||
			strings.Contains(path, "inductor_cache") ||
			strings.Contains(path, "fxgraph") {
			found = true
			return filepath.SkipDir
		}
		return nil
	})

	if found {
		return &VLLMCache{rootPath: root}
	}
	return nil
}

func (v *VLLMCache) Name() string { return "vllm" }

func (v *VLLMCache) EntryCount() int { return 1 } // placeholder

func (v *VLLMCache) CacheSizeBytes() int64 {
	size, _ := getTotalDirSize(v.rootPath)
	return size
}

func (v *VLLMCache) Summary() string {
	// TODO: Parse and summarize key VLLM artifacts
	return "vllm cache (summary pending)"
}

func (v *VLLMCache) Labels() map[string]string {
	return map[string]string{
		"cache.vllm.image/entry-count":      fmt.Sprintf("%d", v.EntryCount()),
		"cache.vllm.image/cache-size-bytes": fmt.Sprintf("%d", v.CacheSizeBytes()),
	}
}

func (v *VLLMCache) Metadata() []CacheEntry {
	// TODO: Return actual cache metadata
	return []CacheEntry{}
}

func (v *VLLMCache) ManifestTag() string {
	return fmt.Sprintf("./%s", constants.MCVVLLMManifestDir)
}

func (v *VLLMCache) CacheTag() string {
	return fmt.Sprintf("./%s", constants.MCVMCVVLLMCacheDir)
}

func (v *VLLMCache) SetTmpPath(path string) {
	if path != "" {
		v.tmpPath = path
	}
}
