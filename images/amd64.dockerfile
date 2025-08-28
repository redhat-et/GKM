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
 && rm -rf /var/lib/apt/lists/*

# Install ROCm apt repo
RUN wget https://repo.radeon.com/amdgpu-install/6.4.3/ubuntu/jammy/amdgpu-install_6.4.60403-1_all.deb && \
   apt install -y ./amdgpu-install_6.4.60403-1_all.deb && \
   apt update && \
   apt install -y amd-smi-lib \
   && rm -rf /var/lib/apt/lists/*

RUN ln -s /opt/rocm-6.4.3/bin/amd-smi /usr/bin/amd-smi

COPY --from=builder /usr/src/mcv/_output/bin/linux_amd64/mcv /mcv
COPY mcv/images/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]

# [ podman | docker ] build --progress=plain -t quay.io/gkm/mcv -f mcv/images/amd64.dockerfile .
