package database

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

// The Agent doesn't know if a given cache is in use or not, so CSI
// writes usage data to a file for the Agent to read. CSI owns these files
// and performs the Create/Update/Delete of these files. The Agent is a Reader
// of the files.
//
// Usage files:
//   /run/gkm/usage/<Namespace>/<Name>/<Digest>/usage.json
//
// Functions in this file are used to manage the usage files.

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
	Digest      string `json:"digest"`

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
			err := loadJSONFromUsageFile(path, &usage)
			if err == nil {
				for _, id := range usage.VolumeId {
					if id == volumeId {
						return fileFound
					}
				}
			} else {
				log.Error(err, "GetUsageDataByVolumeId(): loadJSONFromUsageFile() error", "VolumeId", volumeId, "Path", path)
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

func GetUsageData(crNamespace, crName, digest string, log logr.Logger) (*UsageData, error) {
	// Build filepath string from namespace and name (e.g., "/run/gkm/usage/<Namespace>/<Name>/<Digest>/usage.json")
	usagePath, err := BuildDbDir(defaultUsageDir, crNamespace, crName, digest, log)
	if err != nil {
		return nil, err
	}
	usagePath = filepath.Join(usagePath, utils.UsageFilename)

	var usage UsageData
	if err = loadJSONFromUsageFile(usagePath, &usage); err != nil {
		return nil, fmt.Errorf("not found")
	}

	return &usage, nil
}

func AddUsageData(crNamespace, crName, digest, volumeId string, size int64, log logr.Logger) error {
	if crName == "" {
		return fmt.Errorf("custom resource name is required")
	}
	if digest == "" {
		return fmt.Errorf("digest is required")
	}
	if volumeId == "" {
		return fmt.Errorf("volume id is required")
	}

	// Build filepath string from namespace, name and digest
	//  (e.g., "/run/gkm/usage/<Namespace>/<Name>/<Digest>/usage.json")
	parentDir, err := BuildDbDir(defaultUsageDir, crNamespace, crName, digest, log)
	if err != nil {
		return err
	}
	usagePath := filepath.Join(parentDir, utils.UsageFilename)

	// Check to see if cache is already created
	var curUsage UsageData
	if err = loadJSONFromUsageFile(usagePath, &curUsage); err != nil {
		// Creating new instance
		err = os.MkdirAll(parentDir, 0755)
		if err != nil {
			log.Error(err, "AddUsageData(): unable to create parent directory", "parentDir", parentDir)
			return err
		}

		curUsage.VolumeId = append(curUsage.VolumeId, volumeId)
		curUsage.CrName = crName
		curUsage.CrNamespace = crNamespace
		curUsage.Digest = digest
		curUsage.VolumeSize = size
		curUsage.RefCount = 1
	} else {
		// Updating instance
		if curUsage.VolumeSize != size {
			log.Info("AddUsageData(): size being updated",
				"crNamespace", crNamespace,
				"crName", crName,
				"digest", digest,
				"size", size,
				"VolumeSize", curUsage.VolumeSize)
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
	err = saveJSONToUsageFile(usagePath, &curUsage)
	if err != nil {
		log.Error(err, "AddUsageData(): failed to save usage to file",
			"crNamespace", crNamespace,
			"crName", crName,
			"digest", digest)
		return err
	}

	return nil
}

func DeleteUsageData(volumeId string, log logr.Logger) error {
	usagePath := defaultUsageDir

	var fileFound = errors.New("file found")
	err := filepath.WalkDir(usagePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			if d.Name() == utils.UsageFilename {
				var usage UsageData
				err := loadJSONFromUsageFile(path, &usage)
				if err == nil {
					found := false
					newVolumeIdList := []string{}
					for _, id := range usage.VolumeId {
						if id == volumeId {
							found = true
							if usage.RefCount == 1 {
								// Last Entry, Remove File
								err = os.Remove(path)
								if err != nil {
									log.Error(err, "DeleteUsageData(): error deleting usage file, continuing",
										"VolumeId", usage.VolumeId, "path", path)
								}

								// Check if Digest Directory is empty and if so remove.
								digestDir := filepath.Dir(path)
								empty := IsDirEmpty(digestDir, "")
								if empty {
									err = os.RemoveAll(digestDir)
									if err != nil {
										log.Error(err, "DeleteUsageData(): error deleting digestDir directory, continuing",
											"VolumeId", usage.VolumeId, "digestDir", digestDir)
									}

									// Check if Custom Resource Name Directory is empty and if so remove.
									crNameDir := filepath.Dir(digestDir)
									empty := IsDirEmpty(crNameDir, "")
									if empty {
										err = os.RemoveAll(crNameDir)
										if err != nil {
											log.Error(err, "DeleteUsageData(): error deleting crNameDir directory, continuing",
												"VolumeId", usage.VolumeId, "crNameDir", crNameDir)
										}

										// Check if Custom Resource Namespace Directory is empty and if so remove.
										crNamespaceDir := filepath.Dir(crNameDir)
										empty = IsDirEmpty(crNamespaceDir, "")
										if empty {
											err = os.RemoveAll(crNamespaceDir)
											if err != nil {
												log.Error(err, "DeleteUsageData(): error deleting crNamespaceDir directory, continuing",
													"VolumeId", usage.VolumeId, "crNamespaceDir", crNamespaceDir)
											}
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
						err = saveJSONToUsageFile(path, &usage)
						if err != nil {
							log.Error(err, "DeleteUsageData(): error updating crNamespaceDir file, continuing",
								"VolumeId", volumeId, "path", path)
						}
						return fileFound
					}
				}
			} else {
				log.Error(err, "DeleteUsageData(): unknown file found in directory, continuing",
					"File", d.Name(), "path", path)
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

func loadJSONFromUsageFile(filename string, usage *UsageData) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, usage)
}

func saveJSONToUsageFile(filename string, usage *UsageData) error {
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
