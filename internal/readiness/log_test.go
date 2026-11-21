package readiness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func logItem(r Report) *Item {
	for i := range r.Items {
		if r.Items[i].ID == "RDY-LOG-01" {
			return &r.Items[i]
		}
	}
	return nil
}

// PGL-028
func TestPGL028LogNoteReadiness(t *testing.T) {
	require.Nil(t, logItem(Evaluate(healthy())), "no note, no item")

	in := healthy()
	in.Instance.MonitoringRole = "pglens"
	in.Skips = []CheckSkip{
		{Check: "logs", Reason: "logs_sql_denied"},
		{Check: "logs", Reason: "logs_sql_denied"},
		{Check: "logs", Reason: "logs_messages_not_english"},
	}
	r := Evaluate(in)
	it := logItem(r)
	require.NotNil(t, it)
	require.Equal(t, SeverityInfo, it.Severity)
	require.Equal(t, LevelFull, r.Level)
	require.NotContains(t, ids(r), "RDY-CHK-02", "a log note is not a skipped check")
	require.Equal(t, "logs: logs_messages_not_english; logs: logs_sql_denied", it.Evidence)
	require.Len(t, it.Impact, 2)
	require.Len(t, it.Fixes, 2)
	snippets := it.Fixes[0].Snippet + it.Fixes[1].Snippet
	require.Contains(t, snippets, "ALTER SYSTEM SET lc_messages = 'C';")
	require.Contains(t, snippets, "GRANT EXECUTE ON FUNCTION pg_read_binary_file(text, bigint, bigint, boolean) TO pglens;")
	require.Contains(t, snippets, "GRANT EXECUTE ON FUNCTION pg_current_logfile(text) TO pglens;")

	for _, note := range logNotes {
		in := healthy()
		in.Skips = []CheckSkip{{Check: "logs", Reason: note}}
		it := logItem(Evaluate(in))
		require.NotNil(t, it, note)
		require.Len(t, it.Fixes, 1, note)
		require.NotEmpty(t, it.Fixes[0].Steps, note)
	}

	in = healthy()
	in.Skips = []CheckSkip{{Check: "logs", Reason: "logs_new_thing"}}
	it = logItem(Evaluate(in))
	require.NotNil(t, it)
	require.True(t, strings.Contains(it.Impact[0], "logs_new_thing"))

	in.Locale = "it"
	in.Skips = []CheckSkip{{Check: "logs", Reason: "logs_sql_denied"}}
	require.Equal(t, "La lettura del log PostgreSQL è limitata", logItem(Evaluate(in)).Title)
}

// AEX-016: the auto_explain format note files RDY-AEX-01, never RDY-CHK-02 or RDY-LOG-01.
func TestAEX016AutoExplainFormatReadiness(t *testing.T) {
	in := healthy()
	in.Skips = []CheckSkip{{Check: "logs", Reason: "auto_explain_format_not_json"}}
	r := Evaluate(in)
	require.Contains(t, ids(r), "RDY-AEX-01")
	require.NotContains(t, ids(r), "RDY-CHK-02")
	require.Nil(t, logItem(r))
	var it *Item
	for i := range r.Items {
		if r.Items[i].ID == "RDY-AEX-01" {
			it = &r.Items[i]
		}
	}
	require.Equal(t, SeverityInfo, it.Severity)
	require.Equal(t, "logs: auto_explain_format_not_json", it.Evidence)
	require.Len(t, it.Fixes, 1)
	require.Equal(t, ChannelPostgresConfig, it.Fixes[0].Channel)
	require.Contains(t, it.Fixes[0].Snippet, "ALTER SYSTEM SET auto_explain.log_format = 'json';")

	in.Locale = "it"
	require.Contains(t, ids(Evaluate(in)), "RDY-AEX-01")
}
