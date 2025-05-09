package node

import (
	"context"
	"log"
	"time"

	"github.com/redhat-et/TKDK/tcv/pkg/accelerator"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// CreateNodeCRD creates or updates the Node CRD
func CreateNodeCRD(client *clientset.Clientset, nodeName string, accs map[string]accelerator.Accelerator) error {
	log.Println("Ensuring Node CRD exists...")

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}

	_, err := client.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
	if err != nil {
		log.Printf("Node CRD creation failed: %v", err)
		return err
	}
	log.Printf("Node CRD created: %s", nodeName)
	return nil
}

// UpdateNodeStatus updates the node CRD status with accelerator info
func UpdateNodeStatus(client *clientset.Clientset, nodeName string, accs map[string]accelerator.Accelerator) {
	log.Println("Updating node status with accelerator info...")
	for accType, acc := range accs {
		log.Printf("Node status update - Accelerator: %s, Running: %v", accType, acc.IsRunning())
	}
}

// MonitorNodeStatus continuously monitors the node status and updates the CRD when changes occur
func MonitorNodeStatus(client *clientset.Clientset, nodeName string, accs map[string]accelerator.Accelerator) {
	log.Println("Starting NodeStatus monitoring...")
	for {
		UpdateNodeStatus(client, nodeName, accs)
		select {
		case <-time.After(30 * time.Second):
		case <-context.Background().Done():
			return
		}
	}
}
