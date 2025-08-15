/*
Copyright Istio Authors
Copyright Red Hat Inc.

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

package fetcher

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/redhat-et/MCU/mcv/pkg/accelerator"
	"github.com/redhat-et/MCU/mcv/pkg/accelerator/devices"
	"github.com/redhat-et/MCU/mcv/pkg/config"
	"github.com/redhat-et/MCU/mcv/pkg/constants"
	"github.com/redhat-et/MCU/mcv/pkg/preflightcheck"
	"github.com/redhat-et/MCU/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
)

// A quick list of TODOS:
// 1. Add image caching to avoid the overhead of pulling the images down every time
// 2. Don't create directories/files in $HOME/.triton/cache if they already exist.

type tritonCacheExtractor struct {
	acc accelerator.Accelerator
}

type imgMgr struct {
	fetcher   ImgFetcher
	extractor TritonCacheExtractor
}

// TritonCacheExtractor extracts the Triton cache from an image.
type TritonCacheExtractor interface {
	ExtractCache(img v1.Image) error
}

// ImgMgr retrieves cache images.
type ImgMgr interface {
	FetchAndExtractCache(imgName string) error
}

// Factory function to create a new ImgMgr.
func New() ImgMgr {
	var a accelerator.Accelerator

	if config.IsGPUEnabled() {
		r := accelerator.GetRegistry()
		acc, err := accelerator.New(config.GPU, true)
		if err != nil {
			logging.Errorf("failed to init GPU accelerators: %v", err)
		} else {
			r.MustRegister(acc) // Register the accelerator with the registry
			a = acc
		}
		// defer accelerator.Shutdown() // TODO CALL IN CLEANUP
	}

	return &imgMgr{
		fetcher:   NewImgFetcher(),
		extractor: &tritonCacheExtractor{acc: a},
	}
}

type imgFetcher struct {
	fetcher Fetcher
}

type ImgFetcher interface {
	FetchImg(imgName string) (v1.Image, error)
}

// func saveImageLocally(path string, img v1.Image, ref name.Reference) error {
// 	out, err := os.Create(path)
// 	if err != nil {
// 		return fmt.Errorf("failed to create cache file: %w", err)
// 	}
// 	defer out.Close()

// 	err = tarball.WriteToFile(path, ref, img)
// 	if err != nil {
// 		return fmt.Errorf("failed to write image to cache: %w", err)
// 	}
// 	return nil
// }

func loadImageFromTarball(path string) (v1.Image, error) {
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load image from tarball: %w", err)
	}
	logging.Debug("loaded image from tarball!!!!!!!!")
	return img, nil
}

func NewImgFetcher() ImgFetcher {
	return &imgFetcher{fetcher: NewFetcher()}
}

// FetchImg pulls the image from the registry and extracts the TritonCache
func (i *imgFetcher) FetchImg(imgName string) (v1.Image, error) {
	if i.fetcher == nil {
		logging.Error("Error with fetcher!!!!!!!!")
		return nil, fmt.Errorf("failed to configure fetcher")
	}

	imageWithTag := imgName
	if !strings.Contains(imgName, ":") {
		imageWithTag = fmt.Sprintf("%s:latest", imgName)
	}

	img, err := i.fetcher.FetchImg(imageWithTag)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}
	logging.Debug("Img retrieved successfully!!!!!!!!")

	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("failed to get image digest: %w", err)
	}
	logging.Debugf("Img Digest: %s", digest)

	size, err := img.Size()
	if err != nil {
		return nil, fmt.Errorf("failed to get image digest: %w", err)
	}
	logging.Debugf("Img Size: %v\n", size)

	return img, nil
}

func (e *tritonCacheExtractor) ExtractCache(img v1.Image) error {
	var extractedDirs []string
	var devInfo []devices.TritonGPUInfo

	// Fetch image manifest
	manifest, err := img.Manifest()
	if err != nil {
		return fmt.Errorf("failed to fetch manifest: %w", err)
	}

	if config.IsGPUEnabled() && !config.IsSkipPrecheckEnabled() {
		devInfo, err = preflightcheck.GetAllGPUInfo(e.acc)
		if err != nil {
			return fmt.Errorf("failed to get GPU info: %w", err)
		}

		// Summary check first (labels only)
		if _, _, err := preflightcheck.CompareTritonSummaryLabelToGPU(img, devInfo); err != nil {
			return fmt.Errorf("summary check failed: %w", err)
		}
	}

	// Always cleanup temp dirs at the end
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := utils.CleanupMCVDirs(ctx, ""); err != nil {
			logging.Warnf("cleanup failed: %v", err)
		}
	}()

	var extractErr error

	switch manifest.MediaType {
	case types.DockerManifestSchema2:
		extractedDirs, extractErr = extractDockerImg(img)
	default:
		// Try to parse it as the "compat" variant image with a single "application/vnd.oci.image.layer.v1.tar+gzip" layer.
		extractedDirs, extractErr = extractOCIStandardImg(img)
		if extractErr != nil {
			// Otherwise, try to parse it as the *oci* variant image with custom artifact media types.
			extractedDirs, extractErr = extractOCIArtifactImg(img)
		}
	}

	if extractErr != nil {
		return fmt.Errorf("could not extract Triton Cache: %w", extractErr)
	}

	// Full manifest compatibility check (after extraction)
	manifestPath := filepath.Join(constants.MCVManifestDir, constants.ManifestFileName)
	if config.IsGPUEnabled() && config.IsBaremetalEnabled() {
		if err := preflightcheck.CompareTritonCacheManifestToGPU(manifestPath, devInfo); err != nil {
			for _, dir := range extractedDirs {
				if rmErr := os.RemoveAll(dir); rmErr != nil {
					logging.Warnf("Failed to clean up extracted kernel dir %s: %v", dir, rmErr)
				}
			}
			return fmt.Errorf("manifest check failed: %w", err)
		}
	}

	return nil
}

func (i *imgMgr) FetchAndExtractCache(imgName string) error {
	img, err := i.fetcher.FetchImg(imgName)
	if err != nil {
		return err
	}

	err = i.extractor.ExtractCache(img)
	if err != nil {
		return err
	}

	return nil
}

// extractOCIArtifactImg extracts the triton cache from the
// *oci* variant Triton Kernel Cache image:  //TODO ADD URL
func extractOCIArtifactImg(img v1.Image) ([]string, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must be single-layered.
	if len(layers) != 1 {
		return nil, fmt.Errorf("number of layers must be 1 but got %d", len(layers))
	}

	// The layer type of the Triton cache itself in *oci* variant.
	const cacheLayerMediaType = "application/cache.triton.content.layer.v1+triton"

	// Find the target layer walking through the layers.
	var layer v1.Layer
	for _, l := range layers {
		mt, ret := l.MediaType()
		if ret != nil {
			return nil, fmt.Errorf("could not retrieve the media type: %v", ret)
		}
		if mt == cacheLayerMediaType {
			layer = l
			break
		}
	}

	if layer == nil {
		return nil, fmt.Errorf("could not find the layer of type %s", cacheLayerMediaType)
	}

	// Somehow go-container registry recognizes custom artifact layers as compressed ones,
	// while the GPU Kernel Cache/Binary layer is actually uncompressed and therefore
	// the content itself is a GPU Kernel Cache/Binary. So using "Uncompressed()" here result in errors
	// since internally it tries to umcompress it as gzipped blob.
	r, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("could not get layer content: %v", err)
	}
	defer r.Close()

	dirs, err := extractTritonCacheDirectory(r)
	if err != nil {
		return nil, fmt.Errorf("could not extract Triton Kernel Cache: %v", err)
	}
	return dirs, nil
}

// extractDockerImg extracts the Triton Kernel Cache from the
// *compat* variant GPU Kernel Cache/Binary image with the standard Docker
// media type: application/vnd.docker.image.rootfs.diff.tar.gzip.
// https://github.com/maryamtahhan/mcv/blob/main/spec-compat.md
func extractDockerImg(img v1.Image) ([]string, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must have at least one layer.
	if len(layers) == 0 {
		return nil, errors.New("number of layers must be greater than zero")
	}

	layer := layers[len(layers)-1]
	mt, err := layer.MediaType()
	if err != nil {
		return nil, fmt.Errorf("could not get media type: %v", err)
	}

	// Media type must be application/vnd.docker.image.rootfs.diff.tar.gzip.
	if mt != types.DockerLayer {
		return nil, fmt.Errorf("invalid media type %s (expect %s)", mt, types.DockerLayer)
	}

	r, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("could not get layer content: %v", err)
	}
	defer r.Close()

	dirs, err := extractTritonCacheDirectory(r)
	if err != nil {
		return nil, fmt.Errorf("could not extract Triton Kernel Cache: %v", err)
	}
	return dirs, nil
}

// extractOCIStandardImg extracts the Triton Kernel Cache from the
// *compat* variant Triton Kernel image with the standard OCI media type: application/vnd.oci.image.layer.v1.tar+gzip.
// https://github.com/maryamtahhan/mcv/blob/main/spec-compat.md
func extractOCIStandardImg(img v1.Image) ([]string, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("could not fetch layers: %v", err)
	}

	// The image must have at least one layer.
	if len(layers) == 0 {
		return nil, fmt.Errorf("number of layers must be greater than zero")
	}

	layer := layers[len(layers)-1]
	mt, err := layer.MediaType()
	if err != nil {
		return nil, fmt.Errorf("could not get media type: %v", err)
	}

	// Check if the layer is "application/vnd.oci.image.layer.v1.tar+gzip".
	if types.OCILayer != mt {
		return nil, fmt.Errorf("invalid media type %s (expect %s)", mt, types.OCILayer)
	}

	r, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("could not get layer content: %v", err)
	}
	defer r.Close()

	dirs, err := extractTritonCacheDirectory(r)
	if err != nil {
		return nil, fmt.Errorf("could not extract Triton Kernel Cache: %v", err)
	}
	return dirs, nil
}

// Extracts the triton cache and manifest in a given reader for tar.gz.
// This is only used for *compat* variant.
func extractTritonCacheDirectory(r io.Reader) ([]string, error) {
	var extractedDirs []string
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layer as tar.gz: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// Ensure top-level output directories exist once
	if err = os.MkdirAll(constants.TritonCacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}
	if err = os.MkdirAll(constants.MCVManifestDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create manifest directory: %w", err)
	}

	for {
		h, ret := tr.Next()
		if ret == io.EOF {
			break
		} else if ret != nil {
			return nil, fmt.Errorf("error reading tar archive: %w", ret)
		}

		// Skip irrelevant files
		if !strings.HasPrefix(h.Name, constants.MCVTritonCacheDir) &&
			!strings.HasPrefix(h.Name, "io.triton.manifest/manifest.json") {
			continue
		}

		// Determine output path
		var filePath string
		if strings.HasPrefix(h.Name, constants.MCVTritonCacheDir) {
			rel := strings.TrimPrefix(h.Name, constants.MCVTritonCacheDir)
			if rel == "" {
				continue
			}
			filePath = filepath.Join(constants.TritonCacheDir, rel)

			topDir := filepath.Join(constants.TritonCacheDir, filepath.Dir(rel))
			if !stringInSlice(topDir, extractedDirs) {
				extractedDirs = append(extractedDirs, topDir)
			}
		} else if strings.HasPrefix(h.Name, "io.triton.manifest/") {
			rel := strings.TrimPrefix(h.Name, "io.triton.manifest/")
			filePath = filepath.Join(constants.MCVManifestDir, rel)
		}

		// Ensure parent dir exists
		if err = os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", filePath, err)
		}

		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(filePath, os.FileMode(h.Mode)); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", filePath, err)
			}
		case tar.TypeReg:
			if err = writeFile(filePath, tr, os.FileMode(h.Mode)); err != nil {
				return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
			}
		default:
			logging.Debugf("Skipping unsupported type: %c in file %s", h.Typeflag, h.Name)
		}
	}

	// Fix up cache JSONs
	err = filepath.Walk(constants.TritonCacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasPrefix(info.Name(), "__grp__") && strings.HasSuffix(info.Name(), ".json") {
			if err := utils.RestoreFullPathsInGroupJSON(path, constants.TritonCacheDir); err != nil {
				logging.Warnf("failed to restore full paths in %s: %v", path, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error restoring full paths in cache JSON files: %w", err)
	}

	return extractedDirs, nil
}

func writeFile(filePath string, tarReader io.Reader, mode os.FileMode) error {
	// Create any parent directories if needed
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directories for %s: %w", filePath, err)
	}

	outFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", filePath, err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, tarReader); err != nil {
		return fmt.Errorf("failed to copy content to file %s: %w", filePath, err)
	}

	if err := os.Chmod(filePath, mode); err != nil {
		return fmt.Errorf("failed to set file permissions for %s: %w", filePath, err)
	}

	return nil
}

func stringInSlice(str string, list []string) bool {
	for _, s := range list {
		if s == str {
			return true
		}
	}
	return false
}
