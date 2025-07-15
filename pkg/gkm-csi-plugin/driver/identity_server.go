package driver

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/rs/zerolog/log"

	"github.com/redhat-et/GKM/pkg/utils"
)

// GetPluginInfo returns the name and volume of our driver
func (d *Driver) GetPluginInfo(context.Context, *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	log.Info().Msg("Request: GetPluginInfo")

	return &csi.GetPluginInfoResponse{
		Name:          utils.CsiDriverName,
		VendorVersion: Version,
	}, nil
}

// GetPluginCapabilities returns a list of the capabilities of this controller plugin
func (d *Driver) GetPluginCapabilities(context.Context, *csi.GetPluginCapabilitiesRequest,
) (*csi.GetPluginCapabilitiesResponse, error) {
	log.Info().Msg("Request: GetPluginCapabilities")

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}

// Probe is a health check for the driver
func (d *Driver) Probe(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	/*
		err := d.GkmClient.Ping()
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "unable to connect to GKM API: %s", err)
		}
	*/

	return &csi.ProbeResponse{
		Ready: &wrappers.BoolValue{
			Value: true,
		},
	}, nil
}

// func (d *Driver) mustEmbedUnimplementedIdentityServer() {}
