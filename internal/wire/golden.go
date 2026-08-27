package wire

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var updateFlag = flag.Bool("update", false, "update golden files")

// AssertGolden compares got with the golden file at path.
// If update is true, it overwrites the golden file.
func AssertGolden(t *testing.T, got any, path string, update bool) {
	t.Helper()
	data, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)
	// Canonical: sorted keys already via MarshalIndent, but ensure stable
	var out bytes.Buffer
	require.NoError(t, json.Indent(&out, data, "", "  "))
	canonical := out.Bytes()
	if update || *updateFlag {
		dir := filepath.Dir(path)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(path, append(canonical, '\n'), 0644))
		return
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file not found %s: %v (run with -update)", path, err)
	}
	require.Equal(t, string(expected), string(append(canonical, '\n')))
}
