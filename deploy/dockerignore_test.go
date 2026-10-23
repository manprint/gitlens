package deploy

import (
	"os"
	"testing"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
	"github.com/stretchr/testify/require"
)

// The Dockerfiles copy the whole build context. The web build in the host
// tree, under internal/webui/dist, is not part of a release, and the server
// image's build embeds whatever the directory holds. The ignore file keeps the
// host's bundle out, and keeps the placeholder page, which the agent image
// needs to compile the embed. Docker matches the file with this package.
func TestDockerignore_HostWebBuildStaysOutOfTheContext(t *testing.T) {
	file, err := os.Open("../.dockerignore")
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	patterns, err := ignorefile.ReadAll(file)
	require.NoError(t, err)
	matcher, err := patternmatcher.New(patterns)
	require.NoError(t, err)

	for _, path := range []string{
		"internal/webui/dist/.built",
		"internal/webui/dist/assets",
		"internal/webui/dist/assets/app-1234abcd.js",
		"dist/index.html",
	} {
		excluded, err := matcher.MatchesOrParentMatches(path)
		require.NoError(t, err)
		require.True(t, excluded, "%s is in the build context", path)
	}
	for _, path := range []string{
		"internal/webui/dist/index.html",
		"internal/webui/webui.go",
		"cmd/pglens-server/main.go",
	} {
		excluded, err := matcher.MatchesOrParentMatches(path)
		require.NoError(t, err)
		require.False(t, excluded, "%s is missing from the build context", path)
	}
}
