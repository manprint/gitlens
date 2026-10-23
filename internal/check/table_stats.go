package check

import (
	"context"
	"sort"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&tableStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})})
}

type tableStatsCheck struct {
	selectors *scopedSelectors
}

type tableStatsRow struct {
	Schema, Relation                                         string
	SeqScan, SeqTupRead, IdxScan, IdxTupFetch                int64
	NTupIns, NTupUpd, NTupDel, NTupHotUpd                    int64
	NLiveTup, NDeadTup, NModSinceAnalyze                     int64
	LastVacuum, LastAutovacuum, LastAnalyze, LastAutoanalyze *time.Time
	AutovacuumCount, AutoanalyzeCount                        float64
	Relpages                                                 int64
	RelTuples                                                float64
	RelfrozenXIDAge                                          int64
	TotalBytes, TableBytes, ToastBytes                       int64
}

func (c *tableStatsCheck) Name() string { return "table_stats" }
func (c *tableStatsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *tableStatsCheck) DefaultInterval() time.Duration { return 5 * time.Minute }
func (c *tableStatsCheck) Timeout() time.Duration         { return 30 * time.Second }
func (c *tableStatsCheck) SetTopN(topN int)               { c.selectors.SetTopN(topN) }

func (c *tableStatsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, `
SELECT s.schemaname, s.relname,
       COALESCE(s.seq_scan, 0), COALESCE(s.seq_tup_read, 0), COALESCE(s.idx_scan, 0), COALESCE(s.idx_tup_fetch, 0),
       COALESCE(s.n_tup_ins, 0), COALESCE(s.n_tup_upd, 0), COALESCE(s.n_tup_del, 0), COALESCE(s.n_tup_hot_upd, 0),
       COALESCE(s.n_live_tup, 0), COALESCE(s.n_dead_tup, 0), COALESCE(s.n_mod_since_analyze, 0),
       s.last_vacuum, s.last_autovacuum, s.last_analyze, s.last_autoanalyze,
       COALESCE(s.autovacuum_count, 0), COALESCE(s.autoanalyze_count, 0),
       COALESCE(c.relpages, 0), COALESCE(c.reltuples, 0), age(c.relfrozenxid),
       pg_total_relation_size(c.oid), pg_table_size(c.oid),
       COALESCE(pg_total_relation_size(c.reltoastrelid), 0)
FROM pg_stat_user_tables s
JOIN pg_class c ON c.oid = s.relid
WHERE c.relkind = 'r'`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var parsed []tableStatsRow
	for rows.Next() {
		var r tableStatsRow
		if err := rows.Scan(&r.Schema, &r.Relation, &r.SeqScan, &r.SeqTupRead, &r.IdxScan, &r.IdxTupFetch,
			&r.NTupIns, &r.NTupUpd, &r.NTupDel, &r.NTupHotUpd, &r.NLiveTup, &r.NDeadTup, &r.NModSinceAnalyze,
			&r.LastVacuum, &r.LastAutovacuum, &r.LastAnalyze, &r.LastAutoanalyze, &r.AutovacuumCount,
			&r.AutoanalyzeCount, &r.Relpages, &r.RelTuples, &r.RelfrozenXIDAge, &r.TotalBytes, &r.TableBytes, &r.ToastBytes); err != nil {
			return Result{}, err
		}
		parsed = append(parsed, r)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	selector, cycle := c.selectors.next(t)
	selected, truncated := selectRelations(selector, cycle, parsed)
	byKey := make(map[string]tableStatsRow, len(parsed))
	for _, r := range parsed {
		byKey[relationKey(r)] = r
	}
	result := tableStatsResult(selected, byKey, t.Clock().Now())
	// Truncated is the hard MaxKeys cap only. Reporting the top-N subset of
	// a database with more tables than that is the normal case, and flagging
	// it as truncated kept cardinality_budget_exceeded firing on every real
	// database and stopped the server from ever pruning dropped tables.
	result.Truncated = truncated
	result.Partial = len(selected) < len(parsed)
	return result, nil
}

// oldestRelationsKept is how many relations per database are reported on top
// of the dead-tuple and size selection, ranked by relfrozenxid age. Append-only
// tables have no dead tuples and are usually small, so neither ranking of the
// selector reaches them, and they are the relations closest to wraparound.
const oldestRelationsKept = 20

// selectRelations picks the relations one scrape reports: the selector's
// dead-tuple and size selection, widened by the oldest relations by
// relfrozenxid age. The selector keeps its own state across scrapes, and the
// age ranking is recomputed each time without touching that state. The union
// can exceed the selector's MaxKeys by at most oldestRelationsKept.
func selectRelations(selector *cardinality.Selector, cycle uint64, parsed []tableStatsRow) ([]cardinality.Candidate, bool) {
	selected, truncated := selector.Select(cycle, relationCandidates(parsed))
	selector.Forget(cycle)
	held := make(map[string]bool, len(selected))
	for _, c := range selected {
		held[c.Key] = true
	}
	for _, key := range oldestRelationKeys(parsed, oldestRelationsKept) {
		if !held[key] {
			held[key] = true
			// tableStatsResult reads only the key of a selected candidate.
			selected = append(selected, cardinality.Candidate{Key: key})
		}
	}
	return selected, truncated
}

func relationKey(r tableStatsRow) string { return r.Schema + "." + r.Relation }

func relationCandidates(parsed []tableStatsRow) []cardinality.Candidate {
	candidates := make([]cardinality.Candidate, 0, len(parsed))
	for _, r := range parsed {
		candidates = append(candidates, cardinality.Candidate{Key: relationKey(r), Primary: float64(r.NDeadTup), Secondary: float64(r.TotalBytes)})
	}
	return candidates
}

// oldestRelationKeys returns the keys of the k relations with the greatest
// relfrozenxid age, oldest first, ties broken by key.
func oldestRelationKeys(rows []tableStatsRow, k int) []string {
	ranked := make([]tableStatsRow, len(rows))
	copy(ranked, rows)
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].RelfrozenXIDAge != ranked[j].RelfrozenXIDAge {
			return ranked[i].RelfrozenXIDAge > ranked[j].RelfrozenXIDAge
		}
		return relationKey(ranked[i]) < relationKey(ranked[j])
	})
	if len(ranked) > k {
		ranked = ranked[:k]
	}
	keys := make([]string, 0, len(ranked))
	for _, r := range ranked {
		keys = append(keys, relationKey(r))
	}
	return keys
}

func tableStatsResult(selected []cardinality.Candidate, rows map[string]tableStatsRow, now time.Time) Result {
	var result Result
	for _, candidate := range selected {
		r, ok := rows[candidate.Key]
		if !ok {
			continue
		}
		labels := map[string]string{"schemaname": r.Schema, "relname": r.Relation}
		add := func(name string, value float64, kind pgtype.MetricKind) {
			result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_table_" + name, Labels: labels, Value: value, Kind: kind})
		}
		for _, m := range []struct {
			name  string
			value int64
		}{
			{"seq_scan", r.SeqScan}, {"seq_tup_read", r.SeqTupRead}, {"idx_scan", r.IdxScan}, {"idx_tup_fetch", r.IdxTupFetch},
			{"n_tup_ins", r.NTupIns}, {"n_tup_upd", r.NTupUpd}, {"n_tup_del", r.NTupDel}, {"n_tup_hot_upd", r.NTupHotUpd},
		} {
			add(m.name, float64(m.value), pgtype.KindCounter)
		}
		for _, m := range []struct {
			name  string
			value float64
		}{
			{"n_live_tup", float64(r.NLiveTup)}, {"n_dead_tup", float64(r.NDeadTup)}, {"n_mod_since_analyze", float64(r.NModSinceAnalyze)},
			{"autovacuum_count", r.AutovacuumCount}, {"autoanalyze_count", r.AutoanalyzeCount}, {"relpages", float64(r.Relpages)}, {"reltuples", r.RelTuples},
			{"relfrozenxid_age", float64(r.RelfrozenXIDAge)}, {"total_bytes", float64(r.TotalBytes)}, {"table_bytes", float64(r.TableBytes)}, {"toast_bytes", float64(r.ToastBytes)},
		} {
			add(m.name, m.value, pgtype.KindGauge)
		}
		deadTupleRatio := 0.0
		if r.NLiveTup > 0 {
			deadTupleRatio = float64(r.NDeadTup) / float64(r.NLiveTup)
		}
		// Keep the typed relation contract dense even for an empty table:
		// the ratio is defined as zero when there are no live tuples, rather
		// than being persisted as NULL and making consumers handle a missing
		// metric on an otherwise valid table row.
		add("dead_tuple_ratio", deadTupleRatio, pgtype.KindGauge)
		for _, m := range []struct {
			name  string
			value *time.Time
		}{
			{"last_vacuum", r.LastVacuum}, {"last_autovacuum", r.LastAutovacuum}, {"last_analyze", r.LastAnalyze}, {"last_autoanalyze", r.LastAutoanalyze},
		} {
			if m.value != nil {
				age := now.Sub(*m.value).Seconds()
				if age < 0 {
					age = 0
				}
				add(m.name+"_age_seconds", age, pgtype.KindGauge)
			}
		}
	}
	return result
}
