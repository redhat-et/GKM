#!/bin/bash

# Filter out help if it was entered
if [[ "$1" == "help" || "$1" == "-help" || "$1" == "--help" ]]; then
    echo ""
    echo "./generate-files.sh will generate a yaml file from the base files"
    echo "   and the input which can then be applied to a Kubernetes cluster."
    echo "   Generated filename is printed from script and files can be found"
    echo "   in the \"output/\" directory."
    echo "Syntax:"
    echo "  ./generate-files.sh <ACCESS> <SCOPE> <GPU> <COSIGN-VERSION> [<ENVIRONMENT>]"
    echo "Where:"
    echo "  <ACCESS> is \"rox\" or \"rwo\" and required."
    echo "  <SCOPE> is \"namespace\" or \"cluster\" and required."
    echo "  <GPU> is \"cuda\" or \"rocm\" and required."
    echo "  <COSIGN-VERSION> is \"v2\" or \"v3\" and required."
    echo "  <ENVIRONMENT> is \"kind\" or \"nfd\" and optional."
    echo "Samples:"
    echo "  ./generate-files.sh rwo namespace rocm v3 kind"
    echo "  ./generate-files.sh rox cluster cuda v2 nfd"
    echo "  ./generate-files.sh rox namespace rocm v3"
    echo ""
    exit 0
fi


CALL_POPD=false
if [[ "$PWD" != */examples ]]; then
    pushd examples &>/dev/null
    if [[ $? -ne 0 ]]; then
        echo "ERROR: Must run from \"./GKM\" or \"./GKM/examples\""
        exit 1
    fi
    CALL_POPD=true
fi


#
# Setup tools
#

# On macOS, check if gsed (GNU sed) is installed
if command -v gsed >/dev/null 2>&1; then
    SED="gsed"
else
    # Fallback to macOS default sed (BSD)
    SED="sed"
fi

KUSTOMIZE=../bin/kustomize
if ! command -v ${KUSTOMIZE} >/dev/null 2>&1; then
    echo "Error: ${KUSTOMIZE} not installed. Run 'make kustomize' to install."
    exit 1
fi


#
# Process Input Variables
#
ACCESS=$1
SCOPE=$2
GPU_ARCH=$3
COSIGN_VERSION=$4
ENVIRONMENT=$5

# Overridable Input Variables
CUSTOM_AFFINITY=${CUSTOM_AFFINITY:-""}
CUSTOM_TOLERATION=${CUSTOM_TOLERATION:-""}
DEBUG=${DEBUG:-false}
# Node Selector:
#  CUSTOM_NODE_SELECTOR_1 is for Pod 1 or DaemonSet 1
#  CUSTOM_NODE_SELECTOR_2 is for Pod 2 or DaemonSet 2
#  CUSTOM_NODE_SELECTOR_3 is for Pod 3 or DaemonSet 3
CUSTOM_NODE_SELECTOR_1=${CUSTOM_NODE_SELECTOR_1:-""}
CUSTOM_NODE_SELECTOR_2=${CUSTOM_NODE_SELECTOR_2:-""}
CUSTOM_NODE_SELECTOR_3=${CUSTOM_NODE_SELECTOR_3:-""}

# Constants
BASE_DIR_COMMON="base/common"
OUTPUT_DIR="output"
AFFINITY_NFD_CUDA_FILE="patch/affinity-nfd-cuda.txt"
AFFINITY_NFD_ROCM_FILE="patch/affinity-nfd-rocm.txt"
NODE_SELECTOR_KIND_TRUE_FILE="patch/node-selector-kind-true.txt"
NODE_SELECTOR_KIND_FALSE_FILE="patch/node-selector-kind-false.txt"
TOLERATION_KIND_FILE="patch/toleration-kind.txt"
TOLERATION_NFD_CUDA_FILE="patch/toleration-nfd-cuda.txt"

# AccessMode of the PVC, valid values: rox (ReadOnlyMany) or rwo (ReadWriteOnce)
if [[ "$ACCESS" == "rox" ]]; then
    BASE_DIR_ACCESS="base/access/rox"
    VARIANTS_DIR_ACCESS="variants/access/rox"
elif [[ "$ACCESS" == "rwo" ]]; then
    BASE_DIR_ACCESS="base/access/rwo"
    VARIANTS_DIR_ACCESS="variants/access/rwo"
else
    echo "ERROR: Parameter 1 (ACCESS) must be \"rox\" or \"rwo\"."
    exit 1
fi

# Scope of the GKM Cache (GKMCache or ClusterGKMCache), valid values: cluster or namespace
if [[ "$SCOPE" == "cluster" ]]; then
    BASE_DIR_SCOPE="base/scope/cluster"
    VARIANTS_DIR_SCOPE="variants/scope/cluster"
elif [[ "$SCOPE" == "namespace" ]]; then
    BASE_DIR_SCOPE="base/scope/namespace"
    VARIANTS_DIR_SCOPE="variants/scope/namespace"
else
    echo "ERROR: Parameter 2 (SCOPE) must be \"cluster\" or \"namespace\"."
    exit 1
fi

# GPU Architecture, valid values: cuda or rocm
if [[ "$GPU_ARCH" != "cuda" && "$GPU_ARCH" != "rocm" ]]; then
    echo "ERROR: Parameter 3 (GPU_ARCH) must be \"cuda\" or \"rocm\"."
    exit 1
fi

# CoSign Version used to sign OCI Image, valid values: v2 or v3
if [[ "$COSIGN_VERSION" != "v2" && "$COSIGN_VERSION" != "v3" ]]; then
    echo "ERROR: Parameter 4 (COSIGN_VERSION) must be \"v2\" or \"v3\"."
    exit 1
fi

# Environment is to indicate KIND Cluster, valid values: kind
if [ -n "${ENVIRONMENT+x}" ]; then
    if [[ "$ENVIRONMENT" == "kind" ]]; then
        ENV_FILENAME_SUFFIX="-kind"
        if [[ "$GPU_ARCH" != "rocm" ]]; then
            echo "ERROR: KIND Cluster is currently only deployed with simulated ROCm GPUs."
            exit 1
        fi
    elif [[ "$ENVIRONMENT" == "nfd" ]]; then
        ENV_FILENAME_SUFFIX="-nfd"
    elif [[ "$ENVIRONMENT" != "" ]]; then
        echo "ERROR: Parameter 5 (ENVIRONMENT) must be \"kind\", \"nfd\" or not specified."
        exit 1
    fi
fi

# Generic Variables based on input
NAME_SUFFIX="${ACCESS}-${SCOPE}-${GPU_ARCH}-${COSIGN_VERSION}"
OBJECT_NAME="gkm-test-obj-${NAME_SUFFIX}"
if [[ "$COSIGN_VERSION" == "v2" ]]; then
    OCI_IMAGE="quay.io/gkm/cache-examples:vector-add-cache-${GPU_ARCH}-${COSIGN_VERSION}"
else
    OCI_IMAGE="quay.io/gkm/cache-examples:vector-add-cache-${GPU_ARCH}"
fi
COSIGN_VERSION_LABEL="cosign-${COSIGN_VERSION}"
NAMESPACE_1="gkm-test-ns-1-${NAME_SUFFIX}"
if [[ "$SCOPE" == "namespace" ]]; then
    NAMESPACE_2=${NAMESPACE_1}
else
    NAMESPACE_2="gkm-test-ns-2-${NAME_SUFFIX}"
fi


#
# Build overlays/scope/kustomization.yaml file with Namespace and GKMCache or ClusterGKMCache
# Build overlays/access/kustomization.yaml file with Pods or DaemonSets
# Broken into two files to control ordering of objects.
#
cat <<EOF > overlays/scope/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../../${BASE_DIR_COMMON}
- ../../${BASE_DIR_SCOPE}

components:
- ../../${VARIANTS_DIR_SCOPE}

nameSuffix: -${NAME_SUFFIX}
EOF

cat <<EOF > overlays/access/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../../${BASE_DIR_ACCESS}

components:
- ../../${VARIANTS_DIR_ACCESS}

nameSuffix: -${NAME_SUFFIX}
EOF


#
# Set Dynamic Patching. These patches are added based off input and inject a patch (via the
# Modify the Variants section below) to the Variant kustization.env files by replacing a TAG
# with the generated patch or replacing the TAG with nothing if patch not need.
#

if [[ "$ENVIRONMENT" == "kind" ]]; then
    # Where in a POD or DaemonSet Spec an initContainer is inserted
    KIND_INIT_CONTAINER_PATH_POD="/spec/initContainers"
    KIND_INIT_CONTAINER_PATH_DAEMON_SET="/spec/template/spec/initContainers"

    if [[ "$ACCESS" == "rox" ]]; then
        # ReadOnlyMany (rox) implies a Pod is being deployed, so set the path properly
        KIND_INIT_CONTAINER_PATH=${KIND_INIT_CONTAINER_PATH_POD}
    elif [[ "$ACCESS" == "rwo" ]]; then
        # ReadWriteOnce (rwo) implies a DaemonSet is being deployed, so set the path properly
        KIND_INIT_CONTAINER_PATH=${KIND_INIT_CONTAINER_PATH_DAEMON_SET}
    fi

    # KIND_INIT_CONTAINER holds a patch that is used to add an initContainer to Pods or DaemonSets
    # when KIND Cluster is being used. It sets the permissions of the PVC VolumeMount so that the
    # Pod can access it. Only needed in KIND.
    KIND_INIT_CONTAINER="    # For KIND Cluster, add initContainer that sets the permissions on the PVC VolumeMount
    - op: add
      path: ${KIND_INIT_CONTAINER_PATH}
      value: []
    - op: add
      path: ${KIND_INIT_CONTAINER_PATH}/-
      value:
        name: fix-permissions
        image: quay.io/fedora/fedora-minimal
        securityContext:
          runAsUser: 0
        command:
          - sh
          - -c
          - |
            chown -R 1000:1000 /cache
            chmod -R 775 /cache
        volumeMounts:
          - name: kernel-volume
            mountPath: /cache"

    # KIND_INIT_CONTAINER contains a multiline string, so special characters need
    # to be stripped for sed to process properly.
    ESCAPED_KIND_INIT_CONTAINER=$(printf '%s\n' "$KIND_INIT_CONTAINER" \
        | sed -e 's/[\/&]/\\&/g' -e ':a;N;$!ba;s/\n/\\n/g')
fi


if [[ "$ACCESS" == "rox" ]]; then
    # ACCESS_ROX_ACCESS_MODE holds a patch that is used to add ReadOnlyMany to the AccessMode field
    # in a GKMCache or ClusterGKMCache. Kubernetes does not have a way to be queried to determine if
    # ReadOnlyMany is supported by a StorageClass so GKM Operator/Agent need tobe told.
    ACCESS_ROX_ACCESS_MODE="    # Append ReadOnlyMany to the spec.accessModes slice in the GKMCache or ClusterGKMCache
    - op: add
      path: /spec/accessModes/-
      value: ReadOnlyMany"

    # ACCESS_ROX_ACCESS_MODE contains a multiline string, so special characters need
    # to be stripped for sed to process properly.
    ESCAPED_ACCESS_ROX_ACCESS_MODE=$(printf '%s\n' "$ACCESS_ROX_ACCESS_MODE" \
        | sed -e 's/[\/&]/\\&/g' -e ':a;N;$!ba;s/\n/\\n/g')
fi


# Affinity:
if [[ "$CUSTOM_AFFINITY" != "" ]]; then
    AFFINITY_INSTANCE=$(cat ${CUSTOM_AFFINITY})
elif [[ "$ENVIRONMENT" == "nfd" ]]; then
    if [[ "$GPU_ARCH" == "cuda" ]]; then
        AFFINITY_INSTANCE=$(cat ${AFFINITY_NFD_CUDA_FILE})
    elif [[ "$GPU_ARCH" == "rocm" ]]; then
        AFFINITY_INSTANCE=$(cat ${AFFINITY_NFD_ROCM_FILE})
    fi
fi

# Tolerations:
if [[ "$CUSTOM_TOLERATION" != "" ]]; then
    TOLERATION_INSTANCE=$(cat ${CUSTOM_TOLERATION})
elif [[ "$ENVIRONMENT" == "kind" ]]; then
    TOLERATION_INSTANCE=$(cat ${TOLERATION_KIND_FILE})
elif [[ "$ENVIRONMENT" == "nfd" ]]; then
    if [[ "$GPU_ARCH" == "cuda" ]]; then
        TOLERATION_INSTANCE=$(cat ${TOLERATION_NFD_CUDA_FILE})
    fi
fi

if [[ "$AFFINITY_INSTANCE" != "" || "$TOLERATION_INSTANCE" != "" ]]; then
    POD_TEMPLATE_ADD_GKMCACHE="    # Add a Affinity/Toleration to GKMCache or ClusterGKMCache
    - op: add
      path: /spec/podTemplate
      value: {}
    - op: add
      path: /spec/podTemplate/spec
      value: {}"

    if [[ "$AFFINITY_INSTANCE" != "" ]]; then
        POD_TEMPLATE_ADD_GKMCACHE+="
    - op: add
      path: /spec/podTemplate/spec/affinity
      value:
${AFFINITY_INSTANCE}"
    fi

    if [[ "$TOLERATION_INSTANCE" != "" ]]; then
        POD_TEMPLATE_ADD_GKMCACHE+="
    - op: add
      path: /spec/podTemplate/spec/tolerations
      value: []
    - op: add
      path: /spec/podTemplate/spec/tolerations/-
      value:
${TOLERATION_INSTANCE}"
    fi

    # POD_TEMPLATE_ADD_GKMCACHE contains a multiline string, so special characters need
    # to be stripped for sed to process properly.
    ESCAPED_POD_TEMPLATE_ADD_GKMCACHE=$(printf '%s\n' "$POD_TEMPLATE_ADD_GKMCACHE" \
        | sed -e 's/[\/&]/\\&/g' -e ':a;N;$!ba;s/\n/\\n/g')


    # Where in a POD or DaemonSet Spec affinity/toleration/is inserted
    AFFINITY_PATH_POD="/spec/affinity"
    AFFINITY_PATH_DAEMON_SET="/spec/template/spec/affinity"
    TOLERATION_PATH_POD="/spec/tolerations"
    TOLERATION_PATH_DAEMON_SET="/spec/template/spec/tolerations"

    if [[ "$ACCESS" == "rox" ]]; then
        # ReadOnlyMany (rox) implies a Pod is being deployed, so set the path properly
        AFFINITY_PATH=${AFFINITY_PATH_POD}
        TOLERATION_PATH=${TOLERATION_PATH_POD}
    elif [[ "$ACCESS" == "rwo" ]]; then
        # ReadWriteOnce (rwo) implies a DaemonSet is being deployed, so set the path properly
        AFFINITY_PATH=${AFFINITY_PATH_DAEMON_SET}
        TOLERATION_PATH=${TOLERATION_PATH_DAEMON_SET}
    fi

    if [[ "$AFFINITY_INSTANCE" != "" ]]; then
        AFFINITY_ADD_POD_DS="    # Add a Affinity to Pod or DaemonSet
    - op: add
      path: ${AFFINITY_PATH}
      value:
${AFFINITY_INSTANCE}"

        # AFFINITY_ADD_POD_DS contains a multiline string, so special characters need
        # to be stripped for sed to process properly.
        ESCAPED_AFFINITY_ADD_POD_DS=$(printf '%s\n' "$AFFINITY_ADD_POD_DS" \
            | sed -e 's/[\/&]/\\&/g' -e ':a;N;$!ba;s/\n/\\n/g')
    fi

    if [[ "$TOLERATION_INSTANCE" != "" ]]; then
        TOLERATION_ADD_POD_DS="    # Add a Toleration to Pod or DaemonSet
    - op: add
      path: ${TOLERATION_PATH}
      value: []
    - op: add
      path: ${TOLERATION_PATH}/-
      value:
${TOLERATION_INSTANCE}"

        # TOLERATION_ADD_POD_DS contains a multiline string, so special characters need
        # to be stripped for sed to process properly.
        ESCAPED_TOLERATION_ADD_POD_DS=$(printf '%s\n' "$TOLERATION_ADD_POD_DS" \
            | sed -e 's/[\/&]/\\&/g' -e ':a;N;$!ba;s/\n/\\n/g')
    fi
fi


# Node Selector:
#  .._SELECTOR_1 is for Pod 1 or DaemonSet 1
#  .._SELECTOR_2 is for Pod 2 or DaemonSet 2
#  .._SELECTOR_3 is for Pod 3 or DaemonSet 3

# Where in a POD or DaemonSet Spec a Node Selector is inserted
NODE_SELECTOR_PATH_POD="/spec/nodeSelector"
NODE_SELECTOR_PATH_DAEMON_SET="/spec/template/spec/nodeSelector"

if [[ "$ACCESS" == "rox" ]]; then
    # ReadOnlyMany (rox) implies a Pod is being deployed, so set the path properly
    NODE_SELECTOR_PATH=${NODE_SELECTOR_PATH_POD}
elif [[ "$ACCESS" == "rwo" ]]; then
    # ReadWriteOnce (rwo) implies a DaemonSet is being deployed, so set the path properly
    NODE_SELECTOR_PATH=${NODE_SELECTOR_PATH_DAEMON_SET}
fi

if [[ "$CUSTOM_NODE_SELECTOR_1" != "" ]]; then
    NODE_SELECTOR_INSTANCE_1=$(cat ${CUSTOM_NODE_SELECTOR_1})
fi

if [[ "$NODE_SELECTOR_INSTANCE_1" != "" ]]; then
    NODE_SELECTOR_1="    # Add NodeSelector to Pod/DaemonSet 1
    - op: add
      path: ${NODE_SELECTOR_PATH}
      value:
${NODE_SELECTOR_INSTANCE_1}"

    # NODE_SELECTOR_1 contains a multiline string, so special characters need
    # to be stripped for sed to process properly.
    ESCAPED_NODE_SELECTOR_1=$(printf '%s\n' "$NODE_SELECTOR_1" \
        | sed -e 's/[\/&]/\\&/g' -e ':a;N;$!ba;s/\n/\\n/g')
fi


if [[ "$CUSTOM_NODE_SELECTOR_2" != "" ]]; then
    NODE_SELECTOR_INSTANCE_2=$(cat ${CUSTOM_NODE_SELECTOR_2})
elif [[ "$ENVIRONMENT" == "kind" && "$ACCESS" == "rwo" ]]; then
    NODE_SELECTOR_INSTANCE_2=$(cat ${NODE_SELECTOR_KIND_TRUE_FILE})
fi

if [[ "$NODE_SELECTOR_INSTANCE_2" != "" ]]; then
    NODE_SELECTOR_2="    # Add NodeSelector to Pod/DaemonSet 2
    - op: add
      path: ${NODE_SELECTOR_PATH}
      value:
${NODE_SELECTOR_INSTANCE_2}"

    # NODE_SELECTOR_2 contains a multiline string, so special characters need
    # to be stripped for sed to process properly.
    ESCAPED_NODE_SELECTOR_2=$(printf '%s\n' "$NODE_SELECTOR_2" \
        | sed -e 's/[\/&]/\\&/g' -e ':a;N;$!ba;s/\n/\\n/g')
fi


if [[ "$CUSTOM_NODE_SELECTOR_3" != "" ]]; then
    NODE_SELECTOR_INSTANCE_3=$(cat ${CUSTOM_NODE_SELECTOR_3})
elif [[ "$ENVIRONMENT" == "kind" && "$ACCESS" == "rwo" ]]; then
    NODE_SELECTOR_INSTANCE_3=$(cat ${NODE_SELECTOR_KIND_FALSE_FILE})
fi

if [[ "$NODE_SELECTOR_INSTANCE_3" != "" ]]; then
    NODE_SELECTOR_3="    # Add NodeSelector to Pod/DaemonSet 3
    - op: add
      path: ${NODE_SELECTOR_PATH}
      value:
${NODE_SELECTOR_INSTANCE_3}"

    # NODE_SELECTOR_3 contains a multiline string, so special characters need
    # to be stripped for sed to process properly.
    ESCAPED_NODE_SELECTOR_3=$(printf '%s\n' "$NODE_SELECTOR_3" \
        | sed -e 's/[\/&]/\\&/g' -e ':a;N;$!ba;s/\n/\\n/g')
fi


#
# Modify the Variants Using sed to replace variables
#

# Set the Namespace name in Namespace 1 object
pushd ${BASE_DIR_COMMON} > /dev/null
${SED} \
 -e "s/NAMESPACE_1/${NAMESPACE_1}/g" \
 namespace-1.env > namespace-1.yaml
popd > /dev/null

# Set the Namespace name in Namespace 2 object only if Cluster scoped
if [[ "$SCOPE" == "cluster" ]]; then
    pushd ${BASE_DIR_SCOPE} > /dev/null
    ${SED} \
    -e "s/NAMESPACE_2/${NAMESPACE_2}/g" \
    namespace-2.env > namespace-2.yaml
    popd > /dev/null
fi

# UPDATE Pod or DaemonSet
# For both rox and rwo, for each Pod or DaemonSet:
# - set the Namespace
# - set the PVC Claim in the Volume to the generated GKMCache or ClusterGKMCache name
# - insert the KIND Init Container if KIND Cluster, otherwise remove the placeholder
pushd ${VARIANTS_DIR_ACCESS} > /dev/null
${SED} \
 -e "s/NAMESPACE_1/${NAMESPACE_1}/g" \
 -e "s/NAMESPACE_2/${NAMESPACE_2}/g" \
 -e "s/OBJECT_NAME/${OBJECT_NAME}/g" \
 -e "s@KIND_INIT_CONTAINER@${ESCAPED_KIND_INIT_CONTAINER}@g" \
 -e "s@TOLERATION_ADD_POD_DS@${ESCAPED_TOLERATION_ADD_POD_DS}@g" \
 -e "s@AFFINITY_ADD_POD_DS@${ESCAPED_AFFINITY_ADD_POD_DS}@g" \
 -e "s@NODE_SELECTOR_1@${ESCAPED_NODE_SELECTOR_1}@g" \
 -e "s@NODE_SELECTOR_2@${ESCAPED_NODE_SELECTOR_2}@g" \
 -e "s@NODE_SELECTOR_3@${ESCAPED_NODE_SELECTOR_3}@g" \
 kustomization.env > kustomization.yaml
popd > /dev/null

# UPDATE GKMCache or ClusterGKMCache
# For both cluster and namespace, for each GKMCache or ClusterGKMCache:
# - set the Namespace for GKMCache object (not ClusterGKMCache)
# - set OCI Image
# - add the Cosign Version label
# - set the workload namespace list for the ClusterGKMCache (not for GKMCache)
# - add "readOnlyMany" to the AccessModes field, or remove the placeholder
pushd ${VARIANTS_DIR_SCOPE} > /dev/null
${SED} \
 -e "s/OBJECT_NAME/${OBJECT_NAME}/g" \
 -e "s@OCI_IMAGE@${OCI_IMAGE}@g" \
 -e "s/COSIGN_VERSION_LABEL/${COSIGN_VERSION_LABEL}/g" \
 -e "s/NAMESPACE_1/${NAMESPACE_1}/g" \
 -e "s/NAMESPACE_2/${NAMESPACE_2}/g" \
 -e "s@ACCESS_ROX_ACCESS_MODE@${ESCAPED_ACCESS_ROX_ACCESS_MODE}@g" \
 -e "s@POD_TEMPLATE_ADD_GKMCACHE@${ESCAPED_POD_TEMPLATE_ADD_GKMCACHE}@g" \
 kustomization.env > kustomization.yaml

# If using ReadOnlyMany (rox), then add "ReadOnlyMany" to the GKMCache or ClusterGKMCache
# AccessMode field.
#if [[ "$ACCESS" == "rox" ]]; then
#    echo "${ACCESS_ROX_ACCESS_MODE}" >> kustomization.yaml
#fi
popd > /dev/null


#
# Generate the Yaml with all the objects
#
OUTPUT_FILENAME=${OUTPUT_DIR}/${NAME_SUFFIX}${ENV_FILENAME_SUFFIX}.yaml
mkdir -p "${OUTPUT_DIR}"

${KUSTOMIZE} build overlays/scope > ${OUTPUT_FILENAME} || exit 1
echo "---" >> ${OUTPUT_FILENAME}
${KUSTOMIZE} build overlays/access >> ${OUTPUT_FILENAME} || exit 1

if [[ "${DEBUG}" == true ]]; then
    cat ${OUTPUT_FILENAME}
    echo
fi

echo "${OUTPUT_FILENAME}"

if [[ "$CALL_POPD" == true ]]; then
    popd &>/dev/null || exit
fi
