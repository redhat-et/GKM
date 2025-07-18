FROM public.ecr.aws/docker/library/golang:1.24 AS builder

COPY mcv/ /usr/src/mcv
WORKDIR /usr/src/mcv

ENV CGO_ENABLED=1
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgpgme-dev \
    libbtrfs-dev \
    build-essential \
    git \
    libc-dev \
    libffi-dev \
    linux-headers-amd64 \
 && rm -rf /var/lib/apt/lists/*

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
 && rm -rf /var/lib/apt/lists/*

# Install ROCm apt repo
RUN wget https://repo.radeon.com/amdgpu-install/latest/ubuntu/jammy/amdgpu-install_6.4.60401-1_all.deb && \
    apt-get update && \
    apt-get install -y ./amdgpu-install_6.4.60401-1_all.deb && \
    amdgpu-install --usecase=rocm --no-dkms -y && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/src/mcv/_output/bin/linux_amd64/mcv /mcv
COPY mcv/images/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]

# [ podman | docker ] build --progress=plain -t quay.io/tkm/mcv -f mcv/images/amd64.dockerfile .
