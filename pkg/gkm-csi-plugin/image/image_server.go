package image

import (
	"context"
	"fmt"
	"net"
	"os/exec"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	ctrl "sigs.k8s.io/controller-runtime"

	pb "github.com/redhat-et/GKM/pkg/gkm-csi-plugin/proto"
	"github.com/redhat-et/GKM/pkg/utils"
)

type ImageServer struct {
	grpcServer *grpc.Server
	log        logr.Logger

	NodeName  string
	Namespace string
	ImagePort string
	cacheDir  string
	TestMode  bool
	noGpu     bool

	pb.UnimplementedGkmCsiServiceServer
}

func (s *ImageServer) LoadKernelImage(ctx context.Context, req *pb.LoadKernelImageRequest,
) (*pb.LoadKernelImageResponse, error) {
	var namespace string

	if req.Namespace != nil {
		namespace = *req.Namespace
	} else {
		namespace = utils.ClusterScopedSubDir
	}

	s.log.Info("Received Load Kernel Image Request",
		"CRD Name", req.Name,
		"Image", req.Image.Url,
		"Namespace", namespace)

	err := s.ExtractImage(ctx, req.Image.Url, namespace, req.Name)

	if err != nil {
		return &pb.LoadKernelImageResponse{Message: "Load Image Request Failed"}, err
	}
	return &pb.LoadKernelImageResponse{Message: "Load Image Request Succeeded"}, nil
}

func (s *ImageServer) UnloadKernelImage(ctx context.Context, req *pb.UnloadKernelImageRequest,
) (*pb.UnloadKernelImageResponse, error) {
	var namespace string

	if req.Namespace != nil {
		namespace = *req.Namespace
	} else {
		namespace = utils.ClusterScopedSubDir
	}

	s.log.Info("Received Unload Kernel Image Request",
		"CRD Name", req.Name,
		"Namespace", namespace)

	if err := s.RemoveImage(namespace, req.Name); err != nil {
		s.log.Error(fmt.Errorf("failed to remove cache"), "Unload Failure",
			"CRD Name", req.Name,
			"Namespace", req.Namespace,
			"err", err)

		return &pb.UnloadKernelImageResponse{Message: "Unload Image Request Failed"}, err
	}

	return &pb.UnloadKernelImageResponse{Message: "Unload Image Request Succeeded"}, nil
}

// NewImageServer returns an ImageServer instance that implements gRPC endpoints
// for GKM to manage GPU Kernel Caches that are loaded via OCI Images.
func NewImageServer(nodeName, namespace, imagePort, cacheDir string, noGpu bool) (*ImageServer, error) {
	if !cmdExists(utils.TcvBinary) {
		return nil, fmt.Errorf("TCV must be installed")
	}

	return &ImageServer{
		grpcServer: grpc.NewServer(),
		NodeName:   nodeName,
		Namespace:  namespace,
		ImagePort:  imagePort,
		cacheDir:   cacheDir,
		noGpu:      noGpu,
	}, nil
}

func (s *ImageServer) Run(ctx context.Context) error {
	s.log = ctrl.Log.WithName("gkm-image-server")

	err := s.initializeFilesystem()
	if err != nil {
		return err
	}

	lis, err := net.Listen("tcp", s.ImagePort)
	if err != nil {
		s.log.Error(err, "failed to listen", "ImagePort", s.ImagePort)
		return err
	}

	pb.RegisterGkmCsiServiceServer(s.grpcServer, s)
	s.log.Info("gRPC server", "listening at", lis.Addr())
	if err := s.grpcServer.Serve(lis); err != nil {
		s.log.Error(err, "failed to serve")
		return err
	}

	return nil
}

func cmdExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}
