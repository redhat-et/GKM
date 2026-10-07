package signing

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/redhat-et/GKM/mcv/pkg/registryclient"
	"github.com/redhat-et/GKM/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
	"go.podman.io/storage"
)

// ResolveDigest resolves an image to a digest for signing, matching
// cosign CLI behavior:
//   - Digest references are used as-is even if the image is not in the registry
//     (cosign SignedUnknown path — signature tags/bundles can still be uploaded).
//   - Tag references try the registry first, then fall back to a local
//     containers-storage / Docker manifest digest.
func ResolveDigest(imageRef string) (string, error) {
	normalized := utils.NormalizeImageTag(imageRef)
	ref, err := name.ParseReference(normalized)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	if dig, ok := ref.(name.Digest); ok {
		logging.Debugf("Using provided digest for signing (cosign allows signing digests not yet in registry): %s", dig)
		return dig.String(), nil
	}

	remoteDig, err := registryclient.ResolveRegistryDigest(normalized)
	if err == nil {
		return remoteDig, nil
	}
	logging.Debugf("registry digest resolve failed, trying local image: %v", err)

	localDig, err := resolveLocalImageDigest(ref)
	if err != nil {
		return "", fmt.Errorf("image %q not found in registry or local storage (create/push the image first): %w", imageRef, err)
	}
	logging.Warnf("Signing local digest %s (image not found in registry). "+
		"cosign tree / verify against the registry require the image to be pushed first; "+
		"prefer: mcv --push --sign --image %s", localDig, normalized)
	return localDig, nil
}

// resolveLocalImageDigest returns repo@sha256:… for an image present in local
// containers storage (podman/buildah) or the Docker daemon. Prefers the
// manifest digest, never the image config ID.
func resolveLocalImageDigest(ref name.Reference) (string, error) {
	var errs []error

	dig, err := resolveLocalDigestFromContainersStorage(ref)
	if err == nil {
		return dig, nil
	}
	errs = append(errs, err)

	dig, err = resolveLocalDigestFromDocker(ref)
	if err == nil {
		return dig, nil
	}
	errs = append(errs, err)
	return "", fmt.Errorf("local digest lookup failed: %v", errs)
}

func resolveLocalDigestFromContainersStorage(ref name.Reference) (string, error) {
	storeOpts, err := storage.DefaultStoreOptions()
	if err != nil {
		return "", err
	}
	store, err := storage.GetStore(storeOpts)
	if err != nil {
		return "", err
	}
	defer shutdownStore(store)

	img, err := store.Image(ref.String())
	if err != nil {
		// Try normalized tag form (name.Tag vs bare name).
		img, err = store.Image(utils.NormalizeImageTag(ref.String()))
		if err != nil {
			return "", err
		}
	}

	dgst, err := manifestDigestFromStorageImage(img)
	if err != nil {
		return "", err
	}
	return ref.Context().Digest(dgst).String(), nil
}

// manifestDigestFromStorageImage returns a manifest digest, never the image
// config ID. containers/storage may list the image ID in Digests/Digest.
// Supports any hash algorithm (SHA256, SHA384, SHA512) for both image IDs
// and manifest digests.
func manifestDigestFromStorageImage(img *storage.Image) (string, error) {
	if img == nil {
		return "", fmt.Errorf("image is nil")
	}

	// Extract the hex portion of the image ID for comparison (algorithm-agnostic)
	var imageIDHex string
	if idx := strings.Index(img.ID, ":"); idx > 0 {
		imageIDHex = img.ID[idx+1:]
	} else {
		imageIDHex = img.ID
	}

	isImageID := func(d string) bool {
		if d == "" {
			return true
		}
		// Direct match
		if d == img.ID {
			return true
		}
		// Check if it's the image ID with a different or missing algorithm prefix
		if idx := strings.Index(d, ":"); idx > 0 {
			return d[idx+1:] == imageIDHex
		}
		return d == imageIDHex
	}

	for _, d := range img.Digests {
		s := d.String()
		if !isImageID(s) {
			return s, nil
		}
	}
	if s := img.Digest.String(); !isImageID(s) {
		return s, nil
	}
	return "", fmt.Errorf("local image %q has no manifest digest (only image id %s)", img.ID, img.ID)
}

func resolveLocalDigestFromDocker(ref name.Reference) (string, error) {
	ctx := context.Background()
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", err
	}
	defer apiClient.Close()

	inspect, err := apiClient.ImageInspect(ctx, ref.String())
	if err != nil {
		inspect, err = apiClient.ImageInspect(ctx, utils.NormalizeImageTag(ref.String()))
		if err != nil {
			return "", err
		}
	}

	repo := ref.Context().Name()
	return selectDockerRepoDigest(ref.String(), repo, inspect.RepoDigests)
}

// selectDockerRepoDigest returns a repo@digest entry matching the requested
// repository. RepoDigests from other registries are ignored to avoid signing
// the wrong manifest when an image was previously pushed elsewhere.
func selectDockerRepoDigest(imageRef, repo string, repoDigests []string) (string, error) {
	for _, rd := range repoDigests {
		if strings.HasPrefix(rd, repo+"@") {
			return rd, nil
		}
	}
	if len(repoDigests) == 0 {
		return "", fmt.Errorf("docker image %q has no RepoDigests", imageRef)
	}
	return "", fmt.Errorf(
		"docker image %q has no RepoDigests for repository %q (found %d for other repositories)",
		imageRef, repo, len(repoDigests),
	)
}

// shutdownStore mirrors buildah CLI: Shutdown(false) on the shared default
// store often returns ErrLayerUsedByContainer when other Podman/Buildah
// containers still hold mounts. That is expected, not a sign/verify failure.
func shutdownStore(store storage.Store) {
	if _, err := store.Shutdown(false); err != nil {
		if err == storage.ErrLayerUsedByContainer {
			logging.Debugf("storage shutdown skipped (layers in use by other containers): %v", err)
			return
		}
		logging.Warnf("storage shutdown failed: %v", err)
	}
}
