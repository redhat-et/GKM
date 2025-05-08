# Build the manager binary
FROM public.ecr.aws/docker/library/golang:1.24.3 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Install required system packages for gpgme and NVML
RUN apt-get update && \
    apt-get install -y \
        libgpgme-dev \
        btrfs-progs \
        libbtrfs-dev \
        libgpgme11-dev \
        libseccomp-dev \
        pkg-config \
        build-essential && \
    apt-get clean

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache deps before building and copying source
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/controller/ internal/controller/

# Build the binary with CGO enabled
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -a -o /workspace/manager cmd/main.go

# Use a minimal Ubuntu base image that supports CGO binaries
FROM public.ecr.aws/docker/library/ubuntu:22.04

# Copy the binary from the builder
COPY --from=builder /workspace/manager /manager

# Install required runtime libraries for CGO (GPGME and others)
RUN apt-get update && \
    apt-get install -y \
        libgpgme11 \
        libbtrfs0 \
        libseccomp2 && \
    apt-get clean

# Run as non-root user
USER 65532:65532

ENTRYPOINT ["/manager"]
