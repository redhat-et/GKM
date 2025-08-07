package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redhat-et/GKM/pkg/gkm-csi-plugin/driver"

	"github.com/redhat-et/GKM/pkg/utils"
)

func main() {
	// Process inputs from Environment Variables. These are set in the CSI DaemonSet Yaml by pulling
	// values from the gkm-config ConfigMap Object.

	// Setup logging before anything else so code can log errors.
	logLevel := os.Getenv("GO_LOG")
	log := utils.InitializeLogging(logLevel, "setup", nil)
	log.Info("Logging", "Level", logLevel)

	nodeName := strings.TrimSpace(os.Getenv("KUBE_NODE_NAME"))
	if nodeName == "" {
		nodeName = "local"
	}

	ns := strings.TrimSpace(os.Getenv("GKM_NAMESPACE"))
	if ns == "" {
		ns = "default"
	}

	socketFilename := os.Getenv("CSI_ENDPOINT")
	if socketFilename == "" {
		socketFilename = utils.DefaultSocketFilename
	}

	// Parse command line variables
	var versionInfo = flag.Bool("version", false, "Print the driver version")
	var testMode = flag.Bool("test", false, "Flag to indicate in a Test Mode. Creates a stubbed out Kubelet Server")
	flag.Parse()

	// Process command line variables
	if *versionInfo {
		log.Info("CSI Driver", "Version", driver.Version)
		return
	}

	// Setup CSI Driver, which receives CSI requests from Kubelet
	d, err := driver.NewDriver(log, nodeName, ns, socketFilename, utils.DefaultCacheDir, *testMode)
	if err != nil {
		log.Error(err, "Failed to create new Driver object")
		return
	}
	log.Info("Created a new driver:", "driver", d)

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

	// Listen for termination requests
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log = ctrl.Log.WithName("gkm-csi-main")
	log.Info("Running until SIGINT/SIGTERM received")
	sig := <-c
	log.Info("Received signal:", "sig", sig)
	cancel()
}
