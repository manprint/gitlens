package check

import (
	"fmt"
	"strconv"
	"testing"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/stretchr/testify/require"
)

func TestStatStatements_Requires(t *testing.T) {
	t.Parallel()
	c := &statStatementsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50}), caches: make(map[string]*lru.Cache[int64, string])}
	req := c.Requires()
	require.Equal(t, ScopeDatabase, req.Scope)
	require.Contains(t, req.Extensions, "pg_stat_statements")
	require.Equal(t, "stat_statements", c.Name())
	require.Equal(t, int64(60000000000), int64(c.DefaultInterval()))
	require.Equal(t, int64(15000000000), int64(c.Timeout()))
}

func TestStatStatements_NegativeQueryID(t *testing.T) {
	t.Parallel()
	queryID := int64(-1234567890)
	queryIDStr := fmt.Sprintf("%d", queryID)
	parsed, err := strconv.ParseInt(queryIDStr, 10, 64)
	require.NoError(t, err)
	require.Equal(t, queryID, parsed)
}

func TestStatStatements_MetricLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		queryID int64
	}{
		{"positive", 12345},
		{"negative", -67890},
		{"zero", 0},
		{"large", 9223372036854775807},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := fmt.Sprintf("%d", tt.queryID)
			roundtrip, err := strconv.ParseInt(label, 10, 64)
			require.NoError(t, err)
			require.Equal(t, tt.queryID, roundtrip)
		})
	}
}
