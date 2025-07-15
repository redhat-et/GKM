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

	"github.com/redhat-et/GKM/pkg/utils"
)

const (
	// TestDir is the temporary directory for storing files used during testing.
	TestDir = "/tmp/csi-gkm"

	// DefaultSocketFilename is the location of the Unix domain socket for this CSI driver
	// for Kubelet to send requests.
	TestSocketFilename string = "unix:///tmp/csi-gkm/notused.sock"

	// TestImagePort is the location of port the Image Server will listen on for GKM
	// to send requests.
	TestImagePort string = ":50051"

	// TestCacheDir is the default root directory to store the expanded the GPU Kernel
	// images.
	TestCacheDir = "/tmp/csi-gkm/caches"
)

func TestNodePublishVolume(t *testing.T) {
	t.Run("Publish a volume", func(t *testing.T) {
		// Setup logging before anything else so code can log errors.
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))
		log := ctrl.Log.WithName("gkm-csi-driver")

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
		gkmCacheNs1 := ""
		volumeId1 := "csi-0123456789abcdef000000000000000000000000000000000000000000000001"
		podUid1 := "fedcba98-7654-3210-dead-000000000001"
		podName1 := "testPod-1"
		podNs1 := "testpod1"
		gkmCacheCrd1 := "rocmKernel"

		// Instance 2: Namespace Scoped
		gkmCacheNs2 := "testpod2"
		volumeId2 := "csi-0123456789abcdef000000000000000000000000000000000000000000000002"
		podUid2 := "fedcba98-7654-3210-dead-000000000002"
		podName2 := "testPod-2"
		podNs2 := "testpod2"
		gkmCacheCrd2 := "rocmKernel"

		// Instance 1 - Populated the Request Structure for a NodePublishVolume() call.
		pubReq1, err := buildNodePublishVolumeRequest(volumeId1, podUid1, podName1, podNs1, gkmCacheCrd1)
		require.NoError(t, err)

		// TEST: Instance 1 - Call NodePublishVolume() where it is expected to fail because
		// the OCI Image has not been downloaded.
		t.Logf("TEST: Instance 1 - Calling NodePublishVolume() to Fail")
		_, err = d.NodePublishVolume(context.Background(), pubReq1)
		t.Logf("Instance 1 - NodePublishVolume() err: %v", err)
		require.Error(t, err)

		// Instance 1 - Create files from the OCI Image download
		err = createImageFiles(TestCacheDir, gkmCacheNs1, gkmCacheCrd1)
		require.NoError(t, err)

		// TEST: Instance 1 - Call NodePublishVolume() where it is expected to pass.
		t.Logf("TEST: Instance 1 - Calling NodePublishVolume() to Succeed")
		_, err = d.NodePublishVolume(context.Background(), pubReq1)
		t.Logf("Instance 1 - NodePublishVolume() err: %v", err)
		require.NoError(t, err)

		// Instance 2 - Populated the Request Structure for a NodePublishVolume() call.
		pubReq2, err := buildNodePublishVolumeRequest(volumeId2, podUid2, podName2, podNs2, gkmCacheCrd2)
		require.NoError(t, err)

		// Instance 2 - Create files from the OCI Image download
		err = createImageFiles(TestCacheDir, gkmCacheNs2, gkmCacheCrd2)
		require.NoError(t, err)

		// TEST: Instance 2 - Call NodePublishVolume() where it is expected to pass.
		t.Logf("TEST: Instance 2 - Calling NodePublishVolume() to Succeed")
		_, err = d.NodePublishVolume(context.Background(), pubReq2)
		t.Logf("Instance 2 - NodePublishVolume() err: %v", err)
		require.NoError(t, err)

		// Instance 1 - Populated the Request Structure for a NodeUnpublishVolume() call.
		unpubReq1, err := buildNodeUnpublishVolumeRequest(volumeId1, podUid1)
		require.NoError(t, err)

		// TEST: Instance 1 - Call NodeUnpublishVolume() where it is expected to pass.
		t.Logf("TEST: Instance 1 - Calling NodeUnpublishVolume() to Succeed")
		_, err = d.NodeUnpublishVolume(context.Background(), unpubReq1)
		t.Logf("NodePublishVolume() err: %v", err)
		require.NoError(t, err)

		// TEST: Instance 1 - Call NodeUnpublishVolume() even though it is already deleted,
		// expected to pass.
		t.Logf("TEST: Instance 1 - Calling NodeUnpublishVolume() to Succeed")
		_, err = d.NodeUnpublishVolume(context.Background(), unpubReq1)
		t.Logf("NodePublishVolume() err: %v", err)
		require.NoError(t, err)

		// Instance 2 - Populated the Request Structure for a NodeUnpublishVolume() call.
		unpubReq2, err := buildNodeUnpublishVolumeRequest(volumeId2, podUid2)
		require.NoError(t, err)

		// TEST: Instance 2 - Call NodeUnpublishVolume() where it is expected to pass.
		t.Logf("TEST: Instance 2 - Calling NodeUnpublishVolume() to Succeed")
		_, err = d.NodeUnpublishVolume(context.Background(), unpubReq2)
		t.Logf("NodePublishVolume() err: %v", err)
		require.NoError(t, err)
	})
}

func buildNodePublishVolumeRequest(volumeId, podUid, podName, podNs, gkmCacheCrd string,
) (*csi.NodePublishVolumeRequest, error) {
	req := csi.NodePublishVolumeRequest{
		VolumeId:          volumeId,
		StagingTargetPath: "",
		TargetPath:        TestDir + "/kubelet/pods/" + podUid + "/volumes/kubernetes.io~csi/kernel-volume/mount",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
	}
	req.VolumeContext = make(map[string]string)
	req.VolumeContext["csi.storage.k8s.io/ephemeral"] = "true"
	req.VolumeContext["csi.storage.k8s.io/pod.name"] = podName
	req.VolumeContext["csi.storage.k8s.io/pod.namespace"] = podNs
	req.VolumeContext["csi.storage.k8s.io/pod.uid"] = podUid
	req.VolumeContext["csi.storage.k8s.io/serviceAccount.name"] = "default"

	if gkmCacheCrd != "" {
		req.VolumeContext["csi.gkm.io/GKMCache"] = gkmCacheCrd
	}

	return &req, nil
}

func buildNodeUnpublishVolumeRequest(volumeId, podUid string,
) (*csi.NodeUnpublishVolumeRequest, error) {
	req := csi.NodeUnpublishVolumeRequest{
		VolumeId:   volumeId,
		TargetPath: TestDir + "/kubelet/pods/" + podUid + "/volumes/kubernetes.io~csi/kernel-volume/mount",
	}

	return &req, nil
}

func createImageFiles(cacheDir, namespace, kernelName string) error {
	outputDir := cacheDir

	if namespace == "" {
		namespace = utils.ClusterScopedSubDir
	}
	outputDir = filepath.Join(outputDir, namespace)

	if kernelName != "" {
		outputDir = filepath.Join(outputDir, kernelName)
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
