package check

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"

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
	// ScanTracked reports that the server can read last_idx_scan (PostgreSQL
	// 16 and later). LastScan is the last scan since Reset, nil when the index
	// was not scanned since then. Reset is the later of the database's and the
	// index's own statistics reset, nil when neither is known.
	ScanTracked bool
	LastScan    *time.Time
	Reset       *time.Time
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
	// last_idx_scan exists from PostgreSQL 16 and the index's own stats_reset
	// from 19. Older servers select NULL in their place, so the query reads the
	// same columns on every version. The database's reset is read on every
	// version from pg_stat_database.
	scanTracked := t.PGVersion() >= pgtype.PG16
	lastScanExpr, indexResetExpr := "NULL::timestamptz", "NULL::timestamptz"
	if scanTracked {
		lastScanExpr = "s.last_idx_scan"
	}
	if t.PGVersion() >= pgtype.PG19 {
		indexResetExpr = "s.stats_reset"
	}
	// pg_stat_user_indexes lists the indexes of every session's temporary tables
	// under their pg_temp_N schemas. They are gone with their session, so they
	// are left out, as table_stats leaves its temporary tables out.
	rows, err := conn.Query(ctx, `
SELECT s.schemaname, s.relname, s.indexrelname,
       COALESCE(s.idx_scan,0), COALESCE(s.idx_tup_read,0), COALESCE(s.idx_tup_fetch,0),
       COALESCE(io.idx_blks_read,0), COALESCE(io.idx_blks_hit,0),
       (GREATEST(ic.relpages, 0)::bigint * current_setting('block_size')::bigint),
       ix.indisunique, ix.indisprimary, ix.indisvalid, s.indexrelid,
       `+lastScanExpr+`, `+indexResetExpr+`,
       (SELECT d.stats_reset FROM pg_stat_database d WHERE d.datname = current_database())
FROM pg_stat_user_indexes s
JOIN pg_index ix ON ix.indexrelid = s.indexrelid
JOIN pg_class ic ON ic.oid = s.indexrelid
LEFT JOIN pg_statio_user_indexes io ON io.indexrelid = s.indexrelid
WHERE ic.relpersistence <> 't'`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	parsed := make([]indexStatsRow, 0)
	for rows.Next() {
		var r indexStatsRow
		var indexReset, databaseReset *time.Time
		if err := rows.Scan(&r.Schema, &r.Relation, &r.Index, &r.IdxScan, &r.IdxTupRead, &r.IdxTupFetch, &r.IdxBlksRead, &r.IdxBlksHit, &r.EstBytes, &r.Unique, &r.Primary, &r.Valid, &r.OID, &r.LastScan, &indexReset, &databaseReset); err != nil {
			return Result{}, err
		}
		r.ScanTracked = scanTracked
		r.Reset = latestTime(indexReset, databaseReset)
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
		if !relationTimeLeft(ctx) {
			break
		}
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
		// last_idx_scan is absent on PostgreSQL 15, where the server cannot tell
		// an unscanned index from one it does not know about. On 16 and later it
		// is empty when the index was never scanned since stats_reset, which is
		// empty when no reset is known.
		if r.ScanTracked {
			factLabels["last_idx_scan"] = formatScanTime(r.LastScan)
			factLabels["stats_reset"] = formatScanTime(r.Reset)
		}
		result.Facts = append(result.Facts, Fact{Kind: "index_def", Key: r.Schema + "." + r.Index, Labels: factLabels, ValueText: r.Definition})
	}
	return result
}

// formatScanTime renders a last scan or statistics reset time for a fact
// label. Nil renders as the empty string.
func formatScanTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// latestTime is the later of the two times that are set, nil when neither is.
func latestTime(a, b *time.Time) *time.Time {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case b.After(*a):
		return b
	}
	return a
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
		sum := sha256.Sum256([]byte(normalizeDefinition(def)))
		return hex.EncodeToString(sum[:])
	}
	normalized := normalizeDefinition(def[start : end+1])
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// normalizeDefinition lowercases the definition and collapses its whitespace,
// but only outside quoted text. A string literal and a quoted identifier are
// case-sensitive: 'ACTIVE' and 'active' select different rows, and "Name" and
// "NAME" are different columns. A doubled quote inside a span closes and
// reopens it, so the text is kept either way.
//
// The input is always pg_get_indexdef output, which never writes a backslash
// escape: it doubles a quote, and it writes a backslash as it is or doubled
// depending on standard_conforming_strings (checked on PostgreSQL 15 and 18).
// It never writes an E-prefixed literal either. A backslash is therefore not an
// escape here, and a quote always closes a span. Text that is not from that
// function may not be normalised this way.
func normalizeDefinition(def string) string {
	var b strings.Builder
	var quote rune
	pendingSpace := false
	for _, r := range def {
		if quote == 0 && unicode.IsSpace(r) {
			pendingSpace = b.Len() > 0
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		default:
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
