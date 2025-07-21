package driver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"k8s.io/mount-utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// Name is the name of the driver
const Name string = "GKM CSI Driver"

// Version is the current version of the driver to set in the User-Agent header
var Version string = "0.0.1"

// Driver implement the CSI endpoints for Identity, Node and Controller
type Driver struct {
	client.Client
	SocketFilename string
	NodeName       string
	Namespace      string
	cacheDir       string
	TestMode       bool
	mounter        mount.Interface
	grpcServer     *grpc.Server
	log            logr.Logger

	csi.UnimplementedNodeServer
	csi.UnimplementedControllerServer
	csi.UnimplementedIdentityServer
}

// NewDriver returns a CSI driver that implements gRPC endpoints for CSI
func NewDriver(log logr.Logger,
	nodeName, namespace, socketFilename, cacheDir string,
	testMode bool) (*Driver, error) {
	return &Driver{
		NodeName:       nodeName,
		Namespace:      namespace,
		SocketFilename: socketFilename,
		cacheDir:       cacheDir,
		TestMode:       testMode,
		mounter:        mount.New(""),
		grpcServer:     &grpc.Server{},
		log:            log,
	}, nil
}

// NewTestDriver returns a new GKM CSI driver specifically setup to call a fake GKM API
/*
func NewTestDriver(fc *gkmgo.FakeClient) (*Driver, error) {
	d, err := NewDriver("https://gkm-api.example.com", "NO_API_KEY_NEEDED", "TEST1", "default", "12345678")
	d.SocketFilename = "unix:///csi/csi.sock"
	if fc != nil {
		d.GkmClient = fc
	} else {
		d.GkmClient, _ = gkmgo.NewFakeClient()
	}

	d.DiskHotPlugger = &FakeDiskHotPlugger{}
	d.TestMode = true // Just stops so much logging out of failures, as they are often expected during the tests

	zerolog.SetGlobalLevel(zerolog.PanicLevel)

	return d, err
}
*/

// Run the driver's gRPC server
func (d *Driver) Run(ctx context.Context) error {
	d.log = ctrl.Log.WithName("gkm-csi-driver")
	d.log.Info("Parsing the socket filename to make a gRPC server", "socketFilename", d.SocketFilename)
	urlParts, _ := url.Parse(d.SocketFilename)
	d.log.Info("Parsed socket filename")

	grpcAddress := path.Join(urlParts.Host, filepath.FromSlash(urlParts.Path))
	if urlParts.Host == "" {
		grpcAddress = filepath.FromSlash(urlParts.Path)
	}
	d.log.Info("Generated gRPC address", "grpcAddress", grpcAddress)

	// remove any existing left-over socket
	if err := os.Remove(grpcAddress); err != nil && !os.IsNotExist(err) {
		d.log.Error(err, "failed to remove unix domain socket file", "address", grpcAddress)
		return fmt.Errorf("failed to remove unix domain socket file %s, error: %s", grpcAddress, err)
	}
	d.log.Info("Removed any existing old socket")

	grpcListener, err := net.Listen(urlParts.Scheme, grpcAddress)
	if err != nil {
		d.log.Error(err, "failed to listen on unix domain socket file",
			"urlParts.Scheme", urlParts.Scheme,
			"address", grpcAddress)
		return fmt.Errorf("failed to listen: %v", err)
	}
	d.log.Info("Created gRPC listener")

	// log gRPC response errors for better observability
	errHandler := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
	) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			d.log.Info("method failed", "method", info.FullMethod)
		}
		return resp, err
	}

	if d.TestMode {
		d.grpcServer = grpc.NewServer()
	} else {
		d.grpcServer = grpc.NewServer(grpc.UnaryInterceptor(errHandler))
	}
	d.log.Info("Created new RPC server")

	csi.RegisterIdentityServer(d.grpcServer, d)
	d.log.Info("Registered Identity server")
	csi.RegisterControllerServer(d.grpcServer, d)
	d.log.Info("Registered Controller server")
	csi.RegisterNodeServer(d.grpcServer, d)
	d.log.Info("Registered Node server")

	d.log.Info("Starting gRPC server", "grpc_address", grpcAddress)

	var eg errgroup.Group

	eg.Go(func() error {
		go func() {
			<-ctx.Done()
			d.log.Info("Stopping gRPC because the context was cancelled")
			d.grpcServer.GracefulStop()
		}()
		d.log.Info("Awaiting gRPC requests")
		return d.grpcServer.Serve(grpcListener)
	})

	d.log.Info("Running gRPC server, waiting for a signal to quit the process...", "grpc_address", grpcAddress)

	return eg.Wait()
}
