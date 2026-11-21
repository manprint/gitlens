package server

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMCP014: the README tool table lists exactly the registry, in its order,
// each tool with its REST route.
func TestMCP014_ReadmeToolTableMatchesRegistry(t *testing.T) {
	raw, err := os.ReadFile("../../README.md")
	require.NoError(t, err)
	readme := string(raw)
	begin := strings.Index(readme, "<!-- mcp-tools:begin -->")
	end := strings.Index(readme, "<!-- mcp-tools:end -->")
	require.True(t, begin >= 0 && end > begin, "README must hold the mcp-tools markers")

	row := regexp.MustCompile("^\\| `(pglens_[a-z_]+)` \\| [^|]+ \\| `GET ([^`]+)` \\|$")
	var got, want []string
	for _, line := range strings.Split(readme[begin:end], "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			got = append(got, m[1]+" "+m[2])
		}
	}
	for _, tool := range mcpTools() {
		want = append(want, tool.Name+" "+tool.Path)
	}
	require.Equal(t, want, got)
}
