package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/utils"
)

const (
	TargetLabelValue = "true"

	VolumeNameToMutate = "mydata" // optional filter by volume name
)

var (
	podWebhookLog = logf.Log.WithName("webhook-pod")
)

type PodMutator struct {
	client.Client
	Scheme  *runtime.Scheme
	Decoder admission.Decoder
}

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=Fail,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Only mutate on CREATE
	if req.Operation != admissionv1.Create {
		return admission.Allowed("Skipping non-create operation")
	}

	pod := &corev1.Pod{}
	if err := m.Decoder.Decode(req, pod); err != nil {
		podWebhookLog.Error(err, "Error Decoding Pod object", "req", req)
		return admission.Errored(http.StatusBadRequest, err)
	}
	podName := pod.Name
	if podName == "" {
		podName = pod.GenerateName
	}

	// Filter by label
	if pod.Labels[utils.GKMCachePvcMutation] != TargetLabelValue {
		podWebhookLog.V(1).Info("Label on Pod Not Found",
			"PodName", podName,
			"Namespace", pod.Namespace,
			"Label", utils.GKMCachePvcMutation,
		)
		return admission.Allowed("pod does not match target label")
	}

	// Need to know the Node
	nodeName := m.getNodeName(pod)
	if nodeName == "" {
		err := fmt.Errorf("Node not known")
		podWebhookLog.Error(err, "Node not known", "PodName", podName, "Namespace", pod.Namespace)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Track if we mutate
	mutated := false

	// Iterate volumes
	for i, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim == nil {
			continue
		}

		podWebhookLog.Info("Examining Vol",
			"PodName", podName,
			"Namespace", pod.Namespace,
			"Vol Name", vol.Name,
			"Claim Name", vol.PersistentVolumeClaim.ClaimName,
		)

		pvcName, err := m.getPvcName(ctx, podName, pod.Namespace, vol.PersistentVolumeClaim.ClaimName, nodeName)
		if err == nil {
			pod.Spec.Volumes[i].PersistentVolumeClaim.ClaimName = pvcName
			mutated = true
		}
	}

	if !mutated {
		//return admission.Allowed("no pvc volumes mutated")
		podWebhookLog.Info("Denied: Dependency not ready yet",
			"PodName", podName,
			"Namespace", pod.Namespace,
		)
		return admission.Denied("dependency not ready yet")
	}

	mutatedBytes, err := json.Marshal(pod)
	if err != nil {
		podWebhookLog.Info("Error: Marshal error", "err", err)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// Return patch response
	return admission.PatchResponseFromRaw(req.Object.Raw, mutatedBytes)
}

func (m *PodMutator) getNodeName(pod *corev1.Pod) string {
	if pod.Spec.Affinity == nil ||
		pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return ""
	}

	r := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	for _, t := range r.NodeSelectorTerms {
		for _, mf := range t.MatchFields {
			if mf.Key != "metadata.name" {
				continue
			}

			if mf.Operator != corev1.NodeSelectorOpIn {
				continue
			}

			if len(mf.Values) == 0 {
				continue
			}

			// DaemonSet controller sets exactly one node value
			return mf.Values[0]
		}
	}

	return ""
}

func (m *PodMutator) getPvcName(ctx context.Context, podName, namespace, cacheName, nodeName string) (string, error) {
	// Look for GKMCache first (namespace based). If not found, then look for ClusterGKMCache.
	gkmCacheNodeList := &gkmv1alpha1.GKMCacheNodeList{}
	labelSelector := map[string]string{
		utils.GKMCacheLabelHostname:  nodeName,
		utils.GKMCacheNodeLabelCache: cacheName,
	}
	err := m.Client.List(
		ctx,
		gkmCacheNodeList,
		client.InNamespace(namespace),
		client.MatchingLabels(labelSelector),
	)
	if err == nil {
		if gkmCacheNodeList.GetItemsLen() == 1 {
			gkmCacheNode := gkmCacheNodeList.Items[0]

			podWebhookLog.Info("GKMCacheNode Found",
				"PodName", podName,
				"Namespace", namespace,
				"CacheName", cacheName,
				"CacheNodeName", gkmCacheNode.GetName(),
				"Node", nodeName,
			)

			resolvedDigest := ""
			var cacheStatus gkmv1alpha1.CacheStatus
			for tmpCacheName, cacheList := range gkmCacheNode.Status.CacheStatuses {
				if tmpCacheName == cacheName {
					for digest, tmpCacheStatus := range cacheList {
						// Use the first Digest found. Structure is setup to support multiple digests,
						// but there is no way at the moment to add additional digests.
						// TODO: resolvedDigest should be at a high level in the structure and the GKMCacheNode
						// only supports one GKMCache.
						resolvedDigest = digest
						cacheStatus = tmpCacheStatus
						break
					}
				}
			}

			if resolvedDigest == "" {
				podWebhookLog.Info("ResolvedDigest NOT set",
					"PodName", podName,
					"Namespace", namespace,
					"CacheName", cacheName,
					"CacheNodeName", gkmCacheNode.GetName(),
					"Node", nodeName,
				)
				err = fmt.Errorf("ResolvedDigest not set yet")
				return "", err
			}

			podWebhookLog.Info("ResolvedDigest Found",
				"PodName", podName,
				"Namespace", namespace,
				"CacheName", cacheName,
				"CacheNodeName", gkmCacheNode.GetName(),
				"Node", nodeName,
				"ResolvedDigest", resolvedDigest,
			)

			pvcStatus, pvcStatusExisted := cacheStatus.PvcStatus[namespace]
			if !pvcStatusExisted {
				podWebhookLog.Info("Can't get PVC Status",
					"PodName", podName,
					"Namespace", namespace,
					"CacheName", cacheName,
					"CacheNodeName", gkmCacheNode.GetName(),
					"Node", nodeName,
					"ResolvedDigest", resolvedDigest,
				)
				err = fmt.Errorf("PVC Status not set yet")
				return "", err
			}

			if pvcStatus.PvcName == "" {
				podWebhookLog.Info("PVC Name not set yet",
					"PodName", podName,
					"Namespace", namespace,
					"CacheName", cacheName,
					"CacheNodeName", gkmCacheNode.GetName(),
					"Node", nodeName,
					"ResolvedDigest", resolvedDigest,
				)
				err = fmt.Errorf("PVC Name not set yet")
				return "", err
			}

			return pvcStatus.PvcName, nil
		} else {
			if len(gkmCacheNodeList.GetItems()) == 0 {
				podWebhookLog.Info("List returned NO GKMCache ",
					"PodName", podName,
					"Namespace", namespace,
					"CacheName", cacheName,
					"Node", nodeName,
					"err", err,
				)
			} else {
				for _, gkmCacheNode := range gkmCacheNodeList.GetItems() {
					podWebhookLog.Info("ERROR: More than one GKMCacheNode Found",
						"PodName", podName,
						"Namespace", namespace,
						"CacheName", cacheName,
						"CacheNodeName", gkmCacheNode.GetName(),
						"Node", nodeName,
					)
				}
			}
		}
	} else {
		podWebhookLog.Info("Error reading GKMCacheNode List",
			"PodName", podName,
			"Namespace", namespace,
			"CacheName", cacheName,
			"Node", nodeName,
			"Error", err,
		)
	}

	// Look for ClusterGKMCache
	clusterGkmCacheNodeList := &gkmv1alpha1.ClusterGKMCacheNodeList{}
	clusterLabelSelector := map[string]string{
		utils.GKMCacheLabelHostname:         nodeName,
		utils.GKMClusterCacheNodeLabelCache: cacheName,
	}
	err = m.Client.List(
		ctx,
		clusterGkmCacheNodeList,
		client.MatchingLabels(clusterLabelSelector),
	)
	if err == nil {
		if clusterGkmCacheNodeList.GetItemsLen() == 1 {
			clusterGkmCacheNode := clusterGkmCacheNodeList.Items[0]
			podWebhookLog.Info("ClusterGKMCacheNode Found",
				"PodName", podName,
				"Namespace", namespace,
				"ClusterCacheName", cacheName,
				"ClusterCacheNodeName", clusterGkmCacheNode.GetName(),
				"Node", nodeName,
			)

			resolvedDigest := ""
			var cacheStatus gkmv1alpha1.CacheStatus
			for tmpCacheName, cacheList := range clusterGkmCacheNode.Status.CacheStatuses {
				if tmpCacheName == cacheName {
					for digest, tmpCacheStatus := range cacheList {
						resolvedDigest = digest
						cacheStatus = tmpCacheStatus
					}
				}
			}

			if resolvedDigest == "" {
				podWebhookLog.Info("ResolvedDigest NOT set",
					"PodName", podName,
					"Namespace", namespace,
					"ClusterCacheName", cacheName,
					"ClusterCacheNodeName", clusterGkmCacheNode.GetName(),
					"Node", nodeName,
				)
				err = fmt.Errorf("ResolvedDigest not set yet")
				return "", err
			}

			podWebhookLog.Info("ResolvedDigest Found",
				"PodName", podName,
				"Namespace", namespace,
				"ClusterCacheName", cacheName,
				"ClusterCacheNodeName", clusterGkmCacheNode.GetName(),
				"Node", nodeName,
				"ResolvedDigest", resolvedDigest,
			)

			pvcStatus, pvcStatusExisted := cacheStatus.PvcStatus[namespace]
			if !pvcStatusExisted {
				podWebhookLog.Info("Can't get PVC Status",
					"PodName", podName,
					"Namespace", namespace,
					"ClusterCacheName", cacheName,
					"ClusterCacheNodeName", clusterGkmCacheNode.GetName(),
					"Node", nodeName,
					"ResolvedDigest", resolvedDigest,
				)
				err = fmt.Errorf("PVC Status not set yet")
				return "", err
			}

			if pvcStatus.PvcName == "" {
				podWebhookLog.Info("PVC Name not set yet",
					"PodName", podName,
					"Namespace", namespace,
					"ClusterCacheName", cacheName,
					"ClusterCacheNodeName", clusterGkmCacheNode.GetName(),
					"Node", nodeName,
					"ResolvedDigest", resolvedDigest,
				)
				err = fmt.Errorf("PVC Name not set yet")
				return "", err
			}

			return pvcStatus.PvcName, nil
		} else {
			if len(gkmCacheNodeList.GetItems()) == 0 {
				podWebhookLog.Info("List returned NO ClusterGKMCache ",
					"PodName", podName,
					"Namespace", namespace,
					"CacheName", cacheName,
					"Node", nodeName,
					"err", err,
				)
			} else {
				for _, gkmCacheNode := range gkmCacheNodeList.GetItems() {
					podWebhookLog.Info("ERROR: More than one ClusterGKMCacheNode Found",
						"PodName", podName,
						"Namespace", namespace,
						"CacheName", cacheName,
						"CacheNodeName", gkmCacheNode.GetName(),
						"Node", nodeName,
					)
				}
			}
		}
	} else {
		podWebhookLog.Info("Error reading ClusterGKMCacheNode List",
			"PodName", podName,
			"Namespace", namespace,
			"ClusterCacheName", cacheName,
			"Node", nodeName,
			"Error", err,
		)
	}
	err = fmt.Errorf("unable to determine PVC")
	return "", err
}
