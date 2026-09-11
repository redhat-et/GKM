package cacheplan

import (
	"encoding/json"
	"testing"

	"github.com/redhat-et/GKM/mcv/pkg/constants"
)

const (
	testVLLMRoot   = "/tmp/vllm"
	testVLLMEnv    = "VLLM_CACHE_ROOT=" + testVLLMRoot
	testTritonPath = "/tmp/triton"
	testTritonSub  = "triton"
)

func TestDeriveVLLMExtraMounts(t *testing.T) {
	extras, err := json.Marshal([]Mount{
		{SubPath: testTritonSub, AbsPath: testTritonPath, Env: constants.EnvTritonCacheDir, RequiresWritable: true},
		{SubPath: "flashinfer", AbsPath: "/tmp/.cache/flashinfer", RequiresWritable: true},
	})
	if err != nil {
		t.Fatalf("marshal extras: %v", err)
	}

	labels := map[string]string{
		LabelCacheType:         constants.CacheTypeVLLMTorchCompile,
		LabelCacheRootEnv:      testVLLMEnv,
		LabelCacheMountSubpath: "torch_compile_cache/torch_aot_compile",
		LabelCacheMounts:       string(extras),
	}

	plan, err := Derive(labels)
	if err != nil {
		t.Fatalf("Derive returned error: %v", err)
	}

	if len(plan.Mounts) != 3 {
		t.Fatalf("Mounts: got %d entries, want 3", len(plan.Mounts))
	}
	if plan.Mounts[0].AbsPath != testVLLMRoot || plan.Mounts[0].Env != constants.VLLMCacheRoot {
		t.Errorf("primary mount: got %+v", plan.Mounts[0])
	}
	if plan.Mounts[0].SubPath != "torch_compile_cache/torch_aot_compile" {
		t.Errorf("primary subPath: got %q", plan.Mounts[0].SubPath)
	}
	if plan.Mounts[1].SubPath != testTritonSub || plan.Mounts[1].AbsPath != testTritonPath {
		t.Errorf("triton mount: got %+v", plan.Mounts[1])
	}
	if !plan.Mounts[1].RequiresWritable {
		t.Error("triton mount must be writable: a read-only tree raises PermissionError on miss")
	}

	// Single-mount consumers must keep seeing the primary mount unchanged.
	if plan.MountDir != testVLLMRoot {
		t.Errorf("MountDir: got %q, want %s", plan.MountDir, testVLLMRoot)
	}
	if plan.RequiresWritable {
		t.Error("RequiresWritable: got true, want false when only extra trees need writes")
	}
}

func TestDeriveVLLMRejectsUnsafeExtraMounts(t *testing.T) {
	base := map[string]string{
		LabelCacheType:         constants.CacheTypeVLLMTorchCompile,
		LabelCacheRootEnv:      testVLLMEnv,
		LabelCacheMountSubpath: constants.TorchCompileDir,
	}

	cases := map[string][]Mount{
		"empty subPath":       {{SubPath: "", AbsPath: testTritonPath}},
		"escaping subPath":    {{SubPath: "../etc", AbsPath: testTritonPath}},
		"absolute subPath":    {{SubPath: "/triton", AbsPath: testTritonPath}},
		"relative absPath":    {{SubPath: testTritonSub, AbsPath: "triton"}},
		"duplicate subPath":   {{SubPath: testTritonSub, AbsPath: "/a"}, {SubPath: testTritonSub, AbsPath: "/b"}},
		"shadowing main tree": {{SubPath: constants.TorchCompileDir, AbsPath: testTritonPath}},
	}

	for name, extras := range cases {
		t.Run(name, func(t *testing.T) {
			labels := map[string]string{}
			for k, v := range base {
				labels[k] = v
			}
			raw, err := json.Marshal(extras)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			labels[LabelCacheMounts] = string(raw)
			if _, err := Derive(labels); err == nil {
				t.Errorf("expected error for %s, got nil", name)
			}
		})
	}
}

func TestDeriveVLLMRejectsMalformedExtraMounts(t *testing.T) {
	labels := map[string]string{
		LabelCacheType:    constants.CacheTypeVLLMTorchCompile,
		LabelCacheRootEnv: testVLLMEnv,
		LabelCacheMounts:  "not-json",
	}
	if _, err := Derive(labels); err == nil {
		t.Error("expected error for malformed cache-mounts label")
	}
}

func TestDeriveWithoutExtraMountsHasSingleMount(t *testing.T) {
	labels := map[string]string{
		LabelCacheType:    constants.CacheTypeVLLMTorchCompile,
		LabelCacheRootEnv: "VLLM_CACHE_ROOT=/home/kserve/.cache/vllm",
	}

	plan, err := Derive(labels)
	if err != nil {
		t.Fatalf("Derive returned error: %v", err)
	}
	if len(plan.Mounts) != 1 {
		t.Fatalf("Mounts: got %d entries, want 1", len(plan.Mounts))
	}
	if plan.Mounts[0].AbsPath != "/home/kserve/.cache/vllm" {
		t.Errorf("mount absPath: got %q", plan.Mounts[0].AbsPath)
	}
}
