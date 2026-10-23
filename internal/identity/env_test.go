package identity_test

import (
	"os"
	"testing"
)

// TestMain runs the tests with PGHOST and PGPORT unset. A DSN that names no
// host or port takes them from the environment, so the fingerprints pinned here
// would otherwise move with the machine that runs them. A test that needs one
// sets it with t.Setenv, which the parallel tests never do.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("PGHOST")
	_ = os.Unsetenv("PGPORT")
	os.Exit(m.Run())
}
