//go:build integration

package pgtest

import (
	"os/exec"
	"testing"
)

// PGExtImage is the local image with hypopg and pg_hint_plan
// (test/pgext, make pgext-image).
const PGExtImage = "pglens-pgext:18"

var pgextByMajor = map[int]*PG{}

// PGExt returns the PostgreSQL 18 container started from PGExtImage. It skips
// the test when the image was not built.
func PGExt(t *testing.T) *PG {
	t.Helper()
	if err := exec.Command("docker", "image", "inspect", PGExtImage).Run(); err != nil {
		t.Skipf("%s not built (make pgext-image)", PGExtImage)
	}
	return getOrCreateImage(t, 18, PGExtImage, pgextByMajor)
}
