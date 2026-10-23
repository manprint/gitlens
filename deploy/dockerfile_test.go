package deploy

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// The builder image has to be the patch release that go.mod's go line names. A
// floating tag such as golang:1.27 can resolve to a release below that line,
// and a toolchain below it cannot build the module, nor fetch the one it needs
// when GOTOOLCHAIN is local.
func TestDockerfiles_BuilderImageNamesTheGoLinePatch(t *testing.T) {
	goLine := regexp.MustCompile(`(?m)^go (\S+)$`).FindStringSubmatch(string(readFile(t, filepath.Join("..", "go.mod"))))
	require.NotNil(t, goLine, "go.mod must name its go line")
	builder := regexp.MustCompile(`(?m)^FROM golang:(\S+)@sha256:[0-9a-f]{64} AS builder$`)
	for _, name := range []string{"Dockerfile.agent", "Dockerfile.server"} {
		from := builder.FindStringSubmatch(string(readFile(t, filepath.Join("..", name))))
		require.NotNil(t, from, "%s: the builder stage must name a golang tag", name)
		require.Equal(t, goLine[1], from[1], "%s: the builder image must be the go line's patch release", name)
	}
}

// A tag can be moved to another image, so every base image is also named by
// its digest. The tag stays in front of the digest, so a reader still sees the
// release it names.
func TestDockerfiles_BaseImagesArePinnedToDigests(t *testing.T) {
	from := regexp.MustCompile(`(?m)^FROM (\S+)`)
	digest := regexp.MustCompile(`^\S+@sha256:[0-9a-f]{64}$`)
	for _, name := range []string{"Dockerfile.agent", "Dockerfile.server"} {
		images := from.FindAllStringSubmatch(string(readFile(t, filepath.Join("..", name))), -1)
		require.NotEmpty(t, images, "%s: no base image found", name)
		for _, image := range images {
			require.Regexp(t, digest, image[1], "%s: base image %s is not pinned to a digest", name, image[1])
		}
	}
}

// servedDocuments names the repository paths the web bundle serves, as the
// vite config lists them. The bundle reads each one from the root while it
// builds, so every image build that compiles the bundle needs all of them.
func servedDocuments(t *testing.T) []string {
	t.Helper()
	served := regexp.MustCompile(`(?m)^\s*'[^']+': new URL\('\.\./([^']+)', import\.meta\.url\),?$`)
	var paths []string
	for _, match := range served.FindAllStringSubmatch(string(readFile(t, filepath.Join("..", "web", "vite.config.ts"))), -1) {
		paths = append(paths, match[1])
	}
	require.NotEmpty(t, paths, "web/vite.config.ts must name the documents it serves")
	return paths
}

// The server image's web stage compiles the bundle, and the bundle reads the
// documents it serves from the repository root. A document the stage does not
// copy to the same path fails the whole image build, as it did once for
// docs/LIMITS.md and docs/api.md.
func TestDockerfileServer_WebStageCopiesEveryDocumentTheBundleServes(t *testing.T) {
	dockerfile := string(readFile(t, filepath.Join("..", "Dockerfile.server")))
	for _, path := range servedDocuments(t) {
		copy := regexp.MustCompile(`(?m)^COPY ` + regexp.QuoteMeta(path) + ` /src/` + regexp.QuoteMeta(path) + `$`)
		require.Regexp(t, copy, dockerfile, "Dockerfile.server: the web stage must COPY %s to the same path", path)
	}
}
