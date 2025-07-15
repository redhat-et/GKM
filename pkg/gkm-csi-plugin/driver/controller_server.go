package driver

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateVolume is the first step when a PVC tries to create a dynamic volume.
// This is not needed for GKM.
func (d *Driver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	d.log.Info("Request: CreateVolume, returning unimplemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// DeleteVolume is used once a volume is unused and therefore unmounted, to stop the
// resources being used and subsequent billing.
// This is not needed for GKM.
func (d *Driver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	d.log.Info("Request: DeleteVolume, returning unimplemented", "volume_id", req.VolumeId)
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerPublishVolume is used to mount an underlying volume to a given Kubernetes node.
// This is not needed for GKM.
func (d *Driver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest,
) (*csi.ControllerPublishVolumeResponse, error) {
	d.log.Info("Request: ControllerPublishVolume, returning unimplemented",
		"volume_id", req.VolumeId, "node_id", req.NodeId)
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerUnpublishVolume detaches the volume from the given Kubernetes node it was connected.
// This is not needed for GKM.
func (d *Driver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest,
) (*csi.ControllerUnpublishVolumeResponse, error) {
	d.log.Info("Request: ControllerUnpublishVolume, returning unimplemented", "volume_id", req.VolumeId)
	return nil, status.Error(codes.Unimplemented, "")
}

// ValidateVolumeCapabilities returns the features of the volume, e.g. RW, RO, RWX.
// This is not needed for GKM.
func (d *Driver) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest,
) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	d.log.Info("Request: ValidateVolumeCapabilities, returning unimplemented", "volume_id", req.VolumeId)
	return nil, status.Error(codes.Unimplemented, "")
}

// ListVolumes returns the existing GKM volumes.
// This is not needed for GKM.
func (d *Driver) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	d.log.Info("Request: ListVolumes, returning unimplemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// GetCapacity returns the user's available quota.
// This is not needed for GKM.
func (d *Driver) GetCapacity(context.Context, *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	d.log.Info("Request: GetCapacity, returning unimplemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities returns the capabilities of the controller, what features it implements
func (d *Driver) ControllerGetCapabilities(context.Context, *csi.ControllerGetCapabilitiesRequest,
) (*csi.ControllerGetCapabilitiesResponse, error) {
	d.log.Info("Request: ControllerGetCapabilities")

	rawCapabilities := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_GET_CAPACITY,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		// csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		// csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
	}

	var csc []*csi.ControllerServiceCapability

	for _, cap := range rawCapabilities {
		csc = append(csc, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}

	d.log.V(1).Info("Capabilities for controller requested", "capabilities", csc)

	resp := &csi.ControllerGetCapabilitiesResponse{
		Capabilities: csc,
	}

	return resp, nil
}

// CreateSnapshot is part of implementing Snapshot & Restore functionality.
// This is not needed for GKM.
func (d *Driver) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest,
) (*csi.CreateSnapshotResponse, error) {
	d.log.Info("Request: CreateSnapshot, returning unimplemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// DeleteSnapshot is part of implementing Snapshot & Restore functionality.
// This is not needed for GKM.
func (d *Driver) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest,
) (*csi.DeleteSnapshotResponse, error) {
	d.log.Info("Request: DeleteSnapshot, returning unimplemented", "snapshot_id", req.GetSnapshotId())
	return nil, status.Error(codes.Unimplemented, "")
}

// ListSnapshots retrieves a list of existing snapshots as part of the Snapshot & Restore functionality.
// This is not needed for GKM.
func (d *Driver) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	d.log.Info("Request: ListSnapshots, returning unimplemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerExpandVolume allows for offline expansion of Volumes.
// This is not needed for GKM.
func (d *Driver) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest,
) (*csi.ControllerExpandVolumeResponse, error) {
	d.log.Info("Request: ControllerExpandVolume, returning unimplemented", "volume_id", req.GetVolumeId())
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetVolume is for optional Kubernetes health checking of volumes.
// This is not needed for GKM.
func (d *Driver) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest,
) (*csi.ControllerGetVolumeResponse, error) {
	d.log.Info("Request: ControllerGetVolume, returning unimplemented", "volume_id", req.GetVolumeId())
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerModifyVolume is used to modify a given volume.
// This is not needed for GKM.
func (d *Driver) ControllerModifyVolume(_ context.Context, _ *csi.ControllerModifyVolumeRequest,
) (*csi.ControllerModifyVolumeResponse, error) {
	d.log.Info("Request: ControllerModifyVolume, returning unimplemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// func (d *Driver) mustEmbedUnimplementedControllerServer() {}
