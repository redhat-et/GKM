package preflightcheck

type SummaryTargetInfo struct {
	Backend  string `json:"backend"`
	Arch     string `json:"arch"`
	WarpSize int    `json:"warp_size"`
}

type TritonSummary struct {
	Targets []SummaryTargetInfo `json:"targets"`
}

type TritonCacheData struct {
	Hash                      string     `json:"hash"`
	Target                    Target     `json:"target"`
	Name                      string     `json:"name"`
	NumWarps                  int        `json:"num_warps"`
	NumCtas                   int        `json:"num_ctas,omitempty"`
	NumStages                 int        `json:"num_stages"`
	ClusterDims               []int      `json:"cluster_dims"`
	EnableFpFusion            bool       `json:"enable_fp_fusion"`
	SupportedFp8Dtypes        []string   `json:"supported_fp8_dtypes,omitempty"`
	DeprecatedFp8Dtypes       []string   `json:"deprecated_fp8_dtypes,omitempty"`
	DefaultDotInputPrecision  string     `json:"default_dot_input_precision"`
	AllowedDotInputPrecisions []string   `json:"allowed_dot_input_precisions"`
	MaxNumImpreciseAccDefault int        `json:"max_num_imprecise_acc_default"`
	ExternLibs                [][]string `json:"extern_libs,omitempty"`
	Debug                     bool       `json:"debug"`
	BackendName               string     `json:"backend_name"`
	SanitizeOverflow          bool       `json:"sanitize_overflow"`
	Shared                    int        `json:"shared"`
	Arch                      string     `json:"arch"`
	WarpSize                  int        `json:"warp_size"`

	// Optional/Backend-specific fields
	PtxVersion              *int    `json:"ptx_version,omitempty"`  // CUDA-only
	MaxNReg                 *int    `json:"maxnreg,omitempty"`      // CUDA
	WavesPerEU              *int    `json:"waves_per_eu,omitempty"` // ROCm-only
	LaunchCooperativeGrid   *bool   `json:"launch_cooperative_grid,omitempty"`
	MatrixInstrNonKDim      *int    `json:"matrix_instr_nonkdim,omitempty"`
	KPack                   *int    `json:"kpack,omitempty"`
	AllowFlushDenorm        *bool   `json:"allow_flush_denorm,omitempty"`
	InstructionSchedVariant *string `json:"instruction_sched_variant,omitempty"`
}

type TritonImageData struct {
	Hash       string `json:"hash"`
	DummyKey   string `json:"dummy_key,omitempty"`
	PtxVersion int    `json:"ptx_version,omitempty"`
	NumStages  int    `json:"num_stages,omitempty"`
	NumWarps   int    `json:"num_warps,omitempty"`
	Debug      bool   `json:"debug,omitempty"`
	Target
}

type Target struct {
	Backend  string `json:"backend"`
	Arch     any    `json:"arch"`
	WarpSize int    `json:"warp_size"`
}
