package store

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
)

// SeriesID computes FNV-1a 64 over the canonical SeriesKey and returns the
// two's-complement int64 bit pattern. It is the value stored in metrics.series_id.
func SeriesID(k pgtype.SeriesKey) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(k.Metric))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(k.Instance.String()))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(k.Database))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(k.Labels))
	return int64(h.Sum64()) //nolint:gosec // intentional reinterpretation
}

// Writer provides bulk-write helpers. It is a thin wrapper over a pool; most
// callers use the package-level Write* functions directly with a pgx.Tx.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter creates a Writer. Pool may be nil in tests that do not need DB access.
func NewWriter(pool *pgxpool.Pool) *Writer {
	return &Writer{pool: pool}
}

// MetricRow is one row for the generic metrics hypertable.
type MetricRow struct {
	TS         time.Time
	TenantID   string
	ClusterID  int64
	InstanceID uuid.UUID
	Datname    string
	Metric     string
	Labels     map[string]string
	SeriesID   int64
	Value      float64
}

// StatementRow is one row for metrics_statements.
type StatementRow struct {
	TS                 time.Time
	TenantID           string
	ClusterID          int64
	InstanceID         uuid.UUID
	Datname            string
	QueryID            int64
	CallsRate          *float64
	ExecTimeRateMs     *float64
	RowsRate           *float64
	SharedBlksHitRate  *float64
	SharedBlksReadRate *float64
	WALBytesRate       *float64
}

// ASHRow is one row for metrics_ash.
type ASHRow struct {
	TS            time.Time
	TenantID      string
	ClusterID     int64
	InstanceID    uuid.UUID
	Datname       string
	WaitEventType string
	WaitEvent     string
	State         string
	QueryID       *int64
	Samples       int
	WindowSeconds int
	WindowTicks   int
}

// ReplicationRow is one row for metrics_replication.
type ReplicationRow struct {
	TS                time.Time
	TenantID          string
	ClusterID         int64
	InstanceID        uuid.UUID
	UpstreamID        *uuid.UUID
	SlotName          string
	EdgeType          string
	SyncState         *string
	WriteLagBytes     *int64
	FlushLagBytes     *int64
	ReplayLagBytes    *int64
	WriteLagSec       *float64
	FlushLagSec       *float64
	ReplayLagSec      *float64
	SlotActive        *bool
	SlotWalStatus     *string
	SlotRetainedBytes *int64
}

// TopologyEdgeRow is one row for the topology_edges table — the persisted
// form of the in-memory topology.Edge objects internal/server/pipeline.go
// already computes per envelope for internal/topology.Engine's own
// detection logic. Nothing ever wrote these to the table until this was
// found live while writing phase 6.5's README: GET /clusters/{id}/topology
// (and the `topology` field on GET /clusters) can only ever return an empty
// array without this, since both read from this table exclusively.
type TopologyEdgeRow struct {
	TenantID     string
	ClusterID    int64
	FromInstance uuid.UUID
	ToInstance   uuid.UUID
	EdgeType     string
	SyncState    *string
	Confidence   string
}

// WriteTopologyEdges replaces every topology_edges row FROM fromInstance in
// clusterID with rows, so a since-resolved-away edge (the instance no longer
// reports one, or now reports a different upstream) doesn't linger stale.
// Row volume per instance is at most a handful, so delete-then-insert is
// simpler than a real diff and cheap enough at this scale.
func WriteTopologyEdges(ctx context.Context, tx pgx.Tx, tenantID string, clusterID int64, fromInstance uuid.UUID, rows []TopologyEdgeRow) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM topology_edges WHERE tenant_id=$1 AND cluster_id=$2 AND from_instance=$3`,
		tenantID, clusterID, fromInstance); err != nil {
		return fmt.Errorf("delete stale topology edges: %w", err)
	}
	for _, r := range rows {
		if _, err := tx.Exec(ctx,
			`INSERT INTO topology_edges (tenant_id, cluster_id, from_instance, to_instance, edge_type, sync_state, confidence, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,now())`,
			r.TenantID, r.ClusterID, r.FromInstance, r.ToInstance, r.EdgeType, r.SyncState, r.Confidence); err != nil {
			return fmt.Errorf("insert topology edge: %w", err)
		}
	}
	return nil
}

// QueryTextRow is one row for query_texts upsert.
type QueryTextRow struct {
	TenantID  string
	ClusterID int64
	Datname   string
	QueryID   int64
	PGMajor   int
	QueryText string
	LastSeen  time.Time
}

// WriteMetrics writes generic metric rows with ON CONFLICT DO NOTHING.
// It uses batched INSERT for <500 rows and TEMP staging + CopyFrom otherwise.
func WriteMetrics(ctx context.Context, tx pgx.Tx, rows []MetricRow) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) < 500 {
		batch := &pgx.Batch{}
		for _, r := range rows {
			labelsJSON, err := json.Marshal(r.Labels)
			if err != nil {
				return fmt.Errorf("marshal labels: %w", err)
			}
			if r.Labels == nil {
				labelsJSON = []byte(`{}`)
			}
			batch.Queue(
				`INSERT INTO metrics (ts, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id, value) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9) ON CONFLICT DO NOTHING`,
				r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.Metric, string(labelsJSON), r.SeriesID, r.Value,
			)
		}
		br := tx.SendBatch(ctx, batch)
		defer func() {
			_ = br.Close()
		}()
		for i := range rows {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("write metrics batch row %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("close metrics batch: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE tmp_metrics (LIKE metrics INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create tmp_metrics: %w", err)
	}
	copyRows := make([][]any, 0, len(rows))
	for _, r := range rows {
		labelsJSON, err := json.Marshal(r.Labels)
		if err != nil {
			return fmt.Errorf("marshal labels: %w", err)
		}
		if r.Labels == nil {
			labelsJSON = []byte(`{}`)
		}
		copyRows = append(copyRows, []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.Metric, string(labelsJSON), r.SeriesID, r.Value})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tmp_metrics"}, []string{"ts", "tenant_id", "cluster_id", "instance_id", "datname", "metric", "labels", "series_id", "value"}, pgx.CopyFromRows(copyRows)); err != nil {
		return fmt.Errorf("copy metrics: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metrics (ts, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id, value) SELECT ts, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id, value FROM tmp_metrics ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("insert from tmp_metrics: %w", err)
	}
	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS tmp_metrics`); err != nil {
		return fmt.Errorf("drop tmp_metrics: %w", err)
	}
	return nil
}

// WriteStatements writes statement rows with ON CONFLICT DO NOTHING.
func WriteStatements(ctx context.Context, tx pgx.Tx, rows []StatementRow) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) < 500 {
		batch := &pgx.Batch{}
		for _, r := range rows {
			batch.Queue(
				`INSERT INTO metrics_statements (ts, tenant_id, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, rows_rate, shared_blks_hit_rate, shared_blks_read_rate, wal_bytes_rate) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT DO NOTHING`,
				r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.QueryID, r.CallsRate, r.ExecTimeRateMs, r.RowsRate, r.SharedBlksHitRate, r.SharedBlksReadRate, r.WALBytesRate,
			)
		}
		br := tx.SendBatch(ctx, batch)
		defer func() {
			_ = br.Close()
		}()
		for i := range rows {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("write statements batch row %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("close statements batch: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE tmp_metrics_statements (LIKE metrics_statements INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create tmp_metrics_statements: %w", err)
	}
	copyRows := make([][]any, 0, len(rows))
	for _, r := range rows {
		copyRows = append(copyRows, []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.QueryID, r.CallsRate, r.ExecTimeRateMs, r.RowsRate, r.SharedBlksHitRate, r.SharedBlksReadRate, r.WALBytesRate})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tmp_metrics_statements"}, []string{"ts", "tenant_id", "cluster_id", "instance_id", "datname", "queryid", "calls_rate", "exec_time_rate_ms", "rows_rate", "shared_blks_hit_rate", "shared_blks_read_rate", "wal_bytes_rate"}, pgx.CopyFromRows(copyRows)); err != nil {
		return fmt.Errorf("copy statements: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metrics_statements (ts, tenant_id, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, rows_rate, shared_blks_hit_rate, shared_blks_read_rate, wal_bytes_rate) SELECT ts, tenant_id, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, rows_rate, shared_blks_hit_rate, shared_blks_read_rate, wal_bytes_rate FROM tmp_metrics_statements ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("insert from tmp_metrics_statements: %w", err)
	}
	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS tmp_metrics_statements`); err != nil {
		return fmt.Errorf("drop tmp_metrics_statements: %w", err)
	}
	return nil
}

// WriteASH writes ASH rows with ON CONFLICT DO NOTHING.
func WriteASH(ctx context.Context, tx pgx.Tx, rows []ASHRow) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) < 500 {
		batch := &pgx.Batch{}
		for _, r := range rows {
			batch.Queue(
				`INSERT INTO metrics_ash (ts, tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT DO NOTHING`,
				r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.WaitEventType, r.WaitEvent, r.State, r.QueryID, r.Samples, r.WindowSeconds, r.WindowTicks,
			)
		}
		br := tx.SendBatch(ctx, batch)
		defer func() {
			_ = br.Close()
		}()
		for i := range rows {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("write ash batch row %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("close ash batch: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE tmp_metrics_ash (LIKE metrics_ash INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create tmp_metrics_ash: %w", err)
	}
	copyRows := make([][]any, 0, len(rows))
	for _, r := range rows {
		copyRows = append(copyRows, []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.WaitEventType, r.WaitEvent, r.State, r.QueryID, r.Samples, r.WindowSeconds, r.WindowTicks})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tmp_metrics_ash"}, []string{"ts", "tenant_id", "cluster_id", "instance_id", "datname", "wait_event_type", "wait_event", "state", "queryid", "samples", "window_seconds", "window_ticks"}, pgx.CopyFromRows(copyRows)); err != nil {
		return fmt.Errorf("copy ash: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metrics_ash (ts, tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks) SELECT ts, tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks FROM tmp_metrics_ash ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("insert from tmp_metrics_ash: %w", err)
	}
	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS tmp_metrics_ash`); err != nil {
		return fmt.Errorf("drop tmp_metrics_ash: %w", err)
	}
	return nil
}

// WriteReplication writes replication rows with ON CONFLICT DO NOTHING.
func WriteReplication(ctx context.Context, tx pgx.Tx, rows []ReplicationRow) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows) < 500 {
		batch := &pgx.Batch{}
		for _, r := range rows {
			edgeType := r.EdgeType
			if edgeType == "" {
				edgeType = "streaming"
			}
			batch.Queue(
				`INSERT INTO metrics_replication (ts, tenant_id, cluster_id, instance_id, upstream_id, slot_name, edge_type, sync_state, write_lag_bytes, flush_lag_bytes, replay_lag_bytes, write_lag_sec, flush_lag_sec, replay_lag_sec, slot_active, slot_wal_status, slot_retained_bytes) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT DO NOTHING`,
				r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.UpstreamID, r.SlotName, edgeType, r.SyncState, r.WriteLagBytes, r.FlushLagBytes, r.ReplayLagBytes, r.WriteLagSec, r.FlushLagSec, r.ReplayLagSec, r.SlotActive, r.SlotWalStatus, r.SlotRetainedBytes,
			)
		}
		br := tx.SendBatch(ctx, batch)
		defer func() {
			_ = br.Close()
		}()
		for i := range rows {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("write replication batch row %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("close replication batch: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE tmp_metrics_replication (LIKE metrics_replication INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create tmp_metrics_replication: %w", err)
	}
	copyRows := make([][]any, 0, len(rows))
	for _, r := range rows {
		edgeType := r.EdgeType
		if edgeType == "" {
			edgeType = "streaming"
		}
		copyRows = append(copyRows, []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.UpstreamID, r.SlotName, edgeType, r.SyncState, r.WriteLagBytes, r.FlushLagBytes, r.ReplayLagBytes, r.WriteLagSec, r.FlushLagSec, r.ReplayLagSec, r.SlotActive, r.SlotWalStatus, r.SlotRetainedBytes})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tmp_metrics_replication"}, []string{"ts", "tenant_id", "cluster_id", "instance_id", "upstream_id", "slot_name", "edge_type", "sync_state", "write_lag_bytes", "flush_lag_bytes", "replay_lag_bytes", "write_lag_sec", "flush_lag_sec", "replay_lag_sec", "slot_active", "slot_wal_status", "slot_retained_bytes"}, pgx.CopyFromRows(copyRows)); err != nil {
		return fmt.Errorf("copy replication: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO metrics_replication (ts, tenant_id, cluster_id, instance_id, upstream_id, slot_name, edge_type, sync_state, write_lag_bytes, flush_lag_bytes, replay_lag_bytes, write_lag_sec, flush_lag_sec, replay_lag_sec, slot_active, slot_wal_status, slot_retained_bytes) SELECT ts, tenant_id, cluster_id, instance_id, upstream_id, slot_name, edge_type, sync_state, write_lag_bytes, flush_lag_bytes, replay_lag_bytes, write_lag_sec, flush_lag_sec, replay_lag_sec, slot_active, slot_wal_status, slot_retained_bytes FROM tmp_metrics_replication ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("insert from tmp_metrics_replication: %w", err)
	}
	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS tmp_metrics_replication`); err != nil {
		return fmt.Errorf("drop tmp_metrics_replication: %w", err)
	}
	return nil
}

// WriteQueryTexts upserts query texts with ON CONFLICT DO UPDATE SET last_seen.
func WriteQueryTexts(ctx context.Context, tx pgx.Tx, rows []QueryTextRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(
			`INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text, first_seen, last_seen) VALUES ($1,$2,$3,$4,$5,$6,$7,$7) ON CONFLICT (tenant_id, cluster_id, datname, queryid, pg_major) DO UPDATE SET last_seen = EXCLUDED.last_seen`,
			r.TenantID, r.ClusterID, r.Datname, r.QueryID, r.PGMajor, r.QueryText, r.LastSeen,
		)
	}
	br := tx.SendBatch(ctx, batch)
	defer func() {
		_ = br.Close()
	}()
	for i := range rows {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("write query_texts batch row %d: %w", i, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("close query_texts batch: %w", err)
	}
	return nil
}

// WriteEvents writes counter_reset_detected events. Payload is marshaled to jsonb.
func WriteEvents(ctx context.Context, tx pgx.Tx, rows []EventRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range rows {
		payloadJSON, err := json.Marshal(r.Payload)
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
		batch.Queue(
			`INSERT INTO events (tenant_id, ts, type, cluster_id, instance_id, payload) VALUES ($1,$2,$3,$4,$5,$6::jsonb)`,
			r.TenantID, r.TS, r.Type, r.ClusterID, r.InstanceID, string(payloadJSON),
		)
	}
	br := tx.SendBatch(ctx, batch)
	defer func() {
		_ = br.Close()
	}()
	for i := range rows {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("write events batch row %d: %w", i, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("close events batch: %w", err)
	}
	return nil
}

// EventRow is one row for events.
type EventRow struct {
	TenantID   string
	TS         time.Time
	Type       string
	ClusterID  *int64
	InstanceID *uuid.UUID
	Payload    map[string]any
}

// Writer helpers for pool-backed usage (optional convenience).
func (w *Writer) WriteMetrics(ctx context.Context, tx pgx.Tx, rows []MetricRow) error {
	return WriteMetrics(ctx, tx, rows)
}

func (w *Writer) WriteStatements(ctx context.Context, tx pgx.Tx, rows []StatementRow) error {
	return WriteStatements(ctx, tx, rows)
}

func (w *Writer) WriteASH(ctx context.Context, tx pgx.Tx, rows []ASHRow) error {
	return WriteASH(ctx, tx, rows)
}

func (w *Writer) WriteReplication(ctx context.Context, tx pgx.Tx, rows []ReplicationRow) error {
	return WriteReplication(ctx, tx, rows)
}

func (w *Writer) WriteQueryTexts(ctx context.Context, tx pgx.Tx, rows []QueryTextRow) error {
	return WriteQueryTexts(ctx, tx, rows)
}
