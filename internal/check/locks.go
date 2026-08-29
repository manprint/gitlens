package check

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&locksCheck{}) }

type locksCheck struct{}

func (c *locksCheck) Name() string { return "locks" }
func (c *locksCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *locksCheck) DefaultInterval() time.Duration { return 10 * time.Second }
func (c *locksCheck) Timeout() time.Duration         { return 10 * time.Second }

type lockRow struct {
	PID             int32
	Datname         string
	Username        string
	ApplicationName string
	ClientAddr      string
	State           string
	WaitEventType   string
	WaitEvent       string
	XactAge         float64
	StateAge        float64
	Query           string
	BlockedBy       []int32
}

type lockTree struct {
	SampledAt time.Time  `json:"sampled_at"`
	Nodes     []lockNode `json:"nodes"`
}

type lockNode struct {
	PID             int32   `json:"pid"`
	Datname         string  `json:"datname"`
	Username        string  `json:"usename"`
	ApplicationName string  `json:"application_name"`
	ClientAddr      string  `json:"client_addr"`
	State           string  `json:"state"`
	WaitEventType   string  `json:"wait_event_type"`
	WaitEvent       string  `json:"wait_event"`
	XactAge         float64 `json:"xact_age"`
	StateAge        float64 `json:"state_age"`
	Query           string  `json:"query"`
	BlockedBy       []int32 `json:"blocked_by"`
}

const locksQuery = `
SELECT a.pid,
       a.datname,
       a.usename,
       a.application_name,
       a.client_addr::text,
       a.state,
       COALESCE(a.wait_event_type, ''),
       COALESCE(a.wait_event, ''),
       COALESCE(EXTRACT(epoch FROM now() - a.xact_start), 0),
       COALESCE(EXTRACT(epoch FROM now() - a.state_change), 0),
       COALESCE(a.query, ''),
       pg_blocking_pids(a.pid)
FROM pg_stat_activity a
WHERE a.backend_type = 'client backend'
  AND a.pid <> pg_backend_pid()`

func (c *locksCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, locksQuery)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var parsed []lockRow
	for rows.Next() {
		var r lockRow
		if err := rows.Scan(&r.PID, &r.Datname, &r.Username, &r.ApplicationName, &r.ClientAddr, &r.State, &r.WaitEventType, &r.WaitEvent, &r.XactAge, &r.StateAge, &r.Query, &r.BlockedBy); err != nil {
			return Result{}, err
		}
		if len(r.Query) > 2048 {
			r.Query = r.Query[:2048]
		}
		parsed = append(parsed, r)
	}
	return buildLocksResult(parsed, t.Clock().Now()), rows.Err()
}

func buildLocksResult(rows []lockRow, sampledAt time.Time) Result {
	blocked := 0
	blocking := make(map[int32]struct{})
	waits := make(map[string]int)
	maxAge := 0.0
	nodes := make([]lockNode, 0, len(rows))
	for _, r := range rows {
		if len(r.BlockedBy) == 0 {
			continue
		}
		blocked++
		for _, pid := range r.BlockedBy {
			blocking[pid] = struct{}{}
		}
		waits[r.WaitEvent]++
		if r.StateAge > maxAge {
			maxAge = r.StateAge
		}
		nodes = append(nodes, lockNode(r))
	}
	metrics := []pgtype.Metric{
		{Name: "pg_blocked_sessions", Value: float64(blocked), Kind: pgtype.KindGauge},
		{Name: "pg_blocking_sessions", Value: float64(len(blocking)), Kind: pgtype.KindGauge},
		{Name: "pg_max_block_age_seconds", Value: maxAge, Kind: pgtype.KindGauge},
	}
	type waitCount struct {
		name  string
		count int
	}
	ordered := make([]waitCount, 0, len(waits))
	for name, count := range waits {
		ordered = append(ordered, waitCount{name, count})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].count == ordered[j].count {
			return ordered[i].name < ordered[j].name
		}
		return ordered[i].count > ordered[j].count
	})
	other := 0
	for i, w := range ordered {
		if i < 10 {
			metrics = append(metrics, pgtype.Metric{Name: "pg_lock_waits", Value: float64(w.count), Kind: pgtype.KindGauge, Labels: map[string]string{"wait_event": w.name}})
		} else {
			other += w.count
		}
	}
	if other > 0 {
		metrics = append(metrics, pgtype.Metric{Name: "pg_lock_waits", Value: float64(other), Kind: pgtype.KindGauge, Labels: map[string]string{"wait_event": "other"}})
	}
	result := Result{Metrics: metrics}
	// Persist an empty tree as well as a populated one. Without the empty
	// snapshot, a released blocker remains visible in the API until the
	// fifteen-minute retention cleanup, rather than disappearing on the next
	// locks scrape.
	if raw, err := json.Marshal(lockTree{SampledAt: sampledAt, Nodes: nodes}); err == nil {
		result.Facts = []Fact{{Kind: "lock_tree", Key: "current", ValueJSON: raw}}
	}
	return result
}
