package main

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/redact"
	"github.com/manprint/pglens/internal/sqltag"
	"github.com/manprint/pglens/internal/wire"
)

// Automatic plans from auto_explain (plan 007 phase 7).
const (
	planFactKind             = "plan"
	planFactsPerScrape       = 50
	planBodyMaxBytes         = 262144
	planMemoTTL              = time.Hour
	planMemoMax              = 5000
	planParametersMaxBytes   = 4096
	autoExplainFormatNotJSON = "auto_explain_format_not_json"
)

// planDropReasons are the reasons of pg_log_plans_dropped, always reported.
var planDropReasons = []string{"no_queryid", "budget", "credential", "format"}

// planMemo remembers the (datname, queryid, plan_hash) shipped with a body
// during the last hour, so later samples of the same plan ship without it.
type planMemo struct {
	seen map[string]time.Time
}

// shipBody reports whether a sample of key carries the plan body, and records
// it when it does.
func (m *planMemo) shipBody(key string, now time.Time) bool {
	if m.seen == nil {
		m.seen = map[string]time.Time{}
	}
	if at, ok := m.seen[key]; ok && now.Sub(at) < planMemoTTL {
		return false
	}
	if len(m.seen) >= planMemoMax {
		oldestKey, oldest := "", time.Time{}
		for k, at := range m.seen {
			if now.Sub(at) >= planMemoTTL {
				delete(m.seen, k)
				continue
			}
			if oldestKey == "" || at.Before(oldest) {
				oldestKey, oldest = k, at
			}
		}
		if len(m.seen) >= planMemoMax {
			delete(m.seen, oldestKey)
		}
	}
	m.seen[key] = now
	return true
}

// planScrape collects the plan facts of one scrape.
type planScrape struct {
	memo          *planMemo
	now           time.Time
	facts         []wire.Fact
	read          int
	dropped       map[string]int
	formatNotJSON bool
	// parameters ships Query Parameters, cut to planParametersMaxBytes.
	parameters bool
}

func newPlanScrape(memo *planMemo, now time.Time) *planScrape {
	if memo == nil {
		memo = &planMemo{}
	}
	return &planScrape{memo: memo, now: now, dropped: map[string]int{}}
}

// add turns one autoexplain.plan entry into a plan fact, or counts why not.
// A sample whose statement or plan may hold a credential, or whose shipped
// parameters may, is dropped whole (I-4). Parameter values never reach a
// label, a key, a metric or a log line.
func (ps *planScrape) add(e pglog.Entry) {
	ps.read++
	p, err := pglog.ParseAutoExplain(e.Message)
	if err != nil {
		ps.dropped["format"]++
		ps.formatNotJSON = ps.formatNotJSON || errors.Is(err, pglog.ErrPlanFormat)
		return
	}
	if e.QueryID == 0 {
		ps.dropped["no_queryid"]++
		return
	}
	if redact.MayHoldCredential(p.QueryText) || redact.MayHoldCredential(string(p.Plan)) || (ps.parameters && redact.MayHoldCredential(p.Parameters)) {
		ps.dropped["credential"]++
		return
	}
	if len(ps.facts) >= planFactsPerScrape {
		ps.dropped["budget"]++
		return
	}
	hash, err := agent.PlanHash(p.Plan)
	if err != nil {
		ps.dropped["format"]++
		return
	}
	queryID := strconv.FormatInt(e.QueryID, 10)
	body := map[string]any{}
	if len(p.Plan) <= planBodyMaxBytes && ps.memo.shipBody(e.Database+"\x00"+queryID+"\x00"+hash, ps.now) {
		body["plan"] = p.Plan
	}
	if ps.parameters && p.Parameters != "" {
		body["parameters"] = cutUTF8(p.Parameters, planParametersMaxBytes)
	}
	value, err := json.Marshal(body)
	if err != nil {
		ps.dropped["format"]++
		return
	}
	labels := map[string]string{
		"source":      "auto_explain",
		"ts":          e.Time.UTC().Format(time.RFC3339Nano),
		"datname":     e.Database,
		"queryid":     queryID,
		"plan_hash":   hash,
		"analyzed":    strconv.FormatBool(p.Analyzed),
		"duration_ms": strconv.FormatFloat(p.DurationMS, 'f', -1, 64),
	}
	if id := sqltag.TraceID(sqltag.Parse(p.QueryText)); id != "" {
		labels["trace_id"] = id
	}
	ps.facts = append(ps.facts, wire.Fact{
		Kind:      planFactKind,
		Key:       queryID + ":" + hash + ":" + strconv.FormatInt(e.Time.UnixNano(), 10),
		Labels:    labels,
		ValueJSON: value,
	})
}

func (ps *planScrape) metrics() []wire.Metric {
	out := []wire.Metric{gauge("pg_log_plans", float64(ps.read), nil)}
	for _, r := range planDropReasons {
		out = append(out, gauge("pg_log_plans_dropped", float64(ps.dropped[r]), map[string]string{"reason": r}))
	}
	return out
}

// cutUTF8 returns the longest prefix of s of at most max bytes that ends on a
// character boundary.
func cutUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
