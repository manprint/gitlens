package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&slruCheck{}) }

// slruQuery reads the counters of each SLRU cache: the small buffer pools in
// front of the commit log, subtransaction parents, multixact members and
// offsets, commit timestamps, serializable conflicts and NOTIFY. The columns
// are the same from 15 to 19.
const slruQuery = `SELECT name, blks_zeroed, blks_hit, blks_read, blks_written, blks_exists, flushes, truncates
  FROM pg_catalog.pg_stat_slru`

// slruNames maps the cache names before PostgreSQL 17 to the ones 17 gave
// them, so a cache keeps one series across a major upgrade.
var slruNames = map[string]string{
	"CommitTs":        "commit_timestamp",
	"MultiXactMember": "multixact_member",
	"MultiXactOffset": "multixact_offset",
	"Notify":          "notify",
	"Serial":          "serializable",
	"Subtrans":        "subtransaction",
	"Xact":            "transaction",
}

// slruName is a cache's name as PostgreSQL 17 and later report it.
func slruName(name string) string {
	if canonical, ok := slruNames[name]; ok {
		return canonical
	}
	return name
}

// slruCheck reports the SLRU caches' counters. Reads that miss a cache go to
// disk under an exclusive lock on it: a busy multixact or subtransaction
// cache stalls every session that needs it, which the hit ratio and the read
// rate show.
type slruCheck struct{}

func (c *slruCheck) Name() string { return "slru" }
func (c *slruCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *slruCheck) DefaultInterval() time.Duration { return time.Minute }
func (c *slruCheck) Timeout() time.Duration         { return 5 * time.Second }

// slruCounters names the metric of each counter column, in query order.
var slruCounters = []string{
	"pg_slru_blks_zeroed_total", "pg_slru_blks_hit_total", "pg_slru_blks_read_total",
	"pg_slru_blks_written_total", "pg_slru_blks_exists_total", "pg_slru_flushes_total", "pg_slru_truncates_total",
}

func (c *slruCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, slruQuery)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	// Each cache has its own stats_reset (pg_stat_reset_slru resets one), so
	// the result carries no reset hint: a reset shows as that cache's
	// counters going backwards.
	var result Result
	for rows.Next() {
		var name string
		values := make([]int64, len(slruCounters))
		dest := []any{&name}
		for i := range values {
			dest = append(dest, &values[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return Result{}, err
		}
		labels := map[string]string{"slru": slruName(name)}
		for i, metric := range slruCounters {
			result.Metrics = append(result.Metrics, pgtype.Metric{Name: metric, Value: float64(values[i]), Kind: pgtype.KindCounter, Labels: labels})
		}
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}
