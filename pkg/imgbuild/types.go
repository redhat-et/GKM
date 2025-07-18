package imgbuild

import "github.com/redhat-et/TKDK/mcv/pkg/cache"

const DockerfileTemplate = `FROM scratch
LABEL org.opencontainers.image.title={{ .ImageTitle }}
COPY "./{{ .CacheDir }}." "./{{ .CacheDir }}"
COPY "./{{ .ManifestDir }}/manifest.json" "./{{ .ManifestDir }}/manifest.json"
`

type DockerfileData struct {
	ImageTitle  string
	CacheDir    string
	ManifestDir string
}

type buildContext struct {
	Caches           []cache.Cache
	Labels           map[string]string
	ManifestTag      string
	CacheTag         string
	CacheBuildDir    string
	ManifestBuildDir string
	ManifestPath     string
	BuildRoot        string
}
