package server

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/i18n"
	"github.com/manprint/pglens/internal/watch"
)

// describeDB is what the describer reads through: the pool, or a fake.
type describeDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// AlertDescriber writes the detail of an alert notification from what the
// server stores: the rule, the cluster and the server it fired on, and the
// evidence its metric or event points to (the sessions, tables and rows the
// agent captured, the oldest or largest tables, the lagging standbys, the
// holders of the connections). It works for any rule: a rule it has no text
// for is described from its own summary, metric and threshold.
type AlertDescriber struct {
	db     describeDB
	locale string
	loc    *time.Location
	now    func() time.Time
}

// NewAlertDescriber describes alerts in locale (an i18n code, en if unknown).
func NewAlertDescriber(db describeDB, locale string) *AlertDescriber {
	return &AlertDescriber{db: db, locale: i18n.Resolve(locale), loc: time.UTC, now: time.Now}
}

// InLocation writes the times of the notifications in loc (PGLENS_TZ), with
// the zone's abbreviation; nil keeps UTC.
func (d *AlertDescriber) InLocation(loc *time.Location) *AlertDescriber {
	if loc != nil {
		d.loc = loc
	}
	return d
}

// DescribeIn implements alert.LocaleDescriber: Describe in locale, for a
// channel whose language differs from the server default.
func (d *AlertDescriber) DescribeIn(ctx context.Context, a alert.Alert, phase, locale string) *notify.Detail {
	in := *d
	in.locale = i18n.Resolve(locale)
	return in.Describe(ctx, a, phase)
}

// Describe implements alert.Describer. A query that fails leaves its part
// out: the notification still says what it can.
func (d *AlertDescriber) Describe(ctx context.Context, a alert.Alert, phase string) *notify.Detail {
	r := d.rule(ctx, a)
	tg := d.target(ctx, a)
	incs, incErr := d.incidents(ctx, a, r)
	det := &notify.Detail{Locale: d.locale, Title: d.title(a, r, tg, phase), Fields: d.fields(a, r, tg, phase, incs)}
	if phase != "resolve" {
		ev := d.evidence(ctx, a, r, tg, incs, incErr)
		det.Sections = append(det.Sections, ev.sections...)
		if act := d.actions(r, ev.code); act != nil {
			det.Sections = append(det.Sections, *act)
		}
	}
	det.Footer = "pglens · " + d.t("field.rule", nil) + " " + r.ID
	return det
}

func (d *AlertDescriber) t(key string, args i18n.Args) string { return i18n.T(d.locale, key, args) }

// rule is the rule of a: a built-in one, else the stored one, else what the
// alert itself says.
func (d *AlertDescriber) rule(ctx context.Context, a alert.Alert) alert.Rule {
	for _, b := range alert.Builtin() {
		if b.ID == a.RuleID {
			return b
		}
	}
	r := alert.Rule{ID: a.RuleID}
	var metric, cmp, event pgtype.Text
	var threshold pgtype.Float8
	var seconds int
	err := d.db.QueryRow(ctx, `SELECT severity, scope, metric, comparator, threshold, for_seconds, event_type, summary FROM alert_rules WHERE rule_id = $1 ORDER BY (tenant_id = 'default') DESC LIMIT 1`, a.RuleID).
		Scan(&r.Severity, &r.Scope, &metric, &cmp, &threshold, &seconds, &event, &r.Summary)
	if err != nil {
		return alert.Rule{ID: a.RuleID, Severity: a.Severity, Summary: a.Summary}
	}
	r.Metric, r.Comparator, r.Threshold, r.EventType = metric.String, alert.Comparator(cmp.String), threshold.Float64, event.String
	r.For = time.Duration(seconds) * time.Second
	return r
}

// target is where an alert fired, as the console names it.
type target struct {
	cluster   string
	instance  string // display name, else addr:port
	addr      string // addr:port, when the name differs
	role      string
	version   string
	lastSeen  *time.Time
	started   *time.Time
	hasServer bool
}

func (d *AlertDescriber) target(ctx context.Context, a alert.Alert) target {
	var tg target
	if a.ClusterID != nil {
		var name string
		if err := d.db.QueryRow(ctx, `SELECT COALESCE(name, '') FROM clusters WHERE cluster_id = $1 LIMIT 1`, *a.ClusterID).Scan(&name); err != nil || name == "" {
			name = d.t("cluster.unnamed", i18n.Args{"id": strconv.FormatInt(*a.ClusterID, 10)})
		}
		tg.cluster = name
	}
	if a.InstanceID == nil {
		return tg
	}
	var display, addr, role string
	var port, version int
	var lastSeen, started pgtype.Timestamptz
	err := d.db.QueryRow(ctx, `SELECT COALESCE(display_name, ''), addr, port, pg_version, role, last_seen, agent_started_at FROM instances WHERE instance_id = $1`, *a.InstanceID).
		Scan(&display, &addr, &port, &version, &role, &lastSeen, &started)
	if err != nil {
		tg.instance, tg.hasServer = a.InstanceID.String(), true
		return tg
	}
	tg.hasServer = true
	hostPort := addr + ":" + strconv.Itoa(port)
	tg.instance = hostPort
	if display != "" && display != hostPort {
		tg.instance, tg.addr = display, hostPort
	}
	tg.role = d.t("role."+role, nil)
	tg.version = pgVersion(version)
	if lastSeen.Valid {
		tg.lastSeen = &lastSeen.Time
	}
	if started.Valid {
		tg.started = &started.Time
	}
	return tg
}

// pgVersion writes server_version_num as PostgreSQL does: 180001 is 18.1,
// 90624 is 9.6.24.
func pgVersion(v int) string {
	switch {
	case v >= 100000:
		return fmt.Sprintf("%d.%d", v/10000, v%10000)
	case v >= 10000:
		return fmt.Sprintf("%d.%d.%d", v/10000, v/100%100, v%100)
	}
	return strconv.Itoa(v)
}

func (d *AlertDescriber) title(a alert.Alert, r alert.Rule, tg target, phase string) string {
	title := r.Summary
	switch {
	case i18n.Has("rule." + r.ID + ".title"):
		title = d.t("rule."+r.ID+".title", nil)
	case r.EventType != "" && i18n.Has("event."+r.EventType):
		title = d.t("event."+r.EventType, nil)
	case title == "":
		title = a.Summary
	}
	if title == "" {
		title = r.ID
	}
	badge := d.t("sev.fire", i18n.Args{"sev": string(a.Severity)})
	if phase == "resolve" {
		badge = d.t("sev.resolve", nil)
	}
	subject := tg.instance
	if subject == "" {
		subject = tg.cluster
	}
	if a.Datname != "" {
		subject = strings.TrimPrefix(subject+" / "+a.Datname, " / ")
	}
	if subject == "" {
		return d.t("title.nosubject", i18n.Args{"badge": badge, "title": title})
	}
	return d.t("title.full", i18n.Args{"badge": badge, "title": title, "subject": subject})
}

func (d *AlertDescriber) fields(a alert.Alert, r alert.Rule, tg target, phase string, incs []watch.Incident) []notify.Field {
	var out []notify.Field
	add := func(key, value string) {
		if value != "" {
			out = append(out, notify.Field{Label: d.t(key, nil), Value: value})
		}
	}
	add("field.problem", d.problem(r))
	add("field.cluster", tg.cluster)
	if tg.hasServer {
		args := i18n.Args{"name": tg.instance, "addr": tg.addr, "role": tg.role, "version": tg.version}
		switch {
		case tg.role == "":
			add("field.server", tg.instance)
		case tg.addr != "":
			add("field.server", d.t("server.value_addr", args))
		default:
			add("field.server", d.t("server.value", args))
		}
	}
	add("field.database", a.Datname)
	if len(incs) > 0 {
		add("field.table", d.incidentObject(incs[0]))
		for _, q := range d.incidentStatements(incs[0]) {
			add("field.statement", q)
		}
	}
	if r.Metric != "" && phase != "resolve" {
		args := i18n.Args{"value": d.fmtValue(r.Metric, a.Value), "cmp": cmpSymbol(r.Comparator), "threshold": d.fmtValue(r.Metric, r.Threshold), "for": d.fmtDuration(r.For)}
		if r.For > 0 {
			add("field.measured", d.t("measured.value", args))
		} else {
			add("field.measured", d.t("measured.value_now", args))
		}
	}
	since := d.t("since.firing", i18n.Args{"at": d.fmtTime(a.StartedAt), "duration": d.fmtDuration(d.now().Sub(a.StartedAt))})
	if phase == "resolve" {
		end := a.LastEvalAt
		if a.ResolvedAt != nil {
			end = *a.ResolvedAt
		}
		since = d.t("since.resolved", i18n.Args{"at": d.fmtTime(a.StartedAt), "duration": d.fmtDuration(end.Sub(a.StartedAt))})
	}
	add("field.since", since)
	add("field.details", labelList(a.Labels))
	add("field.page", d.page(r, tg))
	return out
}

// problem says what the rule means, with its threshold as configured.
func (d *AlertDescriber) problem(r alert.Rule) string {
	args := i18n.Args{"threshold": d.fmtValue(r.Metric, r.Threshold), "for": d.fmtDuration(r.For)}
	switch {
	case i18n.Has("rule." + r.ID + ".problem"):
		return d.t("rule."+r.ID+".problem", args)
	case r.Summary != "":
		return r.Summary
	case r.EventType != "":
		ev := r.EventType
		return d.t("rule.generic_event.problem", i18n.Args{"type": ev})
	case r.Metric != "":
		return d.t("rule.generic.problem", i18n.Args{"metric": r.Metric, "cmp": cmpSymbol(r.Comparator), "threshold": args["threshold"]})
	}
	return ""
}

// page is where the console shows the alert's evidence.
func (d *AlertDescriber) page(r alert.Rule, tg target) string {
	switch {
	case incidentTypesFor(r) != nil && tg.hasServer:
		return d.t("page.locks", i18n.Args{"instance": tg.instance})
	case logClassesFor(r) != nil && tg.hasServer:
		return d.t("page.logs", i18n.Args{"instance": tg.instance})
	case isReplicationRule(r) || (r.Scope == alert.ScopeCluster && tg.cluster != ""):
		return d.t("page.cluster", i18n.Args{"cluster": tg.cluster})
	case tg.hasServer:
		return d.t("page.instance", i18n.Args{"instance": tg.instance})
	}
	return d.t("page.alerts", nil)
}

func labelList(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ", ")
}

func cmpSymbol(c alert.Comparator) string {
	switch c {
	case alert.GT:
		return ">"
	case alert.GE:
		return "≥"
	case alert.LT:
		return "<"
	case alert.LE:
		return "≤"
	case alert.EQ:
		return "="
	case alert.NE:
		return "≠"
	}
	return string(c)
}

func (d *AlertDescriber) fmtTime(t time.Time) string {
	return t.In(d.loc).Format("2006-01-02 15:04:05 MST")
}

// fmtValue writes a metric's value in the unit its name ends with.
func (d *AlertDescriber) fmtValue(metric string, v float64) string {
	switch {
	case strings.HasSuffix(metric, "_seconds"):
		return d.fmtDuration(time.Duration(v * float64(time.Second)))
	case strings.HasSuffix(metric, "_ratio"):
		return i18n.Number(d.locale, v*100) + "%"
	case strings.HasSuffix(metric, "_bytes"):
		return d.fmtBytes(v)
	case strings.HasSuffix(metric, "_total"):
		// A counter is stored as its per-second rate.
		switch {
		case v == 0:
			return i18n.Number(d.locale, 0)
		case math.Abs(v) < 1:
			return d.t("rate.per_minute", i18n.Args{"n": v * 60})
		}
		return d.t("rate.per_second", i18n.Args{"n": v})
	case strings.HasSuffix(metric, "_age"), strings.HasSuffix(metric, "_count"):
		return d.fmtCount(v)
	}
	return i18n.Number(d.locale, v)
}

// fmtDuration writes d in its two largest units: 2,5 s, 3 min 12 s, 1 h 5 min.
func (d *AlertDescriber) fmtDuration(v time.Duration) string {
	if v < 0 {
		v = 0
	}
	unit := func(key string, n float64) string { return d.t(key, i18n.Args{"n": n}) }
	pair := func(bigKey string, big int64, smallKey string, small int64) string {
		if small == 0 {
			return unit(bigKey, float64(big))
		}
		return unit(bigKey, float64(big)) + " " + unit(smallKey, float64(small))
	}
	switch {
	case v < time.Second:
		return unit("dur.ms", float64(v.Milliseconds()))
	case v < time.Minute:
		return unit("dur.s", math.Round(v.Seconds()*10)/10)
	case v < time.Hour:
		return pair("dur.min", int64(v/time.Minute), "dur.s", int64(v%time.Minute/time.Second))
	case v < 24*time.Hour:
		return pair("dur.h", int64(v/time.Hour), "dur.min", int64(v%time.Hour/time.Minute))
	}
	return pair("dur.d", int64(v/(24*time.Hour)), "dur.h", int64(v%(24*time.Hour)/time.Hour))
}

func (d *AlertDescriber) fmtCount(v float64) string {
	switch {
	case math.Abs(v) >= 1e9:
		return d.t("count.billion", i18n.Args{"n": v / 1e9})
	case math.Abs(v) >= 1e6:
		return d.t("count.million", i18n.Args{"n": v / 1e6})
	}
	return i18n.Number(d.locale, v)
}

func (d *AlertDescriber) fmtBytes(v float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	i := 0
	for math.Abs(v) >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return i18n.Number(d.locale, math.Round(v*10)/10) + " " + units[i]
}

var plainIdent = regexp.MustCompile(`^[a-z_][a-z0-9_$]*$`)

// reservedIdents are the keywords a table is often named after, which must be
// quoted.
var reservedIdents = map[string]bool{"all": true, "analyse": true, "analyze": true, "and": true, "any": true, "array": true, "as": true, "asc": true, "both": true, "case": true, "cast": true, "check": true, "collate": true, "column": true, "constraint": true, "create": true, "default": true, "desc": true, "distinct": true, "do": true, "else": true, "end": true, "except": true, "false": true, "fetch": true, "for": true, "foreign": true, "from": true, "grant": true, "group": true, "having": true, "in": true, "into": true, "is": true, "limit": true, "not": true, "null": true, "offset": true, "on": true, "only": true, "or": true, "order": true, "primary": true, "references": true, "select": true, "table": true, "then": true, "to": true, "true": true, "union": true, "unique": true, "user": true, "using": true, "when": true, "where": true, "window": true, "with": true}

// quoteIdent quotes a name the way quote_ident does for the common cases.
func quoteIdent(s string) string {
	if plainIdent.MatchString(s) && !reservedIdents[s] {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// clipText cuts s to n runes, ending with "…" when it cut.
func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
