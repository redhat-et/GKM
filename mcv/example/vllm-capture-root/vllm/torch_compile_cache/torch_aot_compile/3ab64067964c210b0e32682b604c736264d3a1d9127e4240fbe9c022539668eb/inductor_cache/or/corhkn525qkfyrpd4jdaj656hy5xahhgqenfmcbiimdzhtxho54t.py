r"""
Compile-time auto-tuning block: 

import torch
from math import inf, nan
from torch._dynamo.testing import rand_strided
from torch._dynamo.utils import preserve_rng_state
from torch._inductor.select_algorithm import AlgorithmSelectorCache
from torch._inductor.async_compile import AsyncCompile

async_compile = AsyncCompile()
generate_example_value = AlgorithmSelectorCache.generate_example_value
empty_strided_cuda = torch._C._dynamo.guards._empty_strided_cuda
empty_strided_xpu = torch._C._dynamo.guards._empty_strided_xpu
get_raw_stream = torch._C._cuda_getCurrentRawStream


# kernel path: /tmp/vllm/torch_compile_cache/torch_aot_compile/3ab64067964c210b0e32682b604c736264d3a1d9127e4240fbe9c022539668eb/inductor_cache/wc/cwcjomdr5fom2w2cyxyccuy6pbfz43izsaqkvlzietuk4vyleh2r.py
# Topologically Sorted Source Nodes: [fused_add_rms_norm_maybe_inplace, humming_gemm_1], Original ATen: [vllm_ir.fused_add_rms_norm, humming.humming_gemm]
# Source node to ATen node mapping:
#   fused_add_rms_norm_maybe_inplace => add_tensor_2, add_tensor_3, convert_element_type_default_4, convert_element_type_default_5, convert_element_type_default_7, mean_dim_1, mul_tensor_2, mul_tensor_3, pow_tensor_scalar_1, rsqrt_default_1
#   humming_gemm_1 => humming_gemm_1
# Graph fragment:
#   %humming_gemm : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=humming_gemm]
#   %arg6_1 : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=arg6_1]
#   %buf2 : Tensor "f32[s72, 1][1, s72]cuda:0" = PlaceHolder[target=buf2]
#   %arg5_1 : Tensor "f16[2048][1]cuda:0" = PlaceHolder[target=arg5_1]
#   %convert_element_type_default_4 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%humming_gemm, torch.float32), kwargs = {})
#   %convert_element_type_default_5 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%arg6_1, torch.float32), kwargs = {})
#   %add_tensor_2 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=3] = call_function[target=torch.ops.aten.add.Tensor](args = (%convert_element_type_default_4, %convert_element_type_default_5), kwargs = {})
#   %pow_tensor_scalar_1 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.pow.Tensor_Scalar](args = (%add_tensor_2, 2), kwargs = {})
#   %mean_dim_1 : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mean.dim](args = (%pow_tensor_scalar_1, [-1], True), kwargs = {})
#   %add_tensor_3 : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.add.Tensor](args = (%mean_dim_1, 1e-05), kwargs = {})
#   %rsqrt_default_1 : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.rsqrt.default](args = (%add_tensor_3,), kwargs = {})
#   %mul_tensor_2 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%add_tensor_2, %rsqrt_default_1), kwargs = {})
#   %convert_element_type_default_7 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%mul_tensor_2, torch.float16), kwargs = {})
#   %mul_tensor_3 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%convert_element_type_default_7, %arg5_1), kwargs = {})
#   %humming_gemm_1 : Tensor "f16[s72, 16384][16384, 1]cuda:0"[num_users=2] = call_function[target=torch.ops.humming.humming_gemm.default](args = (), kwargs = {layer_config: {"shape_n": 16384, "shape_k": 2048, "pad_shape_n": 0, "pad_shape_k": 0, "num_experts": 0, "b_dtype": "float8e4m3", "a_dtype": "float16", "c_dtype": "float16", "bs_dtype": "float16", "as_dtype": null, "input_scale_group_size": 0, "weight_scale_group_size": 0, "weight_scale_group_size_n": 0, "weight_scale_type": "channel", "weight_scale_2_type": "none", "use_int_weight_scale": false, "use_fused_e8m0_scale": false, "has_zero_point": false, "is_fp_zero_point": false, "has_bias": false, "mma_type": "mma", "use_packed_k_layout": false}, inputs: %mul_tensor_3, weight: %arg7_1, weight_scale: %arg8_1, compute_config: {"use_batch_invariant": false, "use_f16_accum": false, "gemm_type": "dense"}, locks: %arg9_1})
#   return %buf2,%buf3
triton_red_fused_fused_add_rms_norm_humming_gemm_0 = async_compile.triton('triton_red_fused_fused_add_rms_norm_humming_gemm_0', '''
import triton
import triton.language as tl

from torch._inductor.runtime import triton_helpers, triton_heuristics
from torch._inductor.runtime.triton_helpers import libdevice, math as tl_math
from torch._inductor.runtime.hints import AutotuneHint, ReductionHint, TileHint, DeviceProperties
triton_helpers.set_driver_to_gpu()

@triton_heuristics.reduction(
    size_hints={'x': 2048, 'r0_': 2048},
    reduction_hint=ReductionHint.INNER,
    filename=__file__,
    triton_meta={'signature': {'in_ptr0': '*fp16', 'in_ptr1': '*fp16', 'in_ptr2': '*fp16', 'out_ptr1': '*fp16', 'xnumel': 'i32', 'r0_numel': 'i32', 'XBLOCK': 'constexpr', 'R0_BLOCK': 'constexpr'}, 'device': DeviceProperties(type='cuda', index=0, multi_processor_count=40, cc=75, major=7, regs_per_multiprocessor=65536, max_threads_per_multi_processor=1024, max_threads_per_block=1024, warp_size=32), 'constants': {}, 'native_matmul': False, 'enable_fp_fusion': True, 'launch_pdl': False, 'disable_ftz': False, 'configs': [{(0,): [['tt.divisibility', 16]], (1,): [['tt.divisibility', 16]], (2,): [['tt.divisibility', 16]], (3,): [['tt.divisibility', 16]], (5,): [['tt.divisibility', 16]]}]},
    inductor_meta={'grid_type': 'Grid1D', 'kernel_name': 'triton_red_fused_fused_add_rms_norm_humming_gemm_0', 'mutated_arg_names': [], 'optimize_mem': True, 'no_x_dim': False, 'atomic_add_found': False, 'num_load': 5, 'num_store': 1, 'num_reduction': 1, 'autotune_hints': set(), 'tiling_scores': {'x': 0, 'r0_': 33558528}, 'kernel_num_gb': 0.02516992, 'kernel_flop': 0, 'backend_hash': '5E96B05ED70B79647D32C11935EAF35B0D56E688005EEF62A1E751F67D2F444C', 'assert_indirect_indexing': True, 'autotune_local_cache': True, 'autotune_pointwise': True, 'autotune_remote_cache': None, 'force_disable_caches': False, 'dynamic_scale_rblock': True, 'incremental_autotune': False, 'max_autotune': False, 'max_autotune_pointwise': False, 'min_split_scan_rblock': 256, 'spill_threshold': 16, 'store_cubin': False, 'deterministic': False, 'batch_invariant': False, 'force_filter_reduction_configs': False, 'mix_order_reduction_allow_multi_stages': True, 'dynamic_disable_pipelining': True, 'are_deterministic_algorithms_enabled': False}
)
@triton.jit
def triton_red_fused_fused_add_rms_norm_humming_gemm_0(in_ptr0, in_ptr1, in_ptr2, out_ptr1, xnumel, r0_numel, XBLOCK : tl.constexpr, R0_BLOCK : tl.constexpr):
    r0_numel = 2048
    rnumel = r0_numel
    RBLOCK: tl.constexpr = R0_BLOCK
    xoffset = tl.program_id(0) * XBLOCK
    xindex = xoffset + tl.arange(0, XBLOCK)[:, None]
    xmask = xindex < xnumel
    r0_base = tl.arange(0, R0_BLOCK)[None, :]
    rbase = r0_base
    x0 = xindex
    _tmp7 = tl.full([XBLOCK, R0_BLOCK], 0, tl.float32)
    for r0_offset in tl.range(0, r0_numel, R0_BLOCK):
        r0_index = r0_offset + r0_base
        r0_mask = r0_index < r0_numel
        roffset = r0_offset
        rindex = r0_index
        r0_1 = r0_index
        tmp0 = tl.load(in_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp2 = tl.load(in_ptr1 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp1 = tmp0.to(tl.float32)
        tmp3 = tmp2.to(tl.float32)
        tmp4 = tmp1 + tmp3
        tmp5 = tmp4 * tmp4
        tmp6 = tl.broadcast_to(tmp5, [XBLOCK, R0_BLOCK])
        tmp8 = _tmp7 + tmp6
        _tmp7 = tl.where(r0_mask & xmask, tmp8, _tmp7)
    tmp7 = tl.sum(_tmp7, 1)[:, None]
    for r0_offset in tl.range(0, r0_numel, R0_BLOCK):
        r0_index = r0_offset + r0_base
        r0_mask = r0_index < r0_numel
        roffset = r0_offset
        rindex = r0_index
        r0_1 = r0_index
        tmp9 = tl.load(in_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp11 = tl.load(in_ptr1 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp21 = tl.load(in_ptr2 + (r0_1), r0_mask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp10 = tmp9.to(tl.float32)
        tmp12 = tmp11.to(tl.float32)
        tmp13 = tmp10 + tmp12
        tmp14 = tl.full([1, 1], 2048.0, tl.float32)
        tmp15 = (tmp7 / tmp14)
        tmp16 = tl.full([1, 1], 1e-05, tl.float32)
        tmp17 = tmp15 + tmp16
        tmp18 = libdevice.rsqrt(tmp17)
        tmp19 = tmp13 * tmp18
        tmp20 = tmp19.to(tl.float32)
        tmp22 = tmp20 * tmp21
        tl.store(out_ptr1 + (r0_1 + 2048*x0), tmp22, r0_mask & xmask)
''', device_str='cuda')


# kernel path: /tmp/vllm/torch_compile_cache/torch_aot_compile/3ab64067964c210b0e32682b604c736264d3a1d9127e4240fbe9c022539668eb/inductor_cache/bm/cbmgoaz7f2z3ayv65pumtq622uls5sjavhqhacv5ata5jkvpqzpj.py
# Topologically Sorted Source Nodes: [getitem_2, silu, getitem_3, mul, humming_gemm_2], Original ATen: [aten.slice, aten.silu, aten.mul, humming.humming_gemm]
# Source node to ATen node mapping:
#   getitem_2 => slice_1
#   getitem_3 => slice_2
#   humming_gemm_2 => humming_gemm_2
#   mul => mul_30
#   silu => add_30, convert_element_type, convert_element_type_1, div, exp, neg
# Graph fragment:
#   %humming_gemm_1 : Tensor "f16[s72, 16384][16384, 1]cuda:0" = PlaceHolder[target=humming_gemm_1]
#   %slice_1 : Tensor "f16[s72, 8192][16384, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.slice.Tensor](args = (%humming_gemm_1, 1, 0, 8192), kwargs = {})
#   %convert_element_type : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=2] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%slice_1, torch.float32), kwargs = {})
#   %neg : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.neg.default](args = (%convert_element_type,), kwargs = {})
#   %exp : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.exp.default](args = (%neg,), kwargs = {})
#   %add_30 : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.add.Tensor](args = (%exp, 1), kwargs = {})
#   %div : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.div.Tensor](args = (%convert_element_type, %add_30), kwargs = {})
#   %convert_element_type_1 : Tensor "f16[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%div, torch.float16), kwargs = {})
#   %slice_2 : Tensor "f16[s72, 8192][16384, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.slice.Tensor](args = (%humming_gemm_1, 1, 8192, 9223372036854775807), kwargs = {})
#   %mul_30 : Tensor "f16[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%convert_element_type_1, %slice_2), kwargs = {})
#   %humming_gemm_2 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.humming.humming_gemm.default](args = (), kwargs = {layer_config: {"shape_n": 2048, "shape_k": 8192, "pad_shape_n": 0, "pad_shape_k": 0, "num_experts": 0, "b_dtype": "float8e4m3", "a_dtype": "float16", "c_dtype": "float16", "bs_dtype": "float16", "as_dtype": null, "input_scale_group_size": 0, "weight_scale_group_size": 0, "weight_scale_group_size_n": 0, "weight_scale_type": "channel", "weight_scale_2_type": "none", "use_int_weight_scale": false, "use_fused_e8m0_scale": false, "has_zero_point": false, "is_fp_zero_point": false, "has_bias": false, "mma_type": "mma", "use_packed_k_layout": false}, inputs: %mul_30, weight: %arg10_1, weight_scale: %arg11_1, compute_config: {"use_batch_invariant": false, "use_f16_accum": false, "gemm_type": "dense"}, locks: %arg12_1})
#   return %buf6
triton_poi_fused_humming_gemm_mul_silu_slice_1 = async_compile.triton('triton_poi_fused_humming_gemm_mul_silu_slice_1', '''
import triton
import triton.language as tl

from torch._inductor.runtime import triton_helpers, triton_heuristics
from torch._inductor.runtime.triton_helpers import libdevice, math as tl_math
from torch._inductor.runtime.hints import AutotuneHint, ReductionHint, TileHint, DeviceProperties
triton_helpers.set_driver_to_gpu()

@triton_heuristics.pointwise(
    size_hints={'x': 16777216}, 
    filename=__file__,
    triton_meta={'signature': {'in_ptr0': '*fp16', 'out_ptr0': '*fp16', 'xnumel': 'i32', 'XBLOCK': 'constexpr'}, 'device': DeviceProperties(type='cuda', index=0, multi_processor_count=40, cc=75, major=7, regs_per_multiprocessor=65536, max_threads_per_multi_processor=1024, max_threads_per_block=1024, warp_size=32), 'constants': {}, 'native_matmul': False, 'enable_fp_fusion': True, 'launch_pdl': False, 'disable_ftz': False, 'configs': [{(0,): [['tt.divisibility', 16]], (1,): [['tt.divisibility', 16]], (2,): [['tt.divisibility', 16]]}]},
    inductor_meta={'grid_type': 'Grid1D', 'kernel_name': 'triton_poi_fused_humming_gemm_mul_silu_slice_1', 'mutated_arg_names': [], 'optimize_mem': True, 'no_x_dim': False, 'atomic_add_found': False, 'num_load': 2, 'num_store': 1, 'num_reduction': 0, 'autotune_hints': set(), 'tiling_scores': {'x': 134217728}, 'kernel_num_gb': 0.100663296, 'kernel_flop': 0, 'backend_hash': '5E96B05ED70B79647D32C11935EAF35B0D56E688005EEF62A1E751F67D2F444C', 'assert_indirect_indexing': True, 'autotune_local_cache': True, 'autotune_pointwise': True, 'autotune_remote_cache': None, 'force_disable_caches': False, 'dynamic_scale_rblock': True, 'incremental_autotune': False, 'max_autotune': False, 'max_autotune_pointwise': False, 'min_split_scan_rblock': 256, 'spill_threshold': 16, 'store_cubin': False, 'deterministic': False, 'batch_invariant': False, 'force_filter_reduction_configs': False, 'mix_order_reduction_allow_multi_stages': True, 'dynamic_disable_pipelining': True, 'are_deterministic_algorithms_enabled': False},
    min_elem_per_thread=0
)
@triton.jit
def triton_poi_fused_humming_gemm_mul_silu_slice_1(in_ptr0, out_ptr0, xnumel, XBLOCK : tl.constexpr):
    xoffset = tl.program_id(0) * XBLOCK
    xindex = xoffset + tl.arange(0, XBLOCK)[:]
    xmask = tl.full([XBLOCK], True, tl.int1)[:]
    x0 = (xindex % 8192)
    x1 = xindex // 8192
    x2 = xindex
    tmp0 = tl.load(in_ptr0 + (x0 + 16384*x1), None).to(tl.float32)
    tmp8 = tl.load(in_ptr0 + (8192 + x0 + 16384*x1), None).to(tl.float32)
    tmp1 = tmp0.to(tl.float32)
    tmp2 = -tmp1
    tmp3 = libdevice.exp(tmp2)
    tmp4 = tl.full([1], 1.0, tl.float32)
    tmp5 = tmp3 + tmp4
    tmp6 = (tmp1 / tmp5)
    tmp7 = tmp6.to(tl.float32)
    tmp9 = tmp7 * tmp8
    tl.store(out_ptr0 + (x2), tmp9, None)
''', device_str='cuda')


# kernel path: /tmp/vllm/torch_compile_cache/torch_aot_compile/3ab64067964c210b0e32682b604c736264d3a1d9127e4240fbe9c022539668eb/inductor_cache/xy/cxy5or7oi2o2kyld33oplfcyqjr5bsfkrqpvpxnwjblmxyd5ro7d.py
# Topologically Sorted Source Nodes: [fused_add_rms_norm_maybe_inplace, fused_add_rms_norm_maybe_inplace_1], Original ATen: [vllm_ir.fused_add_rms_norm]
# Source node to ATen node mapping:
#   fused_add_rms_norm_maybe_inplace => add_tensor_2, convert_element_type_default_4, convert_element_type_default_5, convert_element_type_default_6
#   fused_add_rms_norm_maybe_inplace_1 => add_tensor, add_tensor_1, convert_element_type_default, convert_element_type_default_1, convert_element_type_default_3, mean_dim, mul_tensor, mul_tensor_1, pow_tensor_scalar, rsqrt_default
# Graph fragment:
#   %humming_gemm_2 : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=humming_gemm_2]
#   %humming_gemm : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=humming_gemm]
#   %arg6_1 : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=arg6_1]
#   %buf9 : Tensor "f32[s72, 1][1, s72]cuda:0" = PlaceHolder[target=buf9]
#   %arg13_1 : Tensor "f16[2048][1]cuda:0" = PlaceHolder[target=arg13_1]
#   %convert_element_type_default_4 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%humming_gemm, torch.float32), kwargs = {})
#   %convert_element_type_default_5 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%arg6_1, torch.float32), kwargs = {})
#   %add_tensor_2 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=3] = call_function[target=torch.ops.aten.add.Tensor](args = (%convert_element_type_default_4, %convert_element_type_default_5), kwargs = {})
#   %convert_element_type_default_6 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%add_tensor_2, torch.float16), kwargs = {})
#   %convert_element_type_default : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%humming_gemm_2, torch.float32), kwargs = {})
#   %convert_element_type_default_1 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%convert_element_type_default_6, torch.float32), kwargs = {})
#   %add_tensor : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=2] = call_function[target=torch.ops.aten.add.Tensor](args = (%convert_element_type_default, %convert_element_type_default_1), kwargs = {})
#   %pow_tensor_scalar : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.pow.Tensor_Scalar](args = (%add_tensor, 2), kwargs = {})
#   %mean_dim : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mean.dim](args = (%pow_tensor_scalar, [-1], True), kwargs = {})
#   %add_tensor_1 : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.add.Tensor](args = (%mean_dim, 1e-05), kwargs = {})
#   %rsqrt_default : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.rsqrt.default](args = (%add_tensor_1,), kwargs = {})
#   %mul_tensor : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%add_tensor, %rsqrt_default), kwargs = {})
#   %convert_element_type_default_3 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%mul_tensor, torch.float16), kwargs = {})
#   %mul_tensor_1 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%convert_element_type_default_3, %arg13_1), kwargs = {})
#   return %buf9,%mul_tensor_1
triton_red_fused_fused_add_rms_norm_2 = async_compile.triton('triton_red_fused_fused_add_rms_norm_2', '''
import triton
import triton.language as tl

from torch._inductor.runtime import triton_helpers, triton_heuristics
from torch._inductor.runtime.triton_helpers import libdevice, math as tl_math
from torch._inductor.runtime.hints import AutotuneHint, ReductionHint, TileHint, DeviceProperties
triton_helpers.set_driver_to_gpu()

@triton_heuristics.reduction(
    size_hints={'x': 2048, 'r0_': 2048},
    reduction_hint=ReductionHint.INNER,
    filename=__file__,
    triton_meta={'signature': {'in_out_ptr0': '*fp16', 'in_ptr0': '*fp16', 'in_ptr1': '*fp16', 'in_ptr2': '*fp16', 'xnumel': 'i32', 'r0_numel': 'i32', 'XBLOCK': 'constexpr', 'R0_BLOCK': 'constexpr'}, 'device': DeviceProperties(type='cuda', index=0, multi_processor_count=40, cc=75, major=7, regs_per_multiprocessor=65536, max_threads_per_multi_processor=1024, max_threads_per_block=1024, warp_size=32), 'constants': {}, 'native_matmul': False, 'enable_fp_fusion': True, 'launch_pdl': False, 'disable_ftz': False, 'configs': [{(0,): [['tt.divisibility', 16]], (1,): [['tt.divisibility', 16]], (2,): [['tt.divisibility', 16]], (3,): [['tt.divisibility', 16]], (5,): [['tt.divisibility', 16]]}]},
    inductor_meta={'grid_type': 'Grid1D', 'kernel_name': 'triton_red_fused_fused_add_rms_norm_2', 'mutated_arg_names': ['in_out_ptr0'], 'optimize_mem': True, 'no_x_dim': False, 'atomic_add_found': False, 'num_load': 7, 'num_store': 1, 'num_reduction': 1, 'autotune_hints': set(), 'tiling_scores': {'x': 0, 'r0_': 41947136}, 'kernel_num_gb': 0.033558528, 'kernel_flop': 0, 'backend_hash': '5E96B05ED70B79647D32C11935EAF35B0D56E688005EEF62A1E751F67D2F444C', 'assert_indirect_indexing': True, 'autotune_local_cache': True, 'autotune_pointwise': True, 'autotune_remote_cache': None, 'force_disable_caches': False, 'dynamic_scale_rblock': True, 'incremental_autotune': False, 'max_autotune': False, 'max_autotune_pointwise': False, 'min_split_scan_rblock': 256, 'spill_threshold': 16, 'store_cubin': False, 'deterministic': False, 'batch_invariant': False, 'force_filter_reduction_configs': False, 'mix_order_reduction_allow_multi_stages': True, 'dynamic_disable_pipelining': True, 'are_deterministic_algorithms_enabled': False}
)
@triton.jit
def triton_red_fused_fused_add_rms_norm_2(in_out_ptr0, in_ptr0, in_ptr1, in_ptr2, xnumel, r0_numel, XBLOCK : tl.constexpr, R0_BLOCK : tl.constexpr):
    r0_numel = 2048
    rnumel = r0_numel
    RBLOCK: tl.constexpr = R0_BLOCK
    xoffset = tl.program_id(0) * XBLOCK
    xindex = xoffset + tl.arange(0, XBLOCK)[:, None]
    xmask = xindex < xnumel
    r0_base = tl.arange(0, R0_BLOCK)[None, :]
    rbase = r0_base
    x0 = xindex
    _tmp12 = tl.full([XBLOCK, R0_BLOCK], 0, tl.float32)
    for r0_offset in tl.range(0, r0_numel, R0_BLOCK):
        r0_index = r0_offset + r0_base
        r0_mask = r0_index < r0_numel
        roffset = r0_offset
        rindex = r0_index
        r0_1 = r0_index
        tmp0 = tl.load(in_out_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp2 = tl.load(in_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp4 = tl.load(in_ptr1 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp1 = tmp0.to(tl.float32)
        tmp3 = tmp2.to(tl.float32)
        tmp5 = tmp4.to(tl.float32)
        tmp6 = tmp3 + tmp5
        tmp7 = tmp6.to(tl.float32)
        tmp8 = tmp7.to(tl.float32)
        tmp9 = tmp1 + tmp8
        tmp10 = tmp9 * tmp9
        tmp11 = tl.broadcast_to(tmp10, [XBLOCK, R0_BLOCK])
        tmp13 = _tmp12 + tmp11
        _tmp12 = tl.where(r0_mask & xmask, tmp13, _tmp12)
    tmp12 = tl.sum(_tmp12, 1)[:, None]
    for r0_offset in tl.range(0, r0_numel, R0_BLOCK):
        r0_index = r0_offset + r0_base
        r0_mask = r0_index < r0_numel
        roffset = r0_offset
        rindex = r0_index
        r0_1 = r0_index
        tmp14 = tl.load(in_out_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp16 = tl.load(in_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp18 = tl.load(in_ptr1 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp31 = tl.load(in_ptr2 + (r0_1), r0_mask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp15 = tmp14.to(tl.float32)
        tmp17 = tmp16.to(tl.float32)
        tmp19 = tmp18.to(tl.float32)
        tmp20 = tmp17 + tmp19
        tmp21 = tmp20.to(tl.float32)
        tmp22 = tmp21.to(tl.float32)
        tmp23 = tmp15 + tmp22
        tmp24 = tl.full([1, 1], 2048.0, tl.float32)
        tmp25 = (tmp12 / tmp24)
        tmp26 = tl.full([1, 1], 1e-05, tl.float32)
        tmp27 = tmp25 + tmp26
        tmp28 = libdevice.rsqrt(tmp27)
        tmp29 = tmp23 * tmp28
        tmp30 = tmp29.to(tl.float32)
        tmp32 = tmp30 * tmp31
        tl.store(in_out_ptr0 + (r0_1 + 2048*x0), tmp32, r0_mask & xmask)
''', device_str='cuda')

async_compile.wait(globals())
del async_compile

import triton
import triton.language as tl
from torch._inductor.runtime.triton_heuristics import start_graph, end_graph
from torch._C import _cuda_getCurrentRawStream as get_raw_stream
with torch.cuda._DeviceGuard(0):
    raw_stream0 = get_raw_stream(0)
raw_stream0 = get_raw_stream(0)
buf1 = generate_example_value((2048, 2048), (2048, 1), 'cuda:0', torch.float16, 0, (2048, 2048))
arg6_1 = generate_example_value((2048, 2048), (2048, 1), 'cuda:0', torch.float16, 0, (2048, 2048))
arg5_1 = generate_example_value((2048,), (1,), 'cuda:0', torch.float16, 0, (2048,))
buf3 = generate_example_value((2048, 2048), (2048, 1), 'cuda:0', torch.float16, 0, (2048, 2048))
with torch.cuda._DeviceGuard(0):
    triton_red_fused_fused_add_rms_norm_humming_gemm_0.run(buf1, arg6_1, arg5_1, buf3, 2048, 2048, stream=raw_stream0)
del arg5_1, buf3

raw_stream0 = get_raw_stream(0)
buf5 = generate_example_value((2048, 16384), (16384, 1), 'cuda:0', torch.float16, 0, (2048, 16384))
buf6 = generate_example_value((2048, 8192), (8192, 1), 'cuda:0', torch.float16, 0, (2048, 8192))
with torch.cuda._DeviceGuard(0):
    triton_poi_fused_humming_gemm_mul_silu_slice_1.run(buf5, buf6, 16777216, stream=raw_stream0)
del buf5, buf6

raw_stream0 = get_raw_stream(0)
buf10 = generate_example_value((2048, 2048), (2048, 1), 'cuda:0', torch.float16, 0, (2048, 2048))
arg13_1 = generate_example_value((2048,), (1,), 'cuda:0', torch.float16, 0, (2048,))
with torch.cuda._DeviceGuard(0):
    triton_red_fused_fused_add_rms_norm_2.run(buf10, buf1, arg6_1, arg13_1, 2048, 2048, stream=raw_stream0)
del buf1, arg6_1, buf10, arg13_1

"""
# AOT ID: ['16_inference']
from ctypes import c_void_p, c_long, c_int
import torch
import math
import random
import os
import tempfile
from math import inf, nan
from cmath import nanj
from torch._inductor.hooks import run_intermediate_hooks
from torch._inductor.utils import maybe_profile
from torch._inductor.codegen.memory_planning import _align as align
from torch import device, empty_strided
from torch._inductor.async_compile import AsyncCompile
from torch._inductor.select_algorithm import extern_kernels
from torch._C._dynamo.guards import copy_if_misaligned
import triton
import triton.language as tl
from torch._inductor.runtime.triton_heuristics import start_graph, end_graph
from torch._C import _cuda_getCurrentRawStream as get_raw_stream

aten = torch.ops.aten
inductor_ops = torch.ops.inductor
_quantized = torch.ops._quantized
assert_size_stride = torch._C._dynamo.guards.assert_size_stride
assert_alignment = torch._C._dynamo.guards.assert_alignment
empty_strided_cpu = torch._C._dynamo.guards._empty_strided_cpu
empty_strided_cpu_pinned = torch._C._dynamo.guards._empty_strided_cpu_pinned
empty_strided_cuda = torch._C._dynamo.guards._empty_strided_cuda
empty_strided_xpu = torch._C._dynamo.guards._empty_strided_xpu
empty_strided_mtia = torch._C._dynamo.guards._empty_strided_mtia
reinterpret_tensor = torch._C._dynamo.guards._reinterpret_tensor
alloc_from_pool = torch.ops.inductor._alloc_from_pool
async_compile = AsyncCompile()
empty_strided_p2p = torch._C._distributed_c10d._SymmetricMemory.empty_strided_p2p


# kernel path: /tmp/vllm/torch_compile_cache/torch_aot_compile/3ab64067964c210b0e32682b604c736264d3a1d9127e4240fbe9c022539668eb/inductor_cache/wc/cwcjomdr5fom2w2cyxyccuy6pbfz43izsaqkvlzietuk4vyleh2r.py
# Topologically Sorted Source Nodes: [fused_add_rms_norm_maybe_inplace, humming_gemm_1], Original ATen: [vllm_ir.fused_add_rms_norm, humming.humming_gemm]
# Source node to ATen node mapping:
#   fused_add_rms_norm_maybe_inplace => add_tensor_2, add_tensor_3, convert_element_type_default_4, convert_element_type_default_5, convert_element_type_default_7, mean_dim_1, mul_tensor_2, mul_tensor_3, pow_tensor_scalar_1, rsqrt_default_1
#   humming_gemm_1 => humming_gemm_1
# Graph fragment:
#   %humming_gemm : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=humming_gemm]
#   %arg6_1 : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=arg6_1]
#   %buf2 : Tensor "f32[s72, 1][1, s72]cuda:0" = PlaceHolder[target=buf2]
#   %arg5_1 : Tensor "f16[2048][1]cuda:0" = PlaceHolder[target=arg5_1]
#   %convert_element_type_default_4 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%humming_gemm, torch.float32), kwargs = {})
#   %convert_element_type_default_5 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%arg6_1, torch.float32), kwargs = {})
#   %add_tensor_2 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=3] = call_function[target=torch.ops.aten.add.Tensor](args = (%convert_element_type_default_4, %convert_element_type_default_5), kwargs = {})
#   %pow_tensor_scalar_1 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.pow.Tensor_Scalar](args = (%add_tensor_2, 2), kwargs = {})
#   %mean_dim_1 : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mean.dim](args = (%pow_tensor_scalar_1, [-1], True), kwargs = {})
#   %add_tensor_3 : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.add.Tensor](args = (%mean_dim_1, 1e-05), kwargs = {})
#   %rsqrt_default_1 : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.rsqrt.default](args = (%add_tensor_3,), kwargs = {})
#   %mul_tensor_2 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%add_tensor_2, %rsqrt_default_1), kwargs = {})
#   %convert_element_type_default_7 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%mul_tensor_2, torch.float16), kwargs = {})
#   %mul_tensor_3 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%convert_element_type_default_7, %arg5_1), kwargs = {})
#   %humming_gemm_1 : Tensor "f16[s72, 16384][16384, 1]cuda:0"[num_users=2] = call_function[target=torch.ops.humming.humming_gemm.default](args = (), kwargs = {layer_config: {"shape_n": 16384, "shape_k": 2048, "pad_shape_n": 0, "pad_shape_k": 0, "num_experts": 0, "b_dtype": "float8e4m3", "a_dtype": "float16", "c_dtype": "float16", "bs_dtype": "float16", "as_dtype": null, "input_scale_group_size": 0, "weight_scale_group_size": 0, "weight_scale_group_size_n": 0, "weight_scale_type": "channel", "weight_scale_2_type": "none", "use_int_weight_scale": false, "use_fused_e8m0_scale": false, "has_zero_point": false, "is_fp_zero_point": false, "has_bias": false, "mma_type": "mma", "use_packed_k_layout": false}, inputs: %mul_tensor_3, weight: %arg7_1, weight_scale: %arg8_1, compute_config: {"use_batch_invariant": false, "use_f16_accum": false, "gemm_type": "dense"}, locks: %arg9_1})
#   return %buf2,%buf3
triton_red_fused_fused_add_rms_norm_humming_gemm_0 = async_compile.triton('triton_red_fused_fused_add_rms_norm_humming_gemm_0', '''
import triton
import triton.language as tl

from torch._inductor.runtime import triton_helpers, triton_heuristics
from torch._inductor.runtime.triton_helpers import libdevice, math as tl_math
from torch._inductor.runtime.hints import AutotuneHint, ReductionHint, TileHint, DeviceProperties
triton_helpers.set_driver_to_gpu()

@triton_heuristics.reduction(
    size_hints={'x': 2048, 'r0_': 2048},
    reduction_hint=ReductionHint.INNER,
    filename=__file__,
    triton_meta={'signature': {'in_ptr0': '*fp16', 'in_ptr1': '*fp16', 'in_ptr2': '*fp16', 'out_ptr1': '*fp16', 'xnumel': 'i32', 'r0_numel': 'i32', 'XBLOCK': 'constexpr', 'R0_BLOCK': 'constexpr'}, 'device': DeviceProperties(type='cuda', index=0, multi_processor_count=40, cc=75, major=7, regs_per_multiprocessor=65536, max_threads_per_multi_processor=1024, max_threads_per_block=1024, warp_size=32), 'constants': {}, 'native_matmul': False, 'enable_fp_fusion': True, 'launch_pdl': False, 'disable_ftz': False, 'configs': [{(0,): [['tt.divisibility', 16]], (1,): [['tt.divisibility', 16]], (2,): [['tt.divisibility', 16]], (3,): [['tt.divisibility', 16]], (5,): [['tt.divisibility', 16]]}]},
    inductor_meta={'grid_type': 'Grid1D', 'kernel_name': 'triton_red_fused_fused_add_rms_norm_humming_gemm_0', 'mutated_arg_names': [], 'optimize_mem': True, 'no_x_dim': False, 'atomic_add_found': False, 'num_load': 5, 'num_store': 1, 'num_reduction': 1, 'autotune_hints': set(), 'tiling_scores': {'x': 0, 'r0_': 33558528}, 'kernel_num_gb': 0.02516992, 'kernel_flop': 0, 'backend_hash': '5E96B05ED70B79647D32C11935EAF35B0D56E688005EEF62A1E751F67D2F444C', 'assert_indirect_indexing': True, 'autotune_local_cache': True, 'autotune_pointwise': True, 'autotune_remote_cache': None, 'force_disable_caches': False, 'dynamic_scale_rblock': True, 'incremental_autotune': False, 'max_autotune': False, 'max_autotune_pointwise': False, 'min_split_scan_rblock': 256, 'spill_threshold': 16, 'store_cubin': False, 'deterministic': False, 'batch_invariant': False, 'force_filter_reduction_configs': False, 'mix_order_reduction_allow_multi_stages': True, 'dynamic_disable_pipelining': True, 'are_deterministic_algorithms_enabled': False}
)
@triton.jit
def triton_red_fused_fused_add_rms_norm_humming_gemm_0(in_ptr0, in_ptr1, in_ptr2, out_ptr1, xnumel, r0_numel, XBLOCK : tl.constexpr, R0_BLOCK : tl.constexpr):
    r0_numel = 2048
    rnumel = r0_numel
    RBLOCK: tl.constexpr = R0_BLOCK
    xoffset = tl.program_id(0) * XBLOCK
    xindex = xoffset + tl.arange(0, XBLOCK)[:, None]
    xmask = xindex < xnumel
    r0_base = tl.arange(0, R0_BLOCK)[None, :]
    rbase = r0_base
    x0 = xindex
    _tmp7 = tl.full([XBLOCK, R0_BLOCK], 0, tl.float32)
    for r0_offset in tl.range(0, r0_numel, R0_BLOCK):
        r0_index = r0_offset + r0_base
        r0_mask = r0_index < r0_numel
        roffset = r0_offset
        rindex = r0_index
        r0_1 = r0_index
        tmp0 = tl.load(in_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp2 = tl.load(in_ptr1 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp1 = tmp0.to(tl.float32)
        tmp3 = tmp2.to(tl.float32)
        tmp4 = tmp1 + tmp3
        tmp5 = tmp4 * tmp4
        tmp6 = tl.broadcast_to(tmp5, [XBLOCK, R0_BLOCK])
        tmp8 = _tmp7 + tmp6
        _tmp7 = tl.where(r0_mask & xmask, tmp8, _tmp7)
    tmp7 = tl.sum(_tmp7, 1)[:, None]
    for r0_offset in tl.range(0, r0_numel, R0_BLOCK):
        r0_index = r0_offset + r0_base
        r0_mask = r0_index < r0_numel
        roffset = r0_offset
        rindex = r0_index
        r0_1 = r0_index
        tmp9 = tl.load(in_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp11 = tl.load(in_ptr1 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp21 = tl.load(in_ptr2 + (r0_1), r0_mask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp10 = tmp9.to(tl.float32)
        tmp12 = tmp11.to(tl.float32)
        tmp13 = tmp10 + tmp12
        tmp14 = tl.full([1, 1], 2048.0, tl.float32)
        tmp15 = (tmp7 / tmp14)
        tmp16 = tl.full([1, 1], 1e-05, tl.float32)
        tmp17 = tmp15 + tmp16
        tmp18 = libdevice.rsqrt(tmp17)
        tmp19 = tmp13 * tmp18
        tmp20 = tmp19.to(tl.float32)
        tmp22 = tmp20 * tmp21
        tl.store(out_ptr1 + (r0_1 + 2048*x0), tmp22, r0_mask & xmask)
''', device_str='cuda')


# kernel path: /tmp/vllm/torch_compile_cache/torch_aot_compile/3ab64067964c210b0e32682b604c736264d3a1d9127e4240fbe9c022539668eb/inductor_cache/bm/cbmgoaz7f2z3ayv65pumtq622uls5sjavhqhacv5ata5jkvpqzpj.py
# Topologically Sorted Source Nodes: [getitem_2, silu, getitem_3, mul, humming_gemm_2], Original ATen: [aten.slice, aten.silu, aten.mul, humming.humming_gemm]
# Source node to ATen node mapping:
#   getitem_2 => slice_1
#   getitem_3 => slice_2
#   humming_gemm_2 => humming_gemm_2
#   mul => mul_30
#   silu => add_30, convert_element_type, convert_element_type_1, div, exp, neg
# Graph fragment:
#   %humming_gemm_1 : Tensor "f16[s72, 16384][16384, 1]cuda:0" = PlaceHolder[target=humming_gemm_1]
#   %slice_1 : Tensor "f16[s72, 8192][16384, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.slice.Tensor](args = (%humming_gemm_1, 1, 0, 8192), kwargs = {})
#   %convert_element_type : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=2] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%slice_1, torch.float32), kwargs = {})
#   %neg : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.neg.default](args = (%convert_element_type,), kwargs = {})
#   %exp : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.exp.default](args = (%neg,), kwargs = {})
#   %add_30 : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.add.Tensor](args = (%exp, 1), kwargs = {})
#   %div : Tensor "f32[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.div.Tensor](args = (%convert_element_type, %add_30), kwargs = {})
#   %convert_element_type_1 : Tensor "f16[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%div, torch.float16), kwargs = {})
#   %slice_2 : Tensor "f16[s72, 8192][16384, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.slice.Tensor](args = (%humming_gemm_1, 1, 8192, 9223372036854775807), kwargs = {})
#   %mul_30 : Tensor "f16[s72, 8192][8192, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%convert_element_type_1, %slice_2), kwargs = {})
#   %humming_gemm_2 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.humming.humming_gemm.default](args = (), kwargs = {layer_config: {"shape_n": 2048, "shape_k": 8192, "pad_shape_n": 0, "pad_shape_k": 0, "num_experts": 0, "b_dtype": "float8e4m3", "a_dtype": "float16", "c_dtype": "float16", "bs_dtype": "float16", "as_dtype": null, "input_scale_group_size": 0, "weight_scale_group_size": 0, "weight_scale_group_size_n": 0, "weight_scale_type": "channel", "weight_scale_2_type": "none", "use_int_weight_scale": false, "use_fused_e8m0_scale": false, "has_zero_point": false, "is_fp_zero_point": false, "has_bias": false, "mma_type": "mma", "use_packed_k_layout": false}, inputs: %mul_30, weight: %arg10_1, weight_scale: %arg11_1, compute_config: {"use_batch_invariant": false, "use_f16_accum": false, "gemm_type": "dense"}, locks: %arg12_1})
#   return %buf6
triton_poi_fused_humming_gemm_mul_silu_slice_1 = async_compile.triton('triton_poi_fused_humming_gemm_mul_silu_slice_1', '''
import triton
import triton.language as tl

from torch._inductor.runtime import triton_helpers, triton_heuristics
from torch._inductor.runtime.triton_helpers import libdevice, math as tl_math
from torch._inductor.runtime.hints import AutotuneHint, ReductionHint, TileHint, DeviceProperties
triton_helpers.set_driver_to_gpu()

@triton_heuristics.pointwise(
    size_hints={'x': 16777216}, 
    filename=__file__,
    triton_meta={'signature': {'in_ptr0': '*fp16', 'out_ptr0': '*fp16', 'xnumel': 'i32', 'XBLOCK': 'constexpr'}, 'device': DeviceProperties(type='cuda', index=0, multi_processor_count=40, cc=75, major=7, regs_per_multiprocessor=65536, max_threads_per_multi_processor=1024, max_threads_per_block=1024, warp_size=32), 'constants': {}, 'native_matmul': False, 'enable_fp_fusion': True, 'launch_pdl': False, 'disable_ftz': False, 'configs': [{(0,): [['tt.divisibility', 16]], (1,): [['tt.divisibility', 16]], (2,): [['tt.divisibility', 16]]}]},
    inductor_meta={'grid_type': 'Grid1D', 'kernel_name': 'triton_poi_fused_humming_gemm_mul_silu_slice_1', 'mutated_arg_names': [], 'optimize_mem': True, 'no_x_dim': False, 'atomic_add_found': False, 'num_load': 2, 'num_store': 1, 'num_reduction': 0, 'autotune_hints': set(), 'tiling_scores': {'x': 134217728}, 'kernel_num_gb': 0.100663296, 'kernel_flop': 0, 'backend_hash': '5E96B05ED70B79647D32C11935EAF35B0D56E688005EEF62A1E751F67D2F444C', 'assert_indirect_indexing': True, 'autotune_local_cache': True, 'autotune_pointwise': True, 'autotune_remote_cache': None, 'force_disable_caches': False, 'dynamic_scale_rblock': True, 'incremental_autotune': False, 'max_autotune': False, 'max_autotune_pointwise': False, 'min_split_scan_rblock': 256, 'spill_threshold': 16, 'store_cubin': False, 'deterministic': False, 'batch_invariant': False, 'force_filter_reduction_configs': False, 'mix_order_reduction_allow_multi_stages': True, 'dynamic_disable_pipelining': True, 'are_deterministic_algorithms_enabled': False},
    min_elem_per_thread=0
)
@triton.jit
def triton_poi_fused_humming_gemm_mul_silu_slice_1(in_ptr0, out_ptr0, xnumel, XBLOCK : tl.constexpr):
    xoffset = tl.program_id(0) * XBLOCK
    xindex = xoffset + tl.arange(0, XBLOCK)[:]
    xmask = tl.full([XBLOCK], True, tl.int1)[:]
    x0 = (xindex % 8192)
    x1 = xindex // 8192
    x2 = xindex
    tmp0 = tl.load(in_ptr0 + (x0 + 16384*x1), None).to(tl.float32)
    tmp8 = tl.load(in_ptr0 + (8192 + x0 + 16384*x1), None).to(tl.float32)
    tmp1 = tmp0.to(tl.float32)
    tmp2 = -tmp1
    tmp3 = libdevice.exp(tmp2)
    tmp4 = tl.full([1], 1.0, tl.float32)
    tmp5 = tmp3 + tmp4
    tmp6 = (tmp1 / tmp5)
    tmp7 = tmp6.to(tl.float32)
    tmp9 = tmp7 * tmp8
    tl.store(out_ptr0 + (x2), tmp9, None)
''', device_str='cuda')


# kernel path: /tmp/vllm/torch_compile_cache/torch_aot_compile/3ab64067964c210b0e32682b604c736264d3a1d9127e4240fbe9c022539668eb/inductor_cache/xy/cxy5or7oi2o2kyld33oplfcyqjr5bsfkrqpvpxnwjblmxyd5ro7d.py
# Topologically Sorted Source Nodes: [fused_add_rms_norm_maybe_inplace, fused_add_rms_norm_maybe_inplace_1], Original ATen: [vllm_ir.fused_add_rms_norm]
# Source node to ATen node mapping:
#   fused_add_rms_norm_maybe_inplace => add_tensor_2, convert_element_type_default_4, convert_element_type_default_5, convert_element_type_default_6
#   fused_add_rms_norm_maybe_inplace_1 => add_tensor, add_tensor_1, convert_element_type_default, convert_element_type_default_1, convert_element_type_default_3, mean_dim, mul_tensor, mul_tensor_1, pow_tensor_scalar, rsqrt_default
# Graph fragment:
#   %humming_gemm_2 : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=humming_gemm_2]
#   %humming_gemm : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=humming_gemm]
#   %arg6_1 : Tensor "f16[s72, 2048][2048, 1]cuda:0" = PlaceHolder[target=arg6_1]
#   %buf9 : Tensor "f32[s72, 1][1, s72]cuda:0" = PlaceHolder[target=buf9]
#   %arg13_1 : Tensor "f16[2048][1]cuda:0" = PlaceHolder[target=arg13_1]
#   %convert_element_type_default_4 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%humming_gemm, torch.float32), kwargs = {})
#   %convert_element_type_default_5 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%arg6_1, torch.float32), kwargs = {})
#   %add_tensor_2 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=3] = call_function[target=torch.ops.aten.add.Tensor](args = (%convert_element_type_default_4, %convert_element_type_default_5), kwargs = {})
#   %convert_element_type_default_6 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%add_tensor_2, torch.float16), kwargs = {})
#   %convert_element_type_default : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%humming_gemm_2, torch.float32), kwargs = {})
#   %convert_element_type_default_1 : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%convert_element_type_default_6, torch.float32), kwargs = {})
#   %add_tensor : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=2] = call_function[target=torch.ops.aten.add.Tensor](args = (%convert_element_type_default, %convert_element_type_default_1), kwargs = {})
#   %pow_tensor_scalar : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.pow.Tensor_Scalar](args = (%add_tensor, 2), kwargs = {})
#   %mean_dim : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mean.dim](args = (%pow_tensor_scalar, [-1], True), kwargs = {})
#   %add_tensor_1 : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.add.Tensor](args = (%mean_dim, 1e-05), kwargs = {})
#   %rsqrt_default : Tensor "f32[s72, 1][1, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.rsqrt.default](args = (%add_tensor_1,), kwargs = {})
#   %mul_tensor : Tensor "f32[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%add_tensor, %rsqrt_default), kwargs = {})
#   %convert_element_type_default_3 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.prims.convert_element_type.default](args = (%mul_tensor, torch.float16), kwargs = {})
#   %mul_tensor_1 : Tensor "f16[s72, 2048][2048, 1]cuda:0"[num_users=1] = call_function[target=torch.ops.aten.mul.Tensor](args = (%convert_element_type_default_3, %arg13_1), kwargs = {})
#   return %buf9,%mul_tensor_1
triton_red_fused_fused_add_rms_norm_2 = async_compile.triton('triton_red_fused_fused_add_rms_norm_2', '''
import triton
import triton.language as tl

from torch._inductor.runtime import triton_helpers, triton_heuristics
from torch._inductor.runtime.triton_helpers import libdevice, math as tl_math
from torch._inductor.runtime.hints import AutotuneHint, ReductionHint, TileHint, DeviceProperties
triton_helpers.set_driver_to_gpu()

@triton_heuristics.reduction(
    size_hints={'x': 2048, 'r0_': 2048},
    reduction_hint=ReductionHint.INNER,
    filename=__file__,
    triton_meta={'signature': {'in_out_ptr0': '*fp16', 'in_ptr0': '*fp16', 'in_ptr1': '*fp16', 'in_ptr2': '*fp16', 'xnumel': 'i32', 'r0_numel': 'i32', 'XBLOCK': 'constexpr', 'R0_BLOCK': 'constexpr'}, 'device': DeviceProperties(type='cuda', index=0, multi_processor_count=40, cc=75, major=7, regs_per_multiprocessor=65536, max_threads_per_multi_processor=1024, max_threads_per_block=1024, warp_size=32), 'constants': {}, 'native_matmul': False, 'enable_fp_fusion': True, 'launch_pdl': False, 'disable_ftz': False, 'configs': [{(0,): [['tt.divisibility', 16]], (1,): [['tt.divisibility', 16]], (2,): [['tt.divisibility', 16]], (3,): [['tt.divisibility', 16]], (5,): [['tt.divisibility', 16]]}]},
    inductor_meta={'grid_type': 'Grid1D', 'kernel_name': 'triton_red_fused_fused_add_rms_norm_2', 'mutated_arg_names': ['in_out_ptr0'], 'optimize_mem': True, 'no_x_dim': False, 'atomic_add_found': False, 'num_load': 7, 'num_store': 1, 'num_reduction': 1, 'autotune_hints': set(), 'tiling_scores': {'x': 0, 'r0_': 41947136}, 'kernel_num_gb': 0.033558528, 'kernel_flop': 0, 'backend_hash': '5E96B05ED70B79647D32C11935EAF35B0D56E688005EEF62A1E751F67D2F444C', 'assert_indirect_indexing': True, 'autotune_local_cache': True, 'autotune_pointwise': True, 'autotune_remote_cache': None, 'force_disable_caches': False, 'dynamic_scale_rblock': True, 'incremental_autotune': False, 'max_autotune': False, 'max_autotune_pointwise': False, 'min_split_scan_rblock': 256, 'spill_threshold': 16, 'store_cubin': False, 'deterministic': False, 'batch_invariant': False, 'force_filter_reduction_configs': False, 'mix_order_reduction_allow_multi_stages': True, 'dynamic_disable_pipelining': True, 'are_deterministic_algorithms_enabled': False}
)
@triton.jit
def triton_red_fused_fused_add_rms_norm_2(in_out_ptr0, in_ptr0, in_ptr1, in_ptr2, xnumel, r0_numel, XBLOCK : tl.constexpr, R0_BLOCK : tl.constexpr):
    r0_numel = 2048
    rnumel = r0_numel
    RBLOCK: tl.constexpr = R0_BLOCK
    xoffset = tl.program_id(0) * XBLOCK
    xindex = xoffset + tl.arange(0, XBLOCK)[:, None]
    xmask = xindex < xnumel
    r0_base = tl.arange(0, R0_BLOCK)[None, :]
    rbase = r0_base
    x0 = xindex
    _tmp12 = tl.full([XBLOCK, R0_BLOCK], 0, tl.float32)
    for r0_offset in tl.range(0, r0_numel, R0_BLOCK):
        r0_index = r0_offset + r0_base
        r0_mask = r0_index < r0_numel
        roffset = r0_offset
        rindex = r0_index
        r0_1 = r0_index
        tmp0 = tl.load(in_out_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp2 = tl.load(in_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp4 = tl.load(in_ptr1 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp1 = tmp0.to(tl.float32)
        tmp3 = tmp2.to(tl.float32)
        tmp5 = tmp4.to(tl.float32)
        tmp6 = tmp3 + tmp5
        tmp7 = tmp6.to(tl.float32)
        tmp8 = tmp7.to(tl.float32)
        tmp9 = tmp1 + tmp8
        tmp10 = tmp9 * tmp9
        tmp11 = tl.broadcast_to(tmp10, [XBLOCK, R0_BLOCK])
        tmp13 = _tmp12 + tmp11
        _tmp12 = tl.where(r0_mask & xmask, tmp13, _tmp12)
    tmp12 = tl.sum(_tmp12, 1)[:, None]
    for r0_offset in tl.range(0, r0_numel, R0_BLOCK):
        r0_index = r0_offset + r0_base
        r0_mask = r0_index < r0_numel
        roffset = r0_offset
        rindex = r0_index
        r0_1 = r0_index
        tmp14 = tl.load(in_out_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp16 = tl.load(in_ptr0 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp18 = tl.load(in_ptr1 + (r0_1 + 2048*x0), r0_mask & xmask, eviction_policy='evict_first', other=0.0).to(tl.float32)
        tmp31 = tl.load(in_ptr2 + (r0_1), r0_mask, eviction_policy='evict_last', other=0.0).to(tl.float32)
        tmp15 = tmp14.to(tl.float32)
        tmp17 = tmp16.to(tl.float32)
        tmp19 = tmp18.to(tl.float32)
        tmp20 = tmp17 + tmp19
        tmp21 = tmp20.to(tl.float32)
        tmp22 = tmp21.to(tl.float32)
        tmp23 = tmp15 + tmp22
        tmp24 = tl.full([1, 1], 2048.0, tl.float32)
        tmp25 = (tmp12 / tmp24)
        tmp26 = tl.full([1, 1], 1e-05, tl.float32)
        tmp27 = tmp25 + tmp26
        tmp28 = libdevice.rsqrt(tmp27)
        tmp29 = tmp23 * tmp28
        tmp30 = tmp29.to(tl.float32)
        tmp32 = tmp30 * tmp31
        tl.store(in_out_ptr0 + (r0_1 + 2048*x0), tmp32, r0_mask & xmask)
''', device_str='cuda')


async_compile.wait(globals())
del async_compile

class Runner:
    def __init__(self, partitions):
        self.partitions = partitions

    def recursively_apply_fns(self, fns):
        new_callables = []
        for fn, c in zip(fns, self.partitions):
            new_callables.append(fn(c))
        self.partitions = new_callables

    def call(self, args):
        arg0_1, arg1_1, arg2_1, arg3_1, arg4_1, arg5_1, arg6_1, arg7_1, arg8_1, arg9_1, arg10_1, arg11_1, arg12_1, arg13_1 = args
        args.clear()
        s72 = arg1_1
        s80 = s72
        assert_size_stride(arg0_1, (s72, 32, 64), (2048, 64, 1), 'input')
        assert_size_stride(arg2_1, (128, 8192), (8192, 1), 'input')
        assert_size_stride(arg3_1, (1, 2048), (2048, 1), 'input')
        assert_size_stride(arg4_1, (1024, ), (1, ), 'input')
        with torch.cuda._DeviceGuard(0):
            torch.cuda.set_device(0)
            arg0_1 = copy_if_misaligned(arg0_1)
            arg2_1 = copy_if_misaligned(arg2_1)
            arg3_1 = copy_if_misaligned(arg3_1)
            arg4_1 = copy_if_misaligned(arg4_1)
            # Topologically Sorted Source Nodes: [view, humming_gemm], Original ATen: [aten.view, humming.humming_gemm]
            buf0 = torch.ops.humming.humming_gemm.default(layer_config='{"shape_n": 2048, "shape_k": 2048, "pad_shape_n": 0, "pad_shape_k": 0, "num_experts": 0, "b_dtype": "float8e4m3", "a_dtype": "float16", "c_dtype": "float16", "bs_dtype": "float16", "as_dtype": null, "input_scale_group_size": 0, "weight_scale_group_size": 0, "weight_scale_group_size_n": 0, "weight_scale_type": "channel", "weight_scale_2_type": "none", "use_int_weight_scale": false, "use_fused_e8m0_scale": false, "has_zero_point": false, "is_fp_zero_point": false, "has_bias": false, "mma_type": "mma", "use_packed_k_layout": false}', inputs=reinterpret_tensor(arg0_1, (s72, 2048), (2048, 1), 0), weight=arg2_1, weight_scale=arg3_1, weight_scale_2=None, compute_config='{"use_batch_invariant": false, "use_f16_accum": false, "gemm_type": "dense"}', tuning_config=None, input_scale=None, zero_point=None, bias=None, outputs=None, sorted_ids=None, expert_ids=None, num_tokens_padded=None, expert_layout=None, locks=arg4_1, top_k=1, valid_shape_m=0)
            del arg0_1
            del arg2_1
            del arg3_1
            del arg4_1
            buf1 = buf0
            assert_size_stride(buf1, (s72, 2048), (2048, 1), 'torch.ops.humming.humming_gemm.default')
            assert_alignment(buf1, 16, 'torch.ops.humming.humming_gemm.default')
            del buf0
            assert_size_stride(arg6_1, (s72, 2048), (2048, 1), 'input')
            assert_size_stride(arg5_1, (2048, ), (1, ), 'input')
            arg6_1 = copy_if_misaligned(arg6_1)
            arg5_1 = copy_if_misaligned(arg5_1)
            buf3 = empty_strided_cuda((s72, 2048), (2048, 1), torch.float16)
            # Topologically Sorted Source Nodes: [fused_add_rms_norm_maybe_inplace, humming_gemm_1], Original ATen: [vllm_ir.fused_add_rms_norm, humming.humming_gemm]
            raw_stream0 = get_raw_stream(0)
            triton_red_fused_fused_add_rms_norm_humming_gemm_0.run(buf1, arg6_1, arg5_1, buf3, s72, 2048, stream=raw_stream0)
            del arg5_1
            assert_size_stride(arg7_1, (128, 65536), (65536, 1), 'input')
            assert_size_stride(arg8_1, (1, 16384), (16384, 1), 'input')
            assert_size_stride(arg9_1, (1024, ), (1, ), 'input')
            arg7_1 = copy_if_misaligned(arg7_1)
            arg8_1 = copy_if_misaligned(arg8_1)
            arg9_1 = copy_if_misaligned(arg9_1)
            # Topologically Sorted Source Nodes: [fused_add_rms_norm_maybe_inplace, humming_gemm_1], Original ATen: [vllm_ir.fused_add_rms_norm, humming.humming_gemm]
            buf4 = torch.ops.humming.humming_gemm.default(layer_config='{"shape_n": 16384, "shape_k": 2048, "pad_shape_n": 0, "pad_shape_k": 0, "num_experts": 0, "b_dtype": "float8e4m3", "a_dtype": "float16", "c_dtype": "float16", "bs_dtype": "float16", "as_dtype": null, "input_scale_group_size": 0, "weight_scale_group_size": 0, "weight_scale_group_size_n": 0, "weight_scale_type": "channel", "weight_scale_2_type": "none", "use_int_weight_scale": false, "use_fused_e8m0_scale": false, "has_zero_point": false, "is_fp_zero_point": false, "has_bias": false, "mma_type": "mma", "use_packed_k_layout": false}', inputs=buf3, weight=arg7_1, weight_scale=arg8_1, weight_scale_2=None, compute_config='{"use_batch_invariant": false, "use_f16_accum": false, "gemm_type": "dense"}', tuning_config=None, input_scale=None, zero_point=None, bias=None, outputs=None, sorted_ids=None, expert_ids=None, num_tokens_padded=None, expert_layout=None, locks=arg9_1, top_k=1, valid_shape_m=0)
            del arg7_1
            del arg8_1
            del arg9_1
            del buf3
            buf5 = buf4
            assert_size_stride(buf5, (s72, 16384), (16384, 1), 'torch.ops.humming.humming_gemm.default')
            assert_alignment(buf5, 16, 'torch.ops.humming.humming_gemm.default')
            del buf4
            buf6 = empty_strided_cuda((s72, 8192), (8192, 1), torch.float16)
            # Topologically Sorted Source Nodes: [getitem_2, silu, getitem_3, mul, humming_gemm_2], Original ATen: [aten.slice, aten.silu, aten.mul, humming.humming_gemm]
            triton_poi_fused_humming_gemm_mul_silu_slice_1_xnumel = 8192*s72
            raw_stream0 = get_raw_stream(0)
            triton_poi_fused_humming_gemm_mul_silu_slice_1.run(buf5, buf6, triton_poi_fused_humming_gemm_mul_silu_slice_1_xnumel, stream=raw_stream0)
            del buf5
            assert_size_stride(arg10_1, (512, 8192), (8192, 1), 'input')
            assert_size_stride(arg11_1, (1, 2048), (2048, 1), 'input')
            assert_size_stride(arg12_1, (1024, ), (1, ), 'input')
            arg10_1 = copy_if_misaligned(arg10_1)
            arg11_1 = copy_if_misaligned(arg11_1)
            arg12_1 = copy_if_misaligned(arg12_1)
            # Topologically Sorted Source Nodes: [getitem_2, silu, getitem_3, mul, humming_gemm_2], Original ATen: [aten.slice, aten.silu, aten.mul, humming.humming_gemm]
            buf7 = torch.ops.humming.humming_gemm.default(layer_config='{"shape_n": 2048, "shape_k": 8192, "pad_shape_n": 0, "pad_shape_k": 0, "num_experts": 0, "b_dtype": "float8e4m3", "a_dtype": "float16", "c_dtype": "float16", "bs_dtype": "float16", "as_dtype": null, "input_scale_group_size": 0, "weight_scale_group_size": 0, "weight_scale_group_size_n": 0, "weight_scale_type": "channel", "weight_scale_2_type": "none", "use_int_weight_scale": false, "use_fused_e8m0_scale": false, "has_zero_point": false, "is_fp_zero_point": false, "has_bias": false, "mma_type": "mma", "use_packed_k_layout": false}', inputs=buf6, weight=arg10_1, weight_scale=arg11_1, weight_scale_2=None, compute_config='{"use_batch_invariant": false, "use_f16_accum": false, "gemm_type": "dense"}', tuning_config=None, input_scale=None, zero_point=None, bias=None, outputs=None, sorted_ids=None, expert_ids=None, num_tokens_padded=None, expert_layout=None, locks=arg12_1, top_k=1, valid_shape_m=0)
            del arg10_1
            del arg11_1
            del arg12_1
            del buf6
            buf8 = buf7
            assert_size_stride(buf8, (s72, 2048), (2048, 1), 'torch.ops.humming.humming_gemm.default')
            assert_alignment(buf8, 16, 'torch.ops.humming.humming_gemm.default')
            del buf7
            assert_size_stride(arg13_1, (2048, ), (1, ), 'input')
            arg13_1 = copy_if_misaligned(arg13_1)
            buf10 = buf8; del buf8  # reuse
            # Topologically Sorted Source Nodes: [fused_add_rms_norm_maybe_inplace, fused_add_rms_norm_maybe_inplace_1], Original ATen: [vllm_ir.fused_add_rms_norm]
            raw_stream0 = get_raw_stream(0)
            triton_red_fused_fused_add_rms_norm_2.run(buf10, buf1, arg6_1, arg13_1, s72, 2048, stream=raw_stream0)
            del arg13_1
            del arg6_1
            del buf1
        return (buf10, )

runner = Runner(partitions=[])
call = runner.call
recursively_apply_fns = runner.recursively_apply_fns


def get_args():
    from torch._dynamo.testing import rand_strided
    arg0_1 = rand_strided((2048, 32, 64), (2048, 64, 1), device='cuda:0', dtype=torch.float16)
    arg1_1 = 2048
    arg2_1 = rand_strided((128, 8192), (8192, 1), device='cuda:0', dtype=torch.int32)
    arg3_1 = rand_strided((1, 2048), (2048, 1), device='cuda:0', dtype=torch.float16)
    arg4_1 = rand_strided((1024, ), (1, ), device='cuda:0', dtype=torch.int32)
    arg5_1 = rand_strided((2048, ), (1, ), device='cuda:0', dtype=torch.float16)
    arg6_1 = rand_strided((2048, 2048), (2048, 1), device='cuda:0', dtype=torch.float16)
    arg7_1 = rand_strided((128, 65536), (65536, 1), device='cuda:0', dtype=torch.int32)
    arg8_1 = rand_strided((1, 16384), (16384, 1), device='cuda:0', dtype=torch.float16)
    arg9_1 = rand_strided((1024, ), (1, ), device='cuda:0', dtype=torch.int32)
    arg10_1 = rand_strided((512, 8192), (8192, 1), device='cuda:0', dtype=torch.int32)
    arg11_1 = rand_strided((1, 2048), (2048, 1), device='cuda:0', dtype=torch.float16)
    arg12_1 = rand_strided((1024, ), (1, ), device='cuda:0', dtype=torch.int32)
    arg13_1 = rand_strided((2048, ), (1, ), device='cuda:0', dtype=torch.float16)
    return [arg0_1, arg1_1, arg2_1, arg3_1, arg4_1, arg5_1, arg6_1, arg7_1, arg8_1, arg9_1, arg10_1, arg11_1, arg12_1, arg13_1]


def benchmark_compiled_module(args, times=10, repeat=10):
    from torch._inductor.utils import print_performance
    fn = lambda: call(list(args))
    return print_performance(fn, times=times, repeat=repeat, device='cuda')


if __name__ == "__main__":
    from torch._inductor.wrapper_benchmark import compiled_module_main
    args = get_args()
    compiled_module_main('None', lambda times, repeat: benchmark_compiled_module(args, times=times, repeat=repeat))
