package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redhat-et/GKM/pkg/gkm-csi-plugin/driver"
	"github.com/redhat-et/GKM/pkg/gkm-csi-plugin/image"
	"github.com/redhat-et/GKM/pkg/utils"
)

var versionInfo = flag.Bool("version", false, "Print the driver version")
var testMode = flag.Bool("test", false, "Flag to indicate in a Test Mode. Creates a stubbed out Kubelet Server")
var noGpu = flag.Bool("nogpu", false, "Flag to indicate in a test scenario and GPU is not present")

func main() {
	// Process input data through environment variables
	nodeName := strings.TrimSpace(os.Getenv("KUBE_NODE_NAME"))
	if nodeName == "" {
		nodeName = "local"
	}
	ns := strings.TrimSpace(os.Getenv("GKM_NAMESPACE"))
	if ns == "" {
		ns = "default"
	}
	logLevel := strings.TrimSpace(os.Getenv("GO_LOG"))
	if logLevel == "" {
		logLevel = "info"
	}
	socketFilename := os.Getenv("CSI_ENDPOINT")
	if socketFilename == "" {
		socketFilename = utils.DefaultSocketFilename
	}
	imagePort := os.Getenv("CSI_IMAGE_SERVER_PORT")
	if imagePort == "" {
		imagePort = utils.DefaultImagePort
	}

	// Parse command line variables
	flag.Parse()

	// Setup logging before anything else so code can log errors.
	log := utils.InitializeLogging(logLevel)

	// Process command line variables
	if *versionInfo {
		log.Info("CSI Driver", "Version", driver.Version)
		return
	}

	_, err := exec.LookPath("tcv")
	log.Info("cmdExists", "cmd", utils.TcvBinary, "err", err)

	// Setup CSI Driver, which receives CSI requests from Kubelet
	d, err := driver.NewDriver(log, nodeName, ns, socketFilename, utils.DefaultCacheDir, *testMode)
	if err != nil {
		log.Error(err, "Failed to create new Driver object")
		return
	}
	log.Info("Created a new driver:", "driver", d)

	// Setup Image Server, which receives OCI Image management requests from GKM
	s, err := image.NewImageServer(nodeName, ns, imagePort, utils.DefaultCacheDir, *noGpu)
	if err != nil {
		log.Error(err, "Failed to create new Image Server object")
		return
	}
	log.Info("Created a new Image Server:", "image", s)

	// Create the context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run the CSI Driver
	if !(*testMode) {
		go func() {
			log.Info("Running the CSI Driver")

			if err := d.Run(ctx); err != nil {
				log.Error(err, "Driver run failure")
				return
			}
		}()
	}

	// Run the Image Server
	go func() {
		log.Info("Running the Image Server")

		if err := s.Run(ctx); err != nil {
			log.Error(err, "Image Server run failure")
			return
		}
	}()

	// Listen for termination requests
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log = ctrl.Log.WithName("gkm-csi-main")
	log.Info("Running until SIGINT/SIGTERM received")
	sig := <-c
	log.Info("Received signal:", "sig", sig)
	cancel()
}
