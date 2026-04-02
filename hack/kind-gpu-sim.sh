#!/bin/bash
set -e

REGISTRY_PORT=5000
ECR_REGISTRY_IMAGE=public.ecr.aws/docker/library/registry:2
CLUSTER_NAME=kind-gpu-sim
LOAD_IMAGE_NAME=not-set
# Detect OS
OS_TYPE=$(uname -s)
if [[ "$OS_TYPE" == "Darwin" ]]; then
  IS_MACOS=true
else
  IS_MACOS=false
fi

if [ "$IS_MACOS" = true ]; then
  PID_CMD="pgrep"
else
  PID_CMD="pidof"
fi

# Detect the available `sed` tool
if command -v gsed &>/dev/null; then
  USE_GSED=true
  SED=gsed  # Use `gsed` if available
else
  USE_GSED=false
  SED=sed   # Fallback to default `sed`
fi

for arg in "$@"; do
  case "$arg" in
    --registry-port=*)
      REGISTRY_PORT="${arg#*=}"
      ;;
    --cluster-name=*)
      CLUSTER_NAME="${arg#*=}"
      ;;
    --image-name=*)
      LOAD_IMAGE_NAME="${arg#*=}"
      ;;
  esac
done

# --- Runtime detection ---
if command -v podman &>/dev/null; then
  echo "Using Podman as container runtime"
  CONTAINER_RUNTIME="podman"
  export KIND_EXPERIMENTAL_PROVIDER=podman
  export DOCKER_HOST=unix:///run/user/$UID/podman/podman.sock
  if [ "$IS_MACOS" = true ]; then
    echo "Skipping systemctl command as it's not available on macOS"
  else
    systemctl --user enable --now podman.socket || true
  fi
elif command -v docker &>/dev/null; then
  echo "Using Docker as container runtime"
  CONTAINER_RUNTIME="docker"
else
  echo "ERROR: Neither Docker nor Podman is installed." >&2
  exit 1
fi

cr() {
  "$CONTAINER_RUNTIME" "$@"
}

CONFIG_FILE=kind-config.yaml
REGISTRY_NAME="kind-registry"

function start_local_registry() {
  echo "Starting local registry on port ${REGISTRY_PORT}..."
  running=$(cr inspect -f '{{.State.Running}}' "${REGISTRY_NAME}" 2>/dev/null || echo "false")
  if [ "$running" != "true" ]; then
    cr run -d --restart=always -p "${REGISTRY_PORT}:5000" \
      --name "${REGISTRY_NAME}" "${ECR_REGISTRY_IMAGE}"
  else
    echo "Registry '${REGISTRY_NAME}' already running."
  fi
  echo "Ensuring the registry is connected to the Kind network..."
  cr network connect kind "${REGISTRY_NAME}" 2>/dev/null || true
}

function generate_kind_config() {
  [ -f "${CONFIG_FILE}" ] && rm -f "${CONFIG_FILE}"
  cat > "${CONFIG_FILE}" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
  - |
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${REGISTRY_PORT}"]
      endpoint = ["http://${REGISTRY_NAME}:5000"]
nodes:
  - role: control-plane
  - role: worker
  - role: worker
EOF
}

function create_kind_cluster() {
  gpu_type="$1"
  generate_kind_config
  kind create cluster --name "${CLUSTER_NAME}" --config "${CONFIG_FILE}"
  cr network connect "kind" "${REGISTRY_NAME}" || true

  worker_nodes=$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | grep -v control-plane)
  for node in $worker_nodes; do
    kubectl label node "$node" hardware-type=gpu --overwrite
    kubectl label node "$node" node-role.kubernetes.io/worker="" --overwrite
    kubectl taint node "$node" gpu=true:NoSchedule --overwrite
    if [ "$gpu_type" = "rocm" ]; then
      kubectl label node "$node" rocm.amd.com/gpu.present=true --overwrite
      kubectl patch node "$node" --type=json -p='[{"op": "add", "path": "/status/capacity/amd.com~1gpu", "value":"2"}]' --subresource=status
    elif [ "$gpu_type" = "nvidia" ]; then
      kubectl label node "$node" nvidia.com/gpu.present=true --overwrite
      kubectl patch node "$node" --type=json -p='[{"op": "add", "path": "/status/capacity/nvidia.com~1gpu", "value":"2"}]' --subresource=status
    fi
  done

  for node in $(kind get nodes --name "${CLUSTER_NAME}"); do
    cr exec "$node" mkdir -p "/etc/containerd/certs.d/localhost:${REGISTRY_PORT}"
    cat <<EOF | cr exec -i "$node" tee "/etc/containerd/certs.d/localhost:${REGISTRY_PORT}/hosts.toml" >/dev/null
[host."http://${REGISTRY_NAME}:5000"]
  capabilities = ["pull", "resolve"]
EOF
    cr exec "$node" kill -SIGHUP $($PID_CMD containerd) 2>/dev/null || echo "Warning: could not reload containerd on $node"
  done
}

function apply_local_registry_configmap() {
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REGISTRY_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
}

# Patching Dockerfile based on the GPU type (rocm or nvidia)
patch_dockerfile() {
  gpu_type="$1"
  echo "Patching Dockerfile for $gpu_type compatibility..."
  # Additional GPU-specific changes
  if [ "$gpu_type" = "nvidia" ]; then
    # Apply NVIDIA-specific patching if needed
    echo "Applying NVIDIA-specific patches to the Dockerfile..."
      if [ "$IS_MACOS" = true ] && [ "$USE_GSED" = false ]; then
        # macOS-specific patching with sed
        ${SED} -i '' 's|^FROM redhat/ubi9-minimal|FROM registry.access.redhat.com/ubi9/ubi-minimal|' deployments/container/Dockerfile
        ${SED} -i '' 's|^FROM public.ecr.aws/ubi9/ubi-minimal|FROM registry.access.redhat.com/ubi9/ubi-minimal|' deployments/container/Dockerfile
        ${SED} -i '' 's|^FROM registry.access.redhat.com/ubi9/ubi9-minimal|FROM registry.access.redhat.com/ubi9/ubi-minimal|' deployments/container/Dockerfile
      else
        # Linux-specific patching with default sed
        ${SED} -i 's|^FROM redhat/ubi9-minimal|FROM registry.access.redhat.com/ubi9/ubi-minimal|' deployments/container/Dockerfile
        ${SED} -i 's|^FROM public.ecr.aws/ubi9/ubi-minimal|FROM registry.access.redhat.com/ubi9/ubi-minimal|' deployments/container/Dockerfile
        ${SED} -i 's|^FROM registry.access.redhat.com/ubi9/ubi9-minimal|FROM registry.access.redhat.com/ubi9/ubi-minimal|' deployments/container/Dockerfile
      fi
  elif [ "$gpu_type" = "rocm" ]; then
    # Apply ROCm-specific patching if needed
    echo "Applying ROCm-specific patches to the Dockerfile..."
    if [ "$IS_MACOS" = true ] && [ "$USE_GSED" = false ]; then
      # macOS-specific patching with sed
      ${SED} -i '' "s|^FROM alpine:3.21.3|FROM public.ecr.aws/docker/library/alpine:3.21.3|" Dockerfile
      ${SED} -i '' "s|^FROM docker.io/golang:1.23.6-alpine3.21|FROM public.ecr.aws/docker/library/golang:1.23.6-alpine3.21|" Dockerfile
      ${SED} -i '' "s|^FROM golang:1.23.6-alpine3.21|FROM public.ecr.aws/docker/library/golang:1.23.6-alpine3.21|" Dockerfile
    else
      # Linux-specific patching with default sed
      ${SED} -i "s|^FROM alpine:3.21.3|FROM public.ecr.aws/docker/library/alpine:3.21.3|" Dockerfile
      ${SED} -i "s|^FROM docker.io/golang:1.23.6-alpine3.21|FROM public.ecr.aws/docker/library/golang:1.23.6-alpine3.21|" Dockerfile
      ${SED} -i "s|^FROM golang:1.23.6-alpine3.21|FROM public.ecr.aws/docker/library/golang:1.23.6-alpine3.21|" Dockerfile
    fi
  fi
}

function build_and_push_images() {
  gpu_type="$1"

  if [ "$gpu_type" = "nvidia" ]; then
    echo " Building NVIDIA device plugin locally..."
    [ ! -d k8s-device-plugin-nvidia ] && git clone https://github.com/NVIDIA/k8s-device-plugin.git k8s-device-plugin-nvidia
    cd k8s-device-plugin-nvidia
    git checkout v0.18.2

    if [ "$CONTAINER_RUNTIME" = "podman" ]; then
      patch_dockerfile "$gpu_type"
      grep FROM deployments/container/Dockerfile
      BUILDAH_FORMAT=docker cr build \
      -t localhost:${REGISTRY_PORT}/nvidia-device-plugin:dev \
      -f deployments/container/Dockerfile .
      cr tag localhost:${REGISTRY_PORT}/nvidia-device-plugin:dev localhost/nvidia-device-plugin:dev
      cr save localhost/nvidia-device-plugin:dev -o /tmp/image.tar
      kind load image-archive /tmp/image.tar --name "$CLUSTER_NAME"
      rm -f /tmp/image.tar
    else
      cr build \
        -t localhost:${REGISTRY_PORT}/nvidia-device-plugin:dev \
        -f deployments/container/Dockerfile .
      cr push localhost:${REGISTRY_PORT}/nvidia-device-plugin:dev

    fi

    cd ..
    return
  fi

  echo " Building ROCm plugin images locally..."
  [ ! -d k8s-device-plugin-rocm ] && git clone https://github.com/RadeonOpenCompute/k8s-device-plugin.git k8s-device-plugin-rocm
  cd k8s-device-plugin-rocm

  patch_dockerfile "$gpu_type"

  cr build -t localhost:${REGISTRY_PORT}/amdgpu-dp:dev -f Dockerfile .

  if [ "$CONTAINER_RUNTIME" = "docker" ]; then
    cr push localhost:${REGISTRY_PORT}/amdgpu-dp:dev
  else
    cr tag localhost:${REGISTRY_PORT}/amdgpu-dp:dev localhost/amdgpu-dp:dev
    cr save localhost/amdgpu-dp:dev -o /tmp/image.tar
    kind load image-archive /tmp/image.tar --name "$CLUSTER_NAME"
    rm -f /tmp/image.tar
  fi
  cd ..
}

function deploy_device_plugin() {
  gpu_type="$1"
  if [ "$gpu_type" = "rocm" ]; then
    deploy_rocm_plugin
  elif [ "$gpu_type" = "nvidia" ]; then
    deploy_nvidia_plugin
  else
    echo " Unknown GPU type: $gpu_type"
    exit 1
  fi
}

function deploy_rocm_plugin() {
  IMAGE_URL="localhost:${REGISTRY_PORT}/amdgpu-dp:dev"
  if [ "$CONTAINER_RUNTIME" = "podman" ]; then
    IMAGE_URL="localhost/amdgpu-dp:dev"
  fi

  cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: amdgpu-device-plugin-daemonset
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: amdgpu-device-plugin
  template:
    metadata:
      labels:
        app: amdgpu-device-plugin
    spec:
      nodeSelector:
        hardware-type: gpu
      tolerations:
        - key: gpu
          operator: Equal
          value: "true"
          effect: NoSchedule
      containers:
        - name: amdgpu-dp-ds
          image: ${IMAGE_URL}
          imagePullPolicy: IfNotPresent
          securityContext:
            privileged: true
EOF

  sleep 5
  kubectl wait --for=condition=Ready -n kube-system pod -l app=amdgpu-device-plugin --timeout=60s || {
    echo >&2 "ERROR: ROCm plugin pods not ready in time"
    exit 1
  }
}

function deploy_nvidia_plugin() {
  IMAGE_URL="localhost:${REGISTRY_PORT}/nvidia-device-plugin:dev"
  if [ "$CONTAINER_RUNTIME" = "podman" ]; then
    IMAGE_URL="localhost/nvidia-device-plugin:dev"
  fi

  cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: nvidia-device-plugin-daemonset
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: nvidia-device-plugin
  template:
    metadata:
      labels:
        app: nvidia-device-plugin
    spec:
      nodeSelector:
        hardware-type: gpu
      tolerations:
        - key: gpu
          operator: Equal
          value: "true"
          effect: NoSchedule
      containers:
        - name: nvidia-device-plugin-ctr
          image: ${IMAGE_URL}
          securityContext:
            privileged: true
          env:
            - name: FAIL_ON_INIT_ERROR
              value: "false"
          volumeMounts:
            - name: device-plugin
              mountPath: /var/lib/kubelet/device-plugins
      volumes:
        - name: device-plugin
          hostPath:
            path: /var/lib/kubelet/device-plugins
            type: DirectoryOrCreate
EOF

  sleep 5
  kubectl wait --for=condition=Ready -n kube-system pod -l app=nvidia-device-plugin --timeout=60s || {
    echo >&2 "ERROR: NVIDIA plugin pods not ready in time"
    exit 1
  }
}

function delete_cluster() {
  if kind get clusters | grep -qx "${CLUSTER_NAME}"; then
    echo "Deleting kind cluster '${CLUSTER_NAME}'..."
    kind delete cluster --name "${CLUSTER_NAME}"
  else
    echo "Kind cluster '${CLUSTER_NAME}' does not exist. Skipping delete."
  fi
}

function delete_registry() {
  echo "Stopping ${REGISTRY_NAME} (if running)..."
  if cr ps -q -f "name=^/${REGISTRY_NAME}$" &>/dev/null; then
    cr stop "${REGISTRY_NAME}" || echo "Warning: Failed to stop ${REGISTRY_NAME}"
  else
    echo "No running container named '${REGISTRY_NAME}' to stop."
  fi

  echo "Removing ${REGISTRY_NAME} (if exists)..."
  if cr ps -aq -f "name=^/${REGISTRY_NAME}$" &>/dev/null; then
    cr rm "${REGISTRY_NAME}" || echo "Warning: Failed to remove ${REGISTRY_NAME}"
  else
    echo "No container named '${REGISTRY_NAME}' to remove."
  fi
}


function usage() {
  echo "Usage: $0 {create [rocm|nvidia]|delete|load}"
  exit 1
}

function load_image() {
  if [ "$CONTAINER_RUNTIME" = "docker" ]; then
    echo "Running: load docker-image ${LOAD_IMAGE_NAME} --name ${CLUSTER_NAME}"
    kind load docker-image "${LOAD_IMAGE_NAME}" --name "${CLUSTER_NAME}"
  else
    cr save ${LOAD_IMAGE_NAME} -o /tmp/image.tar
    kind load image-archive /tmp/image.tar --name "${CLUSTER_NAME}"
    rm -f /tmp/image.tar
  fi
}

case "$1" in
  create)
    gpu_type=${2:-rocm}
    start_local_registry
    create_kind_cluster "$gpu_type"
    apply_local_registry_configmap
    build_and_push_images "$gpu_type"
    deploy_device_plugin "$gpu_type"
    echo " Simulated GPU Kind cluster is ready for '${gpu_type}'!"
    ;;
  delete)
    delete_cluster
    delete_registry
    ;;
  load)
    load_image
    ;;
  *)
    usage
    ;;
esac
