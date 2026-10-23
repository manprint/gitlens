package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

// databaseStatsEnvelope reports the per-database counters of one instance as
// database_stats does: one result, one labelled row per database, and one
// result-level StatsReset, the newest of the databases' resets.
func databaseStatsEnvelope(instID uuid.UUID, cid string, ts time.Time, resetAt *time.Time, commits map[string]float64) wire.Envelope {
	var metrics []wire.Metric
	for _, db := range []string{"app", "postgres"} {
		metrics = append(metrics, wire.Metric{
			Name: "pg_xact_commit_total", Value: commits[db], Kind: "counter",
			Labels: map[string]string{"database": db},
		})
	}
	return wire.Envelope{Instances: []wire.Instance{{
		InstanceID: instID.String(),
		ClusterID:  cid,
		Role:       "primary",
		Results:    []wire.Result{{Check: "database_stats", TS: ts, StatsReset: resetAt, Metrics: metrics}},
	}}}
}

// ratesByDatname returns the rate written for one counter in each database by
// a transaction.
func ratesByDatname(tx *recordingTx, metric string) map[string]float64 {
	rates := make(map[string]float64)
	for _, e := range tx.execs {
		if !strings.Contains(e.sql, "INSERT INTO metrics (") || e.args[5] != metric {
			continue
		}
		rates[e.args[4].(string)] = e.args[8].(float64)
	}
	return rates
}

// resetReasonsByDatname returns the reason of each counter_reset_detected event
// a transaction wrote, keyed by the database the event's series belongs to.
func resetReasonsByDatname(t *testing.T, tx *recordingTx) map[string]string {
	t.Helper()
	reasons := make(map[string]string)
	for _, e := range tx.execs {
		if !strings.Contains(e.sql, "INSERT INTO events (") || e.args[2] != "counter_reset_detected" {
			continue
		}
		var payload struct {
			Reason string            `json:"reason"`
			Labels map[string]string `json:"labels"`
		}
		require.NoError(t, json.Unmarshal([]byte(e.args[5].(string)), &payload))
		reasons[payload.Labels["database"]] = payload.Reason
	}
	return reasons
}

// TestPipeline_ResetOfOneDatabaseLeavesOthersAlone: app is reset again while
// postgres never was. The result-level StatsReset moves for both databases, and
// postgres must keep its rate with no reset event. app's own reset is still
// reported, here as a counter going backwards.
func TestPipeline_ResetOfOneDatabaseLeavesOthersAlone(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(rollbackT0)
	p := NewPipeline(nil, fc)
	instID := uuid.New()
	cid := pgtype.ClusterID(1).String()
	appFirstReset := rollbackT0.Add(-time.Hour)
	appSecondReset := rollbackT0.Add(100 * time.Second)

	s1 := databaseStatsEnvelope(instID, cid, rollbackT0, &appFirstReset, map[string]float64{"app": 100, "postgres": 1000})
	s2 := databaseStatsEnvelope(instID, cid, rollbackT0.Add(60*time.Second), &appFirstReset, map[string]float64{"app": 150, "postgres": 1050})
	s3 := databaseStatsEnvelope(instID, cid, rollbackT0.Add(120*time.Second), &appSecondReset, map[string]float64{"app": 10, "postgres": 1100})

	_, err := processOn(t, p, s1, nil)
	require.NoError(t, err)
	// The second scrape: both databases keep their rate.
	s2tx, err := processOn(t, p, s2, nil)
	require.NoError(t, err)
	require.Empty(t, resetReasonsByDatname(t, s2tx), "no reset event without a reset")
	require.Equal(t, map[string]float64{"app": 50.0 / 60, "postgres": 50.0 / 60}, ratesByDatname(s2tx, "pg_xact_commit_total"))

	s3tx, err := processOn(t, p, s3, nil)
	require.NoError(t, err)
	require.NotContains(t, resetReasonsByDatname(t, s3tx), "postgres", "postgres was never reset")
	require.Equal(t, map[string]string{"app": "counter_went_backwards"}, resetReasonsByDatname(t, s3tx))
	rates := ratesByDatname(s3tx, "pg_xact_commit_total")
	require.Equal(t, map[string]float64{"postgres": 50.0 / 60}, rates, "postgres keeps its rate; app, reset, has none")
}

// TestSeriesStatsReset_OnlyLabelledDatabasesDropTheResult: a result that names
// no database and whose metrics carry no database label (stat_statements,
// archiver) keeps its own StatsReset, as its reset is reported for the whole
// source. Only the labelled per-database rows of database_stats lose it.
func TestSeriesStatsReset_OnlyLabelledDatabasesDropTheResult(t *testing.T) {
	t.Parallel()
	reset := rollbackT0
	plain := wire.Metric{Name: "pg_statements_calls_total", Kind: "counter"}
	labelled := wire.Metric{Name: "pg_xact_commit_total", Kind: "counter", Labels: map[string]string{"database": "app"}}

	require.Equal(t, &reset, seriesStatsReset(wire.Result{StatsReset: &reset}, plain))
	require.Equal(t, &reset, seriesStatsReset(wire.Result{Database: "app", StatsReset: &reset}, labelled))
	require.Nil(t, seriesStatsReset(wire.Result{StatsReset: &reset}, labelled))
}

// TestPipeline_ResetWritesOneEventPerDatabase: a restart resets every counter of
// a database together. The push writes one counter_reset_detected event for each
// database that was reset, counting its series, and not one for each series.
func TestPipeline_ResetWritesOneEventPerDatabase(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(rollbackT0)
	p := NewPipeline(nil, fc)
	instID := uuid.New()
	cid := pgtype.ClusterID(1).String()
	names := []string{"pg_xact_commit_total", "pg_xact_rollback_total", "pg_blks_read_total"}
	envelope := func(ts time.Time, value float64) wire.Envelope {
		var metrics []wire.Metric
		for _, db := range []string{"app", "postgres"} {
			for _, name := range names {
				metrics = append(metrics, wire.Metric{Name: name, Value: value, Kind: "counter", Labels: map[string]string{"database": db}})
			}
		}
		return wire.Envelope{Instances: []wire.Instance{{
			InstanceID: instID.String(), ClusterID: cid, Role: "primary",
			Results: []wire.Result{{Check: "database_stats", TS: ts, Metrics: metrics}},
		}}}
	}

	_, err := processOn(t, p, envelope(rollbackT0, 1000), nil)
	require.NoError(t, err)
	tx, err := processOn(t, p, envelope(rollbackT0.Add(60*time.Second), 10), nil)
	require.NoError(t, err)

	counts := map[string][]int{}
	for _, e := range tx.execs {
		if !strings.Contains(e.sql, "INSERT INTO events (") || e.args[2] != "counter_reset_detected" {
			continue
		}
		var payload struct {
			Count  int               `json:"count"`
			Labels map[string]string `json:"labels"`
		}
		require.NoError(t, json.Unmarshal([]byte(e.args[5].(string)), &payload))
		counts[payload.Labels["database"]] = append(counts[payload.Labels["database"]], payload.Count)
	}
	require.Equal(t, map[string][]int{"app": {3}, "postgres": {3}}, counts,
		"one event per reset database, counting its three series")
}
