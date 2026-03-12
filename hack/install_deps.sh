#!/bin/bash

set -e

echo "================================================"
echo "GKM Dependency Installation for RHEL 10"
echo "================================================"
echo ""

# Minimum required versions
MIN_GO_VERSION="1.25.0"
MIN_PODMAN_VERSION="5.3.1"
MIN_KUBECTL_VERSION="1.11.3"

# CentOS Stream 10 repository URLs
CENTOS_CRB="https://mirror.stream.centos.org/10-stream/CRB/x86_64/os/"
FEDORA_BASE="https://download.fedoraproject.org/pub/fedora/linux/development/rawhide/Everything/x86_64/os/Packages"

# Function to compare versions
version_ge() {
    # Returns 0 (true) if $1 >= $2
    [ "$(printf '%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

# Function to check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

echo "=== Step 1: Importing CentOS Stream GPG key ==="
echo "================================================"
sudo rpm --import https://www.centos.org/keys/RPM-GPG-KEY-CentOS-Official-SHA256 2>/dev/null || echo "Key may already be imported"

echo ""
echo "=== Step 2: Installing system development packages ==="
echo "======================================================"
sudo dnf install -y --repofrompath=centos-crb,${CENTOS_CRB} \
  gpgme-devel libdrm-devel hwloc-devel

echo ""
echo "=== Step 3: Installing btrfs development headers ==="
echo "====================================================="
# First install the base libraries with --nodeps to skip filesystem checks
sudo rpm -ivh --nodeps \
  "${FEDORA_BASE}/l/libbtrfs-6.19-1.fc45.x86_64.rpm" \
  "${FEDORA_BASE}/l/libbtrfsutil-6.19-1.fc45.x86_64.rpm" 2>/dev/null || echo "Libraries may already be installed"

# Now install devel package with --nodeps
sudo rpm -ivh --nodeps \
  "${FEDORA_BASE}/b/btrfs-progs-6.19-1.fc45.x86_64.rpm" 2>/dev/null || echo "btrfs-progs may already be installed"

sudo rpm -ivh --nodeps \
  "${FEDORA_BASE}/b/btrfs-progs-devel-6.19-1.fc45.x86_64.rpm"

echo ""
echo "=== Step 4: Installing Go ${MIN_GO_VERSION}+ ==="
echo "=============================================="
if command_exists go; then
    CURRENT_GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    echo "Found Go version: ${CURRENT_GO_VERSION}"
    if version_ge "${CURRENT_GO_VERSION}" "${MIN_GO_VERSION}"; then
        echo "✓ Go ${CURRENT_GO_VERSION} meets minimum requirement (${MIN_GO_VERSION}+)"
    else
        echo "⚠ Go ${CURRENT_GO_VERSION} is older than required ${MIN_GO_VERSION}"
        echo "Installing Go ${MIN_GO_VERSION}..."
        GO_VERSION="1.25.0"
        GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
        curl -LO "https://go.dev/dl/${GO_TARBALL}"
        sudo rm -rf /usr/local/go
        sudo tar -C /usr/local -xzf "${GO_TARBALL}"
        rm "${GO_TARBALL}"
        echo "✓ Go ${GO_VERSION} installed. Add /usr/local/go/bin to your PATH"
        export PATH=$PATH:/usr/local/go/bin
    fi
else
    echo "Go not found. Installing Go ${MIN_GO_VERSION}..."
    GO_VERSION="1.25.0"
    GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
    curl -LO "https://go.dev/dl/${GO_TARBALL}"
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf "${GO_TARBALL}"
    rm "${GO_TARBALL}"
    echo "✓ Go ${GO_VERSION} installed. Add /usr/local/go/bin to your PATH"
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    export PATH=$PATH:/usr/local/go/bin
fi

echo ""
echo "=== Step 5: Installing Podman ${MIN_PODMAN_VERSION}+ ==="
echo "========================================================"
if command_exists podman; then
    CURRENT_PODMAN_VERSION=$(podman version --format '{{.Client.Version}}' 2>/dev/null || podman --version | awk '{print $3}')
    echo "Found Podman version: ${CURRENT_PODMAN_VERSION}"
    if version_ge "${CURRENT_PODMAN_VERSION}" "${MIN_PODMAN_VERSION}"; then
        echo "✓ Podman ${CURRENT_PODMAN_VERSION} meets minimum requirement (${MIN_PODMAN_VERSION}+)"
    else
        echo "⚠ Podman ${CURRENT_PODMAN_VERSION} is older than required ${MIN_PODMAN_VERSION}"
        echo "Upgrading Podman..."
        sudo dnf upgrade -y podman
    fi
else
    echo "Podman not found. Installing..."
    sudo dnf install -y podman
fi

echo ""
echo "=== Step 6: Installing kubectl ${MIN_KUBECTL_VERSION}+ ==="
echo "=========================================================="
if command_exists kubectl; then
    CURRENT_KUBECTL_VERSION=$(kubectl version --client --short 2>/dev/null | grep -oP 'v\K[0-9.]+' || kubectl version --client -o json 2>/dev/null | grep -oP '"gitVersion": "v\K[0-9.]+' | head -1)
    echo "Found kubectl version: ${CURRENT_KUBECTL_VERSION}"
    if version_ge "${CURRENT_KUBECTL_VERSION}" "${MIN_KUBECTL_VERSION}"; then
        echo "✓ kubectl ${CURRENT_KUBECTL_VERSION} meets minimum requirement (${MIN_KUBECTL_VERSION}+)"
    else
        echo "⚠ kubectl ${CURRENT_KUBECTL_VERSION} is older than required ${MIN_KUBECTL_VERSION}"
        echo "Installing latest kubectl..."
        curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
        chmod +x kubectl
        sudo mv kubectl /usr/local/bin/
    fi
else
    echo "kubectl not found. Installing..."
    curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
    chmod +x kubectl
    sudo mv kubectl /usr/local/bin/
fi

echo ""
echo "=== Step 7: Verification ==="
echo "============================"
echo ""
echo "System Development Packages:"
ls -la /usr/include/gpgme.h 2>/dev/null && echo "  ✓ gpgme-devel" || echo "  ✗ gpgme-devel missing"
ls -la /usr/include/xf86drm.h 2>/dev/null && echo "  ✓ libdrm-devel" || echo "  ✗ libdrm-devel missing"
ls -la /usr/include/hwloc.h 2>/dev/null && echo "  ✓ hwloc-devel" || echo "  ✗ hwloc-devel missing"
ls -la /usr/include/btrfs/version.h 2>/dev/null && echo "  ✓ btrfs/version.h" || echo "  ✗ btrfs headers missing"

echo ""
echo "Build Tools:"
if command_exists go; then
    echo "  ✓ Go $(go version | awk '{print $3}')"
else
    echo "  ✗ Go not found in PATH"
fi

if command_exists podman; then
    echo "  ✓ Podman $(podman --version | awk '{print $3}')"
else
    echo "  ✗ Podman not found"
fi

if command_exists kubectl; then
    echo "  ✓ kubectl $(kubectl version --client --short 2>/dev/null | grep -oP 'v[0-9.]+' || echo 'version installed')"
else
    echo "  ✗ kubectl not found in PATH"
fi

echo ""
echo "pkg-config:"
pkg-config --exists gpgme && echo "  ✓ gpgme.pc (version $(pkg-config --modversion gpgme))" || echo "  ✗ gpgme.pc missing"

echo ""
echo "================================================"
echo "Installation Complete!"
echo "================================================"
echo ""
echo "If Go or kubectl were newly installed, you may need to:"
echo "  - Reload your shell: source ~/.bashrc"
echo "  - Or add to your PATH manually:"
echo "    export PATH=\$PATH:/usr/local/go/bin"
