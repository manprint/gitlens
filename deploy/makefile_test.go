package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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

// make clean puts the committed placeholder back only where a build replaced
// it. A hand edit to an untouched placeholder is the developer's work and
// survives. Build output is removed, with or without the marker that
// make web-build writes, since a plain pnpm build leaves no marker.
func TestMakefile_CleanKeepsAHandEditAndRestoresBuildOutput(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required: clean restores the placeholder from the index")
	}
	const placeholder = "placeholder\n"
	dist := filepath.Join("internal", "webui", "dist")
	newTree := func(t *testing.T) string {
		dir := t.TempDir()
		copyMakefile(t, dir)
		git := func(args ...string) {
			t.Helper()
			out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
			require.NoError(t, err, "git %v: %s", args, out)
		}
		git("init", "-q")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, dist), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, dist, "index.html"), []byte(placeholder), 0o644))
		git("add", "-A")
		git("-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "placeholder")
		return dir
	}
	clean := func(t *testing.T, dir string) {
		t.Helper()
		out, err := exec.Command("make", "-C", dir, "clean").CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	t.Run("hand edit to the placeholder survives", func(t *testing.T) {
		dir := newTree(t)
		index := filepath.Join(dir, dist, "index.html")
		require.NoError(t, os.WriteFile(index, []byte(placeholder+"local edit\n"), 0o644))
		clean(t, dir)
		data, err := os.ReadFile(index)
		require.NoError(t, err)
		require.Equal(t, placeholder+"local edit\n", string(data))
	})

	for _, marked := range []bool{true, false} {
		name := "build output without the marker is removed and the placeholder restored"
		if marked {
			name = "build output with the marker is removed and the placeholder restored"
		}
		t.Run(name, func(t *testing.T) {
			dir := newTree(t)
			index := filepath.Join(dir, dist, "index.html")
			require.NoError(t, os.WriteFile(index, []byte("built\n"), 0o644))
			require.NoError(t, os.MkdirAll(filepath.Join(dir, dist, "assets"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, dist, "assets", "app-1234.js"), []byte("x"), 0o644))
			if marked {
				require.NoError(t, os.WriteFile(filepath.Join(dir, dist, ".built"), []byte("built_at=now\n"), 0o644))
			}
			clean(t, dir)
			data, err := os.ReadFile(index)
			require.NoError(t, err)
			require.Equal(t, placeholder, string(data))
			entries, err := os.ReadDir(filepath.Join(dir, dist))
			require.NoError(t, err)
			require.Len(t, entries, 1, "only index.html may remain")
		})
	}
}

// The evidence log is the only record of which e2e scenarios ran. go test
// prints a line per test only with -v, so without it a passing run leaves a
// bare package ok line, and a scenario that did not run cannot be told from one
// that did.
func TestMakefile_E2EFullPrintsEachScenario(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is required")
	}
	out, err := exec.Command("make", "-n", "-f", filepath.Join("..", "Makefile"), "test-e2e-full").CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Contains(t, strings.Fields(string(out)), "-v", "test-e2e-full must pass -v: %s", out)
}

// make build-images must re-resolve the base images. Without --pull, docker
// reuses a cached golang or distroless image for a tag that has since moved,
// so a local build can pick up an older base than the tag names.
func TestMakefile_BuildImagesPullTheBaseImages(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is required")
	}
	out, err := exec.Command("make", "-n", "-f", filepath.Join("..", "Makefile"), "build-images").CombinedOutput()
	require.NoError(t, err, "%s", out)
	var builds []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "docker build") {
			builds = append(builds, line)
			require.Contains(t, strings.Fields(line), "--pull", line)
		}
	}
	require.Len(t, builds, 2)
}

// go.mod pins the standard library with its go line and has no toolchain
// directive, and the comments that explain the pin must say so. A comment that
// names a toolchain directive as the pin sends a maintainer to add one.
func TestPinComments_NameTheGoLineNotAToolchainDirective(t *testing.T) {
	claim := regexp.MustCompile("`toolchain`\\s+(?:#\\s*)?directive")
	for _, file := range []string{filepath.Join("..", "Makefile"), filepath.Join("..", ".github", "workflows", "ci.yml")} {
		text := string(readFile(t, file))
		require.NotRegexp(t, claim, text, "%s names a toolchain directive as the pin", file)
		require.Contains(t, text, "`go` line in go.mod", file)
	}
}
