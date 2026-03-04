package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

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
	trimDigest := strings.TrimPrefix(resolvedDigest, DigestPrefix)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
			Labels: map[string]string{
				PvLabelCache:        gkmCacheName,
				PvLabelPvcNamespace: pvcNamespace,
				PvLabelNode:         nodeName,
				PvLabelDigest:       trimDigest[:MaxLabelValueLength],
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

	// PV are Cluster scoped. If ClusterGKMCache, then Controller Reference can be set. If
	// GKMCache, then no Controller Reference can be set.
	if gkmCacheNamespace == "" {
		if err := controllerutil.SetControllerReference(ownerObj, pv, scheme); err != nil {
			log.Error(err, "Failed to set controller reference on job",
				"namespace", gkmCacheNamespace,
				"name", gkmCacheName,
			)
			return err
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
) (bool, string, error) {
	found := false
	updatedName := ""

	if pvName != "" {
		found = true
	} else {
		trimDigest := strings.TrimPrefix(resolvedDigest, DigestPrefix)

		pvList := &corev1.PersistentVolumeList{}
		labelSelector := map[string]string{
			PvLabelCache:        gkmCacheName,
			PvLabelPvcNamespace: pvcNamespace,
			PvLabelNode:         nodeName,
			PvLabelDigest:       trimDigest[:MaxLabelValueLength],
		}
		if err :=
			objClient.List(
				ctx,
				pvList,
				client.MatchingLabels(labelSelector)); err != nil {
			return found, updatedName, nil
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
			updatedName = pvList.Items[0].Name
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
			return found, updatedName, err
		}
	}

	return found, updatedName, nil
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
	trimDigest := strings.TrimPrefix(resolvedDigest, DigestPrefix)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: pvcNamespace,
			Labels: map[string]string{
				PvcLabelCache:        gkmCacheName,
				PvcLabelPvcNamespace: pvcNamespace,
				PvcLabelNode:         nodeName,
				PvcLabelDigest:       trimDigest[:MaxLabelValueLength],
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

	if err := controllerutil.SetControllerReference(ownerObj, pvc, scheme); err != nil {
		log.Error(err, "Failed to set controller reference on job",
			"namespace", gkmCacheNamespace,
			"name", gkmCacheName,
		)
		return err
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
) (bool, string, error) {
	found := false
	updatedName := ""

	if pvcName != "" {
		found = true
	} else {
		trimDigest := strings.TrimPrefix(resolvedDigest, DigestPrefix)

		pvcList := &corev1.PersistentVolumeClaimList{}
		labelSelector := map[string]string{
			PvcLabelCache:        gkmCacheName,
			PvcLabelPvcNamespace: pvcNamespace,
			PvcLabelNode:         nodeName,
			PvcLabelDigest:       trimDigest[:MaxLabelValueLength],
		}
		if err :=
			objClient.List(
				ctx,
				pvcList,
				client.MatchingLabels(labelSelector)); err != nil {
			return found, updatedName, nil
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
			updatedName = pvcList.Items[0].Name
			log.Info("Cache outdated, PV found",
				"Name", gkmCacheName,
				"PVC Namespace", pvcNamespace,
				"PVC Name", pvcName,
				"UpdatedName", updatedName,
				"Node", nodeName,
				"Digest", resolvedDigest,
				"NumPVs", len(pvcList.Items),
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
			return found, updatedName, err
		}
	}

	return found, updatedName, nil
}

// PvcExists tries to determine if a particular PVC has already been created.
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
		if err := objClient.List(ctx, &podList,
			client.InNamespace(pvcNamespace),
			client.MatchingFields{"spec.nodeName": nodeName},
		); err != nil {
			log.Info("Unable to retrieve Pod List to check PVC usage",
				"PVC Namespace", pvcNamespace,
				"PVC Name", pvcName,
				"err", err,
			)
			return podUseCnt
		}

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
						)
						podUseCnt++
					}
				}
			}
		}
	}

	return podUseCnt
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
	pvcName string,
	noGpu bool,
	extractImage string,
	log logr.Logger,
) error {
	log.Info("Creating download job", "jobName", jobName, "pvcName", pvcName)

	var jobTTLSecondsAfterFinished int32 = JobTTLSeconds
	var fsGroup int64 = JobFSGroup

	// Make sure Job has not been created yet. We may be working off a stale copy
	// of the GKMCache or ClusterGKMCache. If not found, keep going. If found, return
	// error so code can exit reconcile loop and reenter with updated copy of cache.
	if latestJob, _ := GetLatestJob(
		ctx,
		client,
		jobNamespace,
		pvcName,
		resolvedDigest,
		nodeName,
		log,
	); latestJob != nil {
		log.Info("job already exists", "Job Name", latestJob.Name)
		return fmt.Errorf("current cache outdated for Job")
	}

	// Replace the tag in the Image URL with the Digest. Webhook has verified
	// the image and so pull from the resolved digest.
	updatedImage := ReplaceUrlTag(cacheImage, resolvedDigest)
	if updatedImage == "" {
		err := fmt.Errorf("unable to update image tag with digest")
		log.Error(err, "invalid image or digest", "image", cacheImage, "digest", resolvedDigest)
		return err
	}

	noGpuString := "false"
	if noGpu {
		noGpuString = "true"
	}

	trimDigest := strings.TrimPrefix(resolvedDigest, DigestPrefix)

	container := &corev1.Container{
		Name:                     JobExtractName,
		Image:                    extractImage,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	container.Env = []corev1.EnvVar{
		{Name: JobExtractEnvCacheDir, Value: MountPath},
		{Name: JobExtractEnvImageUrl, Value: updatedImage},
		{Name: JobExtractEnvNoGpu, Value: noGpuString},
	}

	container.VolumeMounts = []corev1.VolumeMount{
		{
			MountPath: MountPath,
			Name:      JobExtractPvcSourceMountName,
			ReadOnly:  false,
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jobName,
			Namespace:    jobNamespace,
			Labels: map[string]string{
				JobExtractLabelPvc:    pvcName,
				JobExtractLabelDigest: trimDigest[:MaxLabelValueLength],
				JobExtractLabelNode:   nodeName,
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
							Name: JobExtractPvcSourceMountName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:    &fsGroup,
						RunAsUser:  &fsGroup,
						RunAsGroup: &fsGroup,
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "gpu",
							Operator: corev1.TolerationOpEqual,
							Value:    "true",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
		},
	}
	// If the NodeName is set, Agent probably is responsible for PVC Extraction,
	// then pin the Job to a Node.
	if nodeName != "" {
		job.Spec.Template.Spec.NodeName = nodeName
		job.Spec.Template.Spec.NodeSelector = map[string]string{
			"kubernetes.io/hostname": nodeName,
		}
	}

	// For KIND Clusters, currently identified by NoGpu, Kubelet can't change the ownership
	// of the directory of a Volume Mount. So an InitContainer is added to the job the manage
	// the ownership.
	if noGpu {
		var rootUser int64 = 0

		commandString :=
			"mkdir -p " + MountPath +
				" && chown -R 1000:1000 " + MountPath +
				" && chmod -R 775 " + MountPath

		initContainer := &corev1.Container{
			Name:  "fix-permissions",
			Image: JobInitImage,
			SecurityContext: &corev1.SecurityContext{
				RunAsUser: &rootUser,
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: MountPath,
					Name:      JobExtractPvcSourceMountName,
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
	trimDigest := strings.TrimPrefix(resolvedDigest, DigestPrefix)

	jobList := &batchv1.JobList{}
	labelSelector := map[string]string{
		JobExtractLabelPvc:    pvcName,
		JobExtractLabelDigest: trimDigest[:MaxLabelValueLength],
		JobExtractLabelNode:   nodeName,
	}
	err := objClient.List(
		ctx,
		jobList,
		client.InNamespace(jobNamespace),
		client.MatchingLabels(labelSelector))
	if err != nil {
		return nil, err
	}

	log.Info("Jobs found",
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
	} else {
		err = fmt.Errorf("no jobs found")
	}
	return latestJob, err
}
