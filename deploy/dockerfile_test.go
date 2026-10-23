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
	builder := regexp.MustCompile(`(?m)^FROM golang:(\S+) AS builder$`)
	for _, name := range []string{"Dockerfile.agent", "Dockerfile.server"} {
		from := builder.FindStringSubmatch(string(readFile(t, filepath.Join("..", name))))
		require.NotNil(t, from, "%s: the builder stage must name a golang tag", name)
		require.Equal(t, goLine[1], from[1], "%s: the builder image must be the go line's patch release", name)
	}
}
