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

	// TcvBinary is the location on the host of the TCV binary.
	TcvBinary = "tcv"

	// Name of the GKM ConfigMap that is used to control how GKM is Deployed and Functions.
	GKMConfigName = "gkm-config"

	// CSI Driver Constants
	CsiDriverName          = "csi.gkm.io"
	CsiCacheNamespaceIndex = "csi.gkm.io/namespace"
	CsiCacheNameIndex      = "csi.gkm.io/GKMCache"
	CsiDriverYamlFile      = "./csi-driver.yaml"

	// GKMCache and ClusterGKMCache Annotations
	GMKCacheAnnotationResolvedDigest = "gkm.io/resolvedDigest"

	// GKMCache and ClusterGKMCache LAbels
	GMKCacheLabelHostname = "kubernetes.io/hostname"
	GMKCacheLabelOwnedBy  = "gkm.io/ownedByCache"

	// GKMOperatorFinalizer is the finalizer that holds a ConfigMap from deletion until
	// cleanup can be performed.
	GKMOperatorFinalizer = "gkm.io.operator/finalizer"

	// ClusterGkmCacheFinalizer is the finalizer that holds a ClusterGKMCacheNode from deletion
	// until ClusterGkmCache is deleted and cleanup can be performed.
	ClusterGkmCacheFinalizer = "gkm.io.clustergkmcachefinalizer/finalizer"
	// NamespaceGkmCacheFinalizer is the finalizer that holds a GKMCacheNode from deletion
	// until GkmCache is deleted and cleanup can be performed.
	NamespaceGkmCacheFinalizer = "gkm.io.namespacegkmcachefinalizer/finalizer"

	// Duration for Kubernetes to Retry a failed request
	RetryDurationOperator = 5 * time.Second

	// Durations to Retry Agent Reconcile
	RetryAgentFailure   = 10 * time.Second // Retry if there was an internal error
	RetryAgentNextStep  = 1 * time.Second  // KubeAPI call updated object so restart Reconcile
	RetryAgentUsagePoll = 5 * time.Second  // Polling Cache to refresh GKMCacheNode Status usage data
)
