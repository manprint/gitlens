package check

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&indexStatsCheck{selector: cardinality.NewSelector(cardinality.Options{TopN: 50})})
}

type indexStatsCheck struct {
	mu       sync.Mutex
	selector *cardinality.Selector
	cycle    uint64
}

type indexStatsRow struct {
	Schema, Relation, Index                                   string
	IdxScan, IdxTupRead, IdxTupFetch, IdxBlksRead, IdxBlksHit int64
	Bytes                                                     int64
	Unique, Primary, Valid                                    bool
	Definition                                                string
}

func (c *indexStatsCheck) Name() string { return "index_stats" }
func (c *indexStatsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *indexStatsCheck) DefaultInterval() time.Duration { return 5 * time.Minute }
func (c *indexStatsCheck) Timeout() time.Duration         { return 30 * time.Second }
func (c *indexStatsCheck) SetTopN(n int) {
	if n > 0 {
		c.mu.Lock()
		c.selector = cardinality.NewSelector(cardinality.Options{TopN: n})
		c.cycle = 0
		c.mu.Unlock()
	}
}

func (c *indexStatsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, `
SELECT s.schemaname, s.relname, s.indexrelname,
       COALESCE(s.idx_scan,0), COALESCE(s.idx_tup_read,0), COALESCE(s.idx_tup_fetch,0),
       COALESCE(io.idx_blks_read,0), COALESCE(io.idx_blks_hit,0), pg_relation_size(s.indexrelid),
       ix.indisunique, ix.indisprimary, ix.indisvalid, pg_get_indexdef(s.indexrelid)
FROM pg_stat_user_indexes s
JOIN pg_index ix ON ix.indexrelid = s.indexrelid
LEFT JOIN pg_statio_user_indexes io ON io.indexrelid = s.indexrelid`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	parsed := make([]indexStatsRow, 0)
	for rows.Next() {
		var r indexStatsRow
		if err := rows.Scan(&r.Schema, &r.Relation, &r.Index, &r.IdxScan, &r.IdxTupRead, &r.IdxTupFetch, &r.IdxBlksRead, &r.IdxBlksHit, &r.Bytes, &r.Unique, &r.Primary, &r.Valid, &r.Definition); err != nil {
			return Result{}, err
		}
		parsed = append(parsed, r)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	c.mu.Lock()
	c.cycle++
	cycle := c.cycle
	selector := c.selector
	c.mu.Unlock()
	byKey := make(map[string]indexStatsRow, len(parsed))
	candidates := make([]cardinality.Candidate, 0, len(parsed))
	for _, r := range parsed {
		key := r.Schema + "." + r.Index
		byKey[key] = r
		candidates = append(candidates, cardinality.Candidate{Key: key, Primary: float64(r.Bytes) - float64(r.IdxScan), Secondary: float64(r.Bytes)})
	}
	selected, truncated := selector.Select(cycle, candidates)
	selector.Forget(cycle)
	result := indexStatsResult(selected, byKey)
	result.Truncated = truncated
	return result, nil
}

func indexStatsResult(selected []cardinality.Candidate, rows map[string]indexStatsRow) Result {
	var result Result
	for _, candidate := range selected {
		r, ok := rows[candidate.Key]
		if !ok {
			continue
		}
		labels := map[string]string{"schemaname": r.Schema, "relname": r.Relation, "indexrelname": r.Index}
		add := func(name string, v float64, k pgtype.MetricKind) {
			result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_index_" + name, Labels: labels, Value: v, Kind: k})
		}
		for _, m := range []struct {
			n string
			v int64
		}{{"idx_scan", r.IdxScan}, {"idx_tup_read", r.IdxTupRead}, {"idx_tup_fetch", r.IdxTupFetch}, {"idx_blks_read", r.IdxBlksRead}, {"idx_blks_hit", r.IdxBlksHit}} {
			add(m.n, float64(m.v), pgtype.KindCounter)
		}
		add("index_bytes", float64(r.Bytes), pgtype.KindGauge)
		add("is_unique", boolFloat(r.Unique), pgtype.KindGauge)
		add("is_primary", boolFloat(r.Primary), pgtype.KindGauge)
		add("is_valid", boolFloat(r.Valid), pgtype.KindGauge)
		factLabels := map[string]string{"schemaname": r.Schema, "relname": r.Relation, "indexrelname": r.Index, "def_hash": definitionHash(r.Definition)}
		result.Facts = append(result.Facts, Fact{Kind: "index_def", Key: r.Schema + "." + r.Index, Labels: factLabels, ValueText: r.Definition})
	}
	return result
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func definitionHash(def string) string { return hashNormalizedDefinition(def) }
func hashNormalizedDefinition(def string) string {
	start := strings.Index(def, "(")
	end := strings.LastIndex(def, ")")
	if start < 0 || end < start {
		sum := sha256.Sum256([]byte(strings.Join(strings.Fields(strings.ToLower(def)), " ")))
		return hex.EncodeToString(sum[:])
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(def[start:end+1])), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}
