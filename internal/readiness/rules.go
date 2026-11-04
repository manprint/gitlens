package readiness

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HostSampleWindow is how recent a host_disk_* sample must be for the data
// directory to count as measured; the server computes HasDiskTotal over it.
const HostSampleWindow = 10 * time.Minute

// Rules returns the rule table in a fixed order. Evaluate sorts the items it
// produces, so the order here is only the order they are evaluated in.
func Rules() []Rule { return rules }

var rules = []Rule{
	{ID: "RDY-EXT-01", Severity: SeverityWarning, Applies: appliesExt01, Build: buildExt01},
	{ID: "RDY-SET-01", Severity: SeverityWarning, Applies: appliesSet01, Build: buildSet01},
	{ID: "RDY-SET-02", Severity: SeverityWarning, Applies: appliesSet02, Build: buildSet02},
	{ID: "RDY-ROLE-01", Severity: SeverityWarning, Applies: appliesRole01, Build: buildRole01},
	{ID: "RDY-HOST-01", Severity: SeverityWarning, Applies: appliesHost01, Build: buildHost01},
	{ID: "RDY-CHK-01", Severity: SeverityWarning, Applies: appliesChk01, Build: buildChk01},
	{ID: "RDY-CHK-02", Severity: SeverityWarning, Applies: appliesChk02, Build: buildChk02},
	{ID: "RDY-ACT-01", Severity: SeverityWarning, Applies: appliesAct01, Build: buildAct01},
	{ID: "RDY-DB-01", Severity: SeverityWarning, Applies: appliesDB01, Build: buildDB01},
	{ID: "RDY-NOTIF-01", Severity: SeverityWarning, Applies: appliesNotif01, Build: buildNotif01},
	{ID: "RDY-T2-01", Severity: SeverityInfo, Applies: appliesT201, Build: buildT201},
	{ID: "RDY-EXT-02", Severity: SeverityInfo, Applies: appliesExt02, Build: buildExt02},
	{ID: "RDY-CMD-01", Severity: SeverityInfo, Applies: appliesCmd01, Build: buildCmd01},
	{ID: "RDY-SIG-01", Severity: SeverityInfo, Applies: appliesSig01, Build: buildSig01},
	{ID: "RDY-SESS-01", Severity: SeverityInfo, Applies: appliesSess01, Build: buildSess01},
	{ID: "RDY-SET-03", Severity: SeverityInfo, Applies: appliesSet03, Build: buildSet03},
}

var plainIdent = regexp.MustCompile(`^[a-z_][a-z0-9_$]*$`)

// roleName is the monitoring role as an SQL identifier, quoted when it needs
// it, or the placeholder when it is not known. Never a password or token.
func roleName(in Input) string {
	name := in.Instance.MonitoringRole
	if name == "" {
		return RoleLiteral
	}
	if plainIdent.MatchString(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// settingValue is the value of a setting and whether it is known. A setting
// the agent did not ship is at its default once the settings are known.
func settingValue(in Input, name, def string) (string, bool) {
	if v, ok := in.Settings[name]; ok {
		return v, true
	}
	if in.SettingsKnown {
		return def, true
	}
	return "", false
}

func isFalse(b *bool) bool { return b != nil && !*b }

func listsExtension(in Input, name string) bool {
	for _, exts := range in.Extensions {
		for _, e := range exts {
			if e == name {
				return true
			}
		}
	}
	return false
}

func sortedDatabases(in Input) []string {
	names := make([]string, 0, len(in.Extensions))
	for db := range in.Extensions {
		names = append(names, db)
	}
	sort.Strings(names)
	return names
}

func preloadHas(value, library string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.Trim(strings.TrimSpace(part), `'"`) == library {
			return true
		}
	}
	return false
}

// RDY-EXT-01 — pg_stat_statements.

func ext01Findings(in Input) (preloadMissing, extMissing bool, preload string) {
	if v, ok := settingValue(in, "shared_preload_libraries", ""); ok {
		preload = v
		preloadMissing = !preloadHas(v, "pg_stat_statements")
	}
	extMissing = in.ExtensionsKnown && !listsExtension(in, "pg_stat_statements")
	return
}

func appliesExt01(in Input) bool {
	p, e, _ := ext01Findings(in)
	return p || e
}

func buildExt01(in Input) Item {
	p, e, preload := ext01Findings(in)
	var evidence []string
	if p {
		evidence = append(evidence, fmt.Sprintf("shared_preload_libraries = %q does not list pg_stat_statements", preload))
	}
	if e {
		evidence = append(evidence, "no monitored database lists the pg_stat_statements extension")
	}
	return Item{
		Title:    "pg_stat_statements is not enabled",
		Summary:  "The server does not load or create pg_stat_statements, so there are no per-statement statistics.",
		Impact:   []string{"Slowest queries", "Statement charts", "Index suggestions"},
		Evidence: strings.Join(evidence, "; "),
		Fixes: []Fix{
			{Channel: ChannelPostgresConfig, Title: "Preload the library", RequiresRestart: true,
				Steps: []string{
					"Add pg_stat_statements to shared_preload_libraries in postgresql.conf (keep the libraries already listed).",
					"Restart PostgreSQL: the setting is read at start.",
				},
				Snippet: "shared_preload_libraries = 'pg_stat_statements'"},
			{Channel: ChannelSQL, Title: "Create the extension in each monitored database",
				Steps: []string{
					"Connect to each monitored database as a DBA (pglens never creates extensions).",
					"Run the statement below.",
				},
				Snippet: "CREATE EXTENSION IF NOT EXISTS pg_stat_statements;"},
		},
	}
}

// RDY-SET-01 — compute_query_id.

func appliesSet01(in Input) bool {
	if in.Instance.PGMajor < 14 {
		return false
	}
	v, ok := settingValue(in, "compute_query_id", "auto")
	return ok && strings.EqualFold(strings.TrimSpace(v), "off")
}

func buildSet01(in Input) Item {
	return Item{
		Title:    "compute_query_id is off",
		Summary:  "Without a query identifier an active session cannot be matched to its statement.",
		Impact:   []string{"Link between active sessions and statements", "Normalised text for active sessions"},
		Evidence: "compute_query_id = off" + sourceSuffix(in, "compute_query_id"),
		Fixes: []Fix{{Channel: ChannelPostgresConfig, Title: "Let PostgreSQL compute the identifier",
			Steps: []string{
				"Set compute_query_id to auto (or on) in postgresql.conf.",
				"Reload the configuration (SELECT pg_reload_conf();): no restart is needed.",
			},
			Snippet: "compute_query_id = auto"}},
	}
}

func sourceSuffix(in Input, name string) string {
	if s := in.SettingSources[name]; s != "" {
		return " (source: " + s + ")"
	}
	return ""
}

// RDY-SET-02 — track_io_timing.

func appliesSet02(in Input) bool {
	v, ok := settingValue(in, "track_io_timing", "off")
	return ok && strings.EqualFold(strings.TrimSpace(v), "off")
}

func buildSet02(in Input) Item {
	fix := Fix{Channel: ChannelSQL, Title: "Turn it on without a restart",
		Steps: []string{
			"Run the statements below as a superuser (or the managed service's equivalent parameter group change).",
			"No restart is needed.",
		},
		Snippet: "ALTER SYSTEM SET track_io_timing = on;\nSELECT pg_reload_conf();"}
	if in.SettingSources["track_io_timing"] == "command line" {
		fix = Fix{Channel: ChannelPostgresConfig, Title: "Change the server start command", RequiresRestart: true,
			Steps: []string{
				"track_io_timing is set on the server command line, which ALTER SYSTEM cannot override.",
				"Change the start command (for a container, the command: line of the compose file) to pass -c track_io_timing=on.",
				"Restart PostgreSQL.",
			},
			Snippet: "-c track_io_timing=on"}
	}
	return Item{
		Title:    "track_io_timing is off",
		Summary:  "PostgreSQL does not time block reads and writes.",
		Impact:   []string{"I/O time charts", "Per-statement I/O time"},
		Evidence: "track_io_timing = off" + sourceSuffix(in, "track_io_timing"),
		Fixes:    []Fix{fix},
	}
}

// RDY-ROLE-01 — pg_monitor and pg_read_all_stats.

func appliesRole01(in Input) bool {
	return isFalse(in.Role.HasPgMonitor) || isFalse(in.Role.HasReadAllStats)
}

func buildRole01(in Input) Item {
	var missing []string
	var snippet []string
	role := roleName(in)
	if isFalse(in.Role.HasPgMonitor) {
		missing = append(missing, "pg_monitor")
		snippet = append(snippet, fmt.Sprintf("GRANT pg_monitor TO %s;", role))
	}
	if isFalse(in.Role.HasReadAllStats) {
		missing = append(missing, "pg_read_all_stats")
		snippet = append(snippet, fmt.Sprintf("GRANT pg_read_all_stats TO %s;", role))
	}
	return Item{
		Title:    "The monitoring role lacks predefined roles",
		Summary:  "The role cannot see other users' sessions and statements, nor every setting.",
		Impact:   []string{"Statistics of other users' sessions", "Statistics of other users' statements", "Settings"},
		Evidence: "the monitoring role is not a member of " + strings.Join(missing, " and "),
		Fixes: []Fix{{Channel: ChannelSQL, Title: "Grant the missing membership",
			Steps: []string{
				"Run the statements below as a superuser, or run deploy/sql/monitoring_user.sql as documented in deploy/sql/README.md.",
			},
			Snippet: strings.Join(snippet, "\n")}},
	}
}

// RDY-HOST-01 — host filesystem charts.

func appliesHost01(in Input) bool {
	if in.Flags.HostLocal == nil || !*in.Flags.HostLocal {
		return false
	}
	if isFalse(in.Flags.DataDirectoryReadable) {
		return true
	}
	return in.Flags.DataDirectoryReadable == nil && !in.HostSeries.HasDiskTotal
}

func buildHost01(in Input) Item {
	evidence := "the agent could not read the data directory's filesystem"
	if in.Flags.DataDirectoryReadable == nil {
		evidence = fmt.Sprintf("no host_disk_* sample in the last %d minutes", int(HostSampleWindow/time.Minute))
	}
	return Item{
		Title:    "The host filesystem is not measured",
		Summary:  "The agent runs next to this server but cannot read its data directory.",
		Impact:   []string{"Host filesystem total and free charts"},
		Evidence: evidence,
		Fixes: []Fix{{Channel: ChannelHost, Title: "Mount the data directory read-only into the agent",
			Steps: []string{
				"Mount the PostgreSQL data directory into the agent container read-only, at the same path as on the server.",
				"Restart the agent.",
			},
			Snippet: "# compose: under the agent service\nvolumes:\n  - /var/lib/postgresql/data:/var/lib/postgresql/data:ro"}},
	}
}

// RDY-CHK-01 — failing checks.

func appliesChk01(in Input) bool { return len(in.FailingChecks) > 0 }

func buildChk01(in Input) Item {
	names := append([]string(nil), in.FailingChecks...)
	sort.Strings(names)
	return Item{
		Title:    "A check is failing",
		Summary:  "The agent reports errors for at least one check, so its data is missing or old.",
		Impact:   []string{"Data of the failing check: " + strings.Join(names, ", ")},
		Evidence: "failing: " + strings.Join(names, ", "),
		Fixes: []Fix{{Channel: ChannelConsole, Title: "Read the agent's error", Route: "/findings",
			Steps: []string{
				"Open Findings and read the entry of the failing check; the agent log has the full error.",
				"Fix the cause it names (a missing privilege or extension is the usual one), then wait one collection interval.",
			}}},
	}
}

// RDY-CHK-02 — skipped checks (other than by tier, version or role).

var identifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func isDesignSkip(reason string) bool {
	return strings.HasPrefix(reason, "requires ") || strings.HasPrefix(reason, "role ")
}

func relevantSkips(in Input) []CheckSkip {
	ext01 := appliesExt01(in)
	var out []CheckSkip
	for _, s := range in.Skips {
		if isDesignSkip(s.Reason) {
			continue
		}
		if ext01 && s.Reason == "missing extension pg_stat_statements" {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Check != out[j].Check {
			return out[i].Check < out[j].Check
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func appliesChk02(in Input) bool { return len(relevantSkips(in)) > 0 }

func buildChk02(in Input) Item {
	skips := relevantSkips(in)
	var evidence, impact []string
	var fixes []Fix
	seen := map[string]bool{}
	generic := false
	for _, s := range skips {
		evidence = append(evidence, s.Check+": "+s.Reason)
		impact = append(impact, "Data of the "+s.Check+" check")
		switch {
		case strings.HasPrefix(s.Reason, "missing extension "):
			ext := strings.TrimPrefix(s.Reason, "missing extension ")
			if identifier.MatchString(ext) && !seen["ext:"+ext] {
				seen["ext:"+ext] = true
				fixes = append(fixes, Fix{Channel: ChannelSQL, Title: "Create the " + ext + " extension",
					Steps: []string{
						"Connect to each monitored database as a DBA (pglens never creates extensions).",
						"Run the statement below.",
					},
					Snippet: "CREATE EXTENSION IF NOT EXISTS " + ext + ";"})
			}
		default:
			generic = true
		}
	}
	if generic || len(fixes) == 0 {
		fixes = append(fixes, Fix{Channel: ChannelHost, Title: "Find why the agent skips the check",
			Steps: []string{
				"Search the agent log for the check name: the line says what the agent found missing.",
				"Give the monitoring role what the check needs (a predefined role, a privilege or an extension), then wait one collection interval.",
			}})
	}
	return Item{
		Title:    "A check is skipped",
		Summary:  "The agent does not run a check on this server for a reason that can be fixed.",
		Impact:   impact,
		Evidence: strings.Join(evidence, "; "),
		Fixes:    fixes,
	}
}

// RDY-ACT-01 — per-application activity.

func appliesAct01(in Input) bool { return isFalse(in.Flags.ByApplication) }

func buildAct01(in Input) Item {
	return Item{
		Title:    "Per-application activity is off",
		Summary:  "The agent does not count connections per application.",
		Impact:   []string{"Per-application connection counts"},
		Evidence: "checks.activity.by_application is false in the agent configuration",
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: "Turn it on in agent.yaml", RequiresRestart: true,
			Steps:   []string{"Set the key below in agent.yaml.", "Restart the agent."},
			Snippet: "checks:\n  activity:\n    by_application: true"}},
	}
}

// RDY-DB-01 — database budget.

func appliesDB01(in Input) bool {
	return in.DatabasesBudget > 0 && in.DatabasesDiscovered > in.DatabasesBudget
}

func buildDB01(in Input) Item {
	return Item{
		Title:    "Some databases are not monitored",
		Summary:  "The agent found more databases than its budget lets it monitor.",
		Impact:   []string{"Databases beyond the budget are not analysed"},
		Evidence: fmt.Sprintf("%d databases found, budget %d", in.DatabasesDiscovered, in.DatabasesBudget),
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: "Raise the per-target database budget", RequiresRestart: true,
			Steps: []string{
				"Raise databases.max of the target in agent.yaml to at least the number of databases found, or narrow the list with include and exclude.",
				"Restart the agent.",
			},
			Snippet: fmt.Sprintf("targets:\n  - name: <target>\n    databases:\n      max: %d", in.DatabasesDiscovered)}},
	}
}

// RDY-NOTIF-01 — no notification channel.

func appliesNotif01(in Input) bool { return in.NotificationChannels == 0 }

func buildNotif01(in Input) Item {
	return Item{
		Title:    "No notification channel is configured",
		Summary:  "Alerts are raised but nothing delivers them.",
		Impact:   []string{"Alert delivery (Slack, Google Chat, webhook)"},
		Evidence: "0 notification channels",
		Fixes: []Fix{
			{Channel: ChannelConsole, Title: "Add a channel in the console", Route: "/settings?tab=notifications",
				Steps: []string{"Open Settings, Notifications, and add a channel."}},
			{Channel: ChannelHost, Title: "Configure it in the server environment",
				Steps: []string{"Set the notification variables of the server as documented in the README, then restart the server."}},
		},
	}
}

// RDY-T2-01 — cancel and terminate.

func appliesT201(in Input) bool { return in.Instance.PermTier >= 0 && in.Instance.PermTier < 2 }

func buildT201(in Input) Item {
	role := roleName(in)
	return Item{
		Title:    "The monitoring role cannot signal other sessions",
		Summary:  "Tier 2 is the pg_signal_backend grant. Cancel and terminate also need allow_signal in the agent configuration.",
		Impact:   []string{"Cancel and terminate of other sessions from the console"},
		Evidence: fmt.Sprintf("permission tier T%d", in.Instance.PermTier),
		Fixes: []Fix{{Channel: ChannelSQL, Title: "Grant tier 2",
			Steps: []string{
				"Run the statements below as a superuser, or run deploy/sql/monitoring_user.sql with tier1=1 and tier2=1 as documented in deploy/sql/README.md.",
			},
			Snippet: fmt.Sprintf("GRANT pg_read_all_data TO %s;\nGRANT pg_signal_backend TO %s;", role, role)}},
	}
}

// RDY-EXT-02 — pgstattuple.

func pgstattupleMissing(in Input) []string {
	if !in.ExtensionsKnown {
		return nil
	}
	var missing []string
	for _, db := range sortedDatabases(in) {
		has := false
		for _, e := range in.Extensions[db] {
			if e == "pgstattuple" {
				has = true
			}
		}
		if !has {
			missing = append(missing, db)
		}
	}
	return missing
}

func appliesExt02(in Input) bool { return len(pgstattupleMissing(in)) > 0 }

func buildExt02(in Input) Item {
	return Item{
		Title:    "pgstattuple is not installed",
		Summary:  "Exact bloat measurement on demand needs the pgstattuple extension.",
		Impact:   []string{"Exact bloat on demand (the pgstattuple command)"},
		Evidence: "not installed in: " + strings.Join(pgstattupleMissing(in), ", "),
		Fixes: []Fix{{Channel: ChannelSQL, Title: "Create the extension",
			Steps: []string{
				"Connect to the database as a DBA (pglens never creates extensions).",
				"Run the statement below.",
			},
			Snippet: "CREATE EXTENSION IF NOT EXISTS pgstattuple;"}},
	}
}

// RDY-CMD-01, RDY-SIG-01, RDY-SESS-01 — switches of the agent.

func appliesCmd01(in Input) bool { return isFalse(in.Flags.CommandsEnabled) }

func buildCmd01(in Input) Item {
	return Item{
		Title:    "On-demand commands are off",
		Summary:  "The agent does not poll the server for commands.",
		Impact:   []string{"On-demand EXPLAIN", "Command audit"},
		Evidence: "commands.enabled is false in the agent configuration",
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: "Turn commands on", RequiresRestart: true,
			Steps:   []string{"Set the key below in agent.yaml (the default is on).", "Restart the agent."},
			Snippet: "commands:\n  enabled: true"}},
	}
}

func appliesSig01(in Input) bool { return isFalse(in.Flags.AllowSignal) }

func buildSig01(in Input) Item {
	return Item{
		Title:    "Cancel and terminate are off (by design)",
		Summary:  "allow_signal is off by default: the agent never signals another session unless you turn it on for the target.",
		Impact:   []string{"Cancel and terminate buttons"},
		Evidence: "allow_signal is false for this target",
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: "Allow signals for the target", RequiresRestart: true,
			Steps: []string{
				"Needs tier 2 (see RDY-T2-01). Set the key below on the target in agent.yaml.",
				"Restart the agent.",
			},
			Snippet: "targets:\n  - name: <target>\n    allow_signal: true"}},
	}
}

func appliesSess01(in Input) bool { return isFalse(in.Flags.SessionQueryText) }

func buildSess01(in Input) Item {
	return Item{
		Title:    "Raw text of active sessions is off",
		Summary:  "The console shows the normalised text of active sessions, not the raw text.",
		Impact:   []string{"Raw text of active sessions (normalised text is still shown)"},
		Evidence: "checks.locks.session_query_text is false",
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: "Collect the raw text", RequiresRestart: true,
			Steps: []string{
				"Raw text may contain literal values such as names or identifiers: turn it on only if that is acceptable.",
				"Set the key below in agent.yaml, then restart the agent.",
			},
			Snippet: "checks:\n  locks:\n    session_query_text: true"}},
	}
}

// RDY-SET-03 — track_activity_query_size.

const minQuerySize = 4096

func querySize(in Input) (int, bool) {
	v, ok := settingValue(in, "track_activity_query_size", "1024")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	return n, true
}

func appliesSet03(in Input) bool {
	n, ok := querySize(in)
	return ok && n < minQuerySize
}

func buildSet03(in Input) Item {
	n, _ := querySize(in)
	return Item{
		Title:    "Long statements are cut in activity",
		Summary:  "track_activity_query_size is below 4096 bytes, so long statements are truncated.",
		Impact:   []string{"Full text of long statements in activity"},
		Evidence: fmt.Sprintf("track_activity_query_size = %d", n),
		Fixes: []Fix{{Channel: ChannelPostgresConfig, Title: "Raise the size", RequiresRestart: true,
			Steps:   []string{"Set the value below in postgresql.conf.", "Restart PostgreSQL: the setting is read at start."},
			Snippet: "track_activity_query_size = 4096"}},
	}
}
