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

func metricTable(metric string) (string, string, error) {
	switch {
	case strings.HasPrefix(metric, "pg_replication_"):
		return "metrics_replication", "replay_lag_sec", nil
	default:
		return "metrics", "value", nil
	}
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
