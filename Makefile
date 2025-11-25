# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

# Allows building bundles in Mac replacing BSD 'sed' command by GNU-compatible 'gsed'
ifeq (,$(shell which gsed 2>/dev/null))
SED ?= sed
else
SED ?= gsed
endif

ARCH=$(shell go env GOARCH)
# Define CONTAINER_FLAGS and include ARCH as an argument
CONTAINER_FLAGS ?= --build-arg TARGETARCH=$(ARCH)

# NO_GPU flag for building without GPU support
NO_GPU_BUILD ?= false

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# gkm.io/gpu-kernel-manager-operator-bundle:$VERSION and gkm.io/gpu-kernel-manager-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= gkm.io/gpu-kernel-manager-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.39.2
# Image URL to use all building/pushing image targets
QUAY_USER ?= gkm
IMAGE_TAG ?= latest
REPO ?= quay.io/$(QUAY_USER)
OPERATOR_IMG ?= $(REPO)/operator:$(IMAGE_TAG)
AGENT_IMG ?=$(REPO)/agent:$(IMAGE_TAG)
CSI_IMG ?=$(REPO)/gkm-csi-plugin:$(IMAGE_TAG)
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.31.0

DEPLOY_PATH ?= config/default

# On undeploy, force indicates all workload pods and GKMCache and ClusterGKMCaches
# instances should be deleted. Default is not to cleanup but fail if they exist.
FORCE ?= ""

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Defaulting to `podman` because the `run-on-kind` which leverages KIND_GPU_SIM_SCRIPT
# (maryamtahhan/kind-gpu-sim) which requires docker.
#CONTAINER_TOOL_PATH := $(shell which docker 2>/dev/null || which podman)
CONTAINER_TOOL_PATH := $(shell which podman 2>/dev/null || which docker)
CONTAINER_TOOL ?= $(shell basename ${CONTAINER_TOOL_PATH})

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# set GOENV
ifeq ($(shell uname),Darwin)
export CGO_LDFLAGS += -Wl,-no_warn_duplicate_libraries
endif

GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
CGO_ENABLED ?= 1
GO_BUILD_FLAGS = GOOS=$(GOOS) \
				 GOARCH=$(GOARCH) \
				 $(if $(strip $(CGO_LDFLAGS)),CGO_LDFLAGS=$(CGO_LDFLAGS))

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: vendors
vendors: ## Refresh vendors directory.
	@echo "### Checking vendors"
	go mod tidy && go mod vendor

.PHONY: explain
explain: ## Run "kubectl explain" on all CRDs.
	CRD_1="ClusterGKMCache" CRD_2="GKMCache" CRD_3="ClusterGKMCacheNode" CRD_4="GKMCacheNode" OUTPUT_DIR="../docs/crds" ./hack/crd_explain_txt.sh

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=agent-role paths="./internal/controller/gkm-agent/..." output:rbac:artifacts:config=config/rbac/gkm-agent
	$(CONTROLLER_GEN) rbac:roleName=operator-role paths="./internal/controller/gkm-operator" output:rbac:artifacts:config=config/rbac/gkm-operator

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e  # Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e:
	go test ./test/e2e/ -v -ginkgo.v

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build-gkm-operator
build-gkm-operator:
	${GO_BUILD_FLAGS} CGO_ENABLED=$(CGO_ENABLED) go build -o bin/gkm-operator ./cmd

.PHONY: build-gkm-agent
build-gkm-agent:
	${GO_BUILD_FLAGS} CGO_ENABLED=$(CGO_ENABLED) go build -o bin/gkm-agent ./agent

.PHONY: build-csi
build-csi:
	${GO_BUILD_FLAGS} CGO_ENABLED=$(CGO_ENABLED) go build -o bin/gkm-csi-plugin ./csi-plugin

.PHONY: build
build: manifests generate fmt vet build-gkm-operator build-gkm-agent build-csi ## Build all binaries.

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: build-image-operator
build-image-operator:
	$(CONTAINER_TOOL) build $(CONTAINER_FLAGS) --progress=plain --load -f Containerfile.gkm-operator -t ${OPERATOR_IMG} .

.PHONY: build-image-agent
build-image-agent:
	$(CONTAINER_TOOL) build  $(CONTAINER_FLAGS) --build-arg NO_GPU=$(NO_GPU_BUILD) --progress=plain --load -f Containerfile.gkm-agent -t ${AGENT_IMG} .

.PHONY: build-image-csi
build-image-csi:
	$(CONTAINER_TOOL) build  $(CONTAINER_FLAGS) --progress=plain --load -f Containerfile.gkm-csi -t ${CSI_IMG} .

# If you wish to build the operator image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: build-images
build-images: build-image-operator build-image-agent build-image-csi ## Build all container images.

.PHONY: push-images
push-images: ## Push all container image.
	$(CONTAINER_TOOL) push ${OPERATOR_IMG}
	$(CONTAINER_TOOL) push ${AGENT_IMG}
	$(CONTAINER_TOOL) push ${CSI_IMG}

# Mapping old commands after rename
.PHONY: docker-build
docker-build: build-images

.PHONY: docker-push
docker-push: push-images

# PLATFORMS defines the target platforms for the operator image be built to provide support to multiple
# architectures. (i.e. make docker-buildx OPERATOR_IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via OPERATOR_IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the operator for cross-platform support
	# copy existing Containerfile and insert --platform=${BUILDPLATFORM} into Containerfile.cross, and preserve the original Containerfile
	$(SED) -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Containerfile > Containerfile.cross
	- $(CONTAINER_TOOL) buildx create --name gpu-kernel-manager-operator-builder
	$(CONTAINER_TOOL) buildx use gpu-kernel-manager-operator-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${OPERATOR_IMG} -f Containerfile.cross .
	- $(CONTAINER_TOOL) buildx rm gpu-kernel-manager-operator-builder
	rm Containerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/operator && $(KUSTOMIZE) edit set image controller=${OPERATOR_IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Cleanup

.PHONY: clean
clean: ## Remove all generated files and binaries
	@echo "Cleaning up generated files and binaries..."
	rm -rf bin/*
	rm -rf dist/*
	rm -rf $(GOBIN)/controller-gen
	rm -rf $(GOBIN)/kustomize
	rm -rf $(GOBIN)/setup-envtest
	rm -rf $(GOBIN)/golangci-lint
	rm -rf $(LOCALBIN)/*
	find . -name 'zz_generated.*' -delete
	find . -name '*.o' -delete
	find . -name '*.out' -delete
	find . -name '*.test' -delete
	find . -name 'cover.out' -delete

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = true
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Deployment
.PHONY: prepare-deploy
prepare-deploy:
	cd config/operator && $(KUSTOMIZE) edit set image quay.io/gkm/operator=${OPERATOR_IMG}
	cd config/agent && $(KUSTOMIZE) edit set image quay.io/gkm/agent=${AGENT_IMG}
	cd config/csi-plugin && $(KUSTOMIZE) edit set image quay.io/gkm/gkm-csi-plugin=${CSI_IMG}
ifdef NO_GPU
	cd config/configMap && \
	  $(SED) \
	    -e '/literals:/a\  - gkm.nogpu=true' \
	    -e 's@gkm\.agent\.image=.*@gkm.agent.image=$(AGENT_IMG)@' \
	    -e 's@gkm\.csi\.image=.*@gkm.csi.image=$(CSI_IMG)@' \
	    kustomization.yaml.env > kustomization.yaml
else
	cd config/configMap && \
	  $(SED) \
	    -e 's@gkm\.agent\.image=.*@gkm.agent.image=$(AGENT_IMG)@' \
	    -e 's@gkm\.csi\.image=.*@gkm.csi.image=$(CSI_IMG)@' \
	    kustomization.yaml.env > kustomization.yaml
endif

.PHONY: deploy
deploy: manifests kustomize prepare-deploy webhook-secret-file deploy-cert-manager redeploy ## Deploy controller and agent to the K8s cluster specified in ~/.kube/config

.PHONY: redeploy
redeploy: ## Redeploy controller and agent to the K8s cluster after deploy and undeploy have been called. Skips some onetime steps in deploy.
	$(KUSTOMIZE) build $(DEPLOY_PATH) | $(KUBECTL) apply -f -
	@echo "Deployment to $(DEPLOY_PATH) completed."

.PHONY: undeploy
undeploy: kustomize delete-webhook-secret-file ## Undeploy operator and agent from the K8s cluster specified in ~/.kube/config.
	@echo "Calling undeploy script"
	$(UNDEPLOY_SCRIPT) $(FORCE)
	@if [ $$? -ne 0 ]; then \
    	exit 1; \
    fi
	$(KUSTOMIZE) build $(DEPLOY_PATH) | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -
	@echo "Undeployment from $(DEPLOY_PATH) completed."

.PHONY: undeploy-force
undeploy-force: ## Same as "make undeploy" but also delete any dependencies.
	$(MAKE) undeploy FORCE=--force

.PHONY: deploy-examples
deploy-examples: ## Deploy the examples to the K8s cluster specified in ~/.kube/config.
	@echo "Create Namespace based GKMCache"
	$(KUBECTL) apply -f examples/namespace/
	@echo "Create Cluster based ClusterGKMCache"
	$(KUBECTL) apply -f examples/cluster/

.PHONY: undeploy-examples
undeploy-examples: ## Undeploy the examples from the K8s cluster specified in ~/.kube/config.
	@echo "Remove Namespace based GKMCache"
	$(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f examples/namespace/
	@echo "Remove Cluster based ClusterGKMCache"
	$(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f examples/cluster/

##@ Kind Cluster Management

KIND_GPU_SIM_SCRIPT := https://raw.githubusercontent.com/maryamtahhan/kind-gpu-sim/refs/heads/main/kind-gpu-sim.sh
KIND_CLUSTER_NAME ?= kind-gpu-sim

# GPU Type (either "rocm" or "nvidia")
# In a KIND Cluster, NO_GPU is true and GPU_TYPE is the simulated GPU type.
GPU_TYPE ?= rocm

# This Makefile may use docker, but when running the KIND cluster with a
# simulated GPU, KIND requires podman. The KIND_GPU_SIM_SCRIPT sets up the
# following podman environment variables to allow podman with KIND.
# To use KIND on this cluster outside of the Makefile, make sure to set
# these variables in local shell:
# export KIND_EXPERIMENTAL_PROVIDER=podman
# export DOCKER_HOST=unix:///run/user/$UID/podman/podman.sock
.PHONY: get-example-images
get-example-images:
	$(CONTAINER_TOOL) pull quay.io/gkm/vector-add-cache:rocm
	wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s load --image-name=quay.io/gkm/vector-add-cache:rocm --cluster-name=$(KIND_CLUSTER_NAME)
	$(CONTAINER_TOOL) pull quay.io/gkm/cache-examples:vector-add-cache-rocm
	wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s load --image-name=quay.io/gkm/cache-examples:vector-add-cache-rocm --cluster-name=$(KIND_CLUSTER_NAME)

.PHONY: deploy-webhook-certs
deploy-webhook-certs:
	$(KUBECTL) apply -k config/webhook

.PHONY: webhook-secret-file
webhook-secret-file:
	@mkdir -p config/secret
	@[ -s config/secret/mutation.env ] || \
	  (echo 'Generating config/secret/mutation.env'; \
	   printf 'MUTATION_SIGNING_KEY=%s\n' "$$(head -c 32 /dev/urandom | base64 | tr -d '\n')" > config/secret/mutation.env)

.PHONY: delete-webhook-secret-file
delete-webhook-secret-file:
	@rm -f 0config/secret/mutation.env

.PHONY: rotate-webhook-secret
rotate-webhook-secret:
	@printf 'MUTATION_SIGNING_KEY=%s\n' "$$(head -c 32 /dev/urandom | base64 | tr -d '\n')" > config/secret/mutation.env
	$(KUSTOMIZE) build config/secret | $(KUBECTL) apply -f -

.PHONY: get-cert-manager-images
get-cert-manager-images:
	@echo "Getting Images ..."
	$(CONTAINER_TOOL) pull quay.io/jetstack/cert-manager-controller:v1.18.0
	$(CONTAINER_TOOL) pull quay.io/jetstack/cert-manager-cainjector:v1.18.0
	$(CONTAINER_TOOL) pull quay.io/jetstack/cert-manager-webhook:v1.18.0
	@if [[ "$$($(KUBECTL) get nodes -o jsonpath='{.items[*].metadata.name}')" =~ "kind" ]]; then \
		echo "Kind detected – loading cert-manager images to kind..."; \
		wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s load --image-name=quay.io/jetstack/cert-manager-controller:v1.18.0 --cluster-name=$(KIND_CLUSTER_NAME); \
		wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s load --image-name=quay.io/jetstack/cert-manager-cainjector:v1.18.0 --cluster-name=$(KIND_CLUSTER_NAME); \
		wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s load --image-name=quay.io/jetstack/cert-manager-webhook:v1.18.0 --cluster-name=$(KIND_CLUSTER_NAME); \
	fi

.PHONY: deploy-cert-manager
deploy-cert-manager: get-cert-manager-images
	@echo "Installing cert-manager base manifests from upstream..."
	$(KUBECTL) apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
	@echo "Checking for Kind cluster..."
	@if [[ "$$($(KUBECTL) get nodes -o jsonpath='{.items[*].metadata.name}')" =~ "kind" ]]; then \
		echo "Kind detected – applying Kind-specific cert-manager patches..."; \
		$(KUBECTL) patch deployment -n cert-manager cert-manager --patch-file=config/kind-gpu/patch-cert-manager-controller.yaml; \
		$(KUBECTL) patch deployment -n cert-manager cert-manager-cainjector --patch-file=config/kind-gpu/patch-cert-manager-cainjector.yaml; \
		$(KUBECTL) patch deployment -n cert-manager cert-manager-webhook --patch-file=config/kind-gpu/patch-cert-manager-webhook.yaml; \
	else \
		echo "Non-Kind cluster – skipping GPU patching for cert-manager."; \
	fi
	@echo "Waiting for cert-manager deployment to become available..."
	$(KUBECTL) wait --for=condition=Available --timeout=120s -n cert-manager deployment/cert-manager
	$(KUBECTL) wait --for=condition=Available --timeout=120s -n cert-manager deployment/cert-manager-webhook
	$(KUBECTL) wait --for=condition=Ready --timeout=120s -n cert-manager pod -l app=webhook

.PHONY: undeploy-cert-manager
undeploy-cert-manager: delete-webhook-secret-file
	@echo "Undeploy cert-manager"
	$(KUBECTL) delete -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml --ignore-not-found=$(ignore-not-found)

.PHONY: setup-kind
setup-kind:
	@echo "Creating Kind GPU cluster with GPU type: $(GPU_TYPE) and cluster name: $(KIND_CLUSTER_NAME)"
	wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s create $(GPU_TYPE) --cluster-name=$(KIND_CLUSTER_NAME)
	@echo "Kind GPU cluster $(KIND_CLUSTER_NAME) created successfully."

.PHONY: kind-load-images
kind-load-images: get-example-images
	@echo "Loading operator image ${OPERATOR_IMG} into Kind cluster: $(KIND_CLUSTER_NAME)"
	wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s load --image-name=${OPERATOR_IMG} --cluster-name=$(KIND_CLUSTER_NAME)
	@echo "Loading agent image ${AGENT_IMG} into Kind cluster: $(KIND_CLUSTER_NAME)"
	wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s load --image-name=${AGENT_IMG} --cluster-name=$(KIND_CLUSTER_NAME)
	@echo "Loading csi-driver image ${CSI_IMG} into Kind cluster: $(KIND_CLUSTER_NAME)"
	wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s load --image-name=${CSI_IMG} --cluster-name=$(KIND_CLUSTER_NAME)
	@echo "Images loaded successfully into Kind cluster: $(KIND_CLUSTER_NAME)"


.PHONY: tmp-cleanup
tmp-cleanup:
	@hack/tmp-cleanup.sh

.PHONY: run-on-kind
run-on-kind: destroy-kind setup-kind deploy-on-kind ## Setup Kind cluster, load images, and deploy
	@echo "Cluster created, images loaded, and agent deployed on Kind GPU cluster."

.PHONY: deploy-on-kind
deploy-on-kind: kind-load-images tmp-cleanup
	## NOTE: config/kind-gpu is an overlay of config/default
	$(MAKE) deploy DEPLOY_PATH=config/kind-gpu NO_GPU=true
	@echo "Add label gkm-test-node= to node kind-gpu-sim-worker."
	$(KUBECTL) label node kind-gpu-sim-worker gkm-test-node=true --overwrite

.PHONY: redeploy-on-kind
redeploy-on-kind: ## Redeploy controller and agent to Kind GPU cluster after run-on-kind and undeploy-on-kind have been called. Skips some onetime steps in deploy.
	$(MAKE) redeploy DEPLOY_PATH=config/kind-gpu NO_GPU=true
	@echo "Deployment to $(DEPLOY_PATH) completed."

.PHONY: undeploy-on-kind
undeploy-on-kind: ## Undeploy operator and agent from the Kind GPU cluster.
	$(MAKE) undeploy FORCE=$(FORCE) DEPLOY_PATH=config/kind-gpu ignore-not-found=$(ignore-not-found)
	@echo "Undeployment from Kind GPU cluster $(KIND_CLUSTER_NAME) completed."

.PHONY: undeploy-on-kind-force
undeploy-on-kind-force: ## Same as "make undeploy-on-kind" but also delete any dependencies.
	$(MAKE) undeploy-on-kind FORCE=--force

.PHONY: destroy-kind
destroy-kind: ## Delete the Kind GPU cluster
	@echo "Deleting Kind GPU cluster: $(KIND_CLUSTER_NAME)"
	wget -qO- $(KIND_GPU_SIM_SCRIPT) | bash -s delete --cluster-name=$(KIND_CLUSTER_NAME)
	@echo "Kind GPU cluster $(KIND_CLUSTER_NAME) deleted successfully."

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
UNDEPLOY_SCRIPT ?= $(shell pwd)/hack/undeploy.sh

## Tool Versions
KUSTOMIZE_VERSION ?= v5.6.0
CONTROLLER_TOOLS_VERSION ?= v0.16.1
ENVTEST_VERSION ?= release-0.19
GOLANGCI_LINT_VERSION ?= v1.59.1

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/operator && $(KUSTOMIZE) edit set image controller=$(OPERATOR_IMG)
	cd config/configMap && \
	  $(SED) -e 's@gkm\.agent\.image=.*@gkm.agent.image=$(AGENT_IMG)@' \
	      -e 's@gkm\.csi\.image=.*@gkm.csi.image=$(CSI_IMG)@' \
		  kustomization.yaml.env > kustomization.yaml
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(CONTAINER_TOOL) build -f bundle.Containerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push OPERATOR_IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push OPERATOR_IMG=$(CATALOG_IMG)
