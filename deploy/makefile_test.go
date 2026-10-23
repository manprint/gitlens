package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// copyMakefile puts the repository's Makefile into dir, so that make runs the
// recipes under test against a tree the test controls.
func copyMakefile(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), readFile(t, filepath.Join("..", "Makefile")), 0o644))
}

// The web build marker records the vite version that built the bundle. It has
// to come from the installed package: a literal goes stale at the next bump
// and then describes a toolchain that did not build the bundle.
func TestMakefile_WebBuildMarkerRecordsInstalledViteVersion(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is required")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required: the recipe reads the installed package with it")
	}
	dir := t.TempDir()
	copyMakefile(t, dir)
	vite := filepath.Join(dir, "web", "node_modules", "vite")
	require.NoError(t, os.MkdirAll(vite, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vite, "package.json"), []byte(`{"name":"vite","version":"9.9.9"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "webui", "dist"), 0o755))

	// PNPM=true stands in for the build itself, which this test does not run.
	out, err := exec.Command("make", "-C", dir, "web-build", "PNPM=true").CombinedOutput()
	require.NoError(t, err, "%s", out)
	marker, err := os.ReadFile(filepath.Join(dir, "internal", "webui", "dist", ".built"))
	require.NoError(t, err)
	require.Regexp(t, `(?m)^vite=9\.9\.9$`, string(marker))
}
