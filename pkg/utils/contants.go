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
	DefaultCacheDir = "/run/gkm/caches"

	// DefaultCacheDir is the default root directory to store the expanded the GPU Kernel
	// images.
	ClusterScopedSubDir = "cluster-scoped"

	// TcvBinary is the location on the host of the TCV binary.
	TcvBinary = "tcv"

	// Name of the GKM ConfigMap that is used to control how GKM is Deployed and Functions.
	GKMConfigName = "gkm-config"

	// CSI Driver Constants
	CsiDriverName          = "csi.gkm.io"
	CsiCacheIndex          = "csi.gkm.io/GKMCache"
	CsiCacheNamespaceIndex = "csi.gkm.io/namespace"
	CsiDriverYamlFile      = "./csi-driver.yaml"

	// GKMOperatorFinalizer is the finalizer that holds a ConfigMap from deletion until
	// cleanup can be performed.
	GKMOperatorFinalizer = "gkm.io.operator/finalizer"

	// Duration for Kubernetes to Retry a failed request
	RetryDurationOperator = 5 * time.Second
)
