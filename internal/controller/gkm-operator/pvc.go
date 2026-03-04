/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gkmOperator

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
	"github.com/redhat-et/GKM/pkg/utils"
)

// GeneratePVCName creates a PVC name from cache namespace and name.
// For namespace-scoped: gkm-cache-{namespace}-{name}
// For cluster-scoped: gkm-clustercache-{name}
func GeneratePVCName(namespace, name string) string {
	if namespace == "" {
		// Cluster-scoped
		return fmt.Sprintf("%s-%s", utils.PVCClusterNamePrefix, name)
	}
	// Namespace-scoped
	return fmt.Sprintf("%s-%s-%s", utils.PVCNamePrefix, namespace, name)
}

// GetPVCSize returns the PVC size from the cache spec, or default if not specified.
func GetPVCSize(cache GKMInstance) string {
	// Access the spec through the client.Object interface
	obj := cache.GetClientObject()

	// Type assert to get the actual cache object with spec
	if gkmCache, ok := obj.(*gkmv1alpha1.GKMCache); ok {
		if gkmCache.Spec.Storage != nil && gkmCache.Spec.Storage.Size != "" {
			return gkmCache.Spec.Storage.Size
		}
	} else if clusterCache, ok := obj.(*gkmv1alpha1.ClusterGKMCache); ok {
		if clusterCache.Spec.Storage != nil && clusterCache.Spec.Storage.Size != "" {
			return clusterCache.Spec.Storage.Size
		}
	}

	return utils.DefaultPVCSize
}

// GetStorageClassName returns the storage class name from the cache spec, or nil for default.
func GetStorageClassName(cache GKMInstance) *string {
	obj := cache.GetClientObject()

	if gkmCache, ok := obj.(*gkmv1alpha1.GKMCache); ok {
		if gkmCache.Spec.Storage != nil {
			return gkmCache.Spec.Storage.StorageClassName
		}
	} else if clusterCache, ok := obj.(*gkmv1alpha1.ClusterGKMCache); ok {
		if clusterCache.Spec.Storage != nil {
			return clusterCache.Spec.Storage.StorageClassName
		}
	}

	return nil
}

// GetAccessMode returns the access mode from the cache spec, or default.
func GetAccessMode(cache GKMInstance) corev1.PersistentVolumeAccessMode {
	obj := cache.GetClientObject()

	if gkmCache, ok := obj.(*gkmv1alpha1.GKMCache); ok {
		if gkmCache.Spec.Storage != nil && gkmCache.Spec.Storage.AccessMode != "" {
			return corev1.PersistentVolumeAccessMode(gkmCache.Spec.Storage.AccessMode)
		}
	} else if clusterCache, ok := obj.(*gkmv1alpha1.ClusterGKMCache); ok {
		if clusterCache.Spec.Storage != nil && clusterCache.Spec.Storage.AccessMode != "" {
			return corev1.PersistentVolumeAccessMode(clusterCache.Spec.Storage.AccessMode)
		}
	}

	return corev1.ReadWriteOnce
}

// CreatePVC creates a PersistentVolumeClaim for the given cache.
func CreatePVC(
	ctx context.Context,
	k8sClient client.Client,
	cache GKMInstance,
	resolvedDigest string,
) (*corev1.PersistentVolumeClaim, error) {
	pvcName := GeneratePVCName(cache.GetNamespace(), cache.GetName())
	size := GetPVCSize(cache)
	storageClassName := GetStorageClassName(cache)
	accessMode := GetAccessMode(cache)

	// Parse the size string
	quantity, err := resource.ParseQuantity(size)
	if err != nil {
		return nil, fmt.Errorf("invalid storage size %s: %w", size, err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cache.GetNamespace(),
			Labels: map[string]string{
				utils.GKMPVCLabelApp:       "gkm",
				utils.GKMPVCLabelComponent: "cache-storage",
				utils.GKMPVCLabelCacheName: cache.GetName(),
			},
			Annotations: map[string]string{
				utils.GKMPVCAnnotationCacheImage:       cache.GetImage(),
				utils.GKMPVCAnnotationExtractionStatus: utils.PVCExtractionStatusPending,
				utils.GKMPVCAnnotationCacheDigest:      resolvedDigest,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{accessMode},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: quantity,
				},
			},
			StorageClassName: storageClassName,
		},
	}

	// Set the cache as the owner of the PVC (for garbage collection)
	if err := controllerutil.SetControllerReference(cache.GetClientObject(), pvc, k8sClient.Scheme()); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the PVC
	if err := k8sClient.Create(ctx, pvc); err != nil {
		return nil, fmt.Errorf("failed to create PVC %s: %w", pvcName, err)
	}

	return pvc, nil
}

// GetPVC retrieves the PVC for the given cache, returns nil if not found.
func GetPVC(
	ctx context.Context,
	k8sClient client.Client,
	cache GKMInstance,
) (*corev1.PersistentVolumeClaim, error) {
	pvcName := GeneratePVCName(cache.GetNamespace(), cache.GetName())
	pvc := &corev1.PersistentVolumeClaim{}

	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      pvcName,
		Namespace: cache.GetNamespace(),
	}, pvc)

	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return pvc, nil
}

// UpdatePVCAnnotation updates a specific annotation on the PVC.
func UpdatePVCAnnotation(
	ctx context.Context,
	k8sClient client.Client,
	pvc *corev1.PersistentVolumeClaim,
	key, value string,
) error {
	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}
	pvc.Annotations[key] = value

	return k8sClient.Update(ctx, pvc)
}

// DeletePVC deletes the PVC for the given cache.
func DeletePVC(
	ctx context.Context,
	k8sClient client.Client,
	cache GKMInstance,
) error {
	pvcName := GeneratePVCName(cache.GetNamespace(), cache.GetName())
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: cache.GetNamespace(),
		},
	}

	err := k8sClient.Delete(ctx, pvc)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete PVC %s: %w", pvcName, err)
	}

	return nil
}

// IsPVCReady checks if the PVC is bound and ready.
func IsPVCReady(pvc *corev1.PersistentVolumeClaim) bool {
	return pvc != nil && pvc.Status.Phase == corev1.ClaimBound
}

// GetPVCActualSize returns the actual allocated size from PVC status.
func GetPVCActualSize(pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil {
		return ""
	}
	if capacity, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
		return capacity.String()
	}
	return ""
}

// GetExtractionStatus returns the extraction status from PVC annotations.
func GetExtractionStatus(pvc *corev1.PersistentVolumeClaim) string {
	if pvc == nil || pvc.Annotations == nil {
		return ""
	}
	return pvc.Annotations[utils.GKMPVCAnnotationExtractionStatus]
}

// SetExtractionStatus updates the extraction status annotation on the PVC.
func SetExtractionStatus(
	ctx context.Context,
	k8sClient client.Client,
	pvc *corev1.PersistentVolumeClaim,
	status string,
) error {
	return UpdatePVCAnnotation(ctx, k8sClient, pvc, utils.GKMPVCAnnotationExtractionStatus, status)
}

// ValidatePVCSize ensures the PVC size is valid and can be parsed.
func ValidatePVCSize(size string) error {
	if size == "" {
		return nil // Will use default
	}

	// Try to parse as a Quantity
	_, err := resource.ParseQuantity(size)
	if err != nil {
		return fmt.Errorf("invalid storage size %s: must be a valid Kubernetes quantity (e.g., 10Gi, 1Ti)", size)
	}

	return nil
}

// NormalizeCacheName converts a cache name to a valid PVC name component.
// Kubernetes PVC names must be DNS-1123 compliant (lowercase alphanumeric, -, .)
func NormalizeCacheName(name string) string {
	// Convert to lowercase
	normalized := strings.ToLower(name)

	// Replace invalid characters with hyphens
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")

	// Ensure it doesn't start or end with a hyphen
	normalized = strings.Trim(normalized, "-")

	return normalized
}
