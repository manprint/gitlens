package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// SORIGIN-004: the signal guard renders its predicate through
// sessionorigin.PredicateSQL; no source of this package spells a pglens
// application-name predicate itself.
func TestSORIGIN004_GuardUsesSharedPredicate(t *testing.T) {
	guard, err := os.ReadFile("exec_signal.go")
	require.NoError(t, err)
	require.Contains(t, string(guard), "sessionorigin.PredicateSQL(")
	require.NotContains(t, string(guard), "isPglensApplication")

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		require.NotContains(t, string(src), "starts_with(", f)
	}
}
