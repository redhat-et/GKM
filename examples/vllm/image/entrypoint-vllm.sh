#!/bin/bash

# Base configuration with defaults
MODE=${MODE:-"serve"}
MODEL=${MODEL:-"Qwen/Qwen1.5-MoE-A2.7B-Chat"}
PORT=${PORT:-8000}

# Benchmark configuration with defaults
INPUT_LEN=${INPUT_LEN:-512}
OUTPUT_LEN=${OUTPUT_LEN:-256}
NUM_PROMPTS=${NUM_PROMPTS:-1000}
NUM_ROUNDS=${NUM_ROUNDS:-3}
MAX_BATCH_TOKENS=${MAX_BATCH_TOKENS:-8192}
NUM_CONCURRENT=${NUM_CONCURRENT:-8}

# Additional args passed directly to vLLM
EXTRA_ARGS=${EXTRA_ARGS:-""}

# Log file location
LOG_PATH="/tmp/vllm.log"

# =========================
# GPU probe configuration
# =========================
# Enable/disable the probe and control strictness.
# If PROBE_STRICT=true and Torch sees != 1 device, the script exits non-zero.
PROBE_ENABLE=${PROBE_ENABLE:-"true"}
PROBE_STRICT=${PROBE_STRICT:-"false"}

# Optional: minimum/maximum devices you expect Torch to see after HIP/CUDA masking
PROBE_EXPECT_MIN_DEVICES=${PROBE_EXPECT_MIN_DEVICES:-1}
PROBE_EXPECT_MAX_DEVICES=${PROBE_EXPECT_MAX_DEVICES:-1}

gpu_probe() {
  [[ "$PROBE_ENABLE" != "true" ]] && return 0

  echo "===== GPU Visibility Probe (before starting vLLM) ====="
  echo "Env: HIP_VISIBLE_DEVICES=${HIP_VISIBLE_DEVICES:-<unset>}  ROCR_VISIBLE_DEVICES=${ROCR_VISIBLE_DEVICES:-<unset>}  HSA_VISIBLE_DEVICES=${HSA_VISIBLE_DEVICES:-<unset>}  CUDA_VISIBLE_DEVICES=${CUDA_VISIBLE_DEVICES:-<unset>}"
  echo "Devices present (container view):"
  ls -l /dev/kfd 2>/dev/null || echo "  /dev/kfd not present"
  ls -l /dev/dri 2>/dev/null || echo "  /dev/dri not present"
  echo

  # Optional: if Ray is present, show status (helps confirm Ray's view of GPU resources)
  if command -v ray >/dev/null 2>&1; then
    echo "[ray status]"
    if ray status 2>/dev/null; then
      echo
    else
      echo "  Ray is installed but no running instance found."
      echo
    fi
  fi

  # Python Torch/ROCm probe (this is definitive for what the *process* will use)
  python3 - <<PY || {
import json, os, sys
try:
    import torch
    n = torch.cuda.device_count()
    devices = []
    for i in range(n):
        torch.cuda.set_device(i)
        p = torch.cuda.get_device_properties(i)
        devices.append({
            "index": i,
            "name": torch.cuda.get_device_name(i),
            "pci_bus_id": getattr(p, "pci_bus_id", "n/a"),
        })
    out = {"torch_device_count": n, "devices": devices}
    print(json.dumps(out, indent=2))
    # Simple strictness gate
    exp_min = int(os.environ.get("PROBE_EXPECT_MIN_DEVICES", "1"))
    exp_max = int(os.environ.get("PROBE_EXPECT_MAX_DEVICES", "1"))
    strict  = os.environ.get("PROBE_STRICT", "false").lower() == "true"
    if strict and not (exp_min <= n <= exp_max):
        print(f"[probe] ERROR: torch sees {n} devices; expected between {exp_min} and {exp_max}.", file=sys.stderr)
        sys.exit(12)
except Exception as e:
    print(json.dumps({"probe_error": str(e)}), file=sys.stderr)
    # If strict, treat as failure
    if os.environ.get("PROBE_STRICT", "false").lower() == "true":
        sys.exit(13)
PY
    if [[ "$PROBE_STRICT" == "true" ]]; then
      echo "[probe] Strict mode enabled; Torch probe failed."
      exit 14
    else
      echo "[probe] Non-strict mode: continuing despite probe error."
    fi
  }
  echo "===== End GPU Visibility Probe ====="
  echo
}

summarize_logs() {
  local logfile="$1"
  echo -e "\n===== Startup Summary ====="
  awk '
    /Loading weights took/ {
      print " Weight Load Time:    " $(NF-1), "seconds"
    }
    /Model loading took/ {
      print " Model Load Time:     " $(NF-1), "seconds"
    }
    /torch\.compile takes/ {
      for (i=1; i<=NF; i++) {
        if ($i == "takes" && $(i+1) ~ /^[0-9.]+$/ && $(i+2) == "s" && $(i+3) == "in" && $(i+4) == "total") {
          print " Torch Compile Time:  " $(i+1), "seconds"
        }
      }
    }
    /Memory profiling takes/ {
      print " Memory Profile Time: " $(NF-1), "seconds"
    }
    /Graph capturing finished/ {
      for (i=1; i<NF; i++) {
        if ($i == "in" && $(i+1) ~ /^[0-9.]+$/ && $(i+2) == "secs,") {
          print " CUDA Graphs Time:    " $(i+1), "seconds"
        }
      }
    }
    /init engine.*took/ {
      print " Total Startup Time:  " $(NF-1), "seconds"
    }
  ' "$logfile"
  echo "============================="
}


watch_for_startup_complete() {
  local logfile="$1"
  while read -r line; do
    echo "$line" >> "$logfile"
    if echo "$line" | grep -q "Application startup complete"; then
      summarize_logs "$logfile"
      break
    fi
  done
}

case $MODE in
  "serve")
    gpu_probe
    echo "Starting vLLM server on port $PORT with model: $MODEL"
    echo "Additional arguments: $EXTRA_ARGS"

    # Kick off the server, stream stdout and stderr, and monitor output live
    (
      # Run summarizer watcher in background
      tail -F "$LOG_PATH" | while read -r line; do
        echo "$line"
        if [[ "$line" == *"Application startup complete."* ]]; then
          summarize_logs "$LOG_PATH"
          break
        fi
      done
    ) &

    # Start vLLM and tee everything to the log file
    python3 -u -m vllm.entrypoints.openai.api_server \
      --model "$MODEL" \
      --port "$PORT" \
      $EXTRA_ARGS > "$LOG_PATH" 2>&1
    ;;

  "benchmark")
    gpu_probe
    echo "Running vLLM benchmarks with model: $MODEL"
    echo "Additional arguments: $EXTRA_ARGS"

    # Create timestamped directory for this benchmark run
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    BENCHMARK_DIR="/data/benchmarks/$TIMESTAMP"
    mkdir -p "$BENCHMARK_DIR"

    echo "Running throughput benchmark..."
    python3 /app/vllm/benchmarks/benchmark_throughput.py \
      --model "$MODEL" \
      --input-len "$INPUT_LEN" \
      --output-len "$OUTPUT_LEN" \
      --num-prompts "$NUM_PROMPTS" \
      --max-num-batched-tokens "$MAX_BATCH_TOKENS" \
      --output-json "$BENCHMARK_DIR/throughput.json" \
      $EXTRA_ARGS
    echo "Throughput benchmark complete - results saved in $BENCHMARK_DIR/throughput.json"

    echo "Running latency benchmark..."
    python3 /app/vllm/benchmarks/benchmark_latency.py \
      --model "$MODEL" \
      --input-len "$INPUT_LEN" \
      --output-len "$OUTPUT_LEN" \
      --output-json "$BENCHMARK_DIR/latency.json" \
      $EXTRA_ARGS
    echo "Latency benchmark complete - results saved in $BENCHMARK_DIR/latency.json"

    echo "All results have been saved to $BENCHMARK_DIR"
    ;;

  *)
    echo "Unknown mode: $MODE"
    echo "Please use 'serve' or 'benchmark'"
    exit 1
    ;;
esac
