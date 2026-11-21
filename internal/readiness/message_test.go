package readiness

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/i18n"
	"github.com/manprint/pglens/internal/wire"
)

// standbyStep is the English step a standby's SQL fixes open with.
var standbyStep = i18n.T("", messagePrefix+"standby_step", nil)

func TestMain(m *testing.M) {
	strictMessages = true
	os.Exit(m.Run())
}

func worstInput() Input {
	return Input{
		Instance:        Instance{ID: "i1", PGMajor: 17, PermTier: 0, IsStandby: true},
		Settings:        map[string]string{"track_activity_query_size": "1024", "compute_query_id": "off"},
		SettingSources:  map[string]string{"compute_query_id": "configuration file"},
		SettingsKnown:   true,
		Extensions:      map[string][]string{"app": {}},
		ExtensionsKnown: true,
		Role:            Role{HasPgMonitor: ptr(false), HasReadAllStats: ptr(false)},
		Skips:           []CheckSkip{{Check: "bloat", Reason: "missing extension pgstattuple"}, {Check: "x", Reason: "something odd"}},
		FailingChecks:   []string{"locks"},
		Flags: wire.AgentFlags{
			CommandsEnabled: ptr(false), AllowSignal: ptr(false), ByApplication: ptr(false),
			SessionQueryText: ptr(false), HostLocal: ptr(true), DataDirectoryReadable: ptr(false),
		},
		DatabasesDiscovered: 12, DatabasesBudget: 10, Now: testNow,
	}
}

// TestRDY018_ReportFollowsTheLocale: every text of the report comes from the
// catalog of the requested language; ids, severities, channels and snippets
// do not change with it (plan 006 F5).
func TestRDY018_ReportFollowsTheLocale(t *testing.T) {
	en := Evaluate(worstInput())
	in := worstInput()
	in.Locale = "it"
	it := Evaluate(in)
	require.Len(t, it.Items, len(Rules()))
	require.Equal(t, en.Level, it.Level)
	require.Equal(t, en.Counts, it.Counts)
	for i, item := range it.Items {
		ref := en.Items[i]
		require.Equal(t, ref.ID, item.ID)
		require.Equal(t, ref.Severity, item.Severity)
		require.NotEqual(t, ref.Title, item.Title, item.ID)
		require.NotEqual(t, ref.Summary, item.Summary, item.ID)
		require.Len(t, item.Impact, len(ref.Impact), item.ID)
		require.Len(t, item.Fixes, len(ref.Fixes), item.ID)
		for j, fix := range item.Fixes {
			require.Equal(t, ref.Fixes[j].Channel, fix.Channel, item.ID)
			require.Equal(t, ref.Fixes[j].Snippet, fix.Snippet, item.ID)
			require.Equal(t, ref.Fixes[j].Route, fix.Route, item.ID)
			require.Len(t, fix.Steps, len(ref.Fixes[j].Steps), item.ID)
			for _, step := range fix.Steps {
				require.False(t, strings.HasPrefix(step, messagePrefix), "untranslated key %s", step)
			}
		}
	}
	for _, item := range it.Items {
		if item.ID == "RDY-EXT-01" {
			require.Equal(t, "pg_stat_statements non è attivo", item.Title)
			require.True(t, strings.HasPrefix(item.Fixes[1].Steps[0], "Questo server è uno standby"))
		}
	}
}
