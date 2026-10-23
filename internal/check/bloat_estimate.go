package check

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
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

// bloatSizeQuery reads one relation's exact size, a table's or an index's. The
// size function locks the relation, so it runs only for the rows that are
// reported.
const bloatSizeQuery = `SELECT pg_relation_size(c.oid) FROM pg_class c WHERE c.oid = $1`

// bloatUnreadableFrom selects the tables pg_stats hides from this role: one
// with a column the role cannot SELECT, or one whose row security applies to
// the role. Such a table gets no estimate, and an empty result would read as
// no bloat, so the scrape reports itself partial. Only tables above the size
// floor of Scrape count, since the others are never reported anyway.
const bloatUnreadableFrom = `FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind = 'r' AND c.relpersistence <> 't' AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND GREATEST(c.relpages, 0)::bigint * current_setting('block_size')::bigint >= 1024 * 1024 AND ((c.relrowsecurity AND row_security_active(c.oid)) OR EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped AND NOT has_column_privilege(c.oid, a.attnum, 'SELECT')))`

// bloatUnreadableQuery counts the tables bloatUnreadableFrom selects.
const bloatUnreadableQuery = `SELECT count(*) ` + bloatUnreadableFrom

// bloatUnreadableNamesQuery names the largest of those tables, the ones the
// fact keeps. It runs only when the count is positive.
var bloatUnreadableNamesQuery = fmt.Sprintf(`SELECT n.nspname::text, c.relname::text `+bloatUnreadableFrom+` ORDER BY c.relpages DESC, n.nspname, c.relname LIMIT %d`, bloatNamedTables)

// bloatNamedTables bounds how many skipped tables the fact names.
const bloatNamedTables = 10

// rowKey names one row of the estimate. An index row carries its table's
// name, so the index's own name keys it, prefixed by the kind: a table and an
// index never share a key.
func (r bloatRow) rowKey() string {
	if r.Kind == "index" {
		return "index " + r.Schema + "." + r.Index
	}
	return "table " + r.Schema + "." + r.Relation
}

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
	// Tables the role cannot see are absent from rows, so they are counted
	// apart and make the result partial (see bloatUnreadableQuery).
	var unreadable int64
	if e = conn.QueryRow(ctx, bloatUnreadableQuery).Scan(&unreadable); e != nil {
		return Result{}, e
	}
	var named []wire.BloatSkippedTable
	if unreadable > 0 {
		if named, e = unreadableTables(ctx, conn); e != nil {
			return Result{}, e
		}
	}
	by := map[string]bloatRow{}
	var tables, indexes []cardinality.Candidate
	for _, r := range parsed {
		k := r.rowKey()
		by[k] = r
		cand := cardinality.Candidate{Key: k, Primary: r.Bloat, Secondary: r.Bloat}
		if r.Kind == "index" {
			indexes = append(indexes, cand)
		} else {
			tables = append(tables, cand)
		}
	}
	// Tables and indexes each get their own budget of top_n, as table_stats
	// and index_stats each do: an index never pushes a table out of the list.
	reportedTables, truncatedTables, e := c.measure(ctx, conn, t, "table", tables, by)
	if e != nil {
		return Result{}, e
	}
	reportedIndexes, truncatedIndexes, e := c.measure(ctx, conn, t, "index", indexes, by)
	if e != nil {
		return Result{}, e
	}
	reported := append(reportedTables, reportedIndexes...)
	res := bloatResult(reported, by)
	res.Metrics = append(res.Metrics, pgtype.Metric{Name: wire.BloatSkippedTablesMetric, Labels: map[string]string{}, Value: float64(unreadable), Kind: pgtype.KindGauge})
	res.Facts = []Fact{bloatUnreadableFactFor(unreadable, named)}
	res.Truncated = truncatedTables || truncatedIndexes
	res.Partial = unreadable > 0 || len(reportedTables) < len(tables) || len(reportedIndexes) < len(indexes)
	return res, nil
}

// measure runs the budget of one kind of row (its own selector, see
// scopedSelectors.nextKind), then reads the exact size of each reported
// relation and recomputes its bloat with the formula of bloat_estimate.sql.
func (c *bloatEstimateCheck) measure(ctx context.Context, conn Conn, t Target, kind string, cand []cardinality.Candidate, by map[string]bloatRow) ([]cardinality.Candidate, bool, error) {
	selector, cycle := c.selectors.nextKind(t, kind)
	sel, truncated := selector.Select(cycle, cand)
	selector.Forget(cycle)
	// A relation whose size cannot be read now (locked past lock_timeout, or
	// dropped) is left out; the others are still reported.
	reported := make([]cardinality.Candidate, 0, len(sel))
	for _, candidate := range sel {
		if !relationTimeLeft(ctx) {
			break
		}
		r := by[candidate.Key]
		var size *int64
		ok, e := readRelation(ctx, conn, bloatSizeQuery, r.OID, &size)
		if e != nil {
			return nil, false, e
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
	return reported, truncated, nil
}

// unreadableTables names up to bloatNamedTables of the tables that
// bloatUnreadableFrom selects, largest first.
func unreadableTables(ctx context.Context, conn Conn) ([]wire.BloatSkippedTable, error) {
	rows, err := conn.Query(ctx, bloatUnreadableNamesQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	named := []wire.BloatSkippedTable{}
	for rows.Next() {
		var s wire.BloatSkippedTable
		if err := rows.Scan(&s.SchemaName, &s.RelName); err != nil {
			return nil, err
		}
		named = append(named, s)
	}
	return named, rows.Err()
}

// bloatUnreadableFactFor is the fact of one scrape. It is written even when
// nothing is skipped, so that a database whose tables became readable reads
// as zero, not as the last count.
func bloatUnreadableFactFor(count int64, named []wire.BloatSkippedTable) Fact {
	if named == nil {
		named = []wire.BloatSkippedTable{}
	}
	raw, _ := json.Marshal(wire.BloatSkippedValue{Count: count, Tables: named})
	return Fact{Kind: wire.BloatUnreadableFact, Key: "unreadable_tables", ValueJSON: raw}
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
