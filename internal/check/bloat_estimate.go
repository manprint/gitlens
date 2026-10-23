package check

import (
	"context"
	_ "embed"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
)

//go:embed bloat_estimate.sql
var bloatEstimateSQL string

func init() {
	Register(&bloatEstimateCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})})
}

type bloatEstimateCheck struct {
	selectors *scopedSelectors
}
type bloatRow struct {
	Schema, Relation, Index, Kind        string
	Real, Expected, Bloat, Ratio, Tuples float64
	OID                                  uint32
}

// bloatSizeQuery reads one table's exact size. The size function locks the
// table, so it runs only for the tables that are reported.
const bloatSizeQuery = `SELECT pg_relation_size(c.oid) FROM pg_class c WHERE c.oid = $1`

func (c *bloatEstimateCheck) Name() string { return "bloat_estimate" }
func (c *bloatEstimateCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *bloatEstimateCheck) DefaultInterval() time.Duration { return 6 * time.Hour }
func (c *bloatEstimateCheck) Timeout() time.Duration         { return 120 * time.Second }

// SetTopN applies checks.bloat_estimate.top_n. The check did not implement
// TopNConfigurable, so the documented key was accepted and silently ignored.
func (c *bloatEstimateCheck) SetTopN(topN int) { c.selectors.SetTopN(topN) }
func (c *bloatEstimateCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, e := t.ConnFor(ctx, t.Database())
	if e != nil {
		return Result{}, e
	}
	defer conn.Release()
	if e = ApplySessionLimits(ctx, conn, c.Timeout()); e != nil {
		return Result{}, e
	}
	rows, e := conn.Query(ctx, bloatEstimateSQL)
	if e != nil {
		return Result{}, e
	}
	defer rows.Close()
	parsed := []bloatRow{}
	for rows.Next() {
		var r bloatRow
		if e = rows.Scan(&r.Schema, &r.Relation, &r.Index, &r.Kind, &r.Real, &r.Expected, &r.Bloat, &r.Ratio, &r.Tuples, &r.OID); e != nil {
			return Result{}, e
		}
		if r.Real < 1024*1024 || r.Tuples < 0 {
			continue
		}
		parsed = append(parsed, r)
	}
	if e = rows.Err(); e != nil {
		return Result{}, e
	}
	by := map[string]bloatRow{}
	cand := []cardinality.Candidate{}
	for _, r := range parsed {
		k := r.Schema + "." + r.Relation
		by[k] = r
		cand = append(cand, cardinality.Candidate{Key: k, Primary: r.Bloat, Secondary: r.Bloat})
	}
	// Previously c.cycle++ on a shared, unsynchronised field: two databases
	// scraping concurrently through this one registered check instance is a
	// plain data race on top of the shared-state problem scopedSelectors
	// documents.
	selector, cycle := c.selectors.next(t)
	sel, tr := selector.Select(cycle, cand)
	selector.Forget(cycle)
	// A table whose size cannot be read now (locked past lock_timeout, or
	// dropped) is left out; the others are still reported. The bloat is
	// recomputed from the exact size with the formula of bloat_estimate.sql.
	reported := make([]cardinality.Candidate, 0, len(sel))
	for _, candidate := range sel {
		r := by[candidate.Key]
		var size *int64
		ok, e := readRelation(ctx, conn, bloatSizeQuery, r.OID, &size)
		if e != nil {
			return Result{}, e
		}
		if !ok || size == nil {
			continue
		}
		r.Real = float64(*size)
		r.Bloat = 0
		if r.Real > r.Expected {
			r.Bloat = r.Real - r.Expected
		}
		r.Ratio = 0
		if r.Real > 0 {
			r.Ratio = r.Bloat / r.Real
		}
		by[candidate.Key] = r
		reported = append(reported, candidate)
	}
	res := bloatResult(reported, by)
	res.Truncated = tr
	res.Partial = len(reported) < len(cand)
	return res, nil
}
func bloatResult(sel []cardinality.Candidate, rows map[string]bloatRow) Result {
	var r Result
	for _, c := range sel {
		v, ok := rows[c.Key]
		if !ok {
			continue
		}
		l := map[string]string{"schemaname": v.Schema, "relname": v.Relation, "indexrelname": v.Index, "object_kind": v.Kind, "method": "estimate"}
		for _, m := range []struct {
			n string
			v float64
		}{{"real_bytes", v.Real}, {"expected_bytes", v.Expected}, {"bytes", v.Bloat}, {"ratio", v.Ratio}} {
			r.Metrics = append(r.Metrics, pgtype.Metric{Name: "pg_bloat_" + m.n, Labels: l, Value: m.v, Kind: pgtype.KindGauge})
		}
	}
	return r
}
