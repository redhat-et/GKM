package imgbuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/redhat-et/GKM/mcv/pkg/cache"
	"github.com/redhat-et/GKM/mcv/pkg/constants"
	"github.com/redhat-et/GKM/mcv/pkg/utils"
	logging "github.com/sirupsen/logrus"
)

func GenerateDockerfile(imageName, cacheDir, manifestDir, outputPath string) error {
	parts := strings.Split(imageName, "/")
	fullImageName := parts[len(parts)-1]
	imageTitle := strings.Split(fullImageName, ":")[0]

	data := DockerfileData{
		ImageTitle:  imageTitle,
		CacheDir:    cacheDir,
		ManifestDir: manifestDir,
	}

	tmpl, err := template.New("dockerfile").Parse(DockerfileTemplate)
	if err != nil {
		return fmt.Errorf("error parsing template: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("error creating Dockerfile: %w", err)
	}
	defer file.Close()

	if err = tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("error reading generated Dockerfile: %w", err)
	}
	logging.Debugf("Generated Dockerfile content:\n\n%s", content)

	if _, err = os.Stat(outputPath); os.IsNotExist(err) {
		return fmt.Errorf("dockerfile not found at %s", outputPath)
	}
	logging.Infof("Dockerfile generated successfully at %s", outputPath)
	return nil
}

func prepareBuildContext(buildType, cacheDir string, spec ...cache.CaptureSpec) (_ *buildContext, err error) {
	var capture cache.CaptureSpec
	if len(spec) > 0 {
		capture = spec[0]
	}
	if err := capture.Validate(cacheDir); err != nil {
		return nil, err
	}

	caches := cache.DetectCaches(cacheDir, spec...)
	if len(caches) == 0 {
		return nil, errors.New("failed to detect cache type")
	}
	logging.Infof("Detected cache components: %v", cache.CacheTypes(caches))

	manifestTag, cacheTag, err := cache.GetTagsFromCaches(caches)
	if err != nil {
		return nil, fmt.Errorf("error retrieving manifest/cache tags: %v", err)
	}
	logging.Debugf("manifestTag: %s", manifestTag)
	logging.Debugf("cacheTag: %s", cacheTag)

	buildRoot := filepath.Join(constants.MCVBuildDir, buildType)

	cacheBuildDir := filepath.Join(buildRoot, cacheTag)
	manifestBuildDir := filepath.Join(buildRoot, manifestTag)

	// The build root is a fixed path, so a failed staging run must not leave
	// partial trees behind: the next attempt would copy into them and could
	// silently reuse stale content. Remove both staging roots on every error
	// return after this point; the success path returns them to the caller,
	// which cleans them up once the image is committed.
	defer func() {
		if err != nil {
			CleanupDirs(cacheBuildDir, manifestBuildDir)
		}
	}()

	if err := os.MkdirAll(cacheBuildDir, 0755); err != nil {
		return nil, err
	}
	logging.Debugf("cache build dir: %s", cacheBuildDir)

	if err := os.MkdirAll(manifestBuildDir, 0755); err != nil {
		return nil, err
	}
	logging.Debugf("manifest build dir: %s", manifestBuildDir)

	if err := cache.CopyDir(cacheDir, cacheBuildDir); err != nil {
		return nil, fmt.Errorf("error copying contents: %v", err)
	}

	// Extra cache trees ride inside the same payload prefix so the image stays
	// single-layered; a second Add/COPY would add a layer, which signatures over
	// (cosign) handle poorly.
	for _, src := range specSources(spec) {
		dest := filepath.Join(cacheBuildDir, src.PayloadName)
		if err := cache.CopyDir(src.AbsPath, dest); err != nil {
			return nil, fmt.Errorf("error copying extra cache tree %s: %w", src.AbsPath, err)
		}
	}

	cache.SetCachesBuildDir(caches, cacheBuildDir)

	labels, err := cache.BuildLabels(caches)
	if err != nil {
		return nil, fmt.Errorf("failed to build image labels: %w", err)
	}
	manifest := cache.BuildManifest(caches)
	manifestPath := filepath.Join(manifestBuildDir, "manifest.json")

	if err := cache.WriteManifest(manifestPath, manifest); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	return &buildContext{
		Caches:           caches,
		Labels:           labels,
		ManifestTag:      manifestTag,
		CacheTag:         cacheTag,
		CacheBuildDir:    cacheBuildDir,
		ManifestBuildDir: manifestBuildDir,
		ManifestPath:     manifestPath,
		BuildRoot:        buildRoot,
	}, nil
}

func specSources(spec []cache.CaptureSpec) []cache.SourceTree {
	if len(spec) == 0 {
		return nil
	}
	return spec[0].Sources
}

func CleanupDirs(dirs ...string) {
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			logging.Warnf("Failed to remove %s: %v", dir, err)
		}
	}
}

func CleanupWithTimeout() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return utils.CleanupMCVDirs(ctx, "")
}

func NormalizeImageTag(imageName string) string {
	if !strings.Contains(imageName, ":") {
		return fmt.Sprintf("%s:latest", imageName)
	}
	return imageName
}

func DockerfilePath(buildRoot string) string {
	return filepath.Join(buildRoot, "Dockerfile")
}
