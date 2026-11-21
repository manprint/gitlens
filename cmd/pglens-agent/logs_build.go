package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/redact"
	"github.com/manprint/pglens/internal/wire"
)

const (
	logsCheck          = "logs"
	logEventKind       = "log_event"
	logFactsPerScrape  = 200
	logMessageMaxBytes = 2000
	logTextMaxBytes    = 8000
	logWithheld        = "[withheld: may hold a credential]"
)

// logScrape is what one interval read: the parsed entries and the byte counts.
type logScrape struct {
	entries       []pglog.Entry
	bytesRead     int64
	bytesSkipped  int64
	statementText bool
	// plans remembers the plan bodies already shipped; nil starts empty.
	plans *planMemo
	// planParameters ships the Query Parameters of automatic plans
	// (checks.logs.plan_parameters, off by default).
	planParameters bool
}

// logText cuts a text to max bytes on a rune boundary and withholds any text
// that may hold a credential (I-4).
func logText(s string, max int) string {
	if s == "" {
		return ""
	}
	if redact.MayHoldCredential(s) {
		return logWithheld
	}
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// logEventFact turns one shipped entry into a log_event fact.
func logEventFact(e pglog.Entry, class string, statementText bool) (wire.Fact, bool) {
	msg := e.Message
	if class == pglog.ClassAutovacuumRun {
		msg, _, _ = strings.Cut(msg, "\n")
	}
	body := map[string]string{"message": logText(msg, logMessageMaxBytes)}
	if statementText {
		for k, v := range map[string]string{"detail": e.Detail, "hint": e.Hint, "context": e.Context, "statement": e.Statement} {
			if t := logText(v, logTextMaxBytes); t != "" {
				body[k] = t
			}
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return wire.Fact{}, false
	}
	labels := map[string]string{
		"ts":       e.Time.UTC().Format(time.RFC3339Nano),
		"class":    class,
		"severity": e.Severity,
		"sqlstate": e.SQLState,
		"datname":  e.Database,
		"usename":  e.User,
		// application_name is free text set by the client.
		"application_name": logText(e.ApplicationName, logMessageMaxBytes),
	}
	if e.PID != 0 {
		labels["pid"] = strconv.Itoa(e.PID)
	}
	if e.QueryID != 0 {
		labels["queryid"] = strconv.FormatInt(e.QueryID, 10)
	}
	for k, v := range labels {
		if v == "" {
			delete(labels, k)
		}
	}
	key := e.SessionID + "/" + strconv.FormatInt(e.LineNum, 10)
	return wire.Fact{Kind: logEventKind, Key: key, Labels: labels, ValueJSON: raw}, true
}

func gauge(name string, v float64, labels map[string]string) wire.Metric {
	return wire.Metric{Name: name, Labels: labels, Value: v, Kind: string(pgtype.KindGauge)}
}

// logResult builds the logs check result of one scrape: per-class line
// counts, the always-present problem counters, byte counts, at most
// logFactsPerScrape log_event facts, at most autovacuumFactsPerScrape
// autovacuum_run facts and at most planFactsPerScrape plan facts.
// formatNotJSON reports an auto_explain plan written in another format.
func logResult(s logScrape, now time.Time) (r wire.Result, formatNotJSON bool) {
	perClass := map[string]int{}
	plans := newPlanScrape(s.plans, now)
	plans.parameters = s.planParameters
	var facts, runs []wire.Fact
	runsDropped := 0
	dropped := 0
	for _, e := range s.entries {
		class := pglog.Classify(e)
		if class == pglog.ClassNone {
			continue
		}
		perClass[class]++
		if class == pglog.ClassAutovacuumRun {
			if f, ok := autovacuumFact(e); ok {
				if len(runs) < autovacuumFactsPerScrape {
					runs = append(runs, f)
				} else {
					runsDropped++
				}
			}
		}
		if class == pglog.ClassAutoExplainPlan {
			plans.add(e)
			continue
		}
		if !pglog.Shipped(class) {
			continue
		}
		if len(facts) >= logFactsPerScrape {
			dropped++
			continue
		}
		if f, ok := logEventFact(e, class, s.statementText); ok {
			facts = append(facts, f)
		}
	}
	classes := make([]string, 0, len(perClass))
	for c := range perClass {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	metrics := make([]wire.Metric, 0, len(classes)+12)
	for _, c := range classes {
		metrics = append(metrics, gauge("pg_log_events", float64(perClass[c]), map[string]string{"class": c}))
	}
	metrics = append(metrics,
		gauge("pg_log_panic_events", float64(perClass[pglog.ClassPanic]+perClass[pglog.ClassServerCrash]), nil),
		gauge("pg_log_corruption_events", float64(perClass[pglog.ClassCorruption]), nil),
		gauge("pg_log_disk_full_events", float64(perClass[pglog.ClassDiskFull]), nil),
		gauge("pg_log_oom_events", float64(perClass[pglog.ClassMemoryOut]), nil),
		gauge("pg_log_bytes_read", float64(s.bytesRead), nil),
		gauge("pg_log_bytes_skipped", float64(s.bytesSkipped), nil),
		gauge("pg_log_events_dropped", float64(dropped), nil),
		gauge("pg_log_autovacuum_dropped", float64(runsDropped), nil),
	)
	metrics = append(metrics, plans.metrics()...)
	facts = append(facts, runs...)
	facts = append(facts, plans.facts...)
	return wire.Result{Check: logsCheck, TS: now, Metrics: metrics, Facts: facts}, plans.formatNotJSON
}
