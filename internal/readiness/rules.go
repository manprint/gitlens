package readiness

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/i18n"
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
		evidence = append(evidence, in.say("ext01.evidence.preload", i18n.Args{"preload": strconv.Quote(preload)}))
	}
	if e {
		evidence = append(evidence, in.say("ext01.evidence.extension", nil))
	}
	return Item{
		Title:    in.say("ext01.title", nil),
		Summary:  in.say("ext01.summary", nil),
		Impact:   in.sayAll("ext01.impact.slowest", "ext01.impact.charts", "ext01.impact.suggestions"),
		Evidence: strings.Join(evidence, "; "),
		Fixes: []Fix{
			{Channel: ChannelPostgresConfig, Title: in.say("ext01.preload.title", nil), RequiresRestart: true,
				Steps:   in.sayAll("ext01.preload.step", "step.restart_pg_at_start"),
				Snippet: "shared_preload_libraries = 'pg_stat_statements'"},
			{Channel: ChannelSQL, Title: in.say("ext01.create.title", nil),
				Steps:   in.sayAll("step.connect_each_dba", "step.run_below"),
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
		Title:    in.say("set01.title", nil),
		Summary:  in.say("set01.summary", nil),
		Impact:   in.sayAll("set01.impact.link", "set01.impact.normalised"),
		Evidence: "compute_query_id = off" + sourceSuffix(in, "compute_query_id"),
		Fixes: []Fix{{Channel: ChannelPostgresConfig, Title: in.say("set01.fix.title", nil),
			Steps:   in.sayAll("set01.fix.step1", "set01.fix.step2"),
			Snippet: "compute_query_id = auto"}},
	}
}

func sourceSuffix(in Input, name string) string {
	if s := in.SettingSources[name]; s != "" {
		return in.say("source_suffix", i18n.Args{"source": s})
	}
	return ""
}

// RDY-SET-02 — track_io_timing.

func appliesSet02(in Input) bool {
	v, ok := settingValue(in, "track_io_timing", "off")
	return ok && strings.EqualFold(strings.TrimSpace(v), "off")
}

func buildSet02(in Input) Item {
	fix := Fix{Channel: ChannelSQL, Title: in.say("set02.sql.title", nil),
		Steps:   in.sayAll("set02.sql.step1", "set02.sql.step2"),
		Snippet: "ALTER SYSTEM SET track_io_timing = on;\nSELECT pg_reload_conf();"}
	if in.SettingSources["track_io_timing"] == "command line" {
		fix = Fix{Channel: ChannelPostgresConfig, Title: in.say("set02.command.title", nil), RequiresRestart: true,
			Steps:   in.sayAll("set02.command.step1", "set02.command.step2", "step.restart_pg"),
			Snippet: "-c track_io_timing=on"}
	}
	return Item{
		Title:    in.say("set02.title", nil),
		Summary:  in.say("set02.summary", nil),
		Impact:   in.sayAll("set02.impact.charts", "set02.impact.statement"),
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
	roles := missing[0]
	if len(missing) == 2 {
		roles = in.say("and", i18n.Args{"first": missing[0], "second": missing[1]})
	}
	return Item{
		Title:    in.say("role01.title", nil),
		Summary:  in.say("role01.summary", nil),
		Impact:   in.sayAll("role01.impact.sessions", "role01.impact.statements", "role01.impact.settings"),
		Evidence: in.say("role01.evidence", i18n.Args{"roles": roles}),
		Fixes: []Fix{{Channel: ChannelSQL, Title: in.say("role01.fix.title", nil),
			Steps:   in.sayAll("role01.fix.step"),
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
	evidence := in.say("host01.evidence.unreadable", nil)
	if in.Flags.DataDirectoryReadable == nil {
		evidence = in.say("host01.evidence.no_sample", i18n.Args{"minutes": int(HostSampleWindow / time.Minute)})
	}
	return Item{
		Title:    in.say("host01.title", nil),
		Summary:  in.say("host01.summary", nil),
		Impact:   in.sayAll("host01.impact"),
		Evidence: evidence,
		Fixes: []Fix{{Channel: ChannelHost, Title: in.say("host01.fix.title", nil),
			Steps:   in.sayAll("host01.fix.step", "step.restart_agent"),
			Snippet: "# compose: under the agent service\nvolumes:\n  - /var/lib/postgresql/data:/var/lib/postgresql/data:ro"}},
	}
}

// RDY-CHK-01 — failing checks.

func appliesChk01(in Input) bool { return len(in.FailingChecks) > 0 }

func buildChk01(in Input) Item {
	names := append([]string(nil), in.FailingChecks...)
	sort.Strings(names)
	checks := i18n.Args{"checks": strings.Join(names, ", ")}
	return Item{
		Title:    in.say("chk01.title", nil),
		Summary:  in.say("chk01.summary", nil),
		Impact:   []string{in.say("chk01.impact", checks)},
		Evidence: in.say("chk01.evidence", checks),
		Fixes: []Fix{{Channel: ChannelConsole, Title: in.say("chk01.fix.title", nil), Route: "/findings",
			Steps: in.sayAll("chk01.fix.step1", "chk01.fix.step2")}},
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
		impact = append(impact, in.say("chk02.impact", i18n.Args{"check": s.Check}))
		switch {
		case strings.HasPrefix(s.Reason, "missing extension "):
			ext := strings.TrimPrefix(s.Reason, "missing extension ")
			if identifier.MatchString(ext) && !seen["ext:"+ext] {
				seen["ext:"+ext] = true
				fixes = append(fixes, Fix{Channel: ChannelSQL, Title: in.say("chk02.extension.title", i18n.Args{"extension": ext}),
					Steps:   in.sayAll("step.connect_each_dba", "step.run_below"),
					Snippet: "CREATE EXTENSION IF NOT EXISTS " + ext + ";"})
			}
		default:
			generic = true
		}
	}
	if generic || len(fixes) == 0 {
		fixes = append(fixes, Fix{Channel: ChannelHost, Title: in.say("chk02.generic.title", nil),
			Steps: in.sayAll("chk02.generic.step1", "chk02.generic.step2")})
	}
	return Item{
		Title:    in.say("chk02.title", nil),
		Summary:  in.say("chk02.summary", nil),
		Impact:   impact,
		Evidence: strings.Join(evidence, "; "),
		Fixes:    fixes,
	}
}

// RDY-ACT-01 — per-application activity.

func appliesAct01(in Input) bool { return isFalse(in.Flags.ByApplication) }

func buildAct01(in Input) Item {
	return Item{
		Title:    in.say("act01.title", nil),
		Summary:  in.say("act01.summary", nil),
		Impact:   in.sayAll("act01.impact"),
		Evidence: in.say("act01.evidence", nil),
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: in.say("act01.fix.title", nil), RequiresRestart: true,
			Steps:   in.sayAll("act01.fix.step", "step.restart_agent"),
			Snippet: "checks:\n  activity:\n    by_application: true"}},
	}
}

// RDY-DB-01 — database budget.

func appliesDB01(in Input) bool {
	return in.DatabasesBudget > 0 && in.DatabasesDiscovered > in.DatabasesBudget
}

func buildDB01(in Input) Item {
	return Item{
		Title:    in.say("db01.title", nil),
		Summary:  in.say("db01.summary", nil),
		Impact:   in.sayAll("db01.impact"),
		Evidence: in.say("db01.evidence", i18n.Args{"found": in.DatabasesDiscovered, "budget": in.DatabasesBudget}),
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: in.say("db01.fix.title", nil), RequiresRestart: true,
			Steps:   in.sayAll("db01.fix.step", "step.restart_agent"),
			Snippet: fmt.Sprintf("targets:\n  - name: <target>\n    databases:\n      max: %d", in.DatabasesDiscovered)}},
	}
}

// RDY-NOTIF-01 — no notification channel.

func appliesNotif01(in Input) bool { return in.NotificationChannels == 0 }

func buildNotif01(in Input) Item {
	return Item{
		Title:    in.say("notif01.title", nil),
		Summary:  in.say("notif01.summary", nil),
		Impact:   in.sayAll("notif01.impact"),
		Evidence: in.say("notif01.evidence", nil),
		Fixes: []Fix{
			{Channel: ChannelConsole, Title: in.say("notif01.console.title", nil), Route: "/settings?tab=notifications",
				Steps: in.sayAll("notif01.console.step")},
			{Channel: ChannelHost, Title: in.say("notif01.env.title", nil),
				Steps: in.sayAll("notif01.env.step")},
		},
	}
}

// RDY-T2-01 — cancel and terminate.

func appliesT201(in Input) bool { return in.Instance.PermTier >= 0 && in.Instance.PermTier < 2 }

func buildT201(in Input) Item {
	role := roleName(in)
	return Item{
		Title:    in.say("t201.title", nil),
		Summary:  in.say("t201.summary", nil),
		Impact:   in.sayAll("t201.impact"),
		Evidence: in.say("t201.evidence", i18n.Args{"tier": in.Instance.PermTier}),
		Fixes: []Fix{{Channel: ChannelSQL, Title: in.say("t201.fix.title", nil),
			Steps:   in.sayAll("t201.fix.step"),
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
		Title:    in.say("ext02.title", nil),
		Summary:  in.say("ext02.summary", nil),
		Impact:   in.sayAll("ext02.impact"),
		Evidence: in.say("ext02.evidence", i18n.Args{"databases": strings.Join(pgstattupleMissing(in), ", ")}),
		Fixes: []Fix{{Channel: ChannelSQL, Title: in.say("ext02.fix.title", nil),
			Steps:   in.sayAll("ext02.fix.step", "step.run_below"),
			Snippet: "CREATE EXTENSION IF NOT EXISTS pgstattuple;"}},
	}
}

// RDY-CMD-01, RDY-SIG-01, RDY-SESS-01 — switches of the agent.

func appliesCmd01(in Input) bool { return isFalse(in.Flags.CommandsEnabled) }

func buildCmd01(in Input) Item {
	return Item{
		Title:    in.say("cmd01.title", nil),
		Summary:  in.say("cmd01.summary", nil),
		Impact:   in.sayAll("cmd01.impact.explain", "cmd01.impact.audit"),
		Evidence: in.say("cmd01.evidence", nil),
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: in.say("cmd01.fix.title", nil), RequiresRestart: true,
			Steps:   in.sayAll("cmd01.fix.step", "step.restart_agent"),
			Snippet: "commands:\n  enabled: true"}},
	}
}

func appliesSig01(in Input) bool { return isFalse(in.Flags.AllowSignal) }

func buildSig01(in Input) Item {
	return Item{
		Title:    in.say("sig01.title", nil),
		Summary:  in.say("sig01.summary", nil),
		Impact:   in.sayAll("sig01.impact"),
		Evidence: in.say("sig01.evidence", nil),
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: in.say("sig01.fix.title", nil), RequiresRestart: true,
			Steps:   in.sayAll("sig01.fix.step", "step.restart_agent"),
			Snippet: "targets:\n  - name: <target>\n    allow_signal: true"}},
	}
}

func appliesSess01(in Input) bool { return isFalse(in.Flags.SessionQueryText) }

func buildSess01(in Input) Item {
	return Item{
		Title:    in.say("sess01.title", nil),
		Summary:  in.say("sess01.summary", nil),
		Impact:   in.sayAll("sess01.impact"),
		Evidence: in.say("sess01.evidence", nil),
		Fixes: []Fix{{Channel: ChannelAgentConfig, Title: in.say("sess01.fix.title", nil), RequiresRestart: true,
			Steps:   in.sayAll("sess01.fix.step1", "sess01.fix.step2"),
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
		Title:    in.say("set03.title", nil),
		Summary:  in.say("set03.summary", nil),
		Impact:   in.sayAll("set03.impact"),
		Evidence: in.say("set03.evidence", i18n.Args{"size": n}),
		Fixes: []Fix{{Channel: ChannelPostgresConfig, Title: in.say("set03.fix.title", nil), RequiresRestart: true,
			Steps:   in.sayAll("set03.fix.step", "step.restart_pg_at_start"),
			Snippet: "track_activity_query_size = 4096"}},
	}
}
