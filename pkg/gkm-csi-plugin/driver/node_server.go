package driver

import (
	"context"
	"fmt"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/redhat-et/GKM/pkg/database"
	"github.com/redhat-et/GKM/pkg/utils"
)

// MaxVolumesPerNode is the maximum number of volumes a single node may host
const MaxVolumesPerNode int64 = 1024

// NodeStageVolume is called after the volume is attached to the instance, so it can be
// partitioned, formatted and mounted to a staging path.
// This is not needed for GKM.
func (d *Driver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest,
) (*csi.NodeStageVolumeResponse, error) {
	d.log.Info("Request: NodeStageVolume, returning unimplemented",
		"volume_id", req.VolumeId, "staging_target_path", req.StagingTargetPath)
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeUnstageVolume unmounts the volume when it's finished with, ready for deletion.
// This is not needed for GKM.
func (d *Driver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest,
) (*csi.NodeUnstageVolumeResponse, error) {
	d.log.Info("Request: NodeUnstageVolume, returning unimplemented",
		"volume_id", req.VolumeId, "staging_target_path", req.StagingTargetPath)
	return nil, status.Error(codes.Unimplemented, "")
}

// NodePublishVolume bind mounts the staging path into the container.
func (d *Driver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest,
) (*csi.NodePublishVolumeResponse, error) {
	d.log.Info("Request: NodePublishVolume",
		"VolumeId", req.VolumeId,
		"StagingTargetPath", req.StagingTargetPath,
		"TargetPath", req.TargetPath,
		"VolumeCapability", req.VolumeCapability,
		"VolumeContext", req.VolumeContext)

	// Validate input parameters.
	if req.VolumeId == "" {
		d.log.Error(fmt.Errorf("must provide a VolumeId to NodePublishVolume"), "invalid input")
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to NodePublishVolume")
	}
	if req.TargetPath == "" {
		d.log.Error(fmt.Errorf("must provide a TargetPath to NodePublishVolume"), "invalid input")
		return nil, status.Error(codes.InvalidArgument, "must provide a TargetPath to NodePublishVolume")
	}
	if req.VolumeCapability == nil {
		d.log.Error(fmt.Errorf("must provide a VolumeCapability to NodePublishVolume"), "invalid input")
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeCapability to NodePublishVolume")
	}

	// Determine if Namespace or Cluster Scoped.
	clusterScoped := false
	crNamespace, ok := req.VolumeContext[utils.CsiCacheNamespaceIndex]
	if !ok {
		// Namespace is not required. If not provided, then assume it is a Cluster scoped
		// ClusterGKMCache instance.
		crNamespace = ""
		clusterScoped = true
	}

	// The Pod Spec should contain CSI fields in the Volume Mount section, and that
	// should contain the name of the GKMCache or ClusterGKMCache CRD that provides
	// OCI Image. Pull that out of the input data. The namespace and CRD name is
	// embedded in the directory structure on the Node if it has been downloaded and
	// expanded.
	crName, ok := req.VolumeContext[utils.CsiCacheNameIndex]
	if !ok {
		if clusterScoped {
			d.log.Error(fmt.Errorf("must provide a ClusterGKMCache"), "invalid input")
			return nil, status.Error(codes.InvalidArgument, "must provide a ClusterGKMCache name")
		} else {
			d.log.Error(fmt.Errorf("must provide a GKMCache"), "invalid input")
			return nil, status.Error(codes.InvalidArgument, "must provide a GKMCache name")
		}
	}

	// Check if the Kernel Cache is already mounted.
	mounted, _ := database.IsTargetBindMount(req.TargetPath, d.log)
	/*
		if err != nil {
			if !os.IsNotExist(err) {
				d.log.Error(err, "unable to verify if targetPath already mounted",
					"targetPath", req.TargetPath, "volumeId", req.VolumeId, "namespace", crNamespace, "name", crName)
				return nil, status.Error(codes.InvalidArgument, "unable to verify if targetPath already mounted")
			}
		}
	*/

	// Code needs to be idempotent, so if already mounted, just return.
	if mounted {
		// Make sure the usage data exists
		_, err := database.GetUsageDataByVolumeId(req.VolumeId, d.log)
		if err != nil {
			d.log.Error(fmt.Errorf("targetPath already mounted, but no usage data stored, continue to try to clean up"),
				"internal error",
				"targetPath", req.TargetPath,
				"volumeId", req.VolumeId,
				"namespace", crNamespace,
				"name", crName)
		} else {
			// Target Mounted and Usage exists, just return
			d.log.Info("kernel cache already bind mounted",
				"targetPath", req.TargetPath, "volumeId", req.VolumeId, "namespace", crNamespace, "name", crName)
			return &csi.NodePublishVolumeResponse{}, nil
		}
	}

	// ELSE Target is not mounted yet, so process the request.

	// Determine if Cache has been extracted and if so, build source directory.
	cacheData, err := database.GetCacheFile(crNamespace, crName, d.log)
	if err != nil {
		if clusterScoped {
			d.log.Error(fmt.Errorf("ClusterGKMCache has not been extracted"), "not ready",
				"targetPath", req.TargetPath, "volumeId", req.VolumeId, "namespace", crNamespace, "name", crName)
			return nil, status.Error(codes.InvalidArgument, "ClusterGKMCache has not been extracted")
		} else {
			d.log.Error(fmt.Errorf("GKMCache has not been extracted"), "not ready",
				"targetPath", req.TargetPath, "volumeId", req.VolumeId, "namespace", crNamespace, "name", crName)
			return nil, status.Error(codes.InvalidArgument, "GKMCache has not been extracted")
		}
	}
	sourcePath, err := database.BuildDbDir(d.cacheDir, crNamespace, crName, cacheData.ResolvedDigest, d.log)
	if err != nil {
		if clusterScoped {
			d.log.Error(fmt.Errorf("ClusterGKMCache processing error"), "internal error",
				"targetPath", req.TargetPath,
				"volumeId", req.VolumeId,
				"namespace", crNamespace,
				"name", crName,
				"digest", cacheData.ResolvedDigest)
			return nil, status.Error(codes.InvalidArgument, "ClusterGKMCache processing error")
		} else {
			d.log.Error(fmt.Errorf("GKMCache processing error"), "internal error",
				"targetPath", req.TargetPath,
				"volumeId", req.VolumeId,
				"namespace", crNamespace,
				"name", crName,
				"digest", cacheData.ResolvedDigest)
			return nil, status.Error(codes.InvalidArgument, "GKMCache processing error")
		}
	}
	if clusterScoped {
		d.log.Info("found ClusterGKMCache extracted cache", "sourcePath", sourcePath)
	} else {
		d.log.Info("found GKMCache extracted cache", "sourcePath", sourcePath)
	}

	// Make sure the target directory is created. This is where the Kernel Cache will
	// be mounted into the Pod.
	if err := os.MkdirAll(req.TargetPath, 0755); err != nil {
		d.log.Error(err, "failed to create target path",
			"targetPath", req.TargetPath,
			"volumeId", req.VolumeId,
			"namespace", crNamespace,
			"name", crName,
			"digest", cacheData.ResolvedDigest)
		return nil, err
	}

	// Perform the bind mount
	d.log.Info("bind mounting kernel cache", "sourcePath", sourcePath, "targetPath", req.TargetPath)
	if err := database.BindMount(sourcePath, req.TargetPath, req.Readonly, d.mounter, d.log); err != nil {
		d.log.Error(err, "failed to bind mount target path",
			"sourcePath", sourcePath,
			"targetPath", req.TargetPath,
			"volumeId", req.VolumeId,
			"namespace", crNamespace,
			"name", crName,
			"digest", cacheData.ResolvedDigest)
		return nil, err
	}

	// Pull the Size from Cache Data and store in the Usage Data.
	size, ok := cacheData.Sizes[cacheData.ResolvedDigest]
	if !ok {
		size = 0
	}

	// Add the Usage Data
	err = database.AddUsageData(crNamespace, crName, cacheData.ResolvedDigest, req.VolumeId, size, d.log)
	if err != nil {
		d.log.Error(err, "unable to save usage data",
			"volumeId", req.VolumeId, "namespace", crNamespace, "name", crName, "digest", cacheData.ResolvedDigest)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume removes the bind mount
func (d *Driver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest,
) (*csi.NodeUnpublishVolumeResponse, error) {
	d.log.Info("Request: NodeUnpublishVolume",
		"VolumeId", req.VolumeId,
		"TargetPath", req.TargetPath)

	if req.VolumeId == "" {
		d.log.Error(fmt.Errorf("must provide a VolumeId to NodeUnpublishVolume"), "invalid input")
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to NodeUnpublishVolume")
	}
	if req.TargetPath == "" {
		d.log.Error(fmt.Errorf("must provide a TargetPath to NodeUnpublishVolume"), "invalid input")
		return nil, status.Error(codes.InvalidArgument, "must provide a TargetPath to NodeUnpublishVolume")
	}

	// Check if already mounted
	mounted, err := database.IsTargetBindMount(req.TargetPath, d.log)
	if err != nil {
		if os.IsNotExist(err) {
			d.log.Info("targetPath does not exist, just continue",
				"VolumeId", req.VolumeId, "TargetPath", req.TargetPath, "Mount", mounted)
		} else {
			d.log.Error(fmt.Errorf("unable to verify if targetPath already mounted"), "Internal Error",
				"VolumeId", req.VolumeId, "TargetPath", req.TargetPath)
			return nil, status.Error(codes.Aborted, "unable to verify if targetPath already mounted")
		}
	}

	// Only attempt to unmount if it's mounted
	if mounted {
		if err := d.mounter.Unmount(req.TargetPath); err != nil {
			d.log.Error(fmt.Errorf("umount failed"), "internal error",
				"VolumeId", req.VolumeId, "TargetPath", req.TargetPath)
			return nil, status.Error(codes.Aborted, "umount failed")
		} else {
			d.log.Info("targetPath has been unmounted", "VolumeId", req.VolumeId, "TargetPath", req.TargetPath)
		}
	} else {
		d.log.Info("targetPath is not mounted, just continue", "VolumeId", req.VolumeId, "TargetPath", req.TargetPath)
	}

	/* Delete Usage Data */
	if err = database.DeleteUsageData(req.VolumeId, d.log); err != nil {
		d.log.Error(err, "usage deletion failed, just continue",
			"VolumeId", req.VolumeId, "TargetPath", req.TargetPath)
	} else {
		d.log.Info("usage data deleted", "VolumeId", req.VolumeId, "TargetPath", req.TargetPath)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetInfo returns some identifier (ID, name) for the current node
func (d *Driver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	d.log.Info("Request: NodeGetInfo")

	return &csi.NodeGetInfoResponse{
		NodeId:            d.NodeName,
		MaxVolumesPerNode: MaxVolumesPerNode,

		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				"region": "unknown",
			},
		},
	}, nil
}

// NodeGetVolumeStats returns the volume capacity statistics available for the the given volume
func (d *Driver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest,
) (*csi.NodeGetVolumeStatsResponse, error) {
	d.log.Info("Request: NodeGetVolumeStats", "volume_id", req.VolumeId)

	if req.VolumeId == "" {
		d.log.Error(fmt.Errorf("must provide a VolumeId to NodeGetVolumeStats"), "invalid input")
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumeId to NodeGetVolumeStats")
	}

	volumePath := req.VolumePath
	if volumePath == "" {
		d.log.Error(fmt.Errorf("must provide a VolumePath to NodeGetVolumeStats"), "invalid input")
		return nil, status.Error(codes.InvalidArgument, "must provide a VolumePath to NodeGetVolumeStats")
	}

	/* Check if Cache Data was created */
	usageData, err := database.GetUsageDataByVolumeId(req.VolumeId, d.log)
	if err != nil {
		d.log.Error(fmt.Errorf("could not map VolumeId to GKMCache"), "invalid input")
		return nil, status.Error(codes.Aborted, "could not map VolumeId to GKMCache NodeUnpublishVolume")
	}

	d.log.Info("Node capacity statistics retrieved",
		"bytes_available", 0,
		"bytes_total", usageData.VolumeSize,
		"bytes_used", usageData.VolumeSize,
		"usageData", usageData)

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: 0,
				Total:     usageData.VolumeSize,
				Used:      usageData.VolumeSize,
				Unit:      csi.VolumeUsage_BYTES,
			},
		},
	}, nil
}

// NodeExpandVolume is used to expand the filesystem inside volumes.
// This is not needed for GKM.
func (d *Driver) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest,
) (*csi.NodeExpandVolumeResponse, error) {
	d.log.Info("Request: NodeExpandVolume, returning unimplemented",
		"volume_id", req.VolumeId, "target_path", req.VolumePath)
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeGetCapabilities returns the capabilities that this node and driver support
func (d *Driver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest,
) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
					},
				},
			},
		},
	}, nil
}
