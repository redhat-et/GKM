package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/redhat-et/GKM/pkg/database"
	gkmTesting "github.com/redhat-et/GKM/pkg/testing"
	"github.com/redhat-et/GKM/pkg/utils"
)

const (
	// TestDir is the temporary directory for storing files used during testing.
	TestDir = "/tmp/gkm-csi"

	// DefaultSocketFilename is the location of the Unix domain socket for this CSI driver
	// for Kubelet to send requests.
	TestSocketFilename string = "unix:///tmp/gkm-csi/notused.sock"

	// TestImagePort is the location of port the Image Server will listen on for GKM
	// to send requests.
	TestImagePort string = ":50051"

	// TestCacheDir is the default root directory to store the expanded the GPU Kernel
	// images.
	TestCacheDir = "/tmp/gkm-csi/caches"
)

type TestData struct {
	CrNamespace  string
	CrName       string
	Digest       string
	VolumeId     string
	Image        string
	PodNamespace string
	PodName      string
	PodUid       string
}

//go:generate go run -tags "testStubs"

func TestNodePublishVolume(t *testing.T) {
	t.Run("Publish a volume", func(t *testing.T) {
		// Setup logging before anything else so code can log errors.
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))
		log := ctrl.Log.WithName("gkm-csi-driver")

		// For Testing, override the location of the stored data
		database.ExportForTestInitializeCachePath(TestCacheDir)

		t.Logf("REMINDER: Use 'sudo env \"PATH=$PATH\" make test' so CSI can test mounting logic.")

		nodeName := "worker-1"
		gkmNamespace := "gkm-system"

		// Get a Driver instance that will be tested.
		d, err := NewDriver(log, nodeName, gkmNamespace, TestSocketFilename, TestCacheDir, true)
		require.NoError(t, err)

		defer func() {
			t.Logf("TEST: Verify cleanup")
			err := cleanUp(TestDir, d, log)
			require.NoError(t, err)
		}()

		// Instance 1: Cluster Scoped
		t1 := TestData{
			CrNamespace:  "",
			CrName:       "rocmKernel",
			Digest:       "1111111111111111111111111111111111111111111111111111111111111111",
			VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000001",
			Image:        "quay.io/test/tstImage1:latest",
			PodNamespace: "testpod1",
			PodName:      "testPod-1",
			PodUid:       "fedcba98-7654-3210-dead-000000000001",
		}

		// Instance 2: Namespace Scoped
		t2 := TestData{
			CrNamespace:  "testpod2",
			CrName:       "rocmKernel",
			Digest:       "2222222222222222222222222222222222222222222222222222222222222222",
			VolumeId:     "csi-0123456789abcdef000000000000000000000000000000000000000000000002",
			Image:        "quay.io/test/tstImage2:latest",
			PodNamespace: "testpod2",
			PodName:      "testPod-2",
			PodUid:       "fedcba98-7654-3210-dead-000000000002",
		}

		// Instance 1 - Populated the Request Structure for a NodePublishVolume() call.
		pubReq1, err := buildNodePublishVolumeRequest(t1)
		require.NoError(t, err)

		// TEST: Instance 1 - Call NodePublishVolume() where it is expected to fail because
		// the OCI Image has not been downloaded.
		t.Logf("TEST 1: Instance 1 - Calling NodePublishVolume() to Fail - Cache Not extracted yet")
		_, err = d.NodePublishVolume(context.Background(), pubReq1)
		t.Logf("Instance 1 - NodePublishVolume() err: %v", err)
		require.Error(t, err)

		// Instance 1 - Create files from the OCI Image download
		err = gkmTesting.ExtractCacheImage(TestCacheDir, t1.CrNamespace, t1.CrName, t1.Digest, t1.Image, log)
		require.NoError(t, err)

		// TEST: Instance 1 - Call NodePublishVolume() where it is expected to pass.
		t.Logf("TEST 2: Instance 1 - Calling NodePublishVolume() to Succeed")
		_, err = d.NodePublishVolume(context.Background(), pubReq1)
		t.Logf("Instance 1 - NodePublishVolume() err: %v", err)
		require.NoError(t, err)

		// Instance 2 - Populated the Request Structure for a NodePublishVolume() call.
		pubReq2, err := buildNodePublishVolumeRequest(t2)
		require.NoError(t, err)

		// Instance 2 - Create files from the OCI Image download
		err = gkmTesting.ExtractCacheImage(TestCacheDir, t2.CrNamespace, t2.CrName, t2.Digest, t2.Image, log)
		require.NoError(t, err)

		// TEST: Instance 2 - Call NodePublishVolume() where it is expected to pass.
		t.Logf("TEST 3: Instance 2 - Calling NodePublishVolume() to Succeed")
		_, err = d.NodePublishVolume(context.Background(), pubReq2)
		t.Logf("Instance 2 - NodePublishVolume() err: %v", err)
		require.NoError(t, err)

		// Instance 1 - Populated the Request Structure for a NodeUnpublishVolume() call.
		unpubReq1, err := buildNodeUnpublishVolumeRequest(t1)
		require.NoError(t, err)

		// TEST: Instance 1 - Call NodeUnpublishVolume() where it is expected to pass.
		t.Logf("TEST 4: Instance 1 - Calling NodeUnpublishVolume() to Succeed")
		_, err = d.NodeUnpublishVolume(context.Background(), unpubReq1)
		t.Logf("NodePublishVolume() err: %v", err)
		require.NoError(t, err)

		// TEST: Instance 1 - Call NodeUnpublishVolume() even though it is already deleted,
		// expected to pass.
		t.Logf("TEST 5: Instance 1 - Calling NodeUnpublishVolume() to Succeed")
		_, err = d.NodeUnpublishVolume(context.Background(), unpubReq1)
		t.Logf("NodePublishVolume() err: %v", err)
		require.NoError(t, err)

		// Instance 2 - Populated the Request Structure for a NodeUnpublishVolume() call.
		unpubReq2, err := buildNodeUnpublishVolumeRequest(t2)
		require.NoError(t, err)

		// TEST: Instance 2 - Call NodeUnpublishVolume() where it is expected to pass.
		t.Logf("TEST 6: Instance 2 - Calling NodeUnpublishVolume() to Succeed")
		_, err = d.NodeUnpublishVolume(context.Background(), unpubReq2)
		t.Logf("NodePublishVolume() err: %v", err)
		require.NoError(t, err)
	})
}

func buildNodePublishVolumeRequest(tstData TestData) (*csi.NodePublishVolumeRequest, error) {
	req := csi.NodePublishVolumeRequest{
		VolumeId:          tstData.VolumeId,
		StagingTargetPath: "",
		TargetPath:        TestDir + "/kubelet/pods/" + tstData.PodUid + "/volumes/kubernetes.io~csi/kernel-volume/mount",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
	}
	req.VolumeContext = make(map[string]string)
	req.VolumeContext["csi.storage.k8s.io/ephemeral"] = "true"
	req.VolumeContext["csi.storage.k8s.io/pod.namespace"] = tstData.PodNamespace
	req.VolumeContext["csi.storage.k8s.io/pod.name"] = tstData.PodName
	req.VolumeContext["csi.storage.k8s.io/pod.uid"] = tstData.PodUid
	req.VolumeContext["csi.storage.k8s.io/serviceAccount.name"] = "default"

	req.VolumeContext[utils.CsiCacheNamespaceIndex] = tstData.CrNamespace
	req.VolumeContext[utils.CsiCacheNameIndex] = tstData.CrName

	return &req, nil
}

func buildNodeUnpublishVolumeRequest(tstData TestData) (*csi.NodeUnpublishVolumeRequest, error) {
	req := csi.NodeUnpublishVolumeRequest{
		VolumeId:   tstData.VolumeId,
		TargetPath: TestDir + "/kubelet/pods/" + tstData.PodUid + "/volumes/kubernetes.io~csi/kernel-volume/mount",
	}

	return &req, nil
}

func createCacheImageFiles(tstData TestData, log logr.Logger) error {
	outputDir, err := database.BuildDbDir(TestCacheDir, tstData.CrNamespace, tstData.CrName, tstData.Digest, log)
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

	if err := database.ExportForTestWriteCacheFile(
		tstData.CrNamespace,
		tstData.CrName,
		tstData.Image,
		tstData.Digest,
		false, // rmDigest
		45000, // size
		log,
	); err != nil {
		return err
	}

	return nil
}

func cleanUp(testDir string, d *Driver, log logr.Logger) error {
	err := os.RemoveAll(testDir)
	if err != nil {
		log.Info("Error cleaning up test data", "testDir", testDir, "err", err)
	}

	entries, err := os.ReadDir(testDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("Test data cleaned up!")
			return nil
		} else {
			log.Info("Error reading test data", "testDir", testDir, "err", err)
			return err
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			// If the "kubelet" directory remains, then probably umount failed.
			// Get the pod name and umount the directory. Will be something like:
			// /tmp/csi-gkm/kubelet/pods/fedcba98-7654-3210-dead-000000000001/volumes/kubernetes.io~csi/kernel-volume/mount
			if name == "kubelet" {
				podsDir := filepath.Join(testDir, "kubelet/pods")
				pods, err := os.ReadDir(podsDir)
				if err == nil {
					for _, pod := range pods {
						podName := pod.Name()
						if pod.IsDir() {
							mountsDir := filepath.Join(podsDir, podName+"/volumes/kubernetes.io~csi/kernel-volume/mount")
							if err := d.mounter.Unmount(mountsDir); err != nil {
								log.Info("umount failed", "mountsDir", mountsDir)
							} else {
								log.Info("umount succeeded", "mountsDir", mountsDir)
							}
						}
					}
				}

				// Now that kubelet directory has been cleaned up, try to delete all test data.
				err = os.RemoveAll(testDir)
				if err != nil {
					log.Info("Error cleaning up test data after Kubelet cleanup", "testDir", testDir, "err", err)
				}
			} else {
				log.Info("Cleanup failed for:", "[DIR]", testDir+"/"+name)
			}
		} else {
			log.Info("Cleanup failed for:", "[FILE]", name)
		}
	}
	return fmt.Errorf("Cleanup failed")
}
