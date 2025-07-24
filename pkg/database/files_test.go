package database

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/redhat-et/GKM/pkg/utils"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	// TestUsageRootDir is the temporary directory for storing files used during testing.
	TestFileRootDir = "/tmp/gkm-files"

	// TestUsageDir is the default root directory to store the expanded the GPU Kernel
	// images.
	TestFileDir = "/tmp/gkm-files/files"
)

func TestIsDirEmpty(t *testing.T) {
	t.Run("Test IsDirEmpty()", func(t *testing.T) {
		// Setup logging before anything else so code can log errors.
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))
		log := ctrl.Log.WithName("utils-files")

		defer func() {
			t.Logf("TEST: Verify cleanup")
			// Clean up
			err := os.RemoveAll(TestFileRootDir)
			require.NoError(t, err)
		}()

		// For Testing, override the location of the stored data
		initializeUsagePath(TestFileDir)

		// Test Data
		namespace := "blue"
		name := "blueKernel"
		digest := "1234567890"
		volumeId := "adbcef-123456"
		digest2 := "0123456789"

		// Test an non-existent directory, nothing exists so internal error, returns not empty
		t.Logf("TEST: 1 - Nonexistent Directory - Should return empty")
		empty := IsDirEmpty(TestFileDir, "")
		require.Equal(t, empty, true)

		// Create a directory with files
		err := AddUsageData(namespace, name, digest, volumeId, 100, log)
		require.NoError(t, err)
		err = AddUsageData(namespace, name, digest2, volumeId, 100, log)
		require.NoError(t, err)

		// Test a top-level directory, sub-directories exist so should not be empty
		t.Logf("TEST: 2 - Root Directory - Should return NOT empty - %s", TestFileDir)
		empty = IsDirEmpty(TestFileDir, "")
		require.Equal(t, empty, false)

		// Test directory with only files, files exist so should not be empty
		testDir, err := BuildDbDir(TestFileDir, namespace, name, digest, log)
		require.NoError(t, err)

		// Test directory with only files, files exist so should not be empty
		t.Logf("TEST: 3 - Base Directory - Should return NOT empty - %s", testDir)
		empty = IsDirEmpty(testDir, "")
		require.Equal(t, empty, false)

		// Test directory with only files, but ignore file, so should be empty
		t.Logf("TEST: 4 - Base Directory but ignore file  - Should return empty - %s Ignore File: %s", testDir, utils.UsageFilename)
		empty = IsDirEmpty(testDir, utils.UsageFilename)
		require.Equal(t, empty, true)

		// Test directory with only files, but ignore file, so should be empty
		t.Logf("TEST: 5 - Base Directory and bad ignore file  - Should return NOT empty - %s Ignore File: %s", testDir, utils.CacheFilename)
		empty = IsDirEmpty(testDir, utils.CacheFilename)
		require.Equal(t, empty, false)
	})
}

func testCleanUpWithVerification(testDir string, testUsageDir string, log logr.Logger) error {
	// Make sure all the files were deleted. Directory should be empty
	found := false
	_ = filepath.WalkDir(testUsageDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Error(err, "testCleanUpWithVerification(): WalkDir error", "Path", path)
			return err
		}

		if !d.IsDir() {
			log.Info("testCleanUpWithVerification(): Found file", "Path", path)
			found = true
		} else if path != testUsageDir {
			log.Info("testCleanUpWithVerification(): Found directory", "Path", path)
			found = true
		}
		return nil
	})

	err := os.RemoveAll(testDir)
	if err != nil {
		log.Info("Error cleaning up test data", "testDir", testDir, "err", err)
	}

	_, err = os.ReadDir(testDir)
	if err != nil {
		if os.IsNotExist(err) {
			if found {
				log.Info("Test cases left data behind, but cleaned up!")
				return fmt.Errorf("test data not fully removed by test cases")
			} else {
				log.Info("Test data cleaned up!")
				return nil
			}
		} else {
			log.Info("Error reading test data", "testDir", testDir, "err", err)
			return err
		}
	}
	return fmt.Errorf("test data not fully cleaned up")
}
