package check

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestIndexStats_MetricNamesMatchColumns(t *testing.T) {
	r := indexStatsRow{Schema: "public", Relation: "orders", Index: "orders_a_idx", Definition: "CREATE INDEX orders_a_idx ON public.orders USING btree (a)"}
	result := indexStatsResult([]cardinality.Candidate{{Key: "public.orders_a_idx"}}, map[string]indexStatsRow{"public.orders_a_idx": r})
	names := map[string]bool{}
	for _, m := range result.Metrics {
		names[m.Name] = true
	}
	for _, n := range []string{"idx_scan", "idx_tup_read", "idx_tup_fetch", "idx_blks_read", "idx_blks_hit", "index_bytes", "is_unique", "is_primary", "is_valid"} {
		require.True(t, names["pg_index_"+n])
	}
}

func TestIndexStats_EmitsIndexDefFact(t *testing.T) {
	result := indexStatsResult([]cardinality.Candidate{{Key: "public.i"}}, map[string]indexStatsRow{"public.i": {Schema: "public", Relation: "t", Index: "i", Definition: "CREATE INDEX i ON public.t (a)"}})
	require.Len(t, result.Facts, 1)
	require.Equal(t, "index_def", result.Facts[0].Kind)
	require.Equal(t, "CREATE INDEX i ON public.t (a)", result.Facts[0].ValueText)
	require.NotEmpty(t, result.Facts[0].Labels["def_hash"])
}

func TestIndexStats_DefHashIgnoresIndexName(t *testing.T) {
	require.Equal(t, definitionHash("CREATE INDEX one ON t (a)"), definitionHash("CREATE INDEX two ON t (a)"))
}
func TestIndexStats_DefHashDistinguishesColumnOrder(t *testing.T) {
	require.NotEqual(t, definitionHash("CREATE INDEX i ON t (a,b)"), definitionHash("CREATE INDEX i ON t (b,a)"))
}
func TestIndexStats_DefHashIsWhitespaceInsensitive(t *testing.T) {
	require.Equal(t, definitionHash("CREATE INDEX i ON t (a,  b)"), definitionHash("create index x on t (a, b)"))
}

// The hash is stored and compared across agents, so a definition with no
// quoted uppercase keeps the hash it has always had.
func TestIndexStats_DefHashKeepsLowercaseDefinitionHash(t *testing.T) {
	require.Equal(t, "aeffb78e1c93abcbc4d3859635b115b76d014dec89499a426390ff6006402e74", definitionHash("CREATE INDEX i ON public.t USING btree (status, created_at) WHERE (status <> 'closed'::text)"))
}

// Literal case is part of a predicate: 'ACTIVE' and 'active' select different
// rows, so partial indexes that differ only in that are not duplicates.
func TestIndexStats_DefHashKeepsStringLiteralCase(t *testing.T) {
	require.NotEqual(t, definitionHash("CREATE INDEX i ON t USING btree (a) WHERE (status = 'ACTIVE'::text)"), definitionHash("CREATE INDEX i ON t USING btree (a) WHERE (status = 'active'::text)"))
}

// A quoted identifier is case-sensitive, so "Name" and "NAME" are two columns.
func TestIndexStats_DefHashKeepsQuotedIdentifierCase(t *testing.T) {
	require.NotEqual(t, definitionHash(`CREATE INDEX i ON t USING btree ("Name")`), definitionHash(`CREATE INDEX i ON t USING btree ("NAME")`))
}

// A doubled quote stays inside its literal, and whitespace inside a literal is
// part of the value.
func TestIndexStats_DefHashKeepsLiteralTextInsideQuotes(t *testing.T) {
	require.NotEqual(t, definitionHash("CREATE INDEX i ON t USING btree (a) WHERE (n = 'it''s ACTIVE'::text)"), definitionHash("CREATE INDEX i ON t USING btree (a) WHERE (n = 'it''s active'::text)"))
	require.NotEqual(t, definitionHash("CREATE INDEX i ON t USING btree (a) WHERE (n = 'a  b'::text)"), definitionHash("CREATE INDEX i ON t USING btree (a) WHERE (n = 'a b'::text)"))
}
func TestIndexStats_BooleanColumnsAreZeroOrOne(t *testing.T) {
	require.Equal(t, float64(1), boolFloat(true))
	require.Equal(t, float64(0), boolFloat(false))
	require.Equal(t, pgtype.KindGauge, indexStatsResult([]cardinality.Candidate{{Key: "k"}}, map[string]indexStatsRow{"k": {}}).Metrics[5].Kind)
}

// On PostgreSQL 16 and later the index_def fact says when the index was last
// scanned since the reset, and it is empty when it never was.
func TestIndexStats_LastScanLabelsOnPG16Plus(t *testing.T) {
	scanned := time.Date(2026, 10, 20, 8, 30, 0, 123000000, time.FixedZone("CEST", 2*3600))
	reset := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rows := map[string]indexStatsRow{
		"public.used":    {Schema: "public", Relation: "t", Index: "used", Definition: "CREATE INDEX used ON public.t (a)", ScanTracked: true, LastScan: &scanned, Reset: &reset},
		"public.unused":  {Schema: "public", Relation: "t", Index: "unused", Definition: "CREATE INDEX unused ON public.t (b)", ScanTracked: true, Reset: &reset},
		"public.noreset": {Schema: "public", Relation: "t", Index: "noreset", Definition: "CREATE INDEX noreset ON public.t (c)", ScanTracked: true},
	}
	result := indexStatsResult([]cardinality.Candidate{{Key: "public.used"}, {Key: "public.unused"}, {Key: "public.noreset"}}, rows)
	labels := map[string]map[string]string{}
	for _, f := range result.Facts {
		labels[f.Key] = f.Labels
	}
	require.Equal(t, "2026-10-20T06:30:00.123Z", labels["public.used"]["last_idx_scan"])
	require.Equal(t, "2026-10-01T00:00:00Z", labels["public.used"]["stats_reset"])
	require.Equal(t, "", labels["public.unused"]["last_idx_scan"])
	require.Equal(t, "2026-10-01T00:00:00Z", labels["public.unused"]["stats_reset"])
	require.Equal(t, "", labels["public.noreset"]["stats_reset"])
	require.Contains(t, labels["public.unused"], "last_idx_scan")
}

// Before PostgreSQL 16 the server cannot read last_idx_scan, so the labels are
// absent: an absent label is not the same as a label that is empty.
func TestIndexStats_NoLastScanLabelsBeforePG16(t *testing.T) {
	result := indexStatsResult([]cardinality.Candidate{{Key: "public.i"}}, map[string]indexStatsRow{"public.i": {Schema: "public", Relation: "t", Index: "i", Definition: "CREATE INDEX i ON public.t (a)"}})
	require.Len(t, result.Facts, 1)
	require.NotContains(t, result.Facts[0].Labels, "last_idx_scan")
	require.NotContains(t, result.Facts[0].Labels, "stats_reset")
}

func TestIndexStats_LatestTimeKeepsTheLaterSetTime(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)
	require.Nil(t, latestTime(nil, nil))
	require.Equal(t, &early, latestTime(&early, nil))
	require.Equal(t, &late, latestTime(nil, &late))
	require.Equal(t, &late, latestTime(&early, &late))
	require.Equal(t, &late, latestTime(&late, &early))
}

func TestIndexStats_RequiresDatabaseScope(t *testing.T) {
	c := &indexStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})}
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, 5*time.Minute, c.DefaultInterval())
	require.Equal(t, 30*time.Second, c.Timeout())
}

// The query selects last_idx_scan only on PostgreSQL 16 and later. Earlier
// servers select NULL in its place, so their facts carry no such label.
func TestIndexStats_ScrapeReadsLastScanOnlyOnPG16Plus(t *testing.T) {
	scanned := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		version pgtype.PGVersion
		column  string
	}{{pgtype.PG15, "NULL::timestamptz"}, {pgtype.PG16, "s.last_idx_scan"}, {pgtype.PG19, "s.last_idx_scan"}} {
		var query string
		conn := &mockConn{
			queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
				query = sql
				return &mockRows{rows: [][]any{{
					"public", "t", "i", int64(1), int64(2), int64(3), int64(4),
					int64(5), int64(4096), true, false, true, uint32(16384),
					scanned, nil, nil,
				}}}, nil
			},
			queryRowFunc: func(context.Context, string, ...any) pgx.Row {
				return &mockRow{vals: []any{int64(4096), "CREATE INDEX i ON t (a)"}}
			},
		}
		target := &SimpleTarget{
			ConnForFunc:    func(context.Context, string) (Conn, error) { return conn, nil },
			ConnFunc:       func(context.Context) (Conn, error) { return conn, nil },
			DatabaseValue:  "app",
			ClockValue:     clock.System(),
			PGVersionValue: tc.version,
		}
		r, err := (&indexStatsCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), target)
		require.NoError(t, err)
		require.Contains(t, query, tc.column, "version %s", tc.version)
		require.Len(t, r.Facts, 1)
		if tc.version < pgtype.PG16 {
			require.NotContains(t, r.Facts[0].Labels, "last_idx_scan")
			continue
		}
		require.Equal(t, "2026-10-20T08:00:00Z", r.Facts[0].Labels["last_idx_scan"])
		require.Equal(t, "", r.Facts[0].Labels["stats_reset"])
	}
}
