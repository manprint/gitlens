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

// PGExt returns the PostgreSQL 18 container started from PGExtImage with
// pg_hint_plan preloaded beside pg_stat_statements (without a hint comment it
// changes no plan). It skips the test when the image was not built.
func PGExt(t *testing.T) *PG {
	t.Helper()
	if err := exec.Command("docker", "image", "inspect", PGExtImage).Run(); err != nil {
		t.Skipf("%s not built (make pgext-image)", PGExtImage)
	}
	return getOrCreateImage(t, 18, PGExtImage, pgextByMajor, "pg_stat_statements,pg_hint_plan")
}
