package cache

import (
	"context"
	"log"
	"time"

	"github.com/redhat-et/TKDK/tcv/pkg/accelerator"
	tkmv1alpha1 "github.com/redhat-et/TKM/api/v1alpha1"
	"github.com/redhat-et/TKM/pkg/tkm-agent/node"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Monitor both Cache and CacheCluster CRDs
func MonitorCacheCRDs(clientset *kubernetes.Clientset, accs map[string]accelerator.Accelerator) {
	log.Println("Monitoring cache CRD updates...")

	go monitorCacheCRD(clientset, accs)
	go monitorCacheClusterCRD(clientset, accs)
}

// Monitor Cache CRD updates
func monitorCacheCRD(clientset *kubernetes.Clientset, accs map[string]accelerator.Accelerator) {
	for {
		cacheList := &tkmv1alpha1.TKMCacheList{}
		err := clientset.RESTClient().Get().
			Resource("TKMCaches").
			Namespace("default").
			Do(context.Background()).
			Into(cacheList)

		if err != nil {
			log.Printf("Error fetching TKMCache CRDs: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, cache := range cacheList.Items {
			if isCRDVerified(cache.Status.Conditions) {
				imageName := cache.Spec.Image
				log.Printf("Cache CRD %s verified. Running preflight checks...", cache.Name)
				if err := node.RunPreflightChecks(accs, imageName); err != nil {
					log.Printf("Preflight check failed: %v", err)
				} else {
					log.Println("Preflight check passed.")
				}
			} else {
				log.Printf("Cache CRD %s is not verified yet.", cache.Name)
			}
		}
		time.Sleep(10 * time.Second)
	}
}

// Monitor CacheCluster CRD updates
func monitorCacheClusterCRD(clientset *kubernetes.Clientset, accs map[string]accelerator.Accelerator) {
	for {
		clusterList := &tkmv1alpha1.ClusterTKMCacheList{}
		err := clientset.RESTClient().Get().
			Resource("ClusterTKMCaches").
			Namespace("default").
			Do(context.Background()).
			Into(clusterList)

		if err != nil {
			log.Printf("Error fetching ClusterTKMCache CRDs: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, cluster := range clusterList.Items {
			if isCRDVerified(cluster.Status.Conditions) {
				imageName := cluster.Spec.Image
				log.Printf("CacheCluster CRD %s verified. Running preflight checks...", cluster.Name)
				if err := node.RunPreflightChecks(accs, imageName); err != nil {
					log.Printf("Preflight check failed: %v", err)
				} else {
					log.Println("Preflight check passed.")
				}
			} else {
				log.Printf("CacheCluster CRD %s is not verified yet.", cluster.Name)
			}
		}
		time.Sleep(10 * time.Second)
	}
}

// Helper to check if CRD is verified
func isCRDVerified(conditions []metav1.Condition) bool {
	for _, condition := range conditions {
		if condition.Type == "Verified" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
