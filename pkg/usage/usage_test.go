package usage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	// TestDir is the temporary directory for storing files used during testing.
	TestDir = "/tmp/gkm"

	// TestUsageDir is the default root directory to store the expanded the GPU Kernel
	// images.
	TestUsageDir = "/tmp/gkm/usage"
)

func TestUsage(t *testing.T) {
	t.Run("Create and delete usage.json files", func(t *testing.T) {
		// Setup logging before anything else so code can log errors.
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))
		log := ctrl.Log.WithName("gkm-usage")

		// For Testing, override the location of the stored data
		initializeUsagePath(TestUsageDir)

		defer func() {
			t.Logf("TEST: Verify cleanup")
			err := cleanUp(TestDir, TestUsageDir, log)
			require.NoError(t, err)
		}()

		// Instance 1: Cluster Scoped
		u1 := UsageData{
			CrName:      "yellowKernel",
			CrNamespace: "",
			VolumeId:    []string{"csi-0123456789abcdef000000000000000000000000000000000000000000000001"},
			RefCount:    1,
			VolumeSize:  654321,
		}

		// Instance 2: Cluster Scoped - Same Cache as Instance 1, different Volume (Pod)
		u2 := UsageData{
			CrName:      "yellowKernel",
			CrNamespace: "",
			VolumeId: []string{
				"csi-0123456789abcdef000000000000000000000000000000000000000000000001",
				"csi-0123456789abcdef000000000000000000000000000000000000000000000002",
			},
			RefCount:   2,
			VolumeSize: 654321,
		}

		// Instance 3: Cluster Scoped - Unique
		u3 := UsageData{
			CrName:      "redKernel",
			CrNamespace: "",
			VolumeId:    []string{"csi-0123456789abcdef000000000000000000000000000000000000000000000003"},
			RefCount:    1,
			VolumeSize:  12345678,
		}

		// Instance 4: Namespace Scoped
		u4 := UsageData{
			CrName:      "blueKernel",
			CrNamespace: "blue",
			VolumeId:    []string{"csi-0123456789abcdef000000000000000000000000000000000000000000000004"},
			RefCount:    1,
			VolumeSize:  4444444,
		}

		// Instance 5: Namespace Scoped - Same Cache as Instance 4, different Volume (Pod)
		u5 := UsageData{
			CrName:      "blueKernel",
			CrNamespace: "blue",
			VolumeId: []string{
				"csi-0123456789abcdef000000000000000000000000000000000000000000000004",
				"csi-0123456789abcdef000000000000000000000000000000000000000000000005",
			},
			RefCount:   2,
			VolumeSize: 4444444,
		}

		// Instance 6: Namespace Scoped - Unique
		u6 := UsageData{
			CrName:      "greenKernel",
			CrNamespace: "green",
			VolumeId:    []string{"csi-0123456789abcdef000000000000000000000000000000000000000000000006"},
			RefCount:    1,
			VolumeSize:  35648325,
		}

		// Instance 7: Cluster Scoped - Not Create
		u7 := UsageData{
			CrName:      "orangeKernel",
			CrNamespace: "",
			VolumeId:    []string{"csi-0123456789abcdef000000000000000000000000000000000000000000000007"},
			RefCount:    0,
			VolumeSize:  0,
		}

		// Instance 8: Namespace Scoped - Not Create
		u8 := UsageData{
			CrName:      "blackKernel",
			CrNamespace: "black",
			VolumeId:    []string{"csi-0123456789abcdef000000000000000000000000000000000000000000000008"},
			RefCount:    0,
			VolumeSize:  0,
		}

		// TEST: AddUsageData(), GetUsageData() and GetUsageDataByVolumeId()
		// TEST: Instance 1 - Cluster - Call AddUsageData() to create Instance 1 - Success.
		t.Logf("TEST: Instance 1 - Cluster - Calling AddUsageData() to Succeed")
		err := AddUsageData(u1.CrNamespace, u1.CrName, u1.VolumeId[0], u1.VolumeSize, log)
		t.Logf("Instance 1 - Cluster - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 1 - Cluster - Calling GetUsageData() to verify data")
		usageData, err := GetUsageData(u1.CrNamespace, u1.CrName, log)
		t.Logf("Instance 1 - Cluster - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u1, usageData)

		t.Logf("Instance 1 - Cluster - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u1.VolumeId[0], log)
		t.Logf("Instance 1 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u1, usageData)

		// TEST: Instance 2 - Cluster - Call AddUsageData() to create Instance 2 (same as Instance 1) - Success.
		t.Logf("TEST: Instance 2 - Cluster - Calling AddUsageData() to Succeed")
		err = AddUsageData(u2.CrNamespace, u2.CrName, u2.VolumeId[1], u2.VolumeSize, log)
		t.Logf("Instance 2 - Cluster - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 2 - Cluster - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u2.CrNamespace, u2.CrName, log)
		t.Logf("Instance 2 - Cluster - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u2, usageData)

		t.Logf("Instance 1 - Cluster - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u2.VolumeId[0], log)
		t.Logf("Instance 1 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u2, usageData)

		t.Logf("Instance 2 - Cluster - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u2.VolumeId[1], log)
		t.Logf("Instance 2 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u2, usageData)

		// TEST: Instance 3 - Cluster - Call AddUsageData() to create Instance 3 - Success.
		t.Logf("TEST: Instance 3 - Cluster - Calling AddUsageData() to Succeed")
		err = AddUsageData(u3.CrNamespace, u3.CrName, u3.VolumeId[0], u3.VolumeSize, log)
		t.Logf("Instance 3 - Cluster - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 3 - Cluster - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u3.CrNamespace, u3.CrName, log)
		t.Logf("Instance 3 - Cluster - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u3, usageData)

		t.Logf("Instance 3 - Cluster - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u3.VolumeId[0], log)
		t.Logf("Instance 3 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u3, usageData)

		// TEST: Instance 4 - Namespaced - Call AddUsageData() to create Instance 1 - Success.
		t.Logf("TEST: Instance 4 - Namespaced - Calling AddUsageData() to Succeed")
		err = AddUsageData(u4.CrNamespace, u4.CrName, u4.VolumeId[0], u4.VolumeSize, log)
		t.Logf("Instance 4 - Namespaced - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u4.CrNamespace, u4.CrName, log)
		t.Logf("Instance 4 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u4, usageData)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u4.VolumeId[0], log)
		t.Logf("Instance 4 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u4, usageData)

		// TEST: Instance 5 - Namespaced - Call AddUsageData() to create Instance 5 (same as Instance 4) - Success.
		t.Logf("TEST: Instance 5 - Namespaced - Calling AddUsageData() to Succeed")
		err = AddUsageData(u5.CrNamespace, u5.CrName, u5.VolumeId[1], u5.VolumeSize, log)
		t.Logf("Instance 5 - Namespaced - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 5 - Namespaced - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u5.CrNamespace, u5.CrName, log)
		t.Logf("Instance 5 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u5, usageData)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u5.VolumeId[0], log)
		t.Logf("Instance 4 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u5, usageData)

		t.Logf("Instance 5 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u5.VolumeId[1], log)
		t.Logf("Instance 5 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u5, usageData)

		// TEST: Instance 6 - Namespaced - Call AddUsageData() to create Instance 6 - Success.
		t.Logf("TEST: Instance 6 - Namespaced - Calling AddUsageData() to Succeed")
		err = AddUsageData(u6.CrNamespace, u6.CrName, u6.VolumeId[0], u6.VolumeSize, log)
		t.Logf("Instance 6 - Namespaced - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u6.CrNamespace, u6.CrName, log)
		t.Logf("Instance 6 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u6, usageData)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u6.VolumeId[0], log)
		t.Logf("Instance 6 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u6, usageData)

		// TEST: ERROR TESTING
		// TEST: Instance 7 - Cluster - Instance Doesn't exist - Failure.
		t.Logf("TEST: Error - Calling GetUsageData() and GetUsageDataByVolumeId() on non-existent instances")
		t.Logf("Instance 7 - Cluster - Calling GetUsageData() to verify failure")
		_, err = GetUsageData(u7.CrNamespace, u7.CrName, log)
		t.Logf("Instance 7 - Cluster - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 7 - Cluster - Calling GetUsageDataByVolumeId() to verify failure")
		_, err = GetUsageDataByVolumeId(u7.VolumeId[0], log)
		t.Logf("Instance 7 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 8 - Namespaced - Instance Doesn't exist - Failure.
		t.Logf("Instance 8 - Namespaced - Calling GetUsageData() to verify failure")
		_, err = GetUsageData(u8.CrNamespace, u8.CrName, log)
		t.Logf("Instance 8 - Namespaced - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 8 - Namespaced - Calling GetUsageDataByVolumeId() to verify failure")
		_, err = GetUsageDataByVolumeId(u8.VolumeId[0], log)
		t.Logf("Instance 8 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Invalid Input
		t.Logf("TEST: Error - Calling AddUsageData() with No CR Name to verify failure")
		err = AddUsageData(u1.CrNamespace, "", u1.VolumeId[0], u1.VolumeSize, log)
		t.Logf("Instance 1 - Cluster - AddUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("TEST: Error - Calling AddUsageData() with No VolumeId to verify failure")
		err = AddUsageData(u5.CrNamespace, u5.CrNamespace, "", u1.VolumeSize, log)
		t.Logf("Instance 5 - Cluster - AddUsageData() err: %v", err)
		require.Error(t, err)

		// TEST: DeleteUsageData(), GetUsageData() and GetUsageDataByVolumeId()
		// TEST: Instance 2 - Cluster - Call DeleteUsageData() to delete Instance 2 - Success.
		t.Logf("TEST: Instance 2 - Cluster - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u2.VolumeId[1], log)
		t.Logf("Instance 2 - Cluster - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 2 - Cluster - Calling GetUsageData() to verify Success (Inst1 still exists)")
		usageData, err = GetUsageData(u2.CrNamespace, u2.CrName, log)
		t.Logf("Instance 2 - Cluster - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u1, usageData)

		t.Logf("Instance 2 - Cluster - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u2.VolumeId[1], log)
		t.Logf("Instance 2 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 1 - Cluster - Calling GetUsageDataByVolumeId() to verify Success")
		usageData, err = GetUsageDataByVolumeId(u2.VolumeId[0], log)
		t.Logf("Instance 1 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u1, usageData)

		// TEST: Instance 1 - Cluster - Call DeleteUsageData() to delete Instance 1 - Success.
		t.Logf("TEST: Instance 1 - Cluster - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u1.VolumeId[0], log)
		t.Logf("Instance 1 - Cluster - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 1 - Cluster - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u1.CrNamespace, u1.CrName, log)
		t.Logf("Instance 1 - Cluster - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 1 - Cluster - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u1.VolumeId[0], log)
		t.Logf("Instance 1 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 3 - Cluster - Call DeleteUsageData() to delete Instance 3 - Success.
		t.Logf("TEST: Instance 3 - Cluster - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u3.VolumeId[0], log)
		t.Logf("Instance 3 - Cluster - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 3 - Cluster - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u3.CrNamespace, u3.CrName, log)
		t.Logf("Instance 3 - Cluster - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 3 - Cluster - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u3.VolumeId[0], log)
		t.Logf("Instance 3 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 5 - Namespaced - Call DeleteUsageData() to delete Instance 5 - Success.
		t.Logf("TEST: Instance 5 - Namespaced - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u5.VolumeId[1], log)
		t.Logf("Instance 5 - Namespaced - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 5 - Namespaced - Calling GetUsageData() to verify Success (Inst4 still exists)")
		usageData, err = GetUsageData(u5.CrNamespace, u5.CrName, log)
		t.Logf("Instance 5 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u4, usageData)

		t.Logf("Instance 5 - Namespaced - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u5.VolumeId[1], log)
		t.Logf("Instance 5 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageDataByVolumeId() to verify Success")
		usageData, err = GetUsageDataByVolumeId(u4.VolumeId[0], log)
		t.Logf("Instance 4 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u4, usageData)

		// TEST: Instance 4 - Namespaced - Call DeleteUsageData() to delete Instance 4 - Success.
		t.Logf("TEST: Instance 4 - Namespaced - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u4.VolumeId[0], log)
		t.Logf("Instance 4 - Namespaced - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u4.CrNamespace, u4.CrName, log)
		t.Logf("Instance 4 - Namespaced - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u4.VolumeId[0], log)
		t.Logf("Instance 4 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 6 - Namespaced - Call DeleteUsageData() to delete Instance 6 - Success.
		t.Logf("TEST: Instance 6 - Namespaced - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u6.VolumeId[0], log)
		t.Logf("Instance 6 - Namespaced - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u6.CrNamespace, u3.CrName, log)
		t.Logf("Instance 6 - Namespaced - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u6.VolumeId[0], log)
		t.Logf("Instance 6 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)
	})
}

func cleanUp(testDir string, testUsageDir string, log logr.Logger) error {
	// Make sure all the files were deleted. Directory should be empty
	found := false
	_ = filepath.WalkDir(testUsageDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Error(err, "cleanUp(): WalkDir error", "Path", path)
			return err
		}

		if !d.IsDir() {
			log.Info("cleanUp(): Found file", "Path", path)
			found = true
		} else if path != testUsageDir {
			log.Info("cleanUp(): Found directory", "Path", path)
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
