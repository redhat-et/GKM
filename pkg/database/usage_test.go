package database

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gkmv1alpha1 "github.com/redhat-et/GKM/api/v1alpha1"
)

const (
	// TestUsageRootDir is the temporary directory for storing files used during testing.
	TestUsageRootDir = "/tmp/gkm-usage"

	// TestUsageDir is the default root directory to store the expanded the GPU Kernel
	// images.
	TestUsageDir = "/tmp/gkm-usage/usage"
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
			err := testCleanUpWithVerification(TestUsageRootDir, TestUsageDir, log)
			require.NoError(t, err)
		}()

		// Instance 1: Cluster Scoped
		u1 := UsageData{
			CrNamespace: "",
			CrName:      "yellowKernel",
			Digest:      "1111111111111111111111111111111111111111111111111111111111111111",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "yellow",
					PodName:      "yellow-1",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000001",
				},
			},
			RefCount:   1,
			VolumeSize: 654321,
		}

		// Instance 2: Cluster Scoped - Same Cache as Instance 1, different Volume (Pod)
		u2 := UsageData{
			CrNamespace: "",
			CrName:      "yellowKernel",
			Digest:      "1111111111111111111111111111111111111111111111111111111111111111",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "yellow",
					PodName:      "yellow-1",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000001",
				},
				{
					PodNamespace: "yellow",
					PodName:      "yellow-2",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000002",
				},
			},
			RefCount:   2,
			VolumeSize: 654321,
		}

		// Instance 3: Cluster Scoped - Unique
		u3 := UsageData{
			CrNamespace: "",
			CrName:      "redKernel",
			Digest:      "3333333333333333333333333333333333333333333333333333333333333333",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "red",
					PodName:      "red-3",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000003",
				},
			},
			RefCount:   1,
			VolumeSize: 12345678,
		}

		// Instance 4: Namespace Scoped
		u4 := UsageData{
			CrNamespace: "blue",
			CrName:      "blueKernel",
			Digest:      "4444444444444444444444444444444444444444444444444444444444444444",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "blue",
					PodName:      "blue-4",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000004",
				},
			},
			RefCount:   1,
			VolumeSize: 4444444,
		}

		// Instance 5: Namespace Scoped - Same Cache as Instance 4, different Volume (Pod)
		u5 := UsageData{
			CrNamespace: "blue",
			CrName:      "blueKernel",
			Digest:      "4444444444444444444444444444444444444444444444444444444444444444",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "blue",
					PodName:      "blue-4",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000004",
				},
				{
					PodNamespace: "blue",
					PodName:      "blue-5",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000005",
				},
			},
			RefCount:   2,
			VolumeSize: 4444444,
		}

		// Instance 6: Namespace Scoped - Unique
		u6 := UsageData{
			CrNamespace: "green",
			CrName:      "greenKernel",
			Digest:      "6666666666666666666666666666666666666666666666666666666666666666",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "green",
					PodName:      "green-6",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000006",
				},
			},
			RefCount:   1,
			VolumeSize: 35648325,
		}

		// Instance 7: Namespace Scoped - Same Custom Resource, Different Digest
		u7 := UsageData{
			CrNamespace: "green",
			CrName:      "greenKernel",
			Digest:      "7777777777777777777777777777777777777777777777777777777777777777",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "green",
					PodName:      "green-7",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000007",
				},
			},
			RefCount:   1,
			VolumeSize: 35648325,
		}

		// Instance 8: Cluster Scoped - Not Create
		u8 := UsageData{
			CrNamespace: "",
			CrName:      "orangeKernel",
			Digest:      "8888888888888888888888888888888888888888888888888888888888888888",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "orange",
					PodName:      "orange-8",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000008",
				},
			},
			RefCount:   0,
			VolumeSize: 0,
		}

		// Instance 9: Namespace Scoped - Not Create
		u9 := UsageData{
			CrNamespace: "black",
			CrName:      "blackKernel",
			Digest:      "9999999999999999999999999999999999999999999999999999999999999999",
			Pods: []gkmv1alpha1.PodData{
				{
					PodNamespace: "black",
					PodName:      "black-9",
					VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000009",
				},
			},
			RefCount:   0,
			VolumeSize: 0,
		}

		// TEST: Invalid Input
		t.Logf("TEST: Error - Calling AddUsageData() with No CR Name to verify failure")
		err := AddUsageData(
			u1.CrNamespace,
			"",
			u1.Digest,
			u1.Pods[0].VolumeId,
			u1.Pods[0].PodNamespace,
			u1.Pods[0].PodName,
			u1.VolumeSize,
			log)
		t.Logf("Instance 1 - Cluster - AddUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("TEST: Error - Calling AddUsageData() with No Digest to verify failure")
		err = AddUsageData(
			u5.CrNamespace,
			"",
			u5.Digest,
			u5.Pods[0].VolumeId,
			u5.Pods[0].PodNamespace,
			u5.Pods[0].PodName,
			u5.VolumeSize,
			log)
		t.Logf("Instance 5 - Cluster - AddUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("TEST: Error - Calling AddUsageData() with No VolumeId to verify failure")
		err = AddUsageData(
			u5.CrNamespace,
			u5.CrNamespace,
			u5.Digest,
			"",
			u5.Pods[0].PodNamespace,
			u5.Pods[0].PodName,
			u5.VolumeSize,
			log)
		t.Logf("Instance 5 - Cluster - AddUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("TEST: Error - Calling  DeleteUsageData() on Empty Directory")
		err = DeleteUsageData(u1.Pods[0].VolumeId, log)
		t.Logf("Instance 1 - Cluster - DeleteUsageData() err: %v", err)
		require.Error(t, err)

		// TEST: AddUsageData(), GetUsageData() and GetUsageDataByVolumeId()
		// TEST: Instance 1 - Cluster - Call AddUsageData() to create Instance 1 - Success.
		t.Logf("TEST: Instance 1 - Cluster - Calling AddUsageData() to Succeed")
		err = AddUsageData(
			u1.CrNamespace,
			u1.CrName,
			u1.Digest,
			u1.Pods[0].VolumeId,
			u1.Pods[0].PodNamespace,
			u1.Pods[0].PodName,
			u1.VolumeSize,
			log)
		t.Logf("Instance 1 - Cluster - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 1 - Cluster - Calling GetUsageData() to verify data")
		usageData, err := GetUsageData(u1.CrNamespace, u1.CrName, u1.Digest, log)
		t.Logf("Instance 1 - Cluster - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u1, usageData)

		t.Logf("Instance 1 - Cluster - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u1.Pods[0].VolumeId, log)
		t.Logf("Instance 1 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u1, usageData)

		// TEST: Instance 2 - Cluster - Call AddUsageData() to create Instance 2 (same as Instance 1) - Success.
		t.Logf("TEST: Instance 2 - Cluster - Calling AddUsageData() to Succeed")
		err = AddUsageData(
			u2.CrNamespace,
			u2.CrName,
			u2.Digest,
			u2.Pods[1].VolumeId,
			u2.Pods[1].PodNamespace,
			u2.Pods[1].PodName,
			u2.VolumeSize,
			log)
		t.Logf("Instance 2 - Cluster - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 2 - Cluster - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u2.CrNamespace, u2.CrName, u2.Digest, log)
		t.Logf("Instance 2 - Cluster - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u2, usageData)

		t.Logf("Instance 1 - Cluster - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u2.Pods[0].VolumeId, log)
		t.Logf("Instance 1 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u2, usageData)

		t.Logf("Instance 2 - Cluster - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u2.Pods[1].VolumeId, log)
		t.Logf("Instance 2 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u2, usageData)

		// TEST: Instance 3 - Cluster - Call AddUsageData() to create Instance 3 - Success.
		t.Logf("TEST: Instance 3 - Cluster - Calling AddUsageData() to Succeed")
		err = AddUsageData(
			u3.CrNamespace,
			u3.CrName,
			u3.Digest,
			u3.Pods[0].VolumeId,
			u3.Pods[0].PodNamespace,
			u3.Pods[0].PodName,
			u3.VolumeSize,
			log)
		t.Logf("Instance 3 - Cluster - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 3 - Cluster - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u3.CrNamespace, u3.CrName, u3.Digest, log)
		t.Logf("Instance 3 - Cluster - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u3, usageData)

		t.Logf("Instance 3 - Cluster - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u3.Pods[0].VolumeId, log)
		t.Logf("Instance 3 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u3, usageData)

		// TEST: Instance 4 - Namespaced - Call AddUsageData() to create Instance 1 - Success.
		t.Logf("TEST: Instance 4 - Namespaced - Calling AddUsageData() to Succeed")
		err = AddUsageData(
			u4.CrNamespace,
			u4.CrName,
			u4.Digest,
			u4.Pods[0].VolumeId,
			u4.Pods[0].PodNamespace,
			u4.Pods[0].PodName,
			u4.VolumeSize,
			log)
		t.Logf("Instance 4 - Namespaced - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u4.CrNamespace, u4.CrName, u4.Digest, log)
		t.Logf("Instance 4 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u4, usageData)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u4.Pods[0].VolumeId, log)
		t.Logf("Instance 4 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u4, usageData)

		// TEST: Instance 5 - Namespaced - Call AddUsageData() to create Instance 5 (same as Instance 4) - Success.
		t.Logf("TEST: Instance 5 - Namespaced - Calling AddUsageData() to Succeed")
		err = AddUsageData(
			u5.CrNamespace,
			u5.CrName,
			u5.Digest,
			u5.Pods[1].VolumeId,
			u5.Pods[1].PodNamespace,
			u5.Pods[1].PodName,
			u5.VolumeSize,
			log)
		t.Logf("Instance 5 - Namespaced - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 5 - Namespaced - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u5.CrNamespace, u5.CrName, u5.Digest, log)
		t.Logf("Instance 5 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u5, usageData)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u5.Pods[0].VolumeId, log)
		t.Logf("Instance 4 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u5, usageData)

		t.Logf("Instance 5 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u5.Pods[1].VolumeId, log)
		t.Logf("Instance 5 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u5, usageData)

		// TEST: Instance 6 - Namespaced - Call AddUsageData() to create Instance 6 - Success.
		t.Logf("TEST: Instance 6 - Namespaced - Calling AddUsageData() to Succeed")
		err = AddUsageData(
			u6.CrNamespace,
			u6.CrName,
			u6.Digest,
			u6.Pods[0].VolumeId,
			u6.Pods[0].PodNamespace,
			u6.Pods[0].PodName,
			u6.VolumeSize,
			log)
		t.Logf("Instance 6 - Namespaced - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u6.CrNamespace, u6.CrName, u6.Digest, log)
		t.Logf("Instance 6 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u6, usageData)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u6.Pods[0].VolumeId, log)
		t.Logf("Instance 6 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u6, usageData)

		// TEST: Instance 7 - Namespaced - Call AddUsageData() to create Instance 7 - Success.
		t.Logf("TEST: Instance 7 - Namespaced - Calling AddUsageData() to Succeed")
		err = AddUsageData(
			u7.CrNamespace,
			u7.CrName,
			u7.Digest,
			u7.Pods[0].VolumeId,
			u7.Pods[0].PodNamespace,
			u7.Pods[0].PodName,
			u7.VolumeSize,
			log)
		t.Logf("Instance 7 - Namespaced - AddUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 7 - Namespaced - Calling GetUsageData() to verify data")
		usageData, err = GetUsageData(u7.CrNamespace, u7.CrName, u7.Digest, log)
		t.Logf("Instance 7 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u7, usageData)

		t.Logf("Instance 7 - Namespaced - Calling GetUsageDataByVolumeId() to verify data")
		usageData, err = GetUsageDataByVolumeId(u7.Pods[0].VolumeId, log)
		t.Logf("Instance 7 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u7, usageData)

		// TEST: ERROR TESTING
		// TEST: Instance 8 - Cluster - Instance Doesn't exist - Failure.
		t.Logf("TEST: Error - Calling GetUsageData() and GetUsageDataByVolumeId() on non-existent instances")
		t.Logf("Instance 8 - Cluster - Calling GetUsageData() to verify failure")
		_, err = GetUsageData(u8.CrNamespace, u8.CrName, u8.Digest, log)
		t.Logf("Instance 8 - Cluster - GetUsageData() err: %v", err)
		require.Error(t, err)
		t.Logf("Instance 8 - Cluster - Calling GetUsageDataByVolumeId() to verify failure")
		_, err = GetUsageDataByVolumeId(u8.Pods[0].VolumeId, log)
		t.Logf("Instance 8 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 9 - Namespaced - Instance Doesn't exist - Failure.
		t.Logf("Instance 9 - Namespaced - Calling GetUsageData() to verify failure")
		_, err = GetUsageData(u9.CrNamespace, u9.CrName, u8.Digest, log)
		t.Logf("Instance 9 - Namespaced - GetUsageData() err: %v", err)
		require.Error(t, err)
		t.Logf("Instance 9 - Namespaced - Calling GetUsageDataByVolumeId() to verify failure")
		_, err = GetUsageDataByVolumeId(u9.Pods[0].VolumeId, log)
		t.Logf("Instance 9 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: DeleteUsageData(), GetUsageData() and GetUsageDataByVolumeId()
		// TEST: Instance 2 - Cluster - Call DeleteUsageData() to delete Instance 2 - Success.
		t.Logf("TEST: Instance 2 - Cluster - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u2.Pods[1].VolumeId, log)
		t.Logf("Instance 2 - Cluster - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 2 - Cluster - Calling GetUsageData() to verify Success (Inst1 still exists)")
		usageData, err = GetUsageData(u2.CrNamespace, u2.CrName, u2.Digest, log)
		t.Logf("Instance 2 - Cluster - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u1, usageData)

		t.Logf("Instance 2 - Cluster - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u2.Pods[1].VolumeId, log)
		t.Logf("Instance 2 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 1 - Cluster - Calling GetUsageDataByVolumeId() to verify Success")
		usageData, err = GetUsageDataByVolumeId(u2.Pods[0].VolumeId, log)
		t.Logf("Instance 1 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u1, usageData)

		// TEST: Instance 1 - Cluster - Call DeleteUsageData() to delete Instance 1 - Success.
		t.Logf("TEST: Instance 1 - Cluster - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u1.Pods[0].VolumeId, log)
		t.Logf("Instance 1 - Cluster - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 1 - Cluster - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u1.CrNamespace, u1.CrName, u1.Digest, log)
		t.Logf("Instance 1 - Cluster - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 1 - Cluster - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u1.Pods[0].VolumeId, log)
		t.Logf("Instance 1 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 3 - Cluster - Call DeleteUsageData() to delete Instance 3 - Success.
		t.Logf("TEST: Instance 3 - Cluster - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u3.Pods[0].VolumeId, log)
		t.Logf("Instance 3 - Cluster - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 3 - Cluster - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u3.CrNamespace, u3.CrName, u3.Digest, log)
		t.Logf("Instance 3 - Cluster - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 3 - Cluster - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u3.Pods[0].VolumeId, log)
		t.Logf("Instance 3 - Cluster - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 5 - Namespaced - Call DeleteUsageData() to delete Instance 5 - Success.
		t.Logf("TEST: Instance 5 - Namespaced - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u5.Pods[1].VolumeId, log)
		t.Logf("Instance 5 - Namespaced - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 5 - Namespaced - Calling GetUsageData() to verify Success (Inst4 still exists)")
		usageData, err = GetUsageData(u5.CrNamespace, u5.CrName, u5.Digest, log)
		t.Logf("Instance 5 - Namespaced - GetUsageData() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u4, usageData)

		t.Logf("Instance 5 - Namespaced - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u5.Pods[1].VolumeId, log)
		t.Logf("Instance 5 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageDataByVolumeId() to verify Success")
		usageData, err = GetUsageDataByVolumeId(u4.Pods[0].VolumeId, log)
		t.Logf("Instance 4 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.NoError(t, err)
		require.Equal(t, &u4, usageData)

		// TEST: Instance 4 - Namespaced - Call DeleteUsageData() to delete Instance 4 - Success.
		t.Logf("TEST: Instance 4 - Namespaced - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u4.Pods[0].VolumeId, log)
		t.Logf("Instance 4 - Namespaced - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u4.CrNamespace, u4.CrName, u4.Digest, log)
		t.Logf("Instance 4 - Namespaced - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 4 - Namespaced - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u4.Pods[0].VolumeId, log)
		t.Logf("Instance 4 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 6 - Namespaced - Call DeleteUsageData() to delete Instance 6 - Success.
		t.Logf("TEST: Instance 6 - Namespaced - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u6.Pods[0].VolumeId, log)
		t.Logf("Instance 6 - Namespaced - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u6.CrNamespace, u6.CrName, u6.Digest, log)
		t.Logf("Instance 6 - Namespaced - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u6.Pods[0].VolumeId, log)
		t.Logf("Instance 6 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)

		// TEST: Instance 7 - Namespaced - Call DeleteUsageData() to delete Instance 7 - Success.
		t.Logf("TEST: Instance 7 - Namespaced - Calling DeleteUsageData() to Succeed")
		err = DeleteUsageData(u7.Pods[0].VolumeId, log)
		t.Logf("Instance 7 - Namespaced - DeleteUsageData() err: %v", err)
		require.NoError(t, err)

		t.Logf("Instance 6 - Namespaced - Calling GetUsageData() to verify Failure")
		_, err = GetUsageData(u7.CrNamespace, u7.CrName, u7.Digest, log)
		t.Logf("Instance 7 - Namespaced - GetUsageData() err: %v", err)
		require.Error(t, err)

		t.Logf("Instance 7 - Namespaced - Calling GetUsageDataByVolumeId() to verify Failure")
		_, err = GetUsageDataByVolumeId(u7.Pods[0].VolumeId, log)
		t.Logf("Instance 7 - Namespaced - GetUsageDataByVolumeId() err: %v", err)
		require.Error(t, err)
	})
}
