package fetcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strings"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/images"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/redhat-et/TKDK/tcv/pkg/constants"
	logging "github.com/sirupsen/logrus"
)

type podmanFetcher struct{}

func (p *podmanFetcher) FetchImg(imgName string) (v1.Image, error) {
	socket := getPodmanSock()
	if socket == "" {
		return nil, fmt.Errorf("failed to retrieve Podman socket for client")
	}
	logging.Info("Initialize Podman client")

	ctx, err := bindings.NewConnection(context.Background(), socket)
	if err != nil {
		return nil, fmt.Errorf("failed to create Podman client: %w", err)
	}

	logging.Info("Check if the image exists")
	options := images.ExistsOptions{}
	_, err = images.Exists(ctx, imgName, &options)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve Podman images: %w", err)
	}

	logging.Info("found the image")

	tmpDir, err := os.MkdirTemp("", constants.PodmanCacheDirPrefix)
	if err != nil {
		return nil, err
	}

	// Create the tarball file
	tarballFilePath := path.Join(tmpDir, "tmp.tar")
	tarballFile, err := os.Create(tarballFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tarball file: %w", err)
	}
	defer tarballFile.Close()

	// Use Export to save the image
	var compress = true
	var format = "docker-archive"
	err = images.Export(ctx, []string{imgName}, tarballFile, &images.ExportOptions{Compress: &compress, Format: &format})
	if err != nil {
		return nil, fmt.Errorf("failed to export image: %v", err)
	}

	logging.Infof("Saved image: %s\n", tarballFile.Name())
	return loadImageFromTarball(tarballFilePath)
}

func getPodmanSock() string {
	// Default socket path for rootful Podman
	defaultSock := "/run/podman/podman.sock"

	// Check if the default rootful socket exists
	if _, err := os.Stat(defaultSock); err == nil {
		// If it exists, return the correct socket syntax
		logging.Infof("Podman socket %s", defaultSock)
		return "unix://" + defaultSock
	}

	// Check for rootless Podman socket (user-specific path)
	usr, err := user.Current()
	if err != nil {
		return ""
	}

	// Construct rootless Podman socket path using the current user's UID
	homeSock := fmt.Sprintf("/run/user/%s/podman/podman.sock", usr.Uid)
	if _, err = os.Stat(homeSock); err == nil {
		logging.Infof("Podman socket %s", homeSock)
		return "unix://" + homeSock
	}

	// If neither socket exists, run the podman command to get the socket path
	app := "podman"
	args := "info --format '{{.Host.RemoteSocket.Path}}'"

	output, err := exec.Command(app, args).Output()
	if err != nil {
		return ""
	}

	socketPath := strings.TrimSpace(string(output))

	if socketPath != "" {
		logging.Infof("Podman socket %s", socketPath)
		return "unix://" + socketPath
	}

	return ""
}
