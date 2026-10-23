package check

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/redact"
	"github.com/stretchr/testify/require"
)

func float64Ptr(v float64) *float64 { return &v }

// The gauge has one series per holder kind, and a kind with no holder reads 0:
// the query answered, so the absence is a known zero.
func TestXminHorizonMetrics_OneSeriesPerKindAndZeroWhenAbsent(t *testing.T) {
	t.Parallel()
	metrics := xminHorizonMetrics([]xminHolder{
		{HolderKind: "session", Identity: "1234", AgeXIDs: 900},
		{HolderKind: "slot", Identity: "cdc", AgeXIDs: 40},
	})
	require.Len(t, metrics, 4)
	got := map[string]float64{}
	for _, m := range metrics {
		require.Equal(t, "pg_xmin_horizon_age_xids", m.Name)
		require.Equal(t, pgtype.KindGauge, m.Kind)
		require.Len(t, m.Labels, 1)
		got[m.Labels["holder_kind"]] = m.Value
	}
	require.Equal(t, map[string]float64{"session": 900, "slot": 40, "standby": 0, "prepared": 0}, got)
}

func TestXminHorizonMetrics_NoHoldersIsZeroForEveryKind(t *testing.T) {
	t.Parallel()
	for _, m := range xminHorizonMetrics(nil) {
		require.Equal(t, 0.0, m.Value, m.Labels["holder_kind"])
	}
}

// The fact names the oldest holder of all kinds. A tie goes to the kind listed
// first, so the same sample always names the same holder.
func TestOldestXminHolder_LargestAgeThenKindOrder(t *testing.T) {
	t.Parallel()
	oldest, ok := oldestXminHolder([]xminHolder{
		{HolderKind: "standby", Identity: "replica", AgeXIDs: 500},
		{HolderKind: "session", Identity: "77", AgeXIDs: 500},
		{HolderKind: "prepared", Identity: "tx", AgeXIDs: 900},
	})
	require.True(t, ok)
	require.Equal(t, xminHolder{HolderKind: "prepared", Identity: "tx", AgeXIDs: 900}, oldest)

	tied, ok := oldestXminHolder([]xminHolder{
		{HolderKind: "standby", Identity: "replica", AgeXIDs: 500},
		{HolderKind: "slot", Identity: "cdc", AgeXIDs: 500},
	})
	require.True(t, ok)
	require.Equal(t, "slot", tied.HolderKind)

	_, ok = oldestXminHolder(nil)
	require.False(t, ok)
}

// A sample with no holder writes no fact, so Replace removes the one of the
// previous sample.
func TestXminHorizonFacts_NoFactWithoutHolder(t *testing.T) {
	t.Parallel()
	facts, err := xminHorizonFacts(nil)
	require.NoError(t, err)
	require.Empty(t, facts)
}

func TestXminHorizonFacts_OneFactWithTheOldestHolder(t *testing.T) {
	t.Parallel()
	facts, err := xminHorizonFacts([]xminHolder{
		{HolderKind: "session", Identity: "1234", AgeXIDs: 12, AgeSeconds: float64Ptr(61.5)},
		{HolderKind: "slot", Identity: "cdc", AgeXIDs: 3000},
	})
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "xmin_horizon", facts[0].Kind)
	require.Equal(t, "oldest", facts[0].Key)
	require.Empty(t, facts[0].ValueText)
	require.JSONEq(t, `{"holder_kind":"slot","identity":"cdc","age_xids":3000}`, string(facts[0].ValueJSON))
}

// The age in seconds is in the fact only when the holder has a start time.
func TestXminHorizonFacts_AgeSecondsWhenKnown(t *testing.T) {
	t.Parallel()
	facts, err := xminHorizonFacts([]xminHolder{{HolderKind: "prepared", Identity: "orders-2026", AgeXIDs: 7, AgeSeconds: float64Ptr(3600)}})
	require.NoError(t, err)
	require.Len(t, facts, 1)
	var got map[string]any
	require.NoError(t, json.Unmarshal(facts[0].ValueJSON, &got))
	require.Equal(t, 3600.0, got["age_seconds"])
	require.Equal(t, "orders-2026", got["identity"])
}

// The identity of a standby or a prepared transaction is free text: it is cut
// to 64 bytes and withheld when it may hold a credential. A session's pid and
// a slot's name are shipped as they are, since a slot name holds no free text.
func TestSanitizeXminHolder_FreeTextIdentitiesAreCutOrWithheld(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("g", 150)
	got := sanitizeXminHolder(xminHolder{HolderKind: "prepared", Identity: long, AgeXIDs: 1})
	require.Equal(t, strings.Repeat("g", 64), got.Identity)

	got = sanitizeXminHolder(xminHolder{HolderKind: "prepared", Identity: "ALTER ROLE x PASSWORD 'hunter2'", AgeXIDs: 1})
	require.Equal(t, redact.Withheld, got.Identity)

	got = sanitizeXminHolder(xminHolder{HolderKind: "standby", Identity: "postgresql://u:p@h/db", AgeXIDs: 1})
	require.Equal(t, redact.Withheld, got.Identity)

	got = sanitizeXminHolder(xminHolder{HolderKind: "slot", Identity: "secret_slot", AgeXIDs: 1})
	require.Equal(t, "secret_slot", got.Identity)

	got = sanitizeXminHolder(xminHolder{HolderKind: "session", Identity: "4242", AgeXIDs: 1})
	require.Equal(t, "4242", got.Identity)
}

// An age the collector reads a few microseconds ahead is not negative.
func TestSanitizeXminHolder_AgesAreClampedAtZero(t *testing.T) {
	t.Parallel()
	got := sanitizeXminHolder(xminHolder{HolderKind: "session", Identity: "1", AgeXIDs: -2, AgeSeconds: float64Ptr(-0.0001)})
	require.Equal(t, int64(0), got.AgeXIDs)
	require.Equal(t, 0.0, *got.AgeSeconds)
}

// The four holder kinds are read through one scrape: rows from the holder
// query become the gauges and the fact of the activity result.
func TestActivity_ScrapeReportsXminHolders(t *testing.T) {
	t.Parallel()
	conn := activityMockConn(func(sql string) [][]any {
		if !strings.Contains(sql, "row_number() OVER") {
			return nil
		}
		return [][]any{
			{"session", "4242", int64(910), float64Ptr(33.5)},
			{"prepared", "batch-2026-10-23", int64(120), float64Ptr(7200)},
			{"standby", "replica_1", int64(55), nil},
		}
	})
	res, err := (&activityCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)

	require.Equal(t, 910.0, metricValue(res, "pg_xmin_horizon_age_xids", map[string]string{"holder_kind": "session"}))
	require.Equal(t, 0.0, metricValue(res, "pg_xmin_horizon_age_xids", map[string]string{"holder_kind": "slot"}))
	require.Equal(t, 55.0, metricValue(res, "pg_xmin_horizon_age_xids", map[string]string{"holder_kind": "standby"}))
	require.Equal(t, 120.0, metricValue(res, "pg_xmin_horizon_age_xids", map[string]string{"holder_kind": "prepared"}))

	var facts []Fact
	for _, f := range res.Facts {
		if f.Kind == "xmin_horizon" {
			facts = append(facts, f)
		}
	}
	require.Len(t, facts, 1)
	require.JSONEq(t, `{"holder_kind":"session","identity":"4242","age_xids":910,"age_seconds":33.5}`, string(facts[0].ValueJSON))
}

// A scrape with no holder keeps the gauges at zero and writes no fact.
func TestActivity_ScrapeWithoutHolderWritesNoFact(t *testing.T) {
	t.Parallel()
	res, err := (&activityCheck{}).Scrape(context.Background(), maintenanceTarget(activityMockConn(func(string) [][]any { return nil })))
	require.NoError(t, err)
	for _, f := range res.Facts {
		require.NotEqual(t, "xmin_horizon", f.Kind)
	}
	require.Equal(t, 0.0, metricValue(res, "pg_xmin_horizon_age_xids", map[string]string{"holder_kind": "session"}))
}

// activityMockConn answers the holder query with the rows rows(sql) returns,
// and every other probe of the activity scrape with an empty or healthy value.
func activityMockConn(rows func(sql string) [][]any) *mockConn {
	return &mockConn{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			return &mockRows{rows: rows(sql)}, nil
		},
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "pg_prepared_xacts"):
				return &mockRow{vals: []any{float64(0), float64(0)}}
			case strings.Contains(sql, "datfrozenxid"):
				return &mockRow{vals: []any{float64(0)}}
			}
			return &mockRow{vals: []any{"100"}}
		},
	}
}
