package alert

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type metricSource struct {
	db       queryer
	interval time.Duration
}
type eventSource struct {
	db       queryer
	interval time.Duration
}

func NewMetricSource(pool *pgxpool.Pool, interval time.Duration) Source {
	return &metricSource{db: pool, interval: interval}
}
func NewEventSource(pool *pgxpool.Pool, interval time.Duration) Source {
	return &eventSource{db: pool, interval: interval}
}

func (s *metricSource) Kind() string { return "metric" }
func (s *eventSource) Kind() string  { return "event" }

func lookback(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = defaultEngineInterval
	}
	if d := 2 * interval; d > 2*time.Minute {
		return d
	}
	return 2 * time.Minute
}

// replicationColumns maps the replication metric names a rule may reference to
// the metrics_replication column that actually holds them.
//
// This used to be a bare `pg_replication_*` prefix test that returned
// replay_lag_sec for every match, so a rule on pg_replication_write_lag_seconds
// (or flush, or any bytes variant) silently compared the *replay* lag instead:
// the rule looked configured, evaluated, alerted, and reported a number that
// was not the one it named. Anything unrecognised is now an error the engine
// reports, rather than a wrong answer.
var replicationColumns = map[string]string{
	"pg_replication_lag_seconds":         "replay_lag_sec",
	"pg_replication_replay_lag_seconds":  "replay_lag_sec",
	"pg_replication_write_lag_seconds":   "write_lag_sec",
	"pg_replication_flush_lag_seconds":   "flush_lag_sec",
	"pg_replication_replay_lag_bytes":    "replay_lag_bytes",
	"pg_replication_write_lag_bytes":     "write_lag_bytes",
	"pg_replication_flush_lag_bytes":     "flush_lag_bytes",
	"pg_replication_slot_retained_bytes": "slot_retained_bytes",
}

func metricTable(metric string) (string, string, error) {
	if column, ok := replicationColumns[metric]; ok {
		return "metrics_replication", column, nil
	}
	if strings.HasPrefix(metric, "pg_replication_") {
		return "", "", fmt.Errorf("unknown replication metric %q: no metrics_replication column holds it", metric)
	}
	return "metrics", "value", nil
}

func (s *metricSource) Samples(ctx context.Context, r Rule, now time.Time) ([]Sample, error) {
	if r.Metric == "" {
		return nil, fmt.Errorf("metric source cannot serve event rule %s", r.ID)
	}
	if r.Scope == ScopeCluster {
		switch r.ID {
		case "replica.all_standbys_lagging", "replica.no_sync_standby":
		default:
			return nil, fmt.Errorf("unsupported cluster alert rule aggregation: %s", r.ID)
		}
	}
	if s.db == nil {
		return nil, nil
	}
	// Cluster-scoped rules need one sample per cluster, aggregated across the
	// cluster's standbys. They used to fall through to the same per-instance
	// query as every other rule, which made both of them mean the opposite of
	// their name and summary: "every standby in the cluster is lagging" fired
	// as soon as *any* single standby lagged, and the alert key carried an
	// instance_id even though the rule is cluster-scoped, so one alert was
	// opened per standby instead of one per cluster.
	if r.Scope == ScopeCluster {
		return s.clusterSamples(ctx, r, now)
	}
	// `up` is the staleness signal. It is exposed as a process-local
	// Prometheus gauge, not as an ingest metric, so read the durable instance
	// last_seen value directly. This keeps alert evaluation shared by all
	// server replicas and, unlike an event-only rule, supplies the false
	// sample needed to resolve the alert when the agent returns.
	if r.Metric == "up" {
		rows, err := s.db.Query(ctx, `SELECT cluster_id, instance_id, '' AS datname, CASE WHEN last_seen >= now() - interval '60 seconds' THEN 1.0 ELSE 0.0 END, last_seen FROM instances`)
		if err != nil {
			return nil, fmt.Errorf("query staleness: %w", err)
		}
		defer rows.Close()
		var out []Sample
		for rows.Next() {
			var cluster int64
			var instance uuid.UUID
			var datname string
			var value float64
			var ts time.Time
			if err := rows.Scan(&cluster, &instance, &datname, &value, &ts); err != nil {
				return nil, err
			}
			// last_seen already records when this outage began. Seed the
			// stale sample at the rule boundary so agent_down's documented
			// 90-second duration is not added after the 60-second staleness
			// threshold a second time.
			if value < 1 && r.For > 0 {
				ts = now.Add(-r.For)
			}
			out = append(out, Sample{ClusterID: &cluster, InstanceID: &instance, Datname: datname, Value: value, TS: ts})
		}
		return out, rows.Err()
	}
	table, valueColumn, err := metricTable(r.Metric)
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-lookback(s.interval))
	var query string
	if table == "metrics" {
		if r.Metric == "pg_deadlocks_total" {
			// Counter deltas represent events. Keep a positive observation in
			// the lookback window so the alert engine cannot miss a deadlock
			// merely because the next scrape persisted a zero rate.
			query = `SELECT DISTINCT ON (cluster_id, instance_id, datname) cluster_id, instance_id, datname, value, ts FROM metrics WHERE metric = $1 AND ts >= $2 AND value > 0 ORDER BY cluster_id, instance_id, datname, ts DESC`
		} else {
			query = `SELECT DISTINCT ON (cluster_id, instance_id, datname) cluster_id, instance_id, datname, value, ts FROM metrics WHERE metric = $1 AND ts >= $2 ORDER BY cluster_id, instance_id, datname, ts DESC`
		}
	} else {
		query = fmt.Sprintf(`SELECT DISTINCT ON (cluster_id, instance_id) cluster_id, instance_id, '' AS datname, %s, ts FROM %s WHERE ts >= $1 ORDER BY cluster_id, instance_id, ts DESC`, valueColumn, table)
	}
	var rows pgx.Rows
	if table == "metrics" {
		rows, err = s.db.Query(ctx, query, r.Metric, cutoff)
	} else {
		rows, err = s.db.Query(ctx, query, cutoff)
	}
	if err != nil {
		return nil, fmt.Errorf("query metric %s: %w", r.Metric, err)
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var cluster int64
		var instance uuid.UUID
		var datname string
		var value float64
		var ts time.Time
		if err := rows.Scan(&cluster, &instance, &datname, &value, &ts); err != nil {
			return nil, err
		}
		out = append(out, Sample{ClusterID: &cluster, InstanceID: &instance, Datname: datname, Value: value, TS: ts})
	}
	return out, rows.Err()
}

// clusterSamples aggregates the cluster-scoped replication rules over the
// latest reading of each standby in each cluster. The samples carry
// ClusterID only (InstanceID stays nil) so alert.Key keys them per cluster,
// and a sample is produced for every cluster that has standbys — including
// when the condition is false, which is what lets the alert resolve.
func (s *metricSource) clusterSamples(ctx context.Context, r Rule, now time.Time) ([]Sample, error) {
	// DISTINCT ON keeps the newest row per standby: metrics_replication holds
	// one row per (primary instance, standby slot) per scrape.
	const latest = `WITH latest AS (
  SELECT DISTINCT ON (instance_id, slot_name)
         cluster_id, replay_lag_sec, sync_state, ts
    FROM metrics_replication
   WHERE ts >= $1
   ORDER BY instance_id, slot_name, ts DESC
)`
	var query string
	switch r.ID {
	case "replica.all_standbys_lagging":
		// min() over the cluster's standbys: greater than the threshold means
		// every one of them is above it.
		query = latest + `
SELECT cluster_id, min(replay_lag_sec)::float8 AS value, max(ts) AS ts
  FROM latest
 WHERE replay_lag_sec IS NOT NULL
 GROUP BY cluster_id`
	case "replica.no_sync_standby":
		// The rule's pg_sync_standby_count is not an ingest metric: no check
		// emits it, and the count is derived here from the sync_state the
		// primary reports for each of its standbys.
		query = latest + `
SELECT cluster_id,
       count(*) FILTER (WHERE sync_state IN ('sync', 'quorum'))::float8 AS value,
       max(ts) AS ts
  FROM latest
 GROUP BY cluster_id`
	default:
		return nil, fmt.Errorf("unsupported cluster alert rule aggregation: %s", r.ID)
	}
	rows, err := s.db.Query(ctx, query, now.Add(-lookback(s.interval)))
	if err != nil {
		return nil, fmt.Errorf("query cluster metric %s: %w", r.Metric, err)
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var cluster int64
		var value float64
		var ts time.Time
		if err := rows.Scan(&cluster, &value, &ts); err != nil {
			return nil, err
		}
		out = append(out, Sample{ClusterID: &cluster, Value: value, TS: ts})
	}
	return out, rows.Err()
}

func (s *eventSource) Samples(ctx context.Context, r Rule, now time.Time) ([]Sample, error) {
	if r.EventType == "" {
		return nil, fmt.Errorf("event source cannot serve metric rule %s", r.ID)
	}
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT cluster_id, instance_id, ts FROM events WHERE type = $1 AND ts >= $2 ORDER BY ts DESC`, r.EventType, now.Add(-lookback(s.interval)))
	if err != nil {
		return nil, fmt.Errorf("query event %s: %w", r.EventType, err)
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var cluster pgtype.Int8
		var instance pgtype.UUID
		var ts time.Time
		if err := rows.Scan(&cluster, &instance, &ts); err != nil {
			return nil, err
		}
		var clusterID *int64
		if cluster.Valid {
			v := cluster.Int64
			clusterID = &v
		}
		var instanceID *uuid.UUID
		if instance.Valid {
			v := uuid.UUID(instance.Bytes)
			instanceID = &v
		}
		out = append(out, Sample{ClusterID: clusterID, InstanceID: instanceID, Value: 1, TS: ts})
	}
	return out, rows.Err()
}
