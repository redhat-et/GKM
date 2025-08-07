package database

import (
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	// TestCacheRootDir is the temporary directory for storing files used during testing.
	TestCacheRootDir = "/tmp/gkm-cache"

	// TestCacheDir is the default root directory to store the expanded the GPU Kernel
	// images.
	TestCacheDir = "/tmp/gkm-cache/caches"
)

type TestData struct {
	CrNamespace string
	CrName      string
	Digest      string
	Image       string
}

func TestReplaceUrlTag(t *testing.T) {
	t.Run("Test replacing tag with digest in URL", func(t *testing.T) {
		// Setup logging before anything else so code can log errors.
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))

		t.Logf("TEST: replaceUrlTag() with typical case - Should Succeed")
		updateUrl := replaceUrlTag("quay.io/test/image:latest", "01234567879")
		require.Equal(t, updateUrl, "quay.io/test/image@sha256:01234567879")

		t.Logf("TEST: replaceUrlTag() with tag with different chars - Should Succeed")
		updateUrl = replaceUrlTag("quay.io/test/image:v12-34", "22ae699979cf")
		require.Equal(t, updateUrl, "quay.io/test/image@sha256:22ae699979cf")

		t.Logf("TEST: replaceUrlTag() with no tag on URL - Should Succeed")
		updateUrl = replaceUrlTag("quay.io/test-33/image", "30c8d86826e6")
		require.Equal(t, updateUrl, "quay.io/test-33/image@sha256:30c8d86826e6")

		t.Logf("TEST: replaceUrlTag() with no image - Should fail (empty string)")
		updateUrl = replaceUrlTag("", "30c8d86826e6")
		require.Equal(t, updateUrl, "")

		t.Logf("TEST: replaceUrlTag() with no digest - Should fail (empty string)")
		updateUrl = replaceUrlTag("quay.io/test-33/image", "")
		require.Equal(t, updateUrl, "")
	})
}

func TestExtractCache(t *testing.T) {
	t.Run("Test extracting, reading and removing cache", func(t *testing.T) {
		// Setup logging before anything else so code can log errors.
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))
		log := ctrl.Log.WithName("common")

		// For Testing, override the location of the stored data
		initializeCachePath(TestCacheDir)

		defer func() {
			t.Logf("TEST: Verify cleanup")
			err := testCleanUpWithVerification(TestCacheRootDir, TestCacheDir, log)
			require.NoError(t, err)
		}()

		noGpu := true

		// Instance 1: Cluster Scoped
		t1 := TestData{
			CrNamespace: "",
			CrName:      "yellowKernel",
			Digest:      "d9cfcee43b201e1616487c15c74b7fcb387086e35feb545c4fb9126f51a20770",
			Image:       "quay.io/gkm/vector-add-cache:rocm",
		}

		// Instance 2: Cluster Scoped
		t2 := TestData{
			CrNamespace: "",
			CrName:      "Red.Kernel",
			Digest:      "d9cfcee43b201e1616487c15c74b7fcb387086e35feb545c4fb9126f51a20770",
			Image:       "quay.io/gkm/vector-add-cache:rocm",
		}

		// Instance 3: Namespace Scoped
		t3 := TestData{
			CrNamespace: "blue",
			CrName:      "blue_Kernel",
			Digest:      "d9cfcee43b201e1616487c15c74b7fcb387086e35feb545c4fb9126f51a20770",
			Image:       "quay.io/gkm/vector-add-cache:rocm",
		}

		// Instance 4: Namespace Scoped - Same Namespace, Different Name
		t4 := TestData{
			CrNamespace: "blue",
			CrName:      "light-Blue-Kernel",
			Digest:      "d9cfcee43b201e1616487c15c74b7fcb387086e35feb545c4fb9126f51a20770",
			Image:       "quay.io/gkm/vector-add-cache:rocm",
		}

		// Instance 5: Namespace Scoped - Unique
		t5 := TestData{
			CrNamespace: "green",
			CrName:      "greenKernel",
			Digest:      "d9cfcee43b201e1616487c15c74b7fcb387086e35feb545c4fb9126f51a20770",
			Image:       "quay.io/gkm/vector-add-cache:rocm",
		}

		// Instance 5: Namespace Scoped - Not loaded
		t6 := TestData{
			CrNamespace: "purple",
			CrName:      "purpleKernel",
			Digest:      "d9cfcee43b201e1616487c15c74b7fcb387086e35feb545c4fb9126f51a20770",
			Image:       "quay.io/gkm/vector-add-cache:rocm",
		}

		// TEST: No Instances - Call GetInstalledCacheList() to retrieve list - Success.
		t.Logf("TEST: No Instance Calling GetInstalledCacheList() to Succeed")
		installedList, err := GetInstalledCacheList(log)
		t.Logf("Instance 1 - Cluster - GetInstalledCacheList() err: %v currList: %v", err, installedList)
		require.NoError(t, err)
		// Find Instance should fail
		err = findInstance(t1, installedList, log)
		require.Error(t, err)
		require.Equal(t, len(*installedList), 0)

		// CREATE and READ Cache
		t.Logf("TEST: Instance 1 - Cluster - ExtractCache() - Should Succeed")
		err = ExtractCache(t1.CrNamespace, t1.CrName, t1.Image, t1.Digest, noGpu, log)
		t.Logf("Instance 1 - Cluster - ExtractCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t1, installedList, log)
		require.NoError(t, err)

		t.Logf("TEST: Instance 2 - Cluster - ExtractCache() - Same Image but different Name - Should Succeed")
		err = ExtractCache(t2.CrNamespace, t2.CrName, t2.Image, t2.Digest, noGpu, log)
		t.Logf("Instance 2 - Cluster - ExtractCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t2, installedList, log)
		require.NoError(t, err)

		t.Logf("TEST: Instance 3 - Namespace - ExtractCache() - Should Succeed")
		err = ExtractCache(t3.CrNamespace, t3.CrName, t3.Image, t3.Digest, noGpu, log)
		t.Logf("Instance 3 - Namespace - ExtractCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t3, installedList, log)
		require.NoError(t, err)

		t.Logf("TEST: Instance 4 - Namespace - ExtractCache() - Same Namespace but Different Name - Should Succeed")
		err = ExtractCache(t4.CrNamespace, t4.CrName, t4.Image, t4.Digest, noGpu, log)
		t.Logf("Instance 4 - Namespace - ExtractCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t4, installedList, log)
		require.NoError(t, err)

		t.Logf("TEST: Instance 5 - Namespace - ExtractCache() - Different Namespace - Should Succeed")
		err = ExtractCache(t5.CrNamespace, t5.CrName, t5.Image, t5.Digest, noGpu, log)
		t.Logf("Instance 5 - Namespace - ExtractCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t5, installedList, log)
		require.NoError(t, err)

		// TEST: Nonexistent Instances - Call GetInstalledCacheList() to retrieve list - Success.
		t.Logf("TEST: Nonexistent Instance Calling GetInstalledCacheList() to Succeed")
		installedList, err = GetInstalledCacheList(log)
		t.Logf("Nonexistent Instance - Namespace - GetInstalledCacheList() err: %v currList: %v", err, installedList)
		require.NoError(t, err)
		// Find Instance should fail
		err = findInstance(t6, installedList, log)
		require.Error(t, err)

		// DELETE Cache
		t.Logf("TEST: Instance 1 - Cluster - RemoveCache() - Should Succeed")
		_, err = RemoveCache(t1.CrNamespace, t1.CrName, t1.Digest, log)
		t.Logf("Instance 1 - Cluster - RemoveCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t1, installedList, log)
		require.Error(t, err)

		t.Logf("TEST: Instance 2 - Cluster - RemoveCache() - Should Succeed")
		_, err = RemoveCache(t2.CrNamespace, t2.CrName, t2.Digest, log)
		t.Logf("Instance 2 - Cluster - RemoveCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t2, installedList, log)
		require.Error(t, err)

		t.Logf("TEST: Instance 3 - Namespace - RemoveCache() - Should Succeed")
		_, err = RemoveCache(t3.CrNamespace, t3.CrName, t3.Digest, log)
		t.Logf("Instance 3 - Namespace - RemoveCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t3, installedList, log)
		require.Error(t, err)

		t.Logf("TEST: Instance 4 - Namespace - RemoveCache() - Should Succeed")
		_, err = RemoveCache(t4.CrNamespace, t4.CrName, t4.Digest, log)
		t.Logf("Instance 4 - Namespace - RemoveCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t4, installedList, log)
		require.Error(t, err)

		t.Logf("TEST: Instance 5 - Namespace - RemoveCache() - Should Succeed")
		_, err = RemoveCache(t5.CrNamespace, t5.CrName, t5.Digest, log)
		t.Logf("Instance 5 - Namespace - RemoveCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t5, installedList, log)
		require.Error(t, err)

		t.Logf("TEST: Instance 6 - Namespace - RemoveCache() - Nonexistent Cache - Should Fail")
		_, err = RemoveCache(t6.CrNamespace, t6.CrName, t6.Digest, log)
		t.Logf("Instance 6 - Namespace - RemoveCache() err: %v", err)
		require.NoError(t, err)
		// Read the filesystem to see if it was extracted
		installedList, err = GetInstalledCacheList(log)
		require.NoError(t, err)
		err = findInstance(t6, installedList, log)
		require.Error(t, err)
		require.Equal(t, len(*installedList), 0)
	})
}

/*
func createImageFiles(cacheDir, namespace, kernelName, digest string) error {
	outputDir := cacheDir

	if namespace == "" {
		namespace = utils.ClusterScopedSubDir
	}
	outputDir = filepath.Join(outputDir, namespace)

	if kernelName != "" {
		outputDir = filepath.Join(outputDir, kernelName)
	}

	if digest != "" {
		outputDir = filepath.Join(outputDir, digest)
	}

	// Directory 1
	sampleDir := filepath.Join(outputDir, "CETLGDE7YAKGU4FRJ26IM6S47TFSIUU7KWBWDR3H2K3QRNRABUCA")
	err := os.MkdirAll(sampleDir, 0755)
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

	return nil
}
*/

func findInstance(inst TestData, instMap *map[CacheKey]bool, log logr.Logger) error {
	key := CacheKey{
		Namespace: inst.CrNamespace,
		Name:      inst.CrName,
		Digest:    inst.Digest,
	}
	_, ok := (*instMap)[key]

	if ok {
		return nil
	} else {
		return fmt.Errorf("not found")
	}
}
