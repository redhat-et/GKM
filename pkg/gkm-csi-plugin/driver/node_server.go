package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/redhat-et/GKM/pkg/usage"
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
		// GKMCache instance.
		crNamespace = utils.ClusterScopedSubDir
		clusterScoped = true
	}

	// The Pod Spec should contain CSI fields in the Volume Mount section, and that
	// should contain the name of the GKMCache or GKMCacheCluster CRD that provides
	// OCI Image. Pull that out of the input data. The namespace and CRD name is
	// embedded in the directory structure on the Node if it has been downloaded and
	// expanded.
	crName, ok := req.VolumeContext[utils.CsiCacheIndex]
	if !ok {
		if clusterScoped {
			d.log.Error(fmt.Errorf("must provide a GKMCacheCluster"), "invalid input")
			return nil, status.Error(codes.InvalidArgument, "must provide a GKMCacheCluster NodePublishVolume")
		} else {
			d.log.Error(fmt.Errorf("must provide a GKMCache"), "invalid input")
			return nil, status.Error(codes.InvalidArgument, "must provide a GKMCache NodePublishVolume")
		}
	}

	// Build up the directory name from the namespace and CRD name.
	sourcePath := d.cacheDir
	sourcePath = filepath.Join(sourcePath, crNamespace)
	sourcePath = filepath.Join(sourcePath, crName)
	if _, err := os.Stat(sourcePath); err != nil {
		if os.IsNotExist(err) {
			if clusterScoped {
				d.log.Error(fmt.Errorf("GKMCacheCluster has not been created"), "invalid input", "name", crName)
				return nil, status.Error(codes.InvalidArgument, "GKMCacheCluster has not been created NodePublishVolume")
			} else {
				d.log.Error(fmt.Errorf("GKMCache has not been created"),
					"invalid input", "name", crName, "namespace", crNamespace)
				return nil, status.Error(codes.InvalidArgument, "GKMCache has not been created NodePublishVolume")
			}
		} else {
			d.log.Error(fmt.Errorf("unable to verify sourcePath"), "invalid input", "sourcePath", sourcePath)
			return nil, status.Error(codes.InvalidArgument, "unable to verify sourcePath")
		}
	}
	d.log.Info("found GKMCache CRD", "sourcePath", sourcePath)

	// Make sure the target directory is created. This is where the Kernel Cache will
	// be mounted into the Pod.
	if err := os.MkdirAll(req.TargetPath, 0755); err != nil {
		d.log.Error(err, "failed to create target path", "volume_id", req.VolumeId, "targetPath", req.TargetPath)
		return nil, err
	}

	// Check if the Kernel Cache is already mounted.
	mounted, err := utils.IsTargetBindMount(req.TargetPath, d.log)
	if err != nil {
		d.log.Error(fmt.Errorf("unable to verify if targetPath already mounted"),
			"invalid input", "targetPath", req.TargetPath)
		return nil, status.Error(codes.InvalidArgument, "unable to verify if targetPath already mounted")
	}

	// Code needs to be idempotent, so if already mounted, just return.
	if mounted {
		_, err := usage.GetUsageDataByVolumeId(req.VolumeId, d.log)
		if err != nil {
			size, err := utils.DirSize(sourcePath)
			if err != nil {
				size = 0
				d.log.Error(err, "unable to get directory size", "sourcePath", sourcePath)
			}

			// Save off the VolumeId mapping to CRD Info
			err = usage.AddUsageData(crNamespace, crName, req.VolumeId, size, d.log)
			if err != nil {
				d.log.Error(err, "unable to save usage data", "VolumeId", req.VolumeId)
			}
		}

		d.log.Info("kernel cache already bind mounted", "sourcePath", sourcePath, "targetPath", req.TargetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	d.log.Info("bind mounting kernel cache", "sourcePath", sourcePath, "targetPath", req.TargetPath)

	// Perform the bind mount
	options := []string{"bind"}
	if req.Readonly {
		options = append(options, "ro")
	}

	if err := d.mounter.Mount(sourcePath, req.TargetPath, "", options); err != nil {
		d.log.Error(fmt.Errorf("bind mount failed"), "invalid input", "sourcePath", sourcePath, "targetPath", req.TargetPath)
		return nil, status.Error(codes.Internal, "bind mount failed")
	}

	// Save off the VolumeId mapping to CRD Info
	size, err := utils.DirSize(sourcePath)
	if err != nil {
		size = 0
		d.log.Error(err, "unable to get directory size", "sourcePath", sourcePath)
	}

	err = usage.AddUsageData(crNamespace, crName, req.VolumeId, size, d.log)
	if err != nil {
		d.log.Error(err, "unable to save usage data", "VolumeId", req.VolumeId)
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

	/* Delete Cache Data */
	err := usage.DeleteUsageData(req.VolumeId, d.log)
	if err != nil {
		d.log.Error(err, "usage deletion failed, just continue", "VolumeId",
			req.VolumeId, "TargetPath", req.TargetPath)
	}

	// Check if already mounted
	// d.mounter.IsLikelyNotMountPoint() doesn't detect bind mounts, so manually search
	// the list of mounts for the Target Path.
	mounted, err := utils.IsTargetBindMount(req.TargetPath, d.log)
	if err != nil {
		if os.IsNotExist(err) {
			d.log.Info("targetPath does not exist, just continue", "VolumeId", req.VolumeId, "TargetPath", req.TargetPath,
				"Mount", mounted)
		} else {
			d.log.Error(fmt.Errorf("unable to verify if targetPath already mounted"),
				"Internal Error", "targetPath", req.TargetPath)
			return nil, status.Error(codes.InvalidArgument, "unable to verify if targetPath already mounted")
		}
	}

	// Only attempt to unmount if it's mounted
	if mounted {
		if err := d.mounter.Unmount(req.TargetPath); err != nil {
			d.log.Error(fmt.Errorf("umount failed"), "invalid input", "targetPath", req.TargetPath)
			return nil, status.Error(codes.Internal, "umount failed")
		}
	} else {
		d.log.Info("targetPath is not mounted, just continue", "VolumeId", req.VolumeId, "TargetPath", req.TargetPath)
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
	usageData, err := usage.GetUsageDataByVolumeId(req.VolumeId, d.log)
	if err != nil {
		d.log.Error(fmt.Errorf("could not map VolumeId to GKMCache"), "invalid input")
		return nil, status.Error(codes.InvalidArgument, "could not map VolumeId to GKMCache NodeUnpublishVolume")
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
