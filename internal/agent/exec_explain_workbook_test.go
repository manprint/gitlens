package agent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
)

func indexOfSQL(sqls []string, match func(string) bool) int {
	return slices.IndexFunc(sqls, match)
}

func TestWBK004_SettingsAppliedInNameOrderInsideTheTransaction(t *testing.T) {
	conn := &explainConn{statement: "SELECT * FROM t WHERE id = 1", plan: []byte(`[{"Plan":{}}]`)}
	settings := map[string]string{"work_mem": "65536", "enable_seqscan": "off", "random_page_cost": "1.1"}
	result, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn},
		command.Args{QueryID: int64Ptr(1), Settings: settings})
	require.NoError(t, err)
	require.Equal(t, [][]any{
		{"enable_seqscan", "off"},
		{"random_page_cost", "1.1"},
		{"work_mem", "65536kB"},
	}, conn.setConfig)
	begin := indexOfSQL(conn.execSQL, func(s string) bool { return s == "BEGIN READ ONLY" })
	first := indexOfSQL(conn.execSQL, func(s string) bool { return s == setConfigSQL })
	explain := indexOfSQL(conn.execSQL, func(s string) bool { return strings.HasPrefix(s, "EXPLAIN (FORMAT JSON") })
	require.True(t, begin >= 0 && begin < first && first < explain, "%v", conn.execSQL)
	require.Equal(t, "ROLLBACK", conn.execSQL[len(conn.execSQL)-1])
	for _, sql := range conn.execSQL {
		require.NotContains(t, sql, "65536", "values are bound, not spliced")
		require.NotContains(t, sql, "SET LOCAL enable", "settings go through set_config")
	}

	var echo struct {
		Settings map[string]string `json:"settings"`
		Hints    *string           `json:"hints"`
	}
	require.NoError(t, json.Unmarshal(result, &echo))
	require.Equal(t, settings, echo.Settings)
	require.Nil(t, echo.Hints)

	plain := &explainConn{statement: "SELECT * FROM t WHERE id = 1", plan: []byte(`[{"Plan":{}}]`)}
	result, err = NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: plain}, command.Args{QueryID: int64Ptr(1)})
	require.NoError(t, err)
	require.Empty(t, plain.setConfig)
	require.NotContains(t, plain.execSQL, hintEnableSQL)
	require.NotContains(t, string(result), `"settings"`)
	require.NotContains(t, string(result), `"hints"`)
}

func TestWBK005_HintPlanMustBeActive(t *testing.T) {
	off := "off"
	for name, tc := range map[string]struct {
		enable *string
		want   string
	}{
		"not loaded": {nil, "hint_plan_inactive"},
		"disabled":   {&off, "hint_plan_disabled"},
	} {
		conn := &explainConn{statement: "SELECT * FROM t", plan: []byte(`[{"Plan":{}}]`), hintEnable: tc.enable}
		_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn},
			command.Args{QueryID: int64Ptr(1), Hints: "SeqScan(t)", Settings: map[string]string{"jit": "off"}})
		require.ErrorContains(t, err, tc.want, name)
		for _, sql := range conn.execSQL {
			require.False(t, strings.HasPrefix(sql, "EXPLAIN"), "%s: no EXPLAIN after %s: %q", name, tc.want, sql)
		}
		require.Empty(t, conn.setConfig, name)
	}
}

func TestWBK006_HintCommentSitsBeforeTheStatement(t *testing.T) {
	on := "on"
	hint := "/*+ SeqScan(t) Leading(t u) */ "

	plain := &explainConn{statement: "SELECT * FROM t JOIN u USING (id)", plan: []byte(`[{"Plan":{}}]`), hintEnable: &on}
	result, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: plain},
		command.Args{QueryID: int64Ptr(1), Hints: "SeqScan(t) Leading(t u)"})
	require.NoError(t, err)
	require.Contains(t, plain.execSQL, "EXPLAIN (FORMAT JSON, VERBOSE, COSTS) "+hint+"SELECT * FROM t JOIN u USING (id)")
	require.Contains(t, string(result), `"hints":"SeqScan(t) Leading(t u)"`)

	verbatim := &explainConn{statement: "SELECT * FROM t WHERE id = $1", plan: []byte(`[{"Plan":{}}]`), hintEnable: &on}
	_, err = NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: verbatim},
		command.Args{QueryID: int64Ptr(1), Hints: "SeqScan(t) Leading(t u)"})
	require.NoError(t, err)
	require.Len(t, verbatim.simpleSQL, 1)
	require.Contains(t, verbatim.simpleSQL[0], "GENERIC_PLAN) "+hint+"SELECT * FROM t WHERE id = $1")

	bound := &explainConn{statement: "SELECT * FROM t WHERE id = $1", plan: []byte(`[{"Plan":{}}]`), hintEnable: &on}
	_, err = NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: bound},
		command.Args{QueryID: int64Ptr(1), Analyze: true, Params: []*string{strPtr("7")}, Hints: "SeqScan(t) Leading(t u)"})
	require.NoError(t, err)
	require.Contains(t, bound.boundSQL, "BUFFERS) "+hint+"SELECT * FROM t WHERE id = $1")
	guard := indexOfSQL(bound.execSQL, func(s string) bool { return strings.HasPrefix(s, "EXPLAIN (VERBOSE, FORMAT JSON, COSTS OFF)") })
	require.GreaterOrEqual(t, guard, 0)
	require.Contains(t, bound.execSQL[guard], hint, "the analyze guard inspects the hinted plan")

	for _, c := range []*explainConn{plain, verbatim, bound} {
		for _, sql := range append(append([]string{}, c.execSQL...), c.guardSQL...) {
			require.NotContains(t, strings.ToUpper(sql), "LOAD ", "the agent never loads pg_hint_plan")
		}
	}
}
