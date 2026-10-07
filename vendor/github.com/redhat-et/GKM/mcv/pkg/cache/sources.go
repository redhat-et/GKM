package cache

import (
	"encoding/json"
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

// MaxLabelBytes is the per-label size limit OCI registries and image tools
// enforce. The extra-tree mount metadata has to fit in one label, so capture
// fails when it cannot.
const MaxLabelBytes = 4096

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

// NewCaptureSpec classifies the given source paths and verifies the result can
// be represented in the image, so callers cannot skip validation by forgetting
// to call Validate. root is the cache root being captured.
func NewCaptureSpec(sourcePaths []string, mountAt, root string) (CaptureSpec, error) {
	trees, err := DetectSourceTrees(sourcePaths)
	if err != nil {
		return CaptureSpec{}, err
	}
	spec := CaptureSpec{Sources: trees, MountAt: mountAt}
	return spec, spec.Validate(root)
}

// DetectSourceTrees classifies user-supplied extra cache directories and checks
// that they can be staged without colliding with the primary payload.
func DetectSourceTrees(paths []string) ([]SourceTree, error) {
	trees := make([]SourceTree, 0, len(paths))
	used := map[string]bool{constants.TorchCompileDir: true}

	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return nil, fmt.Errorf("invalid source path %q: path is empty", p)
		}
		abs, err := filepath.Abs(trimmed)
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
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, fmt.Errorf("source %s cannot be listed: %w", abs, err)
		}
		if len(entries) == 0 {
			logging.Warnf("Source %s is empty; capturing it adds nothing to the image", abs)
		}

		tree := classifySourceTree(abs, entries)
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
func classifySourceTree(absPath string, entries []os.DirEntry) SourceTree {
	base := filepath.Base(absPath)
	lower := strings.ToLower(base)

	// The payload name follows the directory's own basename so two trees of the
	// same kind can be captured, and the kind only decides the env variable and
	// whether the mount has to be writable.
	name := sanitizePayloadName(base)

	switch {
	case lower == SourceKindTriton || looksLikeTritonCache(entries):
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

// looksLikeTritonCache reports whether a directory listing is a Triton JIT
// cache: the SQLite handle plus group records whose JSON embeds absolute kernel
// paths.
func looksLikeTritonCache(entries []os.DirEntry) bool {
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

// MountsLabel encodes the extra trees for the io.kserve.km/cache-mounts label.
// It fails when the encoding does not fit one label: an image carrying extra
// trees without this label would restore them nowhere, so the caller must abort
// the capture rather than ship a payload whose mount metadata was dropped.
func MountsLabel(sources []SourceTree) (string, error) {
	if len(sources) == 0 {
		return "", nil
	}

	mounts := make([]cacheplan.Mount, 0, len(sources))
	for _, s := range sources {
		mounts = append(mounts, s.Mount())
	}

	raw, err := json.Marshal(mounts)
	if err != nil {
		return "", fmt.Errorf("failed to encode extra cache trees: %w", err)
	}
	if len(raw) > MaxLabelBytes {
		return string(raw), fmt.Errorf("extra cache trees need %d bytes of label, over the %d byte limit; "+
			"shorten the source paths or capture fewer trees", len(raw), MaxLabelBytes)
	}
	return string(raw), nil
}

// Validate checks that a capture specification can be fully represented in the
// image. root is the cache root being captured.
func (s CaptureSpec) Validate(root string) error {
	if s.MountAt != "" && !filepath.IsAbs(s.MountAt) {
		return fmt.Errorf("mount path %q must be absolute", s.MountAt)
	}

	absRoot := root
	if root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("invalid cache root %q: %w", root, err)
		}
		absRoot = filepath.Clean(abs)
	}

	for _, src := range s.Sources {
		if isUnder(src.AbsPath, absRoot) {
			return fmt.Errorf("source %s is inside the cache root %s, which is already captured; drop it or capture from separate paths",
				src.AbsPath, absRoot)
		}
	}

	_, err := MountsLabel(s.Sources)
	return err
}

// isUnder reports whether path is dir or nested below it.
func isUnder(path, dir string) bool {
	if dir == "" {
		return false
	}
	dir = filepath.Clean(dir)
	path = filepath.Clean(path)
	if path == dir {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
