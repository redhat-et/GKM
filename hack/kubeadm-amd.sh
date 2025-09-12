#!/bin/bash
# This script sets up a single-node Kubernetes cluster using kubeadm.
# It is specifically designed for a node with AMD GPUs.
# The script includes functions for cleanup, initialization, and cluster creation.
# It also applies necessary configurations such as networking (Flannel)
# and AMD GPU device plugins for Kubernetes.
set -e
set -o pipefail

# URLs for resources
FLANNEL_URL="https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml"
AMDGPU_DP_URL="https://raw.githubusercontent.com/ROCm/k8s-device-plugin/master/k8s-ds-amdgpu-dp.yaml"
AMDGPU_LABELLER_URL="https://raw.githubusercontent.com/ROCm/k8s-device-plugin/master/k8s-ds-amdgpu-labeller.yaml"

# Logging function
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1"
}

# Usage function
usage() {
    echo "Usage: $0 {cleanup|create|init}"
    exit 1
}

# Dependency check
check_dependencies() {
    for cmd in kubectl kubeadm; do
        if ! command -v $cmd &> /dev/null; then
            log "Error: $cmd is not installed."
            exit 1
        fi
    done
}

# Cleanup function
cleanup() {
    echo "Running cleanup..."
    sudo kubeadm reset -f
    rm -rf $HOME/.kube/
    rm -rf /var/lib/gkm/caches/*
    sudo systemctl daemon-reload
    sudo systemctl restart kubelet
    sudo systemctl restart crio
    sudo ls /etc/cni/net.d
    sudo rm -rf /etc/cni/net.d/*
    sudo swapoff -av
    sudo free -h
    sudo setenforce 1
    echo "Cleanup completed."
}

# Initialization function
init() {
    sudo modprobe br_netfilter
    sudo bash -c "echo 1 > /proc/sys/net/ipv4/ip_forward"
}

# Cluster creation function
create() {
    log "Running Kubernetes init and setup..."
    sudo setenforce 0
    sudo kubeadm init --v 99 --pod-network-cidr=10.244.0.0/16 --cri-socket /var/run/crio/crio.sock
    rm -f $HOME/.kube/config
    mkdir -p $HOME/.kube
    sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
    sudo chown $(id -u):$(id -g) $HOME/.kube/config
    kubectl taint nodes --all node-role.kubernetes.io/control-plane- || true
    kubectl get nodes
    kubectl describe node
    kubectl apply -f "$FLANNEL_URL"
    kubectl describe node
    kubectl create -f "$AMDGPU_DP_URL"
    kubectl create -f "$AMDGPU_LABELLER_URL"
    log "Cluster creation and configuration completed."
}

# Main execution
if [[ "$#" -ne 1 ]]; then
    usage
fi

check_dependencies

case "$1" in
    cleanup)
        cleanup
        ;;
    init)
        init
        ;;
    create)
        create
        ;;
    *)
        usage
        ;;
esac
