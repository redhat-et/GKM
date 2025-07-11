package agent

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/redhat-et/GKM/pkg/gkm-agent/cache"
	"github.com/redhat-et/GKM/pkg/gkm-agent/node"
	"github.com/redhat-et/TKDK/tcv/pkg/accelerator"
	logging "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Start initializes the agent and starts the monitoring process
func Start(ctx context.Context) error {
	log.Println("Initializing accelerator registry...")

	// Initialize the accelerator registry
	registry := accelerator.GetRegistry()

	// Create the accelerator using the TCV function
	acc, err := accelerator.New("gpu", true) // Assuming "gpu" as the type, change if needed
	if err != nil {
		logging.Errorf("failed to init GPU accelerators: %v", err)
	} else {
		registry.MustRegister(acc) // Register the accelerator with the registry
	}

	// Get the list of registered accelerators
	accs := accelerator.GetAccelerators()
	if len(accs) == 0 {
		log.Println("No accelerators found on this node.")
		return nil
	}

	for accType, acc := range accs {
		log.Printf("Found accelerator: %s - Running: %v", accType, acc.IsRunning())
	}

	// Get Kubernetes clientset
	clientset, err := getClientset()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
		return err
	}

	// Get node name from environment variable (set by Kubernetes)
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Println("NODE_NAME environment variable is not set. Defaulting to 'localhost'")
		nodeName = "localhost"
	}

	// Update node status with accelerator info
	node.UpdateNodeStatus(clientset, nodeName, accs)

	// Start monitoring CRD updates
	go cache.MonitorCacheCRDs(clientset, accs)

	log.Println("Agent started successfully.")

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		log.Println("gkm-agent stopped.")
		return nil
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down...", sig)
		return nil
	}
}

// getClientset initializes a Kubernetes clientset
func getClientset() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}
