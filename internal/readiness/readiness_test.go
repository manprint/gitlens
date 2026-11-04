package readiness

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/wire"
)

var testNow = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// healthy is an input with every gap closed: Evaluate gives no item.
func healthy() Input {
	return Input{
		Instance:        Instance{ID: "i1", PGMajor: 17, PermTier: 2, MonitoringRole: "pglens"},
		Settings:        map[string]string{"shared_preload_libraries": "pg_stat_statements", "compute_query_id": "auto", "track_io_timing": "on", "track_activity_query_size": "4096"},
		SettingSources:  map[string]string{},
		SettingsKnown:   true,
		Extensions:      map[string][]string{"app": {"pg_stat_statements", "pgstattuple"}},
		ExtensionsKnown: true,
		Role:            Role{HasPgMonitor: ptr(true), HasReadAllStats: ptr(true)},
		HostSeries:      HostSeries{HasDiskTotal: true, HasCPU: true},
		Flags: wire.AgentFlags{
			CommandsEnabled: ptr(true), AllowSignal: ptr(true), ByApplication: ptr(true),
			SessionQueryText: ptr(true), HostLocal: ptr(true), DataDirectoryReadable: ptr(true),
		},
		DatabasesDiscovered:  2,
		DatabasesBudget:      10,
		NotificationChannels: 1,
		Now:                  testNow,
	}
}

func ids(r Report) []string {
	out := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, it.ID)
	}
	return out
}

// RDY-001: the healthy input is full and empty; the report echoes id and time.
func TestRDY001_HealthyIsFull(t *testing.T) {
	r := Evaluate(healthy())
	require.Equal(t, LevelFull, r.Level)
	require.Empty(t, r.Items)
	require.NotNil(t, r.Items, "items serialises as [], not null")
	require.Equal(t, Counts{}, r.Counts)
	require.Equal(t, "i1", r.InstanceID)
	require.Equal(t, testNow, r.GeneratedAt)
}

// RDY-002: every rule fires on its trigger input and not on the healthy one.
func TestRDY002_RulesTriggerAndDoNotTrigger(t *testing.T) {
	cases := []struct {
		id      string
		sev     string
		mutate  func(*Input)
		partial bool
	}{
		{"RDY-EXT-01", SeverityWarning, func(in *Input) { in.Settings["shared_preload_libraries"] = "auto_explain" }, true},
		{"RDY-EXT-01", SeverityWarning, func(in *Input) { in.Extensions = map[string][]string{"app": {"pgstattuple"}} }, true},
		{"RDY-SET-01", SeverityWarning, func(in *Input) { in.Settings["compute_query_id"] = "off" }, true},
		{"RDY-SET-02", SeverityWarning, func(in *Input) { in.Settings["track_io_timing"] = "off" }, true},
		{"RDY-ROLE-01", SeverityWarning, func(in *Input) { in.Role.HasPgMonitor = ptr(false) }, true},
		{"RDY-ROLE-01", SeverityWarning, func(in *Input) { in.Role.HasReadAllStats = ptr(false) }, true},
		{"RDY-HOST-01", SeverityWarning, func(in *Input) { in.Flags.DataDirectoryReadable = ptr(false) }, true},
		{"RDY-CHK-01", SeverityWarning, func(in *Input) { in.FailingChecks = []string{"locks"} }, true},
		{"RDY-CHK-02", SeverityWarning, func(in *Input) {
			in.Skips = []CheckSkip{{Check: "bloat", Reason: "missing extension pgstattuple"}}
		}, true},
		{"RDY-ACT-01", SeverityWarning, func(in *Input) { in.Flags.ByApplication = ptr(false) }, true},
		{"RDY-DB-01", SeverityWarning, func(in *Input) { in.DatabasesDiscovered = 12 }, true},
		{"RDY-NOTIF-01", SeverityWarning, func(in *Input) { in.NotificationChannels = 0 }, true},
		{"RDY-T2-01", SeverityInfo, func(in *Input) { in.Instance.PermTier = 1 }, false},
		{"RDY-EXT-02", SeverityInfo, func(in *Input) { in.Extensions = map[string][]string{"app": {"pg_stat_statements"}} }, false},
		{"RDY-CMD-01", SeverityInfo, func(in *Input) { in.Flags.CommandsEnabled = ptr(false) }, false},
		{"RDY-SIG-01", SeverityInfo, func(in *Input) { in.Flags.AllowSignal = ptr(false) }, false},
		{"RDY-SESS-01", SeverityInfo, func(in *Input) { in.Flags.SessionQueryText = ptr(false) }, false},
		{"RDY-SET-03", SeverityInfo, func(in *Input) { in.Settings["track_activity_query_size"] = "1024" }, false},
	}
	for _, c := range cases {
		in := healthy()
		c.mutate(&in)
		r := Evaluate(in)
		require.Contains(t, ids(r), c.id, c.id)
		for _, it := range r.Items {
			if it.ID == c.id {
				require.Equal(t, c.sev, it.Severity, c.id)
			}
		}
		if c.partial {
			require.Equal(t, LevelPartial, r.Level, c.id)
		} else {
			require.Equal(t, LevelFull, r.Level, "info items never change the level: %s", c.id)
		}
	}
	require.Empty(t, Evaluate(healthy()).Items)
}

// RDY-003: every item has a non-empty impact and evidence and at least one fix
// with a channel, title and step; the rule table has unique ids.
func TestRDY003_ItemShape(t *testing.T) {
	in := Input{
		Instance:        Instance{ID: "i1", PGMajor: 17, PermTier: 0},
		Settings:        map[string]string{"track_activity_query_size": "1024", "compute_query_id": "off"},
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
		DatabasesDiscovered: 12, DatabasesBudget: 10, NotificationChannels: 0, Now: testNow,
	}
	r := Evaluate(in)
	require.Len(t, r.Items, len(Rules()), "every rule fires on the worst input")
	seen := map[string]bool{}
	for _, rule := range Rules() {
		require.False(t, seen[rule.ID], "duplicate rule id %s", rule.ID)
		seen[rule.ID] = true
	}
	for _, it := range r.Items {
		require.NotEmpty(t, it.Title, it.ID)
		require.NotEmpty(t, it.Summary, it.ID)
		require.NotEmpty(t, it.Impact, it.ID)
		require.NotEmpty(t, it.Evidence, it.ID)
		require.NotEmpty(t, it.Fixes, it.ID)
		for _, f := range it.Fixes {
			require.NotEmpty(t, f.Channel, it.ID)
			require.NotEmpty(t, f.Title, it.ID)
			require.NotEmpty(t, f.Steps, it.ID)
			for _, impact := range it.Impact {
				require.NotEmpty(t, impact, it.ID)
			}
		}
	}
}

var banned = regexp.MustCompile(`(?i)password|token|secret`)

// RDY-004: no snippet carries a secret-looking word, whatever the input.
func TestRDY004_NoSecretWordsInSnippets(t *testing.T) {
	in := healthy()
	in.Instance.PermTier = 0
	in.Instance.MonitoringRole = "My Role"
	in.Settings["shared_preload_libraries"] = ""
	in.Settings["compute_query_id"] = "off"
	in.Settings["track_io_timing"] = "off"
	in.Role = Role{HasPgMonitor: ptr(false), HasReadAllStats: ptr(false)}
	in.Skips = []CheckSkip{{Check: "bloat", Reason: "missing extension pgstattuple"}}
	in.Flags.ByApplication = ptr(false)
	in.Flags.CommandsEnabled = ptr(false)
	in.Flags.AllowSignal = ptr(false)
	in.Flags.SessionQueryText = ptr(false)
	in.Flags.DataDirectoryReadable = ptr(false)
	in.DatabasesDiscovered = 99
	r := Evaluate(in)
	require.NotEmpty(t, r.Items)
	for _, it := range r.Items {
		for _, f := range it.Fixes {
			require.False(t, banned.MatchString(f.Snippet), "%s: %q", it.ID, f.Snippet)
		}
	}
}

// RDY-005: a nil flag omits its item (an older agent), and so does unknown
// role membership.
func TestRDY005_NilFlagsOmitItems(t *testing.T) {
	in := healthy()
	in.Flags = wire.AgentFlags{}
	in.Role = Role{}
	r := Evaluate(in)
	require.Empty(t, r.Items)
	require.Equal(t, LevelFull, r.Level)
}

// RDY-006: items are deterministic: warnings first, then infos, each by id.
func TestRDY006_DeterministicOrder(t *testing.T) {
	in := healthy()
	in.NotificationChannels = 0
	in.Settings["compute_query_id"] = "off"
	in.Flags.CommandsEnabled = ptr(false)
	in.Instance.PermTier = 0
	in.FailingChecks = []string{"b", "a"}
	r := Evaluate(in)
	require.Equal(t, []string{"RDY-CHK-01", "RDY-NOTIF-01", "RDY-SET-01", "RDY-CMD-01", "RDY-T2-01"}, ids(r))
	require.Equal(t, Counts{Warning: 3, Info: 2}, r.Counts)
	require.Equal(t, r, Evaluate(in))
	require.Contains(t, r.Items[0].Evidence, "a, b", "failing checks are sorted")
}

// RDY-007: PG < 14 has no compute_query_id; unknown version (0) is below too.
func TestRDY007_PG13NoComputeQueryID(t *testing.T) {
	in := healthy()
	in.Instance.PGMajor = 13
	in.Settings["compute_query_id"] = "off"
	require.NotContains(t, ids(Evaluate(in)), "RDY-SET-01")
}

// RDY-008: compute_query_id is only a gap when it is off; auto is fine, and an
// absent setting is its default (auto) once settings are known.
func TestRDY008_ComputeQueryIDDefaults(t *testing.T) {
	in := healthy()
	delete(in.Settings, "compute_query_id")
	require.NotContains(t, ids(Evaluate(in)), "RDY-SET-01")
	in.Settings["compute_query_id"] = "on"
	require.NotContains(t, ids(Evaluate(in)), "RDY-SET-01")
}

// RDY-009: track_io_timing's fix switches to the start command when the
// setting comes from the command line.
func TestRDY009_TrackIOTimingCommandLine(t *testing.T) {
	in := healthy()
	in.Settings["track_io_timing"] = "off"
	r := Evaluate(in)
	var fix Fix
	for _, it := range r.Items {
		if it.ID == "RDY-SET-02" {
			fix = it.Fixes[0]
		}
	}
	require.Equal(t, ChannelSQL, fix.Channel)
	require.False(t, fix.RequiresRestart)
	require.Contains(t, fix.Snippet, "ALTER SYSTEM SET track_io_timing")

	in.SettingSources["track_io_timing"] = "command line"
	for _, it := range Evaluate(in).Items {
		if it.ID == "RDY-SET-02" {
			fix = it.Fixes[0]
		}
	}
	require.Equal(t, ChannelPostgresConfig, fix.Channel)
	require.True(t, fix.RequiresRestart)
	require.Contains(t, fix.Snippet, "-c track_io_timing=on")
	require.NotContains(t, fix.Snippet, "ALTER SYSTEM")
}

// RDY-010: settings not known: a missing setting proves nothing, so the
// setting items are omitted; absent track_io_timing is "off" only when known.
func TestRDY010_SettingsKnownSemantics(t *testing.T) {
	in := healthy()
	in.Settings = map[string]string{}
	in.SettingsKnown = false
	in.Extensions, in.ExtensionsKnown = nil, false
	r := Evaluate(in)
	for _, id := range []string{"RDY-EXT-01", "RDY-SET-01", "RDY-SET-02", "RDY-SET-03"} {
		require.NotContains(t, ids(r), id, "unknown settings: %s omitted", id)
	}

	in.SettingsKnown = true
	r = Evaluate(in)
	require.Contains(t, ids(r), "RDY-SET-02", "known and absent: track_io_timing is off")
	require.Contains(t, ids(r), "RDY-SET-03", "known and absent: query size is 1024")
	require.Contains(t, ids(r), "RDY-EXT-01", "known and absent: pg_stat_statements is not preloaded")
	require.NotContains(t, ids(r), "RDY-SET-01")
}

// RDY-011: preload parsing is exact: a longer name or a quoted list entry.
func TestRDY011_PreloadParsing(t *testing.T) {
	in := healthy()
	in.Settings["shared_preload_libraries"] = "pg_stat_statements_extra"
	require.Contains(t, ids(Evaluate(in)), "RDY-EXT-01")
	in.Settings["shared_preload_libraries"] = "auto_explain, 'pg_stat_statements' , timescaledb"
	require.NotContains(t, ids(Evaluate(in)), "RDY-EXT-01")
}

// RDY-012: host warning only for a host-local agent; without a
// DataDirectoryReadable flag it falls back to the host series; the standby
// flag does not matter to readiness.
func TestRDY012_HostRule(t *testing.T) {
	in := healthy()
	in.Flags.HostLocal = ptr(false)
	in.Flags.DataDirectoryReadable = ptr(false)
	require.NotContains(t, ids(Evaluate(in)), "RDY-HOST-01", "remote agent: not a gap")

	in.Flags.HostLocal = ptr(true)
	in.Flags.DataDirectoryReadable = nil
	in.HostSeries = HostSeries{}
	require.Contains(t, ids(Evaluate(in)), "RDY-HOST-01")
	in.HostSeries = HostSeries{HasDiskTotal: true}
	require.NotContains(t, ids(Evaluate(in)), "RDY-HOST-01")

	in.Flags.HostLocal = nil
	in.HostSeries = HostSeries{}
	require.NotContains(t, ids(Evaluate(in)), "RDY-HOST-01", "unknown host-locality: omitted")

	in = healthy()
	in.Instance.IsStandby = true
	require.Equal(t, LevelFull, Evaluate(in).Level)
}

// RDY-013: skips that are by design (tier, version, role) are not gaps, and a
// missing pg_stat_statements skip is not repeated when RDY-EXT-01 fires.
func TestRDY013_SkipFiltering(t *testing.T) {
	in := healthy()
	in.Skips = []CheckSkip{
		{Check: "locks", Reason: "requires tier T2"},
		{Check: "x", Reason: "requires PostgreSQL 15"},
		{Check: "y", Reason: "role pg_monitor missing"},
	}
	require.NotContains(t, ids(Evaluate(in)), "RDY-CHK-02")

	in.Skips = []CheckSkip{{Check: "statements", Reason: "missing extension pg_stat_statements"}}
	require.Contains(t, ids(Evaluate(in)), "RDY-CHK-02")
	in.Settings["shared_preload_libraries"] = ""
	r := Evaluate(in)
	require.Contains(t, ids(r), "RDY-EXT-01")
	require.NotContains(t, ids(r), "RDY-CHK-02")
}

// RDY-014: role names are quoted in snippets; unknown role uses the literal.
func TestRDY014_RoleQuoting(t *testing.T) {
	snippet := func(role string) string {
		in := healthy()
		in.Instance.MonitoringRole = role
		in.Role.HasPgMonitor = ptr(false)
		for _, it := range Evaluate(in).Items {
			if it.ID == "RDY-ROLE-01" {
				return it.Fixes[0].Snippet
			}
		}
		return ""
	}
	require.Equal(t, "GRANT pg_monitor TO pglens;", snippet("pglens"))
	require.Equal(t, `GRANT pg_monitor TO "My ""R""";`, snippet(`My "R"`))
	require.True(t, strings.Contains(snippet(""), RoleLiteral))
}

// RDY-015: DB budget: unknown budget (0) never fires; the snippet suggests the
// discovered count.
func TestRDY015_DatabaseBudget(t *testing.T) {
	in := healthy()
	in.DatabasesBudget = 0
	in.DatabasesDiscovered = 500
	require.NotContains(t, ids(Evaluate(in)), "RDY-DB-01")
	in.DatabasesBudget = 10
	in.DatabasesDiscovered = 10
	require.NotContains(t, ids(Evaluate(in)), "RDY-DB-01")
	in.DatabasesDiscovered = 11
	for _, it := range Evaluate(in).Items {
		if it.ID == "RDY-DB-01" {
			require.Contains(t, it.Fixes[0].Snippet, "max: 11")
			return
		}
	}
	t.Fatal("RDY-DB-01 missing")
}

// RDY-016: unknown tier (-1) never fires RDY-T2-01; T2 and T3 do not either.
func TestRDY016_TierUnknown(t *testing.T) {
	in := healthy()
	for _, tier := range []int{-1, 2, 3} {
		in.Instance.PermTier = tier
		require.NotContains(t, ids(Evaluate(in)), "RDY-T2-01", "tier %d", tier)
	}
}

// RDY-017: on a standby the catalogue-changing SQL fixes say to run them on the
// primary; ALTER SYSTEM (one server's own file) does not.
func TestRDY017_StandbySQLFixes(t *testing.T) {
	in := healthy()
	in.Instance.IsStandby = true
	in.Role.HasPgMonitor = ptr(false)
	in.Settings["track_io_timing"] = "off"
	in.Extensions = map[string][]string{"app": {"pg_stat_statements"}}
	r := Evaluate(in)
	found := map[string]bool{}
	for _, it := range r.Items {
		for _, f := range it.Fixes {
			if f.Channel != ChannelSQL {
				continue
			}
			found[it.ID] = true
			if strings.Contains(f.Snippet, "ALTER SYSTEM") {
				require.NotContains(t, f.Steps, standbyStep, it.ID)
			} else {
				require.Equal(t, standbyStep, f.Steps[0], it.ID)
			}
		}
	}
	require.True(t, found["RDY-ROLE-01"] && found["RDY-SET-02"] && found["RDY-EXT-02"])

	in.Instance.IsStandby = false
	for _, it := range Evaluate(in).Items {
		for _, f := range it.Fixes {
			require.NotContains(t, f.Steps, standbyStep, it.ID)
		}
	}
}
