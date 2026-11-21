package pglog

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func autoExplainFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return string(raw)
}

// AEX-001: a JSON auto_explain message gives its duration and the plan as
// EXPLAIN (FORMAT JSON) returns it, without the statement text.
func TestAEX001(t *testing.T) {
	p, err := ParseAutoExplain(autoExplainFixture(t, "autoexplain_json.txt"))
	require.NoError(t, err)
	require.InDelta(t, 0.010, p.DurationMS, 1e-9)
	require.Contains(t, p.QueryText, "SELECT relname FROM pg_class")

	var arr []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(p.Plan, &arr))
	require.Len(t, arr, 1)
	require.Contains(t, arr[0], "Plan")
	require.NotContains(t, arr[0], "Query Text")
	require.NotContains(t, arr[0], "Query Parameters")
	require.NotContains(t, string(p.Plan), "SELECT relname")

	_, err = ParseAutoExplain("duration: 12.5 ms  statement: SELECT 1")
	require.ErrorIs(t, err, ErrNotAutoExplain)
	_, err = ParseAutoExplain("checkpoint starting: time")
	require.ErrorIs(t, err, ErrNotAutoExplain)
	_, err = ParseAutoExplain("duration: 1 ms  plan:\n{\"Query Text\": \"x\"}")
	require.ErrorIs(t, err, ErrPlanFormat)
	_, err = ParseAutoExplain(autoExplainFixture(t, "autoexplain_text.txt"))
	require.ErrorIs(t, err, ErrPlanFormat)
}

// AEX-002: Analyzed is true only when the root plan node has actual times.
func TestAEX002(t *testing.T) {
	p, err := ParseAutoExplain(autoExplainFixture(t, "autoexplain_analyze.txt"))
	require.NoError(t, err)
	require.True(t, p.Analyzed)
	require.Contains(t, string(p.Plan), "Actual Total Time")

	p, err = ParseAutoExplain(autoExplainFixture(t, "autoexplain_json.txt"))
	require.NoError(t, err)
	require.False(t, p.Analyzed)
}
