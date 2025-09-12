# Image name and tag
IMAGE_DIR = image
IMAGE_NAME = quay.io/gkm/vllm-demo
IMAGE_TAG = rocm
IMAGE = $(IMAGE_NAME):$(IMAGE_TAG)
DOCKERFILE = Containerfile.vllm-rocm

# Determine whether to use podman or docker
CONTAINER_TOOL = $(shell command -v podman >/dev/null 2>&1 && echo podman || echo docker)

# Build the image
build:
	@echo "Using $(CONTAINER_TOOL) to build the container image..."
	@$(CONTAINER_TOOL) build --build-arg ENTRYPOINT_PATH=image -t $(IMAGE) -f $(IMAGE_DIR)/$(DOCKERFILE) .

# Push the image to the registry
push:
	@echo "Using $(CONTAINER_TOOL) to push the container image..."
	@$(CONTAINER_TOOL) push $(IMAGE)

# Clean up local images
clean:
	@echo "Using $(CONTAINER_TOOL) to remove the local container image..."
	@$(CONTAINER_TOOL) rmi $(IMAGE)

.PHONY: build push clean
