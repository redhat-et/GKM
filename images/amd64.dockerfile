FROM public.ecr.aws/docker/library/debian:bookworm-slim AS builder

ARG GO_VERSION=1.24.6

ENV CGO_ENABLED=1
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgpgme-dev \
    libbtrfs-dev \
    build-essential \
    git \
    libc-dev \
    libffi-dev \
    linux-headers-amd64 \
    ca-certificates \
    wget \
    pkg-config \
    libassuan-dev \
    libgpg-error-dev \
    libsqlite3-dev \
 && rm -rf /var/lib/apt/lists/*

RUN wget https://go.dev/dl/go"${GO_VERSION}".linux-amd64.tar.gz -O /tmp/go.tgz \
 && rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz \
 && rm /tmp/go.tgz

ENV PATH=$PATH:/usr/local/go/bin
RUN go version

COPY mcv/ /usr/src/mcv
WORKDIR /usr/src/mcv

RUN make tidy-vendor
RUN make build

FROM public.ecr.aws/docker/library/debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgpgme11 \
    libbtrfs0 \
    libffi8 \
    libc6 \
    ca-certificates \
    wget \
    gnupg2 \
    curl \
    lsb-release \
    software-properties-common \
    python3-setuptools \
    python3-wheel \
    dialog \
    rsync \
    pciutils \
   hwdata \
   buildah \
   netavark aardvark-dns \
   fuse-overlayfs fuse3 \
 && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /etc/containers && \
 printf '[storage]\ndriver="overlay"\nrunroot="/run/containers/storage"\ngraphroot="/var/lib/containers/storage"\n[storage.options]\nmount_program="/usr/bin/fuse-overlayfs"\n' \
   > /etc/containers/storage.conf

# Install ROCm apt repo
ARG ROCM_VERSION=7.0.1
ARG AMDGPU_VERSION=7.0.1.70001
ARG OPT_ROCM_VERSION=7.0.1

# Install ROCm apt repo
RUN wget https://repo.radeon.com/amdgpu-install/${ROCM_VERSION}/ubuntu/jammy/amdgpu-install_${AMDGPU_VERSION}-1_all.deb
RUN apt install -y ./*.deb
RUN apt update &&  DEBIAN_FRONTEND=noninteractive apt install -y amd-smi-lib rocm-smi-lib
RUN apt-get clean && rm -rf /var/lib/apt/lists/* && rm -rf ./*.deb
RUN ln -s /opt/rocm-${OPT_ROCM_VERSION}/bin/amd-smi /usr/bin/amd-smi
RUN ln -s /opt/rocm-${OPT_ROCM_VERSION}/bin/rocm-smi /usr/bin/rocm-smi

COPY --from=builder /usr/src/mcv/_output/bin/linux_amd64/mcv /mcv
COPY mcv/images/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]

# [ podman | docker ] build --progress=plain -t quay.io/gkm/mcv -f mcv/images/amd64.dockerfile .
