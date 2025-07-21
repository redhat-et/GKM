package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"

	"github.com/redhat-et/GKM/pkg/utils"
)

var defaultUsageDir string

func init() {
	initializeUsagePath(utils.DefaultUsageDir)
}

// Allow overriding UsageDir location for Testing
func initializeUsagePath(value string) {
	defaultUsageDir = value
}

// UsageData contains metadata about a given Kernel Cache.
// CSI is given a VolumeId from Kubelet that is used to access the data.
// Agent is given a GKMCache CR name and namespace that is used to access the data.
type UsageData struct {
	// Index used by Agent
	CrName      string `json:"cr_name"`
	CrNamespace string `json:"cr_namespace"`

	// Index used by CSI Driver
	VolumeId []string `json:"volume_id"`

	// Usage Data
	RefCount   int32 `json:"ref_count"`
	VolumeSize int64 `json:"volume_size"`
}

func GetUsageDataByVolumeId(volumeId string, log logr.Logger) (*UsageData, error) {
	var usage UsageData

	usagePath := defaultUsageDir

	var fileFound = errors.New("file found")
	err := filepath.WalkDir(usagePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Error(err, "GetUsageDataByVolumeId(): WalkDir error", "VolumeId", volumeId, "Path", path)
			return err
		}

		if !d.IsDir() {
			err := loadJSONFromFile(path, &usage)
			if err == nil {
				for _, id := range usage.VolumeId {
					if id == volumeId {
						return fileFound
					}
				}
			} else {
				log.Error(err, "GetUsageDataByVolumeId(): loadJSONFromFile() error", "VolumeId", volumeId, "Path", path)
			}
		}
		return nil
	})

	if errors.Is(err, fileFound) {
		return &usage, nil
	} else {
		return nil, fmt.Errorf("not found")
	}
}

func GetUsageData(crNamespace, crName string, log logr.Logger) (*UsageData, error) {
	// Build filepath string from namespace and name (e.g., "/run/gkm/usage/<crNamespace>/<crName>/usage.json")
	usagePath := defaultUsageDir

	if crNamespace != "" {
		usagePath = filepath.Join(usagePath, crNamespace)
	} else {
		usagePath = filepath.Join(usagePath, utils.ClusterScopedSubDir)
	}

	if crName != "" {
		usagePath = filepath.Join(usagePath, crName)
	} else {
		return nil, fmt.Errorf("custom resource name is required")
	}

	usagePath = filepath.Join(usagePath, utils.UsageFilename)

	var usage UsageData
	err := loadJSONFromFile(usagePath, &usage)
	if err != nil {
		return nil, fmt.Errorf("not found")
	}

	return &usage, nil
}

func AddUsageData(crNamespace, crName, volumeId string, size int64, log logr.Logger) error {
	// Build filepath string from namespace and name (e.g., "/run/gkm/usage/<namespace>/<name>/usage.json")
	usagePath := defaultUsageDir

	if crNamespace != "" {
		usagePath = filepath.Join(usagePath, crNamespace)
	} else {
		usagePath = filepath.Join(usagePath, utils.ClusterScopedSubDir)
	}

	if crName != "" {
		usagePath = filepath.Join(usagePath, crName)
	} else {
		return fmt.Errorf("custom resource name is required")
	}

	if volumeId == "" {
		return fmt.Errorf("volume id is required")
	}

	parentDir := usagePath
	usagePath = filepath.Join(usagePath, utils.UsageFilename)

	// Check to see if cache is already created
	var curUsage UsageData
	err := loadJSONFromFile(usagePath, &curUsage)
	if err != nil {
		// Creating new instance
		err = os.MkdirAll(parentDir, 0755)
		if err != nil {
			log.Error(err, "AddUsageData(): unable to create parent directory", "parentDir", parentDir)
			return err
		}

		curUsage.VolumeId = append(curUsage.VolumeId, volumeId)
		curUsage.CrName = crName
		curUsage.CrNamespace = crNamespace
		curUsage.VolumeSize = size
		curUsage.RefCount = 1
	} else {
		// Updating instance
		// Make sure this is the same instance
		if crNamespace != curUsage.CrNamespace ||
			crName != curUsage.CrName {
			err = fmt.Errorf("custom resource doesn't match")
			log.Error(err, "AddUsageData(): mismatch", "crNamespace", crNamespace, "crName", crName)
			return err
		}

		if curUsage.VolumeSize != size {
			log.Info("AddUsageData(): size being updated", "size", size, "VolumeSize", curUsage.VolumeSize)
			curUsage.VolumeSize = size
		}

		found := false
		for _, id := range curUsage.VolumeId {
			if id == volumeId {
				found = true
				break
			}
		}
		if !found {
			curUsage.VolumeId = append(curUsage.VolumeId, volumeId)
			curUsage.RefCount++
		}
	}

	// Write date to file
	err = saveJSONToFile(usagePath, &curUsage)
	if err != nil {
		log.Error(err, "AddUsageData(): failed to save usage to file", "crNamespace", crNamespace, "crName", crName)
		return err
	}

	return nil
}

func DeleteUsageData(volumeId string, log logr.Logger) error {
	usagePath := defaultUsageDir

	var fileFound = errors.New("file found")
	err := filepath.WalkDir(usagePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Error(err, "GetUsageDataByVolumeId(): WalkDir error", "VolumeId", volumeId, "Path", path)
			return err
		}

		if !d.IsDir() {
			var usage UsageData
			err := loadJSONFromFile(path, &usage)
			if err == nil {
				found := false
				newVolumeIdList := []string{}
				for _, id := range usage.VolumeId {
					if id == volumeId {
						found = true
						if usage.RefCount == 1 {
							// Last Entry, Remove File and return
							err = os.Remove(path)
							if err != nil {
								log.Error(err, "DeleteUsageData(): error deleting usage file, continuing",
									"VolumeId", usage.VolumeId, "path", path)
							}

							// Check if Custom Resource Directory is empty and if so remove.
							crNameDir := filepath.Dir(path)
							empty, err := utils.IsDirEmpty(crNameDir)
							if err != nil {
								log.Error(err, "DeleteUsageData(): error reading crNameDir directory, continuing",
									"VolumeId", usage.VolumeId, "crNameDir", crNameDir)
							} else if empty {
								err = os.RemoveAll(crNameDir)
								if err != nil {
									log.Error(err, "DeleteUsageData(): error deleting crNameDir directory, continuing",
										"VolumeId", usage.VolumeId, "crNameDir", crNameDir)
								}

								// Check if Custom Resource Names[ace] Directory is empty and if so remove.
								crNamespaceDir := filepath.Dir(crNameDir)
								empty, err := utils.IsDirEmpty(crNamespaceDir)
								if err != nil {
									log.Error(err, "DeleteUsageData(): error reading crNamespaceDir directory, continuing",
										"VolumeId", usage.VolumeId, "crNamespaceDir", crNamespaceDir)
								} else if empty {
									err = os.RemoveAll(crNamespaceDir)
									if err != nil {
										log.Error(err, "DeleteUsageData(): error deleting crNamespaceDir directory, continuing",
											"VolumeId", usage.VolumeId, "crNamespaceDir", crNamespaceDir)
									}
								}
							}

							return fileFound
						} else {
							// Found VolumeId, but more than one entry, so entry still in use.
							// Mark found as true but don't add this id to newVolumeIdList.
							found = true
						}
					} else {
						// Build up a new list of VolumeIds without the one being removed.
						newVolumeIdList = append(newVolumeIdList, id)
					}
				}
				if found {
					usage.RefCount--
					usage.VolumeId = newVolumeIdList
					err = saveJSONToFile(path, &usage)
					if err != nil {
						log.Error(err, "DeleteUsageData(): error updating crNamespaceDir file, continuing",
							"VolumeId", volumeId, "path", path)
					}
					return fileFound
				}
			}
		}
		return nil
	})

	if errors.Is(err, fileFound) {
		return nil
	} else {
		return fmt.Errorf("not found")
	}
}

func loadJSONFromFile(filename string, usage *UsageData) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, usage)
}

func saveJSONToFile(filename string, usage *UsageData) error {
	jsonData, err := json.MarshalIndent(usage, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(filename, jsonData, 0755)
	if err != nil {
		return err
	}

	return nil
}
