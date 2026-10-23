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

	"github.com/manprint/pglens/internal/store"
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
	//
	// An instance whose agent was revoked stops pushing by design: it is
	// inapplicable, not down. It used to stay in the query and page as
	// agent_down for as long as its row existed.
	if r.Metric == "up" {
		// The cut-off is the staleness threshold the server's gauge uses, so an
		// instance is down in the alert exactly when pglens_up reads 0.
		rows, err := s.db.Query(ctx, `SELECT i.cluster_id, i.instance_id, '' AS datname,
			CASE WHEN i.last_seen >= now() - make_interval(secs => $1) THEN 1.0 ELSE 0.0 END, i.last_seen,
			a.revoked_at IS NOT NULL
			FROM instances i JOIN agents a ON a.agent_id = i.agent_id`, store.InstanceStaleAfter.Seconds())
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
			var revoked bool
			if err := rows.Scan(&cluster, &instance, &datname, &value, &ts, &revoked); err != nil {
				return nil, err
			}
			if revoked {
				out = append(out, Sample{ClusterID: &cluster, InstanceID: &instance, Datname: datname, Value: value, TS: ts, Inapplicable: true})
				continue
			}
			// The stale sample is dated For before now, not at last_seen. Its
			// pending episode then has served For as soon as the instance first
			// reads as stale, so agent_down fires on the next evaluation. Dated at
			// last_seen, it would fire For after the last push instead.
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
	if table == "metrics_replication" {
		return s.replicationSamples(ctx, valueColumn, cutoff)
	}
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
	}
	rows, err := s.db.Query(ctx, query, r.Metric, cutoff)
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

// latestStandbys is the newest reading of every standby in the lookback
// window ($1), one row each. metrics_replication describes a standby twice
// when both ends are monitored: the primary's pg_stat_replication row
// (source 'sender', instance_id the primary, slot_name the standby's
// application_name) and the standby's own receiver row (instance_id the
// standby, upstream_id the primary). The primary's view is kept, since it is
// the only one that carries write and flush lag, bytes lag and sync_state;
// a receiver row counts only when its upstream reports no standby at all
// (an upstream that is not monitored). Slot rows describe slots, not
// standbys, and are left out: they carry no lag, and the NULL lag of a slot
// row that happened to be the newest row of its instance failed the scan of
// the whole rule.
func latestStandbys(valueColumn string) string {
	return `WITH r AS (
  SELECT cluster_id, instance_id, upstream_id, slot_name, ` + store.ReplicationSourceSQL + ` AS src,
         ` + valueColumn + `::float8 AS value, sync_state, ts
    FROM metrics_replication
   WHERE ts >= $1
), latest AS (
  SELECT DISTINCT ON (instance_id, slot_name, src) *
    FROM r
   WHERE src IN ('sender', 'receiver') AND value IS NOT NULL
   ORDER BY instance_id, slot_name, src, ts DESC
), standbys AS (
  SELECT * FROM latest l
   WHERE l.src = 'sender'
      OR NOT EXISTS (SELECT 1 FROM r s WHERE s.src = 'sender' AND s.instance_id = l.upstream_id)
)`
}

// replicationSamples serves the per-instance rules on metrics_replication:
// one sample per standby, or per slot for slot_retained_bytes. It used to
// keep one row per instance, whichever was newest, so a primary with several
// standbys was judged on an arbitrary one of them (the value flipped from one
// evaluation to the next, and a lagging standby could hide behind a healthy
// one), and a slot row, whose lag columns are NULL, failed the scan and with
// it the whole rule. A primary's reading of a standby is labelled with the
// standby's name; a standby reading itself is its own subject.
func (s *metricSource) replicationSamples(ctx context.Context, valueColumn string, cutoff time.Time) ([]Sample, error) {
	query := latestStandbys(valueColumn) + `
SELECT cluster_id, instance_id, slot_name, src, value, ts FROM standbys`
	if strings.HasPrefix(valueColumn, "slot_") {
		query = `SELECT DISTINCT ON (instance_id, slot_name) cluster_id, instance_id, slot_name, 'slot', ` + valueColumn + `::float8, ts
  FROM metrics_replication
 WHERE ts >= $1 AND ` + store.ReplicationSourceSQL + ` = 'slot' AND ` + valueColumn + ` IS NOT NULL
 ORDER BY instance_id, slot_name, ts DESC`
	}
	rows, err := s.db.Query(ctx, query, cutoff)
	if err != nil {
		return nil, fmt.Errorf("query replication %s: %w", valueColumn, err)
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var cluster int64
		var instance uuid.UUID
		var name, source string
		var value float64
		var ts time.Time
		if err := rows.Scan(&cluster, &instance, &name, &source, &value, &ts); err != nil {
			return nil, err
		}
		var labels map[string]string
		switch source {
		case "sender":
			labels = map[string]string{"standby": name}
		case "slot":
			labels = map[string]string{"slot_name": name}
		}
		out = append(out, Sample{ClusterID: &cluster, InstanceID: &instance, Labels: labels, Value: value, TS: ts})
	}
	return out, rows.Err()
}

// clusterSamples aggregates the cluster-scoped replication rules over the
// latest reading of each standby in each cluster. The samples carry
// ClusterID only (InstanceID stays nil) so alert.Key keys them per cluster,
// and a sample is produced for every cluster that has standbys — including
// when the condition is false, which is what lets the alert resolve.
func (s *metricSource) clusterSamples(ctx context.Context, r Rule, now time.Time) ([]Sample, error) {
	var query string
	switch r.ID {
	case "replica.all_standbys_lagging":
		// min() over the cluster's standbys: greater than the threshold means
		// every one of them is above it.
		query = latestStandbys("replay_lag_sec") + `
SELECT cluster_id, min(value) AS value, max(ts) AS ts, true
  FROM standbys
 GROUP BY cluster_id`
	case "replica.no_sync_standby":
		// The rule's pg_sync_standby_count is not an ingest metric: no check
		// emits it, and the count is derived here from the sync_state the
		// primary reports for each of its standbys.
		//
		// The rule applies to a cluster whose primary lists synchronous
		// standbys (synchronous_standby_names), and to no other. It used to
		// count every cluster with a replication row, so each asynchronous
		// cluster paged "no synchronous standby" around the clock; and a
		// synchronous primary that had lost its every standby has no
		// pg_stat_replication row left to count, so it was the one case that
		// never produced a sample. The required clusters now drive the
		// query; the others still get an inapplicable sample, which resolves
		// the alert of a cluster switched to asynchronous replication.
		query = latestStandbys("0") + `,
required AS (
  SELECT DISTINCT i.cluster_id
    FROM instances i
    JOIN object_facts f ON f.instance_id = i.instance_id AND f.kind = 'setting' AND f.key = 'synchronous_standby_names'
   WHERE i.role = 'primary' AND i.last_seen >= $1 AND btrim(COALESCE(f.value_text, '')) <> ''
), counted AS (
  SELECT cluster_id,
         count(*) FILTER (WHERE sync_state IN ('sync', 'quorum'))::float8 AS value,
         max(ts) AS ts
    FROM standbys
   WHERE src = 'sender'
   GROUP BY cluster_id
)
SELECT c.cluster_id, COALESCE(n.value, 0), COALESCE(n.ts, $2), c.cluster_id IN (SELECT cluster_id FROM required)
  FROM (SELECT cluster_id FROM required UNION SELECT cluster_id FROM counted) c
  LEFT JOIN counted n ON n.cluster_id = c.cluster_id`
	default:
		return nil, fmt.Errorf("unsupported cluster alert rule aggregation: %s", r.ID)
	}
	args := []any{now.Add(-lookback(s.interval))}
	if r.ID == "replica.no_sync_standby" {
		args = append(args, now)
	}
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query cluster metric %s: %w", r.Metric, err)
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var cluster int64
		var value float64
		var ts time.Time
		var applicable bool
		if err := rows.Scan(&cluster, &value, &ts, &applicable); err != nil {
			return nil, err
		}
		out = append(out, Sample{ClusterID: &cluster, Value: value, TS: ts, Inapplicable: !applicable})
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

// Cleared reports which of the episodes of a spell event rule have cleared. A
// spell's own event says nothing once it has aged out, so the answer comes from
// the state the event describes. Each episode is checked on its own. An
// episode whose subject cannot be identified has not cleared.
func (s *eventSource) Cleared(ctx context.Context, r Rule, episodes []Alert) ([]Alert, error) {
	if s.db == nil {
		return nil, nil
	}
	var out []Alert
	for _, a := range episodes {
		cleared, err := s.isCleared(ctx, r, a)
		if err != nil {
			return nil, err
		}
		if cleared {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *eventSource) isCleared(ctx context.Context, r Rule, a Alert) (bool, error) {
	var query string
	var args []any
	switch r.EventType {
	case "instance_unreachable":
		// The instance is reachable again when it reports within the staleness
		// window. This reads the instance's own state rather than the
		// instance_reachable event, which the server writes only on a transition
		// it sees: an instance that comes back while the server restarts gets no
		// such event, and its spell would otherwise stay open. A revoked agent
		// never answers again, so its instance is cleared too: it is no longer
		// monitored, as the agent_down rule treats it.
		if a.InstanceID == nil {
			return false, nil
		}
		query = `SELECT EXISTS (SELECT 1 FROM instances i WHERE i.instance_id = $1 AND (i.last_seen >= now() - make_interval(secs => $2) OR EXISTS (SELECT 1 FROM agents a WHERE a.agent_id = i.agent_id AND a.revoked_at IS NOT NULL)))`
		args = []any{*a.InstanceID, store.InstanceStaleAfter.Seconds()}
	case "no_primary_in_cluster":
		// The same test the server's staleness check applies: a cluster has a
		// primary while one of its primaries reports within the staleness
		// window.
		if a.ClusterID == nil {
			return false, nil
		}
		query = `SELECT EXISTS (SELECT 1 FROM instances WHERE cluster_id = $1 AND role = 'primary' AND last_seen >= now() - make_interval(secs => $2))`
		args = []any{*a.ClusterID, store.InstanceStaleAfter.Seconds()}
	case "slot_inactive":
		// The episode is per instance. It has cleared when none of the
		// instance's slots reported inactive in the last five minutes, the
		// window the server's slot state reads.
		if a.InstanceID == nil {
			return false, nil
		}
		query = `SELECT NOT EXISTS (SELECT 1 FROM (SELECT DISTINCT ON (slot_name) slot_active FROM metrics_replication WHERE instance_id = $1 AND slot_name <> '' AND slot_active IS NOT NULL AND ts >= now() - interval '5 minutes' AND ` + store.ReplicationSourceSQL + ` = 'slot' ORDER BY slot_name, ts DESC) latest WHERE NOT latest.slot_active)`
		args = []any{*a.InstanceID}
	default:
		return false, nil
	}
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("query cleared %s: %w", r.EventType, err)
	}
	defer rows.Close()
	var cleared bool
	if rows.Next() {
		if err := rows.Scan(&cleared); err != nil {
			return false, err
		}
	}
	return cleared, rows.Err()
}
