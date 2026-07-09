package imgbuild

import (
	"context"
	"errors"
	"fmt"

	"github.com/containers/buildah"
	"github.com/google/go-containerregistry/pkg/name"
	logging "github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/storage"
)

type buildahBuilder struct{}

// shutdownStore mirrors buildah CLI: Shutdown(false) on the shared default
// store often returns ErrLayerUsedByContainer when other Podman/Buildah
// containers still hold mounts. That is expected, not a create/push/pull failure.
func shutdownStore(store storage.Store) {
	if _, err := store.Shutdown(false); err != nil {
		if errors.Is(err, storage.ErrLayerUsedByContainer) {
			logging.Debugf("storage shutdown skipped (layers in use by other containers): %v", err)
			return
		}
		logging.Warnf("storage shutdown failed: %v", err)
	}
}

// deleteBuilder unmounts then deletes the working container so Commit leftovers
// do not leave scratch working-containers behind.
func deleteBuilder(builder *buildah.Builder) {
	if builder == nil {
		return
	}
	if err := builder.Unmount(); err != nil {
		// Already unmounted is fine; still try Delete.
		logging.Debugf("builder unmount: %v", err)
	}
	if err := builder.Delete(); err != nil {
		logging.Warnf("builder.Delete failed: %v", err)
	}
}

func (b *buildahBuilder) CreateImage(imageName, cacheDir string) error {
	prep, err := prepareBuildContext("buildah", cacheDir)
	if err != nil {
		return err
	}
	defer CleanupDirs(prep.CacheBuildDir, prep.ManifestBuildDir)

	// Add OCI title label for consistency with Docker path
	prep.Labels[imageTitleLabel] = imageTitleFromName(imageName)

	buildStoreOptions, err := storage.DefaultStoreOptions()
	if err != nil {
		return fmt.Errorf("failed to get default store options: %w", err)
	}

	conf, err := config.Default()
	if err != nil {
		return fmt.Errorf("error configuring buildah: %v", err)
	}

	capabilitiesForRoot, err := conf.Capabilities("root", nil, nil)
	if err != nil {
		return fmt.Errorf("capabilitiesForRoot error: %v", err)
	}

	buildStore, err := storage.GetStore(buildStoreOptions)
	if err != nil {
		return fmt.Errorf("failed to init storage: %v", err)
	}
	defer shutdownStore(buildStore)

	imageWithTag := NormalizeImageTag(imageName)

	imageRef, err := is.Transport.ParseStoreReference(buildStore, imageWithTag)
	if err != nil {
		return fmt.Errorf("error creating the image reference: %v", err)
	}

	builderOpts := buildah.BuilderOptions{
		Capabilities: capabilitiesForRoot,
		FromImage:    "scratch",
	}

	ctx := context.TODO()
	// Initialize Buildah
	builder, err := buildah.NewBuilder(ctx, buildStore, builderOpts)
	if err != nil {
		return fmt.Errorf("error creating Buildah builder: %v", err)
	}
	defer deleteBuilder(builder)

	addOptions := buildah.AddAndCopyOptions{}
	err = builder.Add(prep.ManifestTag, false, addOptions, prep.ManifestBuildDir+"/.")
	if err != nil {
		return fmt.Errorf("error adding manifest %s to builder: %v", prep.ManifestBuildDir, err)
	}

	err = builder.Add(prep.CacheTag, false, addOptions, prep.CacheBuildDir+"/.")
	if err != nil {
		return fmt.Errorf("error adding %s to builder: %v", prep.CacheBuildDir, err)
	}

	for k, v := range prep.Labels {
		builder.SetLabel(k, v)
	}

	imageID, _, _, err := builder.Commit(ctx, imageRef, buildah.CommitOptions{
		Squash:                true,
		PreferredManifestType: buildah.Dockerv2ImageManifest,
	})
	if err != nil {
		return err
	}
	logging.Infof("Image built locally as %s (id %s); push with --push before --sign", imageWithTag, imageID)

	// Cleanup
	if err := CleanupWithTimeout(); err != nil {
		return fmt.Errorf("cleanup error: %w", err)
	}
	return nil
}

// PushImage pushes a local image to a remote registry
// Returns a digest-pinned reference when the push reports one.
func (b *buildahBuilder) PushImage(imageRef string) (string, error) {
	if imageRef == "" {
		return "", fmt.Errorf("image reference is empty")
	}

	imageRef = NormalizeImageTag(imageRef)
	logging.Infof("Pushing image to registry using buildah: %s", imageRef)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultPushPullTimeout)
	defer cancel()

	// Get default store options
	storeOptions, err := storage.DefaultStoreOptions()
	if err != nil {
		return "", fmt.Errorf("failed to get default store options: %w", err)
	}

	store, err := storage.GetStore(storeOptions)
	if err != nil {
		return "", fmt.Errorf("failed to get storage: %w", err)
	}
	defer shutdownStore(store)

	// Verify the image exists locally before attempting to push
	_, err = store.Image(imageRef)
	if err != nil {
		return "", fmt.Errorf("image '%s' not found in local storage: %w", imageRef, err)
	}

	sysCtx, err := registrySystemContext(imageRef)
	if err != nil {
		return "", err
	}

	// Use alltransports to parse the full docker:// reference
	fullRef := "docker://" + imageRef
	destRef, err := alltransports.ParseImageName(fullRef)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference '%s': %w", fullRef, err)
	}

	pushOpts := buildah.PushOptions{
		Store:         store,
		SystemContext: sysCtx,
	}

	// Perform the push with the destination reference
	_, dgst, err := buildah.Push(ctx, imageRef, destRef, pushOpts)
	if err != nil {
		return "", fmt.Errorf("buildah push failed: %w", err)
	}

	streamDigestRef := ""
	if dgst != "" {
		ref, err := name.ParseReference(imageRef)
		if err != nil {
			return "", fmt.Errorf("failed to parse pushed image reference %q: %w", imageRef, err)
		}
		streamDigestRef = ref.Context().Digest(dgst.String()).String()
	}

	digestRef, err := confirmPushedDigest(imageRef, streamDigestRef)
	if err != nil {
		return "", fmt.Errorf("failed to confirm pushed image digest: %w", err)
	}
	if digestRef != "" {
		logging.Infof("Successfully pushed image: %s (digest %s)", imageRef, digestFromReference(digestRef))
	} else {
		logging.Warnf("Successfully pushed image %s but digest is unknown; use --sign with --push only after digest can be resolved", imageRef)
	}
	return digestRef, nil
}

// PullImage pulls an image from a remote registry
func (b *buildahBuilder) PullImage(imageRef string) error {
	if imageRef == "" {
		return fmt.Errorf("image reference is empty")
	}

	imageRef = NormalizeImageTag(imageRef)
	logging.Infof("Pulling image from registry using buildah: %s", imageRef)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultPushPullTimeout)
	defer cancel()

	// Get default store options
	storeOptions, err := storage.DefaultStoreOptions()
	if err != nil {
		return fmt.Errorf("failed to get default store options: %w", err)
	}

	store, err := storage.GetStore(storeOptions)
	if err != nil {
		return fmt.Errorf("failed to get storage: %w", err)
	}
	defer shutdownStore(store)

	sysCtx, err := registrySystemContext(imageRef)
	if err != nil {
		return err
	}

	pullOpts := buildah.PullOptions{
		Store:         store,
		SystemContext: sysCtx,
	}

	// Perform the pull
	_, err = buildah.Pull(ctx, imageRef, pullOpts)
	if err != nil {
		return fmt.Errorf("buildah pull failed: %w", err)
	}

	logging.Infof("Successfully pulled image: %s", imageRef)
	return nil
}
