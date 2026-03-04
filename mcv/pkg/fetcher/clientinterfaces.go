package fetcher

import (
	"context"
	"io"

	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/docker/docker/client"
)

type DockerClient interface {
	ImageSave(ctx context.Context, images []string, options ...client.ImageSaveOption) (io.ReadCloser, error)
	Close() error
}

type PodmanClient interface {
	Export(ctx context.Context, names []string, w io.Writer, opts *images.ExportOptions) error
	Exists(ctx context.Context, name string, opts *images.ExistsOptions) (bool, error)
}
