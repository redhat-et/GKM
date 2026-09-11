package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/redhat-et/GKM/mcv/pkg/cacheplan"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

// Kinds of extra cache tree that can ride along with a vLLM capture. Modern
// vLLM keeps the Triton JIT cache outside VLLM_CACHE_ROOT (the standalone
// Inductor adaptor never redirects it), so capturing only the vLLM root leaves
// the dominant part of a cold start uncached.
const (
	SourceKindTriton     = "triton"
	SourceKindInductor   = "inductor"
	SourceKindDeepGemm   = "deep_gemm"
	SourceKindFlashinfer = "flashinfer"
	SourceKindGeneric    = "generic"
)

// SourceTree is a cache directory outside the primary cache root that is staged
// into the same OCI payload layer under io.vllm.cache/<PayloadName> and recorded
// so it can be mounted back at AbsPath in the serving container.
type SourceTree struct {
	Kind             string
	PayloadName      string
	AbsPath          string
	Env              string
	RequiresWritable bool
}

// CaptureSpec holds capture-side options that detection cannot infer from the
// primary cache directory alone.
type CaptureSpec struct {
	// Sources are extra trees to stage inside the payload, in flag order.
	Sources []SourceTree

	// MountAt overrides the path the primary cache root is mounted at. It
	// defaults to the directory the cache was captured from, which is the right
	// answer whenever the serving container is the same image with the same env.
	MountAt string
}

// DetectSourceTrees classifies user-supplied extra cache directories and checks
// that they can be staged without colliding with the primary payload.
func DetectSourceTrees(paths []string) ([]SourceTree, error) {
	trees := make([]SourceTree, 0, len(paths))
	used := map[string]bool{constants.TorchCompileDir: true}

	for _, p := range paths {
		abs, err := filepath.Abs(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("invalid source path %q: %w", p, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("source %s is not readable: %w", abs, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("source %s is not a directory", abs)
		}
		if _, err := os.ReadDir(abs); err != nil {
			return nil, fmt.Errorf("source %s cannot be listed: %w", abs, err)
		}

		tree := classifySourceTree(abs)
		if used[tree.PayloadName] {
			return nil, fmt.Errorf("source %s maps to payload name %q, which is already taken",
				abs, tree.PayloadName)
		}
		used[tree.PayloadName] = true

		if tree.Env == "" {
			logging.Warnf("Source %s has no known cache env variable; set it on the serving "+
				"container yourself so the framework finds the restored tree", abs)
		}
		logging.Infof("Capturing extra cache tree: kind=%s path=%s payload=%s env=%s writable=%t",
			tree.Kind, tree.AbsPath, tree.PayloadName, tree.Env, tree.RequiresWritable)
		trees = append(trees, tree)
	}
	return trees, nil
}

// classifySourceTree picks a payload name, cache env variable and writability
// requirement from a directory's name and contents.
func classifySourceTree(absPath string) SourceTree {
	base := filepath.Base(absPath)
	lower := strings.ToLower(base)

	// The payload name follows the directory's own basename so two trees of the
	// same kind can be captured, and the kind only decides the env variable and
	// whether the mount has to be writable.
	name := sanitizePayloadName(base)

	switch {
	case lower == SourceKindTriton || looksLikeTritonCache(absPath):
		return SourceTree{
			Kind:             SourceKindTriton,
			PayloadName:      name,
			AbsPath:          absPath,
			Env:              constants.EnvTritonCacheDir,
			RequiresWritable: true,
		}
	case strings.HasPrefix(lower, "torchinductor"):
		return SourceTree{
			Kind:             SourceKindInductor,
			PayloadName:      name,
			AbsPath:          absPath,
			Env:              constants.EnvTorchInductorCacheDir,
			RequiresWritable: true,
		}
	case lower == SourceKindDeepGemm:
		return SourceTree{
			Kind:             SourceKindDeepGemm,
			PayloadName:      name,
			AbsPath:          absPath,
			Env:              constants.EnvDeepGemmCacheDir,
			RequiresWritable: true,
		}
	case strings.Contains(lower, SourceKindFlashinfer):
		// FLASHINFER_WORKSPACE_BASE points at the workspace base, not at the
		// .cache/flashinfer directory inside it, so the correct value cannot be
		// derived from this path; leave it to the operator.
		return SourceTree{
			Kind:             SourceKindFlashinfer,
			PayloadName:      name,
			AbsPath:          absPath,
			RequiresWritable: true,
		}
	default:
		return SourceTree{
			Kind:             SourceKindGeneric,
			PayloadName:      name,
			AbsPath:          absPath,
			RequiresWritable: true,
		}
	}
}

// looksLikeTritonCache reports whether dir holds a Triton JIT cache: the SQLite
// handle plus group records whose JSON embeds absolute kernel paths.
func looksLikeTritonCache(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	var db, cacheJSON bool
	for _, e := range entries {
		switch {
		case e.Name() == "cache.db":
			db = true
		case e.Name() == "cache.json":
			cacheJSON = true
		case strings.HasPrefix(e.Name(), "__grp__") && strings.HasSuffix(e.Name(), ".json"):
			return true
		}
	}
	return db && cacheJSON
}

func sanitizePayloadName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		}
	}
	if out := b.String(); out != "" && out != "." && out != ".." {
		return out
	}
	return "source"
}

// Mounts returns the extra payload subtrees as mount entries for the
// io.kserve.km/cache-mounts label.
func (s SourceTree) Mount() cacheplan.Mount {
	return cacheplan.Mount{
		SubPath:          s.PayloadName,
		AbsPath:          s.AbsPath,
		Env:              s.Env,
		RequiresWritable: s.RequiresWritable,
	}
}
