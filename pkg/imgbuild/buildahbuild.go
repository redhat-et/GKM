package imgbuild

import (
	"context"
	"fmt"

	"github.com/containers/buildah"
	"github.com/containers/common/pkg/config"
	is "github.com/containers/image/v5/storage"
	"github.com/containers/storage"
	logging "github.com/sirupsen/logrus"
)

type buildahBuilder struct{}

func (b *buildahBuilder) CreateImage(imageName, cacheDir string) error {
	prep, err := prepareBuildContext("buildah", cacheDir)
	if err != nil {
		return err
	}
	defer CleanupDirs(prep.CacheBuildDir, prep.ManifestBuildDir)

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

	defer func() {
		if _, err = buildStore.Shutdown(false); err != nil {
			logging.Errorf("shutdown failed: %v", err)
		}
	}()

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

	defer func() {
		if err = builder.Delete(); err != nil {
			logging.Errorf(" builder.Delete failed: %v", err)
		}
	}()

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

	imageID, _, _, err := builder.Commit(ctx, imageRef, buildah.CommitOptions{Squash: true})
	if err != nil {
		return err
	}
	logging.Infof("Image built! %s", imageID)

	// Cleanup
	if err := CleanupWithTimeout(); err != nil {
		return fmt.Errorf("cleanup error: %w", err)
	}
	return nil
}
