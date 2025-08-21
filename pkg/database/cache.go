package database

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	mcvClient "github.com/redhat-et/MCU/mcv/pkg/client"

	"github.com/redhat-et/GKM/pkg/utils"
)

// The Agent takes the image from a GKMCache or ClusterGKMCache CR and
// expands the Cache on the host. CSI mounts the Cache into a Pod.
// The Agent owns these files and performs the Create/Update/Delete of
// these files. CSI is a Reader of the files.
//
// Cache is expanded to:
//   /var/lib/gkm/caches/<Namespace>/<Name>/<Digest>/<CacheDirs/...
// Agent also stores metadata in:
//   /var/lib/gkm/caches/<Namespace>/<Name>/cache.json
//
// Agent uses MCV to expand the cache, and the functions in this file are
// used to read and manage the cache files.

var defaultCacheDir string
var cacheLock sync.Mutex

func init() {
	initializeCachePath(utils.DefaultCacheDir)
}

// Allow overriding UsageDir location for Testing
var ExportForTestInitializeCachePath = initializeCachePath

func initializeCachePath(value string) {
	defaultCacheDir = value
}

func ExtractCache(crNamespace, crName, image, digest string, noGpu bool, log logr.Logger) (matchedIDs, unmatchedIDs []int, err error) {
	// Replace the tag in the Image URL with the Digest. Webhook has verified
	// the image and so pull from the resolved digest.
	updatedImage := replaceUrlTag(image, digest)
	if updatedImage == "" {
		err := fmt.Errorf("unable to update image tag with digest")
		log.Error(err, "invalid image or digest", "image", image, "digest", digest)
		return nil, nil, err
	}

	// Build Cache Directory string from namespace, name and digest
	cacheDir, err := BuildDbDir(defaultCacheDir, crNamespace, crName, digest, log)
	if err != nil {
		log.Error(err, "unable to generate cache directory",
			"namespace", crNamespace,
			"name", crName,
			"image", image,
			"digest", digest)
		return nil, nil, err
	}

	cacheLock.Lock()
	defer cacheLock.Unlock()

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

		// Cleanup created directories
		removeDirectories(crNamespace, crName, digest, log)
		return nil, nil, err
	}
	log.Info("Cache Extracted", "matchedIds", matchedIds, "unmatchedIds", unmatchedIds)

	// Save off the VolumeId mapping to CRD Info
	size, err := DirSize(cacheDir)
	if err != nil {
		size = 0
		log.Error(err, "unable to get directory size, continuing",
			"cacheDir", cacheDir,
			"namespace", crNamespace,
			"name", crName,
			"digest", digest)
	}

	// Cache was successfully extracted, so write/update Cache file, which contains metadata
	// about the cache.
	if err = writeCacheFile(crNamespace, crName, image, digest, false, size, log); err != nil {
		log.Error(err, "unable to write cache file",
			"namespace", crNamespace,
			"name", crName,
			"digest", digest)
		return nil, nil, err
	}

	return matchedIds, unmatchedIds, nil
}

type CacheKey struct {
	Namespace string
	Name      string
	Digest    string
}

// GetExtractedCacheList() reads the files on the host and returns
// a map of the extracted caches. Map is indexed by Namespace, Name
// and Digest and the value stored in the map ignored (a bool always set to true).
func GetExtractedCacheList(log logr.Logger) (*map[CacheKey]bool, error) {
	list := make(map[CacheKey]bool)
	var key CacheKey

	cacheLock.Lock()
	defer cacheLock.Unlock()

	// Walk the directory looking for the expanded cache, should be something like:
	//   /var/lib/gkm/caches/<Namespace>/<Name>/<Digest>/<CacheDirs/...
	// Where defaultCacheDir is "/var/lib/gkm/caches/" and trying to determine Namespace, Name and Digest.
	_ = filepath.WalkDir(defaultCacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			l1Path := filepath.Dir(path)
			l2Path := filepath.Dir(l1Path)
			l3Path := filepath.Dir(l2Path)
			currDir := filepath.Base(path)

			if l1Path == defaultCacheDir {
				// Found Namespace
				key.Namespace = currDir
				key.Name = ""
			} else if l2Path == defaultCacheDir {
				// Make sure Namespace matches
				if key.Namespace == filepath.Base(l1Path) {
					// Found Name
					key.Name = currDir
				} else {
					log.Info("GetExtractedCacheList(): Error on Name",
						"Path", path,
						"Parent", currDir,
						"key.Namespace", key.Namespace,
						"l1path.Base", filepath.Base(l1Path),
					)
				}
			} else if l3Path == defaultCacheDir {
				// Make sure Namespace and Name matches
				if key.Namespace == filepath.Base(l2Path) &&
					key.Name == filepath.Base(l1Path) {

					// Found Digest
					key.Digest = currDir

					// If Cluster Scoped, use "" as the Namespace in the key
					tmpKey := key
					if tmpKey.Namespace == utils.ClusterScopedSubDir {
						tmpKey.Namespace = ""
					}

					list[tmpKey] = true
				} else {
					log.Info("GetExtractedCacheList(): Error on Digest",
						"Path", path,
						"Parent", currDir,
						"key.Namespace", key.Namespace,
						"l2path.Base", filepath.Base(l2Path),
						"key.Name", key.Name,
						"l1path.Base", filepath.Base(l1Path),
					)
				}
			}
		}
		return nil
	})

	return &list, nil // Directory is not empty
}

func GetCacheFile(crNamespace, crName string, log logr.Logger) (*CacheData, error) {
	cacheLock.Lock()
	defer cacheLock.Unlock()

	return readCacheFile(crNamespace, crName, log)
}

// RemoveCache attempts to remove the extracted cache directories/files from
// the host. They are stored in:
//
//	/var/lib/gkm/caches/<Namespace>/<Name>/<Digest>/<CacheDirectories/Files>
//
// If the Cache is currently mounted in a pod, the request will fail with
// an error and the "inUse" flag will be set to true and returned, "inUse" is
// false otherwise. If the Cache is not in use, the associated Digest directory,
// which contains the cache directories and files, is deleted. If no other Digest
// directories exists, then the cache.json and the Name directory is deleted. Name
// is the name of the GKMCache or ClusterGKMCache custom resource. If no other Name
// directories exist under the Namespace directory, the Namespace directory is also
// deleted.
func RemoveCache(crNamespace, crName, digest string, log logr.Logger) (bool, error) {
	inUse := false

	// Check to see if cache is in use
	if _, err := GetUsageData(crNamespace, crName, digest, log); err == nil {
		log.V(1).Info("cache still in use",
			"namespace", crNamespace,
			"name", crName,
			"digest", digest)
		inUse = true
		return inUse, fmt.Errorf("kernel cache still in use: %w", err)
	}

	cacheLock.Lock()
	defer cacheLock.Unlock()

	return inUse, removeDirectories(crNamespace, crName, digest, log)
}

func removeDirectories(crNamespace, crName, digest string, log logr.Logger) error {
	// Build Cache Directory string from namespace, name and digest
	cacheDir, err := BuildDbDir(defaultCacheDir, crNamespace, crName, digest, log)
	if err != nil {
		log.Error(err, "unable to remove cache",
			"namespace", crNamespace,
			"name", crName,
			"digest", digest)
		return err
	}

	// Remove the Digest directory and the associated extracted cache
	//   /var/lib/gkm/caches/<Namespace>/<Name>/<Digest>
	log.V(1).Info("Deleting GKMCache Digest directory", "directory", cacheDir)
	err = os.RemoveAll(cacheDir)
	if err != nil {
		log.Error(err, "unable to remove GKMCache Digest directory",
			"directory", cacheDir,
			"namespace", crNamespace,
			"name", crName,
			"digest", digest)
		return err
	}

	// Remove the Digest from path, leaving "/var/lib/gkm/caches/<Namespace>/<Name>""
	l1Path := filepath.Dir(cacheDir)

	// See if Name Directory is empty, ignoring the CacheFilename
	empty := IsDirEmpty(l1Path, utils.CacheFilename)
	if empty {
		log.V(1).Info("Deleting GKMCache Name directory", "directory", l1Path)
		err := os.RemoveAll(l1Path)
		if err != nil {
			return fmt.Errorf("unable to remove GKMCache Name directory %s: %w", l1Path, err)
		}

		// Remove the Name from path, leaving "/var/lib/gkm/caches/<Namespace>"
		l2Path := filepath.Dir(l1Path)
		empty := IsDirEmpty(l2Path, "")
		if empty {
			log.V(1).Info("Deleting GKMCache Namespace directory", "directory", l2Path)
			err := os.RemoveAll(l2Path)
			if err != nil {
				return fmt.Errorf("unable to remove GKMCache Namespace directory %s: %w", l2Path, err)
			}
		} else {
			log.V(1).Info("GKMCache Namespace directory not empty", "directory", l2Path)
		}
	} else {
		log.V(1).Info("GKMCache directory not empty", "directory", l1Path)

		// Read the Cache File
		cache, err := readCacheFile(crNamespace, crName, log)
		if err != nil {
			log.Error(err, "unable to read cache file",
				"directory", defaultCacheDir,
				"namespace", crNamespace,
				"name", crName,
				"digest", digest)
			return err
		}

		// Update Cache file, which contains metadata about the cache, removing current Digest.
		if err = writeCacheFile(crNamespace, crName, cache.Image, digest, true, 0, log); err != nil {
			log.Error(err, "unable to rewrite cache file",
				"namespace", crNamespace,
				"name", crName,
				"digest", digest)
			return err
		}
	}

	return nil
}

func replaceUrlTag(imageURL, digest string) string {
	// If invalid input, return empty string
	if imageURL == "" || digest == "" {
		return ""
	}

	// Tokenize the Image URL
	lastColonIndex := strings.LastIndex(imageURL, ":")
	if lastColonIndex == -1 {
		// No tag found, append the new tag
		return imageURL + "@" + digest
	}
	// Extract the part before the tag and append the new tag
	return imageURL[:lastColonIndex] + "@" + digest
}

type CacheData struct {
	ResolvedDigest string           `json:"resolvedDigest"`
	Image          string           `json:"image"`
	Sizes          map[string]int64 `json:"sizes"`
}

func readCacheFile(crNamespace, crName string, log logr.Logger) (*CacheData, error) {
	// Build filepath string from namespace and name (Note: No Digest)
	//  (e.g., "/var/lib/gkm/caches/<Namespace>/<Name>/cache.json")
	cacheFile, err := BuildDbDir(defaultCacheDir, crNamespace, crName, "", log)
	if err != nil {
		return nil, err
	}
	cacheFile = filepath.Join(cacheFile, utils.CacheFilename)
	var cache CacheData
	if err = loadJSONFromCacheFile(cacheFile, &cache); err != nil {
		return nil, fmt.Errorf("not found")
	}

	return &cache, nil
}

var ExportForTestWriteCacheFile = writeCacheFile

func writeCacheFile(crNamespace, crName, image, digest string, rmDigest bool, size int64, log logr.Logger) error {
	// Build filepath string from namespace and name (Note: No Digest)
	//  (e.g., "/var/lib/gkm/caches/<Namespace>/<Name>/cache.json")
	cacheFile, err := BuildDbDir(defaultCacheDir, crNamespace, crName, "", log)
	if err != nil {
		return err
	}
	cacheFile = filepath.Join(cacheFile, utils.CacheFilename)

	var cache CacheData
	cache.Sizes = make(map[string]int64)
	if err = loadJSONFromCacheFile(cacheFile, &cache); err != nil {
		// Unable to read from file. If removing, just return, else populate with input data.
		if rmDigest {
			return nil
		}
	} else {
		// Cache file found, so log data that changed
		if cache.ResolvedDigest != digest {
			log.Info("writeCacheFile(): Updating ResolvedDigest",
				"crNamespace", crNamespace,
				"crName", crName,
				"digest", digest)
		}
		if cache.Image != image {
			log.Info("writeCacheFile(): Updating Image",
				"crNamespace", crNamespace,
				"crName", crName,
				"digest", digest,
				"image", image)
		}
	}

	cache.Image = image
	if rmDigest {
		if cache.ResolvedDigest == digest {
			cache.ResolvedDigest = ""
		}
		delete(cache.Sizes, digest)
	} else {
		cache.ResolvedDigest = digest
		if size != 0 {
			cache.Sizes[digest] = size
		}
	}

	// Write date to file
	err = saveJSONToCacheFile(cacheFile, &cache)
	if err != nil {
		log.Error(err, "writeCacheFile(): failed to save cache to file",
			"crNamespace", crNamespace,
			"crName", crName,
			"digest", digest)
		return err
	}

	return nil
}

func loadJSONFromCacheFile(filename string, cache *CacheData) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, cache)
}

func saveJSONToCacheFile(filename string, cache *CacheData) error {
	jsonData, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(filename, jsonData, 0755)
	if err != nil {
		return err
	}

	return nil
}
