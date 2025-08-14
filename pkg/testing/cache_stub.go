package testing

import (
	"os"
	"path/filepath"

	"github.com/go-logr/logr"

	"github.com/redhat-et/GKM/pkg/database"
)

func ExtractCacheImage(cacheDir, crNamespace, crName, digest, image string, log logr.Logger) error {
	outputDir, err := database.BuildDbDir(cacheDir, crNamespace, crName, digest, log)
	if err != nil {
		return err
	}

	// Directory 1
	sampleDir := filepath.Join(outputDir, "CETLGDE7YAKGU4FRJ26IM6S47TFSIUU7KWBWDR3H2K3QRNRABUCA")
	err = os.MkdirAll(sampleDir, 0755)
	if err != nil {
		return err
	}
	sampleFile := filepath.Join(sampleDir, "__triton_launcher.so")
	file, err := os.Create(sampleFile)
	if err != nil {
		return err
	}
	file.Close()

	// Directory 2
	sampleDir = filepath.Join(outputDir, "CHN6BLIJ7AJJRKY2IETERW2O7JXTFBUD3PH2WE3USNVKZEKXG64Q")
	err = os.MkdirAll(sampleDir, 0755)
	if err != nil {
		return err
	}
	sampleFile = filepath.Join(sampleDir, "hip_utils.so")
	file, err = os.Create(sampleFile)
	if err != nil {
		return err
	}
	file.Close()

	// Directory 3
	sampleDir = filepath.Join(outputDir, "MCELTMXFCSPAMZYLZ3C3WPPYYVTVR4QOYNE52X3X6FIH7Z6N6X5A")
	err = os.MkdirAll(sampleDir, 0755)
	if err != nil {
		return err
	}
	sampleFile = filepath.Join(sampleDir, "__grp__add_kernel.json")
	file, err = os.Create(sampleFile)
	if err != nil {
		return err
	}
	file.Close()
	sampleFile = filepath.Join(sampleDir, "add_kernel.amdgcn")
	file, err = os.Create(sampleFile)
	if err != nil {
		return err
	}
	file.Close()

	// Directory 4
	sampleDir = filepath.Join(outputDir, "c4d45c651d6ac181a78d8d2f3ead424b8b8f07dd23dc3de0a99f425d8a633fc6")
	err = os.MkdirAll(sampleDir, 0755)
	if err != nil {
		return err
	}
	sampleFile = filepath.Join(sampleDir, "hip_utils.so")
	file, err = os.Create(sampleFile)
	if err != nil {
		return err
	}
	file.Close()

	if err := database.ExportForTestWriteCacheFile(crNamespace, crName, image, digest, false, 45000, log); err != nil {
		return err
	}

	return nil
}
