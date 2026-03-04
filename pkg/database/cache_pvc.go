package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	mcvClient "github.com/redhat-et/MCU/mcv/pkg/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redhat-et/GKM/pkg/utils"
)

// ExtractCacheToPVC extracts the GPU Kernel Cache from an OCI image to a PVC-mounted directory.
// This is the PVC-based replacement for ExtractCache().
//
// The extraction process:
// 1. Find the PVC for this cache
// 2. Check if PVC is mounted and accessible
// 3. Extract cache using MCV to the PVC mount point
// 4. Update PVC annotation with extraction status
//
// Parameters:
//   - ctx: Context for Kubernetes API calls
//   - k8sClient: Kubernetes client for PVC operations
//   - crNamespace: Namespace of the GKMCache (empty for ClusterGKMCache)
//   - crName: Name of the GKMCache/ClusterGKMCache
//   - image: OCI image URL
//   - digest: Resolved digest of the image
//   - pvcMountPath: Where the PVC is mounted in this pod (e.g., /mnt/gkm-cache)
//   - noGpu: Whether to skip GPU detection
//   - log: Logger instance
//
// Returns:
//   - matchedIds: List of GPU IDs that match the cache
//   - unmatchedIds: List of GPU IDs that don't match the cache
//   - error: Any error encountered during extraction
func ExtractCacheToPVC(
	ctx context.Context,
	k8sClient client.Client,
	crNamespace, crName, image, digest string,
	pvcMountPath string,
	noGpu bool,
	log logr.Logger,
) (matchedIDs, unmatchedIDs []int, err error) {
	// Replace the tag in the Image URL with the Digest
	updatedImage := replaceUrlTag(image, digest)
	if updatedImage == "" {
		err := fmt.Errorf("unable to update image tag with digest")
		log.Error(err, "invalid image or digest", "image", image, "digest", digest)
		return nil, nil, err
	}

	// Get the PVC for this cache
	pvc, err := GetPVCForCache(ctx, k8sClient, crNamespace, crName)
	if err != nil {
		log.Error(err, "failed to get PVC for cache",
			"namespace", crNamespace,
			"name", crName)
		return nil, nil, err
	}

	if pvc == nil {
		err := fmt.Errorf("PVC not found for cache")
		log.Error(err, "PVC should be created by operator before extraction",
			"namespace", crNamespace,
			"name", crName)
		return nil, nil, err
	}

	// Check if PVC is bound
	if pvc.Status.Phase != corev1.ClaimBound {
		err := fmt.Errorf("PVC is not bound, current phase: %s", pvc.Status.Phase)
		log.Error(err, "PVC must be bound before extraction",
			"namespace", crNamespace,
			"name", crName,
			"pvcName", pvc.Name)
		return nil, nil, err
	}

	// Verify the PVC mount path exists and is accessible
	if _, err := os.Stat(pvcMountPath); os.IsNotExist(err) {
		err := fmt.Errorf("PVC mount path does not exist: %s", pvcMountPath)
		log.Error(err, "PVC should be mounted before calling extraction",
			"namespace", crNamespace,
			"name", crName,
			"pvcMountPath", pvcMountPath)
		return nil, nil, err
	}

	// Build the cache directory path within the PVC
	// Structure: /mnt/gkm-cache/{digest}/
	cacheDir := filepath.Join(pvcMountPath, digest)

	log.Info("Extracting cache to PVC",
		"namespace", crNamespace,
		"name", crName,
		"image", updatedImage,
		"digest", digest,
		"cacheDir", cacheDir,
		"pvcName", pvc.Name)

	// Update PVC annotation to indicate extraction is in progress
	if err := UpdatePVCAnnotation(ctx, k8sClient, pvc, utils.GKMPVCAnnotationExtractionStatus, utils.PVCExtractionStatusExtracting); err != nil {
		log.Error(err, "failed to update PVC extraction status to extracting, continuing anyway",
			"namespace", crNamespace,
			"name", crName,
			"pvcName", pvc.Name)
		// Don't fail extraction if annotation update fails
	}

	// For testing, like in a KIND Cluster, a real GPU may not be available.
	enableGPU := !noGpu
	matchedIds, unmatchedIds, err := mcvClient.ExtractCache(mcvClient.Options{
		ImageName: updatedImage,
		CacheDir:  cacheDir,
		EnableGPU: &enableGPU,
		LogLevel:  "info",
	})
	if err != nil {
		log.Error(err, "unable to extract cache", "namespace", crNamespace, "name", crName, "image", updatedImage)

		// Update PVC annotation to indicate extraction failed
		if updateErr := UpdatePVCAnnotation(ctx, k8sClient, pvc, utils.GKMPVCAnnotationExtractionStatus, utils.PVCExtractionStatusFailed); updateErr != nil {
			log.Error(updateErr, "failed to update PVC extraction status to failed",
				"namespace", crNamespace,
				"name", crName,
				"pvcName", pvc.Name)
		}

		// Cleanup created directories on failure
		if removeErr := os.RemoveAll(cacheDir); removeErr != nil {
			log.Error(removeErr, "failed to cleanup cache directory after extraction failure",
				"cacheDir", cacheDir)
		}

		return nil, nil, err
	}

	log.Info("Cache Extracted to PVC", "matchedIds", matchedIds, "unmatchedIds", unmatchedIds, "pvcName", pvc.Name)

	// Update PVC annotation to indicate extraction completed successfully
	if err := UpdatePVCAnnotation(ctx, k8sClient, pvc, utils.GKMPVCAnnotationExtractionStatus, utils.PVCExtractionStatusCompleted); err != nil {
		log.Error(err, "failed to update PVC extraction status to completed, cache is extracted but status not updated",
			"namespace", crNamespace,
			"name", crName,
			"pvcName", pvc.Name)
		// Don't fail extraction if annotation update fails - cache is already extracted
	}

	return matchedIds, unmatchedIds, nil
}

// GetPVCForCache retrieves the PVC for a given cache.
// This mirrors the PVC name generation logic from the operator.
func GetPVCForCache(ctx context.Context, k8sClient client.Client, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	pvcName := GeneratePVCNameForCache(namespace, name)
	pvc := &corev1.PersistentVolumeClaim{}

	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      pvcName,
		Namespace: namespace,
	}, pvc)

	if err != nil {
		return nil, err
	}

	return pvc, nil
}

// GeneratePVCNameForCache generates the PVC name using the same logic as the operator.
func GeneratePVCNameForCache(namespace, name string) string {
	if namespace == "" {
		// Cluster-scoped
		return fmt.Sprintf("%s-%s", utils.PVCClusterNamePrefix, name)
	}
	// Namespace-scoped
	return fmt.Sprintf("%s-%s-%s", utils.PVCNamePrefix, namespace, name)
}

// UpdatePVCAnnotation updates a specific annotation on the PVC.
func UpdatePVCAnnotation(ctx context.Context, k8sClient client.Client, pvc *corev1.PersistentVolumeClaim, key, value string) error {
	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}
	pvc.Annotations[key] = value

	return k8sClient.Update(ctx, pvc)
}

// GetCacheDirectoryFromPVC returns the path to the extracted cache within the PVC.
// Structure: {pvcMountPath}/{digest}/
func GetCacheDirectoryFromPVC(pvcMountPath, digest string) string {
	return filepath.Join(pvcMountPath, digest)
}

// IsCacheExtractedInPVC checks if a cache has been extracted to the PVC by checking
// if the directory exists and contains files.
func IsCacheExtractedInPVC(pvcMountPath, digest string) bool {
	cacheDir := GetCacheDirectoryFromPVC(pvcMountPath, digest)

	// Check if directory exists
	info, err := os.Stat(cacheDir)
	if err != nil {
		return false
	}

	if !info.IsDir() {
		return false
	}

	// Check if directory is not empty
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return false
	}

	return len(entries) > 0
}

// GetCacheSizeInPVC calculates the size of the extracted cache in the PVC.
func GetCacheSizeInPVC(pvcMountPath, digest string) (int64, error) {
	cacheDir := GetCacheDirectoryFromPVC(pvcMountPath, digest)
	return DirSize(cacheDir)
}

// RemoveCacheFromPVC removes the extracted cache from the PVC.
// This is called when a GKMCache is deleted.
func RemoveCacheFromPVC(pvcMountPath, digest string, log logr.Logger) error {
	cacheDir := GetCacheDirectoryFromPVC(pvcMountPath, digest)

	log.V(1).Info("Removing cache from PVC", "cacheDir", cacheDir, "digest", digest)

	if err := os.RemoveAll(cacheDir); err != nil {
		log.Error(err, "failed to remove cache directory from PVC",
			"cacheDir", cacheDir,
			"digest", digest)
		return fmt.Errorf("failed to remove cache directory %s: %w", cacheDir, err)
	}

	return nil
}

// GetExtractedCacheListFromPVC reads the PVC mount points and returns a map of extracted caches.
// This is the PVC-based replacement for GetExtractedCacheList() which scans host filesystem.
//
// Parameters:
//   - pvcBasePath: Base path where PVCs are mounted (e.g., /mnt/gkm-pvcs)
//   - k8sClient: Kubernetes client to query PVCs and get their metadata
//   - log: Logger instance
//
// Returns:
//   - map[CacheKey]bool: Map of extracted caches indexed by namespace, name, and digest
//
// Note: The function expects PVCs to be mounted at: {pvcBasePath}/{pvcName}/
// and caches to be extracted at: {pvcBasePath}/{pvcName}/{digest}/
func GetExtractedCacheListFromPVC(
	ctx context.Context,
	k8sClient client.Client,
	pvcBasePath string,
	log logr.Logger,
) (*map[CacheKey]bool, error) {
	list := make(map[CacheKey]bool)

	// Check if PVC base path exists
	if _, err := os.Stat(pvcBasePath); os.IsNotExist(err) {
		// PVC base path doesn't exist, return empty list
		log.V(1).Info("PVC base path does not exist, no caches extracted", "pvcBasePath", pvcBasePath)
		return &list, nil
	}

	// List all PVC directories
	pvcDirs, err := os.ReadDir(pvcBasePath)
	if err != nil {
		log.Error(err, "failed to read PVC base path", "pvcBasePath", pvcBasePath)
		return &list, err
	}

	// For each PVC directory, check for extracted caches (digest directories)
	for _, pvcDir := range pvcDirs {
		if !pvcDir.IsDir() {
			continue
		}

		pvcName := pvcDir.Name()
		pvcMountPath := filepath.Join(pvcBasePath, pvcName)

		// Parse the PVC name to get namespace and cache name
		// PVC names follow the pattern: gkm-cache-{namespace}-{name} or gkm-clustercache-{name}
		namespace, name, err := ParsePVCName(pvcName)
		if err != nil {
			log.V(1).Info("Skipping non-GKM PVC", "pvcName", pvcName, "error", err)
			continue
		}

		// List digest directories within this PVC
		digestDirs, err := os.ReadDir(pvcMountPath)
		if err != nil {
			log.Error(err, "failed to read PVC mount path", "pvcMountPath", pvcMountPath)
			continue
		}

		// Add each digest directory to the list
		for _, digestDir := range digestDirs {
			if !digestDir.IsDir() {
				continue
			}

			digest := digestDir.Name()
			key := CacheKey{
				Namespace: namespace,
				Name:      name,
				Digest:    digest,
			}

			// Verify the directory is not empty (has extracted cache files)
			if IsCacheExtractedInPVC(pvcMountPath, digest) {
				list[key] = true
				log.V(1).Info("Found extracted cache in PVC",
					"namespace", namespace,
					"name", name,
					"digest", digest,
					"pvcName", pvcName)
			}
		}
	}

	return &list, nil
}

// ParsePVCName parses a GKM PVC name and returns the namespace and cache name.
// PVC names follow the pattern:
//   - Namespace-scoped: gkm-cache-{namespace}-{name}
//   - Cluster-scoped: gkm-clustercache-{name}
//
// Returns:
//   - namespace: Empty string for cluster-scoped caches
//   - name: Cache name
//   - error: If PVC name doesn't match expected pattern
func ParsePVCName(pvcName string) (namespace, name string, err error) {
	if strings.HasPrefix(pvcName, utils.PVCClusterNamePrefix+"-") {
		// Cluster-scoped: gkm-clustercache-{name}
		name = strings.TrimPrefix(pvcName, utils.PVCClusterNamePrefix+"-")
		if name == "" {
			return "", "", fmt.Errorf("invalid cluster-scoped PVC name: %s", pvcName)
		}
		return "", name, nil
	} else if strings.HasPrefix(pvcName, utils.PVCNamePrefix+"-") {
		// Namespace-scoped: gkm-cache-{namespace}-{name}
		remainder := strings.TrimPrefix(pvcName, utils.PVCNamePrefix+"-")
		parts := strings.SplitN(remainder, "-", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid namespace-scoped PVC name: %s", pvcName)
		}
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("PVC name does not match GKM pattern: %s", pvcName)
}
