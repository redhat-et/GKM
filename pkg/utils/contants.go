package utils

import (
	"time"
)

const (
	// DefaultSocketFilename is the location of the Unix domain socket for this CSI driver
	// for Kubelet to send requests.
	DefaultSocketFilename string = "unix:///var/lib/kubelet/plugins/csi-gkm/csi.sock"

	// DefaultImagePort is the location of port the Image Server will listen on for GKM
	// to send requests.
	DefaultImagePort string = ":50051"

	// DefaultCacheDir is the default root directory to store the expanded the GPU Kernel
	// images.
	DefaultCacheDir = "/var/lib/gkm/caches"
	CacheFilename   = "cache.json"

	// DefaultUsageDir is the default root directory to store the usage data for the GPU Kernel
	// images.
	DefaultUsageDir = "/run/gkm/usage"
	UsageFilename   = "usage.json"

	// DefaultCacheDir is the default root directory to store the expanded the GPU Kernel
	// images.
	ClusterScopedSubDir = "cluster-scoped"

	// Name of the GKM ConfigMap that is used to control how GKM is Deployed and Functions.
	GKMConfigName = "gkm-config"

	// Name of the GKM Namespace where Operator and Agent run.
	GKMDefaultNamespace = "gkm-system"

	// GKMCache and ClusterGKMCache Annotations
	GKMCacheAnnotationResolvedDigest  = "gkm.io/resolvedDigest"
	GKMCacheAnnotationCacheSizeBytes  = "gkm.io/cache-size-bytes"
	GKMClusterAnnotationMutationSig   = "gkm.io/mutationSig"
	GKMClusterAnnotationLastMutatedBy = "gkm.io/lastMutatedBy"

	// GKMCache and ClusterGKMCache Labels
	GKMCacheLabelHostname         = "kubernetes.io/hostname"
	GKMCacheNodeLabelCache        = "gkm.io/gkm-cache"
	GKMClusterCacheNodeLabelCache = "gkm.io/cluster-gkm-cache"
	GKMCachePvcMutation           = "gkm.io/pvc-mutation"

	// PV and PVC Labels
	PvLabelCache         = "cache-name"
	PvLabelPvcNamespace  = "pvc-namespace"
	PvLabelNode          = "node"
	PvLabelDigest        = "digest"
	PvcLabelCache        = "cache-name"
	PvcLabelPvcNamespace = "pvc-namespace"
	PvcLabelNode         = "node"
	PvcLabelDigest       = "digest"

	// OCI Image Label
	ImageLabelCacheSizeBytesSubstring = "cache-size-bytes"

	// Job to Extract Cache
	JobExtractName               = "gkm-kernel-cache-extract"
	JobExtractImage              = "quay.io/gkm/gkm-extract:latest"
	JobInitImage                 = "quay.io/fedora/fedora-minimal"
	JobExtractEnvCacheDir        = "GKM_CACHE_DIR"
	JobExtractEnvImageUrl        = "GKM_IMAGE_URL"
	JobExtractEnvNoGpu           = "NO_GPU"
	JobExtractPvcSourceMountName = "gkm-pvc-source"
	JobExtractLabelPvc           = "pvc"
	JobExtractLabelDigest        = "digest"
	JobExtractLabelNode          = "node"
	JobTTLSeconds                = 3600 // One hour. Can be overwritten by the value in the configmap
	JobFSGroup                   = 1000
	MaxLabelValueLength          = 60 // Labels can only be 63 characters
	DigestPrefix                 = "sha256:"
	MountPath                    = "/cache"

	// Kyverno Annotations
	KyvernoVerifyImagesAnnotation = "kyverno.io/verify-images"

	// GKMOperatorFinalizer is the finalizer that holds a ConfigMap from deletion until
	// cleanup can be performed.
	GKMOperatorFinalizer = "gkm.io.operator/finalizer"

	// ClusterGkmCacheFinalizer is the finalizer that holds a ClusterGKMCache from deletion
	// until ClusterGkmCacheNode is cleaned up.
	ClusterGkmCacheFinalizer = "gkm.io.clustergkmcachenode/finalizer"

	// GkmCacheFinalizer is the finalizer that holds a GKMCache from deletion until
	// GkmCacheNode is cleaned up.
	GkmCacheFinalizer = "gkm.io.gkmcachenode/finalizer"

	// The GkmCacheNode Finalizer is the finalizer that holds a GKMCacheNode from deletion
	// until GkmCache is deleted and cleanup can be performed. Since GkmCacheNode tracks
	// multiple GKMCache, the finalizer is "gkm.io.<GKMCacheName>/finalizer".
	GkmCacheNodeFinalizerPrefix    = "gkm.io."
	GkmCacheNodeFinalizerSubstring = "/finalizer"

	// ConfigMap Indexes
	ConfigMapIndexOperatorLogLevel = "gkm.operator.log.level"
	ConfigMapIndexAgentImage       = "gkm.agent.image"
	ConfigMapIndexAgentLogLevel    = "gkm.agent.log.level"
	ConfigMapIndexExtractImage     = "gkm.extract.image"
	ConfigMapIndexExtractLogLevel  = "gkm.extract.log.level"
	ConfigMapIndexNoGpu            = "gkm.nogpu"
	ConfigMapIndexKindCluster      = "gkm.kind.cluster"
	ConfigMapIndexKyvernoEnabled   = "gkm.kyverno.enabled"

	// Duration for Kubernetes to Retry a failed request
	RetryOperatorConfigMapFailure = 5 * time.Second
	RetryOperatorFailure          = 10 * time.Second // Retry if there was an internal error

	// Durations to Retry Agent Reconcile
	RetryAgentFailure          = 10 * time.Second // Retry if there was an internal error
	RetryAgentNextStep         = 1 * time.Second  // KubeAPI call updated object so restart Reconcile
	RetryAgentUsagePoll        = 5 * time.Second  // Polling Cache to refresh GKMCacheNode Status usage data
	RetryAgentNodeStatusUpdate = 1 * time.Second  // Status Updates not kicking Reconcile

	// Environment Variables
	EnvKyvernoEnabled = "KYVERNO_VERIFICATION_ENABLED"
)
