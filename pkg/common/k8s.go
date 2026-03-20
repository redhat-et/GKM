package common

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/utils"
)

// Common Predicate function for both GKMCache, GKMCacheNode, GKMCacheNode and ClusterCacheNode. Only reconcile
// if a pod event if it is mounting a PVC and is change phase (state)
func PodPredicate(nodeName string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// At Create, the Node is not known, so skip creates and start
			// processing at Update when the Node is known.
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			logger := log.Log.WithName("pod-predicate")

			oldPod := e.ObjectOld.(*corev1.Pod)
			newPod := e.ObjectNew.(*corev1.Pod)

			oldUsing := hasPVC(oldPod) && isActive(oldPod)
			newUsing := hasPVC(newPod) && isActive(newPod)

			if nodeName != "" && newPod.Spec.NodeName != nodeName {
				logger.V(1).Info("Update: NodeName Skip",
					"Old Phase", oldPod.Status.Phase, "New Phase", newPod.Status.Phase,
					"Old PVC", hasPVC(oldPod), "New PVC", hasPVC(newPod),
					"Old Node", newPod.Spec.NodeName, "New Node", newPod.Spec.NodeName, "Node", nodeName,
				)
				return false
			}

			logger.V(1).Info("Update:",
				"Old Phase", oldPod.Status.Phase, "New Phase", newPod.Status.Phase,
				"Old PVC", hasPVC(oldPod), "New PVC", hasPVC(newPod),
				"Old Node", newPod.Spec.NodeName, "New Node", newPod.Spec.NodeName, "Node", nodeName,
				"rval", oldUsing != newUsing,
			)

			// Only trigger if usage changed
			return oldUsing != newUsing
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			pod := e.Object.(*corev1.Pod)

			// Pod deleted → definitely stopped using PVC
			return pod.Spec.NodeName == nodeName && hasPVC(pod)
		},
	}
}

func hasPVC(pod *corev1.Pod) bool {
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			return true
		}
	}
	return false
}

func isActive(pod *corev1.Pod) bool {
	// Even though Pending could be considered Active, the Pod is created in Pending
	// phase, so was not getting a transition. So mark Pending as inactive then when
	// the phase moves to Running it goes Active.
	switch pod.Status.Phase {
	case corev1.PodSucceeded, corev1.PodFailed, corev1.PodPending:
		return false
	default:
		return true
	}
}

// ManagePvcStatusDelete handles Delete calls. If necessary, it will handle the deletion
// of the Job used to extract GPU Kernel Cache to the PVC, then delete the PVC (and PV if
// it was created) if it is not in use. Deletion of GKMCacheNode or ClusterGKMCacheNode
// will not be held up by the PVC being in use.
func ManagePvcStatusDelete(
	ctx context.Context,
	objClient client.Client,
	gkmCacheNamespace string,
	gkmCacheName string,
	nodeName string,
	pvcStatus *gkmv1alpha1.PvcStatus,
	caller gkmv1alpha1.PvcOwner,
	pvcNamespace string,
	resolvedDigest string,
	log logr.Logger,
) (bool, string, bool, bool, error) {
	updated := false
	updateReason := ""
	pvcInUse := false
	pvcDeleting := false
	var err error

	// Try to Delete Job.
	if updated, updateReason, err = DeleteJob(
		ctx,
		objClient,
		pvcNamespace,
		nodeName,
		resolvedDigest,
		pvcStatus,
		caller,
		log,
	); err != nil {
		log.Info("Error deleting Job",
			"Namespace", gkmCacheNamespace,
			"Name", gkmCacheName,
			"Job Namespace", pvcNamespace,
			"Job Name", pvcStatus.JobName,
			"digest", resolvedDigest,
			"error", err,
		)
	} else if updated {
		return updated, updateReason, pvcInUse, pvcDeleting, err
	}

	// Try to Delete PVC.
	if updated, updateReason, pvcInUse, pvcDeleting, err = DeletePvc(
		ctx,
		objClient,
		gkmCacheName,
		nodeName,
		pvcNamespace,
		resolvedDigest,
		pvcStatus,
		caller,
		log,
	); err != nil {
		log.Info("Error deleting PVC",
			"Namespace", gkmCacheNamespace,
			"Name", gkmCacheName,
			"PVC Namespace", pvcNamespace,
			"PVC Name", pvcStatus.PvcName,
			"digest", resolvedDigest,
			"error", err,
		)
	} else if updated || pvcInUse || pvcDeleting {
		return updated, updateReason, pvcInUse, pvcDeleting, err
	}

	// Try to Delete PV.
	if updated, updateReason, err = DeletePv(
		ctx,
		objClient,
		gkmCacheName,
		nodeName,
		pvcNamespace,
		resolvedDigest,
		pvcStatus,
		caller,
		log,
	); err != nil {
		log.Info("Error deleting PV",
			"Namespace", gkmCacheNamespace,
			"Name", gkmCacheName,
			"PVC Namespace", pvcNamespace,
			"PV Name", pvcStatus.PvName,
			"digest", resolvedDigest,
			"error", err,
		)
	} else if updated {
		return updated, updateReason, pvcInUse, pvcDeleting, err
	}

	return updated, updateReason, pvcInUse, pvcDeleting, err
}

// CreatePv calls KubeAPI Server to create a PersistentVolume
func CreatePv(
	ctx context.Context,
	client client.Client,
	scheme *runtime.Scheme,
	ownerObj metav1.Object,
	gkmCacheNamespace string,
	gkmCacheName string,
	nodeName string,
	pvName string,
	pvcNamespace string,
	accessModes []corev1.PersistentVolumeAccessMode,
	storageClass string,
	capacity string,
	resolvedDigest string,
	log logr.Logger,
) error {
	trimDigest := strings.TrimPrefix(resolvedDigest, utils.DigestPrefix)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
			Labels: map[string]string{
				utils.PvLabelCache:        gkmCacheName,
				utils.PvLabelPvcNamespace: pvcNamespace,
				utils.PvLabelNode:         nodeName,
				utils.PvLabelDigest:       trimDigest[:utils.MaxLabelValueLength],
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(capacity),
			},
			AccessModes:                   accessModes,
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              storageClass,
			VolumeMode: func() *corev1.PersistentVolumeMode {
				m := corev1.PersistentVolumeFilesystem
				return &m
			}(),
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/tmp/gkm",
				},
			},
		},
	}
	if nodeName != "" {
		pv.Spec.NodeAffinity = &corev1.VolumeNodeAffinity{
			Required: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{nodeName},
							},
						},
					},
				},
			},
		}
	}

	if err := client.Create(ctx, pv); err != nil {
		log.Error(err, "Failed to create PV.",
			"namespace", gkmCacheNamespace,
			"name", gkmCacheName,
			"PV", pvName,
			"workload namespace", pvcNamespace,
		)
		return err
	}
	log.Info("Created PV",
		"namespace", gkmCacheNamespace,
		"name", gkmCacheName,
		"PV", pvName,
		"workload namespace", pvcNamespace,
	)
	return nil
}

func PvExists(
	ctx context.Context,
	objClient client.Client,
	gkmCacheName string,
	nodeName string,
	pvName string,
	pvcNamespace string,
	resolvedDigest string,
	log logr.Logger,
) (*corev1.PersistentVolume, bool, string, error) {
	found := false
	updatedName := ""
	var retPv *corev1.PersistentVolume

	if pvName != "" {
		found = true
	} else {
		trimDigest := strings.TrimPrefix(resolvedDigest, utils.DigestPrefix)

		pvList := &corev1.PersistentVolumeList{}
		labelSelector := map[string]string{
			utils.PvLabelCache:        gkmCacheName,
			utils.PvLabelPvcNamespace: pvcNamespace,
			utils.PvLabelNode:         nodeName,
			utils.PvLabelDigest:       trimDigest[:utils.MaxLabelValueLength],
		}
		if err :=
			objClient.List(
				ctx,
				pvList,
				client.MatchingLabels(labelSelector)); err != nil {
			return retPv, found, updatedName, nil
		}

		log.Info("PV List",
			"Name", gkmCacheName,
			"PVC Namespace", pvcNamespace,
			"PV Name", pvName,
			"Node", nodeName,
			"Digest", resolvedDigest,
			"NumPVs", len(pvList.Items),
		)

		if len(pvList.Items) == 1 {
			// Since pvName is not set, but found the PV on read, then our copy of the GKMCache or
			// ClusterGKMCache is outdated. Even if we try to keep going, any KubeAPI writes for them
			// will fail. Mark our copy of cache outdated, signalling an exit from reconcile loop.
			retPv = &(pvList.Items[0])
			updatedName = retPv.Name
			log.Info("Cache outdated, PV found",
				"Name", gkmCacheName,
				"PVC Namespace", pvcNamespace,
				"PV Name", pvList.Items[0].Name,
				"UpdatedName", updatedName,
				"Node", nodeName,
				"Digest", resolvedDigest,
				"NumPVs", len(pvList.Items),
			)
		} else if len(pvList.Items) > 1 {
			for i, pv := range pvList.Items {
				log.Info("Found too many PVs",
					"PV Name", pv.Name,
					"Name", gkmCacheName,
					"PVC Namespace", pvcNamespace,
					"Input PV Name", pvName,
					"Node", nodeName,
					"Digest", resolvedDigest,
					"Inst", i,
				)
			}

			// Error case.
			err := fmt.Errorf("Multiple PVs found")
			return retPv, found, updatedName, err
		}
	}

	return retPv, found, updatedName, nil
}

// DeletePv tries to delete PV created by GKM
func DeletePv(
	ctx context.Context,
	objClient client.Client,
	gkmCacheName string,
	nodeName string,
	pvcNamespace string,
	resolvedDigest string,
	pvcStatus *gkmv1alpha1.PvcStatus,
	caller gkmv1alpha1.PvcOwner,
	log logr.Logger,
) (bool, string, error) {
	updated := false
	updateReason := ""

	log.Info("Deleting PV", "PV Name", pvcStatus.PvName)

	var pv *corev1.PersistentVolume
	if pvcStatus.PvName != "" {
		// Have a local copy of the PV name so get a copy of the PV object.
		pv = &corev1.PersistentVolume{}
		if err := objClient.Get(ctx, types.NamespacedName{
			Name: pvcStatus.PvName,
		}, pv); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("PV already deleted, remove stored PV name",
					"PVC Namespace", pvcNamespace,
					"PV Name", pvcStatus.PvName,
					"digest", resolvedDigest,
				)
				pvcStatus.PvName = ""
				updated = true
				updateReason = "Deleting PV"
				err = nil
			} else {
				log.Info("Error returned getting current PV for delete, continuing",
					"PVC Namespace", pvcNamespace,
					"PV Name", pvcStatus.PvName,
					"digest", resolvedDigest,
					"error", err,
				)
			}
			return updated, updateReason, err
		}
	} else {
		// Since PV Name is not set in PVC Status, make sure it doesn't exist.
		var err error
		if pv, _, _, err = PvExists(
			ctx,
			objClient,
			gkmCacheName,
			nodeName,
			pvcStatus.PvName,
			pvcNamespace,
			resolvedDigest,
			log,
		); err != nil {
			log.Info("Error returned getting latest PV for delete, continuing",
				"PVC Namespace", pvcNamespace,
				"PV Name", pvcStatus.PvName,
				"digest", resolvedDigest,
				"error", err,
			)
			return updated, updateReason, err
		} else if pv == nil {
			log.Info("No latest PV for delete, continuing",
				"PVC Namespace", pvcNamespace,
				"PV Name", pvcStatus.PvName,
				"digest", resolvedDigest,
			)
		}
	}

	// PV was found, so try to delete.
	if pv != nil {
		// If deletion already in progress, do nothing
		if !pv.ObjectMeta.DeletionTimestamp.IsZero() {
			log.Info("PV delete already in progress",
				"PVC Namespace", pvcNamespace,
				"PV Name", pv.Name,
				"digest", resolvedDigest,
			)
		} else {
			if caller == pvcStatus.PvcOwner {
				if err := objClient.Delete(
					ctx,
					pv,
				); err != nil {
					log.Info("Error deleting PV",
						"PVC Namespace", pvcNamespace,
						"PV Name", pv.Name,
						"digest", resolvedDigest,
						"error", err,
					)
					return updated, updateReason, err
				} else {
					log.Info("PV deleted",
						"PVC Namespace", pvcNamespace,
						"PV Name", pv.Name,
						"digest", resolvedDigest,
					)
					pvcStatus.PvName = ""
					updated = true
					updateReason = "Deleting PV"
				}
			} else {
				log.Info("Not owner of PV so skip Delete",
					"PVC Namespace", pvcNamespace,
					"PV Name", pv.Name,
					"digest", resolvedDigest,
				)
			}
		}
	}

	return updated, updateReason, nil
}

// CreatePvc calls KubeAPI Server to create a PersistentVolumeClaim
func CreatePvc(
	ctx context.Context,
	client client.Client,
	scheme *runtime.Scheme,
	ownerObj metav1.Object,
	gkmCacheNamespace string,
	gkmCacheName string,
	nodeName string,
	pvName string,
	pvcName string,
	pvcNamespace string,
	accessModes []corev1.PersistentVolumeAccessMode,
	storageClass string,
	capacity string,
	resolvedDigest string,
	log logr.Logger,
) error {
	trimDigest := strings.TrimPrefix(resolvedDigest, utils.DigestPrefix)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: pvcNamespace,
			Labels: map[string]string{
				utils.PvcLabelCache:        gkmCacheName,
				utils.PvcLabelPvcNamespace: pvcNamespace,
				utils.PvcLabelNode:         nodeName,
				utils.PvcLabelDigest:       trimDigest[:utils.MaxLabelValueLength],
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(capacity),
				},
			},
			StorageClassName: &storageClass,
			VolumeMode: func() *corev1.PersistentVolumeMode {
				m := corev1.PersistentVolumeFilesystem
				return &m
			}(),
		},
	}

	// If PV was manually created, add it to the PVC.
	if pvName != "" {
		pvc.Spec.VolumeName = pvName
	}

	if err := client.Create(ctx, pvc); err != nil {
		log.Error(err, "Failed to create PVC.",
			"namespace", gkmCacheNamespace,
			"name", gkmCacheName,
			"pvcNamespace", pvcNamespace,
			"PV", pvName,
			"PVC", pvcName,
		)
		return err
	}
	log.Info("Created PVC",
		"namespace", gkmCacheNamespace,
		"pvcNamespace", pvcNamespace,
		"name", gkmCacheName,
		"PV", pvName,
		"PVC", pvcName,
	)
	return nil
}

// PvcExists tries to determine if a particular PVC has already been created.
func PvcExists(
	ctx context.Context,
	objClient client.Client,
	gkmCacheName string,
	nodeName string,
	pvcName string,
	pvcNamespace string,
	resolvedDigest string,
	log logr.Logger,
) (*corev1.PersistentVolumeClaim, bool, string, error) {
	found := false
	updatedName := ""
	var retPvc *corev1.PersistentVolumeClaim

	if pvcName != "" {
		found = true
	} else {
		trimDigest := strings.TrimPrefix(resolvedDigest, utils.DigestPrefix)

		pvcList := &corev1.PersistentVolumeClaimList{}
		labelSelector := map[string]string{
			utils.PvcLabelCache:        gkmCacheName,
			utils.PvcLabelPvcNamespace: pvcNamespace,
			utils.PvcLabelNode:         nodeName,
			utils.PvcLabelDigest:       trimDigest[:utils.MaxLabelValueLength],
		}
		if err :=
			objClient.List(
				ctx,
				pvcList,
				client.MatchingLabels(labelSelector)); err != nil {
			return retPvc, found, updatedName, nil
		}

		log.Info("PVC List",
			"Name", gkmCacheName,
			"PVC Namespace", pvcNamespace,
			"PVC Name", pvcName,
			"Node", nodeName,
			"Digest", resolvedDigest,
			"NumPVCs", len(pvcList.Items),
		)

		if len(pvcList.Items) == 1 {
			// Since pvcName is not set, but found the PVC on read, then our copy of the GKMCache or
			// ClusterGKMCache is outdated. Even if we try to keep going, any KubeAPI writes for them
			// will fail. Mark our copy of cache outdated, signalling an exit from reconcile loop.
			retPvc = &(pvcList.Items[0])
			updatedName = retPvc.Name
			log.Info("Cache outdated, PVC found",
				"Name", gkmCacheName,
				"PVC Namespace", pvcNamespace,
				"PVC Name", pvcName,
				"UpdatedName", updatedName,
				"Node", nodeName,
				"Digest", resolvedDigest,
				"NumPVCs", len(pvcList.Items),
			)
		} else if len(pvcList.Items) > 1 {
			for i, pvc := range pvcList.Items {
				log.Info("Found too many PVs",
					"PVC Name", pvc.Name,
					"Name", gkmCacheName,
					"PVC Namespace", pvcNamespace,
					"Input PVC Name", pvcName,
					"Node", nodeName,
					"Digest", resolvedDigest,
					"Inst", i,
				)
			}

			// Error case.
			err := fmt.Errorf("Multiple PVCs found")
			return retPvc, found, updatedName, err
		}
	}

	return retPvc, found, updatedName, nil
}

// GetPvcUsedByList walks the Pods in the Namespace and tries to determine which
// Pods are using the given PVC.
func GetPvcUsedByList(
	ctx context.Context,
	objClient client.Client,
	nodeName string,
	pvcNamespace string,
	pvcName string,
	log logr.Logger,
) int {
	podUseCnt := 0

	if pvcName != "" {
		// List all Pods in the same namespace
		var podList corev1.PodList
		filters := []client.ListOption{client.InNamespace(pvcNamespace)}
		if nodeName != "" {
			filters = append(filters, client.MatchingFields{"spec.nodeName": nodeName})
		}
		if err := objClient.List(ctx, &podList,
			filters...,
		); err != nil {
			log.Info("Unable to retrieve Pod List to check PVC usage",
				"PVC Namespace", pvcNamespace,
				"PVC Name", pvcName,
				"err", err,
			)
			return podUseCnt
		}
		log.Info("Retrieve Pod List to check PVC usage",
			"PVC Namespace", pvcNamespace,
			"PVC Name", pvcName,
			"nodeName", nodeName,
			"NumPods", len(podList.Items),
		)
		for _, pod := range podList.Items {
			for _, vol := range pod.Spec.Volumes {
				if pod.Status.Phase != corev1.PodSucceeded &&
					pod.Status.Phase != corev1.PodFailed {
					if vol.PersistentVolumeClaim != nil &&
						strings.Contains(vol.PersistentVolumeClaim.ClaimName, pvcName) {
						log.V(1).Info("PVC used by Pod",
							"PVC Namespace", pvcNamespace,
							"PVC Name", pvcName,
							"Pod", pod.Name,
							"nodeName", nodeName,
						)
						podUseCnt++
					}
				}
			}
		}
	}

	return podUseCnt
}

// DeletePvc tries to delete a PVC.
func DeletePvc(
	ctx context.Context,
	objClient client.Client,
	gkmCacheName string,
	nodeName string,
	pvcNamespace string,
	resolvedDigest string,
	pvcStatus *gkmv1alpha1.PvcStatus,
	caller gkmv1alpha1.PvcOwner,
	log logr.Logger,
) (bool, string, bool, bool, error) {
	updated := false
	updateReason := ""
	pvcInUse := false
	pvcDeleting := false

	log.Info("Deleting download PVC",
		"pvcName", pvcStatus.PvcName,
		"pvName", pvcStatus.PvName,
		"Owner", pvcStatus.PvcOwner,
		"Caller", caller,
	)

	var pvc *corev1.PersistentVolumeClaim
	if pvcStatus.PvcName != "" {
		// Have a local copy of the PVC name so get a copy of the PVC object.
		pvc = &corev1.PersistentVolumeClaim{}
		if err := objClient.Get(ctx, types.NamespacedName{
			Name:      pvcStatus.PvcName,
			Namespace: pvcNamespace,
		}, pvc); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("PVC already deleted, remove stored PVC name",
					"PVC Namespace", pvcNamespace,
					"PVC Name", pvcStatus.PvcName,
					"digest", resolvedDigest,
				)
				pvcStatus.PvcName = ""
				updated = true
				updateReason = "Deleting PVC"
				err = nil
			} else {
				log.Info("Error returned getting current PVC for delete, continuing",
					"PVC Namespace", pvcNamespace,
					"PVC Name", pvcStatus.PvcName,
					"digest", resolvedDigest,
					"error", err,
				)
			}
			return updated, updateReason, pvcInUse, pvcDeleting, err
		}
	} else {
		// Since PVC Name is not set in PVC Status, make sure it doesn't exist.
		var err error
		if pvc, _, _, err = PvcExists(
			ctx,
			objClient,
			gkmCacheName,
			nodeName,
			pvcStatus.PvcName,
			pvcNamespace,
			resolvedDigest,
			log,
		); err != nil {
			log.Info("Error returned getting latest PVC for delete, continuing",
				"PVC Namespace", pvcNamespace,
				"PVC Name", pvcStatus.PvcName,
				"digest", resolvedDigest,
				"error", err,
			)
			return updated, updateReason, pvcInUse, pvcDeleting, err
		} else if pvc == nil {
			log.Info("No latest PVC for delete, continuing",
				"PVC Namespace", pvcNamespace,
				"PVC Name", pvcStatus.PvcName,
				"digest", resolvedDigest,
			)
		}
	}

	// If a PVC was found, then try to delete it if it is not being used.
	if pvc != nil {
		// Check to see if PVC is in use.
		podInUseCnt := GetPvcUsedByList(
			ctx,
			objClient,
			nodeName,
			pvcNamespace,
			pvc.Name,
			log,
		)

		if podInUseCnt == 0 {
			// If deletion already in progress, do nothing
			if !pvc.ObjectMeta.DeletionTimestamp.IsZero() {
				log.Info("PVC delete already in progress",
					"PVC Namespace", pvcNamespace,
					"PVC Name", pvc.Name,
					"digest", resolvedDigest,
				)
				// Deletion already in progress, set the Deleting flag so code will reexamine
				// PVC in future ReconcileLoop.
				pvcDeleting = true
			} else {
				if caller == pvcStatus.PvcOwner {
					// PVC is not in use, so Delete.
					if err := objClient.Delete(
						ctx,
						pvc,
					); err != nil {
						log.Info("Error deleting PVC",
							"PVC Namespace", pvcNamespace,
							"PVC Name", pvc.Name,
							"digest", resolvedDigest,
							"error", err,
						)
						return updated, updateReason, pvcInUse, pvcDeleting, err
					} else {
						log.Info("PVC deleted",
							"PVC Namespace", pvcNamespace,
							"PVC Name", pvc.Name,
							"digest", resolvedDigest,
						)
						pvcStatus.PvcName = ""
						updated = true
						updateReason = "Deleting PVC"
					}
				} else {
					log.Info("Not owner of PVC so skip Delete",
						"PVC Namespace", pvcNamespace,
						"PVC Name", pvc.Name,
						"digest", resolvedDigest,
					)
				}
			}
		} else {
			pvcInUse = true
			log.Info("PVC still in use, so skipping delete",
				"PVC Namespace", pvcNamespace,
				"PVC Name", pvc.Name,
				"digest", resolvedDigest,
			)
		}
	}

	return updated, updateReason, pvcInUse, pvcDeleting, nil
}

// LaunchJob launches a Kubernetes Job that is responsible for extracting the GPU Kernel
// Cache into a PVC.
func LaunchJob(
	ctx context.Context,
	client client.Client,
	scheme *runtime.Scheme,
	ownerObj metav1.Object,
	jobNamespace string,
	jobName string,
	nodeName string,
	cacheImage string,
	resolvedDigest string,
	noGpu bool,
	extractImage string,
	pvcStatus *gkmv1alpha1.PvcStatus,
	podTemplate *gkmv1alpha1.PodTemplate,
	log logr.Logger,
) error {
	log.Info("Creating download job", "jobName", jobName, "pvcName", pvcStatus.PvcName)

	var jobTTLSecondsAfterFinished int32 = utils.JobTTLSeconds
	var fsGroup int64 = utils.JobFSGroup

	// Make sure Job has not been created yet. We may be working off a stale copy
	// of the GKMCache or ClusterGKMCache. If not found, keep going. If found, return
	// error so code can exit reconcile loop and reenter with updated copy of cache.
	if latestJob, _ := GetLatestJob(
		ctx,
		client,
		jobNamespace,
		pvcStatus.PvcName,
		resolvedDigest,
		nodeName,
		log,
	); latestJob != nil {
		log.Info("job already exists", "Job Name", latestJob.Name)
		return fmt.Errorf("current cache outdated for Job")
	}

	// Replace the tag in the Image URL with the Digest. Webhook has verified
	// the image and so pull from the resolved digest.
	updatedImage := utils.ReplaceUrlTag(cacheImage, resolvedDigest)
	if updatedImage == "" {
		err := fmt.Errorf("unable to update image tag with digest")
		log.Error(err, "invalid image or digest", "image", cacheImage, "digest", resolvedDigest)
		return err
	}

	noGpuString := "false"
	if noGpu {
		noGpuString = "true"
	}

	trimDigest := strings.TrimPrefix(resolvedDigest, utils.DigestPrefix)

	container := &corev1.Container{
		Name:                     utils.JobExtractName,
		Image:                    extractImage,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	container.Env = []corev1.EnvVar{
		{Name: utils.JobExtractEnvCacheDir, Value: utils.MountPath},
		{Name: utils.JobExtractEnvImageUrl, Value: updatedImage},
		{Name: utils.JobExtractEnvNoGpu, Value: noGpuString},
	}

	container.VolumeMounts = []corev1.VolumeMount{
		{
			MountPath: utils.MountPath,
			Name:      utils.JobExtractPvcSourceMountName,
			ReadOnly:  false,
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jobName,
			Namespace:    jobNamespace,
			Labels: map[string]string{
				utils.JobExtractLabelPvc:    pvcStatus.PvcName,
				utils.JobExtractLabelDigest: trimDigest[:utils.MaxLabelValueLength],
				utils.JobExtractLabelNode:   nodeName,
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &jobTTLSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers:    []corev1.Container{*container},
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes: []corev1.Volume{
						{
							Name: utils.JobExtractPvcSourceMountName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcStatus.PvcName,
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:    &fsGroup,
						RunAsUser:  &fsGroup,
						RunAsGroup: &fsGroup,
					},
				},
			},
		},
	}
	// If the NodeName is set, Agent probably is responsible for PVC Extraction,
	// then pin the Job to a Node.
	if nodeName != "" {
		job.Spec.Template.Spec.NodeSelector = map[string]string{
			"kubernetes.io/hostname": nodeName,
		}
	}

	// If any PodTemplate fields have been provided, copy them over to the Job now.
	log.Info("Process PodTemplete", "podTemplate", podTemplate)
	if podTemplate != nil {
		if podTemplate.Metadata != nil {
			if len(podTemplate.Metadata.Labels) != 0 {
				job.Spec.Template.Labels = make(map[string]string)
				for key, value := range podTemplate.Metadata.Labels {
					job.Spec.Template.Labels[key] = value
				}
			}
			if len(podTemplate.Metadata.Annotations) != 0 {
				job.Spec.Template.Annotations = make(map[string]string)
				for key, value := range podTemplate.Metadata.Annotations {
					job.Spec.Template.Annotations[key] = value
				}
			}
		}

		if podTemplate.Spec != nil {
			log.Info("Process PodTemplete.Spec set")
			if len(podTemplate.Spec.NodeSelector) != 0 {
				job.Spec.Template.Spec.NodeSelector = podTemplate.Spec.NodeSelector
				log.Info("NodeSelector set", "NodeSelector", job.Spec.Template.Spec.NodeSelector)
			}
			if len(podTemplate.Spec.Tolerations) != 0 {
				job.Spec.Template.Spec.Tolerations = podTemplate.Spec.Tolerations
				log.Info("Tolerations set", "Tolerations", job.Spec.Template.Spec.Tolerations)
			}
			if podTemplate.Spec.Affinity != nil {
				job.Spec.Template.Spec.Affinity = podTemplate.Spec.Affinity
				log.Info("Affinity set", "Affinity", job.Spec.Template.Spec.Affinity)
			}
			if podTemplate.Spec.PriorityClassName != "" {
				job.Spec.Template.Spec.PriorityClassName = podTemplate.Spec.PriorityClassName
				log.Info("PriorityClassName set", "PriorityClassName", job.Spec.Template.Spec.PriorityClassName)
			}
		}
	}

	// For KIND Clusters, currently identified by NoGpu, Kubelet can't change the ownership
	// of the directory of a Volume Mount. So an InitContainer is added to the job the manage
	// the ownership.
	if noGpu {
		var rootUser int64 = 0

		commandString :=
			"mkdir -p " + utils.MountPath +
				" && chown -R 1000:1000 " + utils.MountPath +
				" && chmod -R 775 " + utils.MountPath

		initContainer := &corev1.Container{
			Name:  "fix-permissions",
			Image: utils.JobInitImage,
			SecurityContext: &corev1.SecurityContext{
				RunAsUser: &rootUser,
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: utils.MountPath,
					Name:      utils.JobExtractPvcSourceMountName,
					ReadOnly:  false,
				},
			},
			Command: []string{"/bin/sh"},
			Args:    []string{"-c", commandString},
		}
		job.Spec.Template.Spec.InitContainers = []corev1.Container{*initContainer}
	}

	if err := controllerutil.SetControllerReference(ownerObj, job, scheme); err != nil {
		log.Error(err, "Failed to set controller reference on job",
			"Job namespace", jobNamespace,
			"Job name", jobName,
		)
		return err
	}

	if err := client.Create(ctx, job); err != nil {
		log.Error(err, "Failed to create job.",
			"Job namespace", jobNamespace,
			"Job name", jobName,
		)
		return err
	}
	log.Info("Created job",
		"Job namespace", jobNamespace,
		"Job name", jobName,
	)
	return nil
}

// GetLatestJob calls KubeAPI Server to retrieve the list of Jobs that match the labels for a
// given Cache and Digest.
func GetLatestJob(
	ctx context.Context,
	objClient client.Client,
	jobNamespace string,
	pvcName string,
	resolvedDigest string,
	nodeName string,
	log logr.Logger,
) (*batchv1.Job, error) {
	trimDigest := strings.TrimPrefix(resolvedDigest, utils.DigestPrefix)

	jobList := &batchv1.JobList{}
	labelSelector := map[string]string{
		utils.JobExtractLabelPvc:    pvcName,
		utils.JobExtractLabelDigest: trimDigest[:utils.MaxLabelValueLength],
		utils.JobExtractLabelNode:   nodeName,
	}
	err := objClient.List(
		ctx,
		jobList,
		client.InNamespace(jobNamespace),
		client.MatchingLabels(labelSelector))
	if err != nil {
		return nil, err
	}

	log.Info("List Jobs",
		"Job Namespace", jobNamespace,
		"PVC Name", pvcName,
		"Digest", resolvedDigest,
		"Node", nodeName,
		"NumJobs", len(jobList.Items),
	)
	var latestJob *batchv1.Job
	if len(jobList.Items) > 0 {
		for i, job := range jobList.Items {
			if latestJob == nil || job.CreationTimestamp.After(latestJob.CreationTimestamp.Time) {
				latestJob = &jobList.Items[i]
			}
		}
	}
	return latestJob, err
}

// DeleteJob launches a Kubernetes Job that is responsible for extracting the GPU Kernel
// Cache into a PVC.
func DeleteJob(
	ctx context.Context,
	objClient client.Client,
	jobNamespace string,
	nodeName string,
	resolvedDigest string,
	pvcStatus *gkmv1alpha1.PvcStatus,
	caller gkmv1alpha1.PvcOwner,
	log logr.Logger,
) (bool, string, error) {
	updated := false
	updateReason := ""

	log.Info("Deleting download job", "jobName", pvcStatus.JobName, "pvcName", pvcStatus.PvcName)

	var job *batchv1.Job
	if pvcStatus.JobName != "" {
		// Have a local copy of the Job name so get a copy of the Job object.
		job = &batchv1.Job{}
		if err := objClient.Get(ctx, types.NamespacedName{
			Name:      pvcStatus.JobName,
			Namespace: jobNamespace,
		}, job); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Job already deleted, remove stored Job name",
					"Job Namespace", jobNamespace,
					"Job Name", pvcStatus.JobName,
					"digest", resolvedDigest,
				)
				pvcStatus.JobName = ""
				updated = true
				updateReason = "Deleting Job"
				err = nil
			} else {
				log.Info("Error returned getting current Job for delete, continuing",
					"Job Namespace", jobNamespace,
					"Job Name", pvcStatus.JobName,
					"digest", resolvedDigest,
					"error", err,
				)
			}
			return updated, updateReason, err
		}
	} else {
		// Since JobName is not set in PVC Status, make sure it doesn't exist.
		var err error
		if job, err = GetLatestJob(
			ctx,
			objClient,
			jobNamespace,
			pvcStatus.PvcName,
			resolvedDigest,
			nodeName,
			log,
		); err != nil {
			log.Info("Error returned getting latest Job for delete, continuing",
				"Job Namespace", jobNamespace,
				"Job Name", pvcStatus.JobName,
				"digest", resolvedDigest,
				"error", err,
			)
			return updated, updateReason, err
		} else if job == nil {
			log.Info("No latest Job for delete, continuing",
				"Job Namespace", jobNamespace,
				"Job Name", pvcStatus.JobName,
				"digest", resolvedDigest,
			)
		}
	}

	// If a Job was found, then delete it.
	if job != nil {
		// If deletion already in progress, do nothing
		if !job.ObjectMeta.DeletionTimestamp.IsZero() {
			log.Info("Job delete already in progress",
				"Job Namespace", jobNamespace,
				"Job Name", job.Name,
				"digest", resolvedDigest,
			)
		} else {
			if caller == pvcStatus.PvcOwner {
				// Indicate to delete associated Job Pods first.
				policy := metav1.DeletePropagationForeground

				if err := objClient.Delete(
					ctx,
					job,
					client.PropagationPolicy(policy),
				); err != nil {
					log.Info("Error deleting Job",
						"Job Namespace", jobNamespace,
						"Job Name", job.Name,
						"digest", resolvedDigest,
						"error", err,
					)
					return updated, updateReason, err
				} else {
					log.Info("Job deleted",
						"Job Namespace", jobNamespace,
						"Job Name", pvcStatus.JobName,
						"digest", resolvedDigest,
					)
					pvcStatus.JobName = ""
					updated = true
					updateReason = "Deleting Job"
				}
			} else {
				log.Info("Not owner of Job so skip Delete",
					"Job Namespace", jobNamespace,
					"Job Name", job.Name,
					"digest", resolvedDigest,
				)
			}
		}
	}

	return updated, updateReason, nil
}
