package check

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&indexStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})})
}

type indexStatsCheck struct {
	selectors *scopedSelectors
}

type indexStatsRow struct {
	Schema, Relation, Index                                   string
	IdxScan, IdxTupRead, IdxTupFetch, IdxBlksRead, IdxBlksHit int64
	Bytes                                                     int64
	Unique, Primary, Valid                                    bool
	Definition                                                string
	// EstBytes ranks the index before its exact size is read: the pages of
	// the index as of the last VACUUM or ANALYZE.
	EstBytes int64
	OID      uint32
}

// indexDefinitionQuery reads one index's exact size and definition. Both
// lock the index's table, so they run only for the indexes that are reported.
const indexDefinitionQuery = `SELECT pg_relation_size(c.oid), pg_get_indexdef(c.oid) FROM pg_class c WHERE c.oid = $1`

func (c *indexStatsCheck) Name() string { return "index_stats" }
func (c *indexStatsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *indexStatsCheck) DefaultInterval() time.Duration { return 5 * time.Minute }
func (c *indexStatsCheck) Timeout() time.Duration         { return 30 * time.Second }
func (c *indexStatsCheck) SetTopN(n int)                  { c.selectors.SetTopN(n) }

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
       COALESCE(io.idx_blks_read,0), COALESCE(io.idx_blks_hit,0),
       (GREATEST(ic.relpages, 0)::bigint * current_setting('block_size')::bigint),
       ix.indisunique, ix.indisprimary, ix.indisvalid, s.indexrelid
FROM pg_stat_user_indexes s
JOIN pg_index ix ON ix.indexrelid = s.indexrelid
JOIN pg_class ic ON ic.oid = s.indexrelid
LEFT JOIN pg_statio_user_indexes io ON io.indexrelid = s.indexrelid`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	parsed := make([]indexStatsRow, 0)
	for rows.Next() {
		var r indexStatsRow
		if err := rows.Scan(&r.Schema, &r.Relation, &r.Index, &r.IdxScan, &r.IdxTupRead, &r.IdxTupFetch, &r.IdxBlksRead, &r.IdxBlksHit, &r.EstBytes, &r.Unique, &r.Primary, &r.Valid, &r.OID); err != nil {
			return Result{}, err
		}
		parsed = append(parsed, r)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	selector, cycle := c.selectors.next(t)
	byKey := make(map[string]indexStatsRow, len(parsed))
	candidates := make([]cardinality.Candidate, 0, len(parsed))
	for _, r := range parsed {
		key := r.Schema + "." + r.Index
		byKey[key] = r
		candidates = append(candidates, cardinality.Candidate{Key: key, Primary: float64(r.EstBytes) - float64(r.IdxScan), Secondary: float64(r.EstBytes)})
	}
	selected, truncated := selector.Select(cycle, candidates)
	selector.Forget(cycle)
	// An index whose size or definition cannot be read now (its table is
	// locked past lock_timeout, or it was dropped) is left out; the others are
	// still reported.
	reported := make([]cardinality.Candidate, 0, len(selected))
	for _, candidate := range selected {
		r := byKey[candidate.Key]
		var bytes *int64
		var definition *string
		ok, err := readRelation(ctx, conn, indexDefinitionQuery, r.OID, &bytes, &definition)
		if err != nil {
			return Result{}, err
		}
		if !ok || bytes == nil || definition == nil {
			continue
		}
		r.Bytes, r.Definition = *bytes, *definition
		byKey[candidate.Key] = r
		reported = append(reported, candidate)
	}
	result := indexStatsResult(reported, byKey)
	result.Truncated = truncated
	result.Partial = len(reported) < len(candidates)
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
