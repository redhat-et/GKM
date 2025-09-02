package fetcher

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
)

// --- Mocks ---
type mockDockerClient struct {
	shouldFail bool
}

func (m *mockDockerClient) ImageSave(ctx context.Context, imgs []string, options ...client.ImageSaveOption) (io.ReadCloser, error) {
	if m.shouldFail {
		return nil, errors.New("mock docker failure")
	}
	return io.NopCloser(bytes.NewReader([]byte("fake image data"))), nil
}

func (m *mockDockerClient) Close() error {
	return nil
}

type mockPodmanClient struct {
	exists    bool
	exportErr error
}

func (m *mockPodmanClient) Exists(ctx context.Context, name string, opts *images.ExistsOptions) (bool, error) {
	return m.exists, nil
}

func (m *mockPodmanClient) Export(ctx context.Context, names []string, w io.Writer, opts *images.ExportOptions) error {
	if m.exportErr != nil {
		return m.exportErr
	}
	_, _ = w.Write([]byte("mock image"))
	return nil
}

// --- Tests ---
// func TestDockerFetcher_Success(t *testing.T) {
// 	df := &dockerFetcher{
// 		client: &mockDockerClient{},
// 	}

// 	img, err := df.FetchImg("quay.io/gkm/vector-add-cache:rocm")
// 	assert.NoError(t, err)
// 	assert.NotNil(t, img)
// }

func TestDockerFetcher_Failure(t *testing.T) {
	df := &dockerFetcher{
		client: &mockDockerClient{shouldFail: true},
	}

	img, err := df.FetchImg("mock/image:tag")
	assert.Error(t, err)
	assert.Nil(t, img)
}

// func TestPodmanFetcher_Success(t *testing.T) {
// 	pf := &podmanFetcher{
// 		client: &mockPodmanClient{exists: true},
// 	}

// 	img, err := pf.FetchImg("quay.io/gkm/vector-add-cache:rocm")
// 	assert.NoError(t, err)
// 	assert.NotNil(t, img)
// }

func TestPodmanFetcher_ImageNotFound(t *testing.T) {
	pf := &podmanFetcher{
		client: &mockPodmanClient{exists: false},
	}

	img, err := pf.FetchImg("mock/image:tag")
	assert.Error(t, err)
	assert.Nil(t, img)
}

func TestRemoteFetcher(t *testing.T) {
	rf := &remoteFetcher{}

	_, err := rf.FetchImg("quay.io/gkm/vector-add-cache:rocm")
	if err != nil {
		t.Log("Expected error in offline test:", err)
	}
}

func TestFallbackToRemote(t *testing.T) {
	f := NewFetcher() // This will use actual local clients if available

	img, err := f.FetchImg("quay.io/gkm/vector-add-cache:rocm")
	if err != nil {
		t.Log("Expected error if offline:", err)
	} else {
		assert.NotNil(t, img)
	}
}

func TestLoadImageFromTarball(t *testing.T) {
	_, err := loadImageFromTarball("/tmp/nonexistent.tar")
	assert.Error(t, err)
}

func TestFetchToTempTar(t *testing.T) {
	mockFetchFn := func(w io.Writer) error {
		_, err := w.Write([]byte("mock tar data"))
		return err
	}

	img, err := fetchToTempTar(mockFetchFn)
	assert.NoError(t, err)
	assert.NotNil(t, img)
}
