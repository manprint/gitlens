package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// SORIGIN-004: the activity check renders its pglens predicate through
// sessionorigin.PredicateSQL; no source of this package spells a pglens
// application-name predicate itself.
func TestSORIGIN004_ActivityUsesSharedPredicate(t *testing.T) {
	src, err := os.ReadFile("activity.go")
	require.NoError(t, err)
	require.Contains(t, string(src), "sessionorigin.PredicateSQL(")
	require.Contains(t, activityOriginQuery, "usename = current_user")

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		body, err := os.ReadFile(f)
		require.NoError(t, err)
		require.NotContains(t, string(body), "starts_with(", f)
	}
}
