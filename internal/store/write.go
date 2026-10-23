package store

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
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
	TS                  time.Time
	TenantID            string
	ClusterID           int64
	InstanceID          uuid.UUID
	Datname             string
	QueryID             int64
	CallsRate           *float64
	ExecTimeRateMs      *float64
	RowsRate            *float64
	SharedBlksHitRate   *float64
	SharedBlksReadRate  *float64
	WALBytesRate        *float64
	TempBlksWrittenRate *float64
	// SampleSeconds is the interval the rates cover: rate * SampleSeconds is
	// the amount observed in that interval.
	SampleSeconds *float64
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

// ReplicationSourceSQL classifies a metrics_replication row as 'receiver'
// (a standby's view of its own upstream), 'sender' (a primary's view of one
// standby) or 'slot'. Rows written before migration 0014 carry no source and
// are told apart by the columns only their kind fills.
const ReplicationSourceSQL = `COALESCE(source, CASE WHEN upstream_id IS NOT NULL THEN 'receiver' WHEN slot_active IS NOT NULL THEN 'slot' ELSE 'sender' END)`

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
	// Source is "receiver", "sender" or "slot": which view of replication
	// the row holds.
	Source               string
	SlotType             *string
	SlotSafeWALSizeBytes *int64
}

type TableStatRow struct {
	TS                                                                               time.Time
	TenantID                                                                         string
	ClusterID                                                                        int64
	InstanceID                                                                       uuid.UUID
	Datname, Schemaname, Relname                                                     string
	SeqScan, SeqTupRead, IdxScan, IdxTupFetch, NTupIns, NTupUpd, NTupDel, NTupHotUpd *float64
	NLiveTup, NDeadTup, NModSinceAnalyze                                             *int64
	DeadTupleRatio, LastVacuumAgeSeconds                                             *float64
	LastVacuum, LastAutovacuum, LastAnalyze, LastAutoanalyze                         *time.Time
	AutovacuumCount, AutoanalyzeCount                                                *float64
	Relpages                                                                         *int64
	RelTuples                                                                        *float64
	RelfrozenXIDAge                                                                  *int64
	TotalBytes, TableBytes, ToastBytes                                               *int64
}

type IndexStatRow struct {
	TS                                                        time.Time
	TenantID                                                  string
	ClusterID                                                 int64
	InstanceID                                                uuid.UUID
	Datname, Schemaname, Relname, IndexRelname                string
	IdxScan, IdxTupRead, IdxTupFetch, IdxBlksRead, IdxBlksHit *float64
	IndexBytes                                                *int64
	IsUnique, IsPrimary, IsValid                              *bool
	DefHash                                                   *string
}

type BloatRow struct {
	TS                                                             time.Time
	TenantID                                                       string
	ClusterID                                                      int64
	InstanceID                                                     uuid.UUID
	Datname, Schemaname, Relname, IndexRelname, ObjectKind, Method string
	RealBytes, ExpectedBytes, BloatBytes                           *int64
	BloatRatio                                                     *float64
}

// QueryPlanRow is one distinct plan shape captured by an explain command.
type QueryPlanRow struct {
	TenantID   string
	InstanceID uuid.UUID
	ClusterID  int64
	Datname    string
	QueryID    int64
	PlanHash   string
	Analyzed   bool
	CapturedAt time.Time
	Plan       []byte
}

type ObjectFactRow struct {
	TenantID                       string
	ClusterID                      int64
	InstanceID                     uuid.UUID
	Datname, Kind, Key             string
	Labels                         map[string]string
	ValueText                      *string
	ValueJSON                      []byte
	FirstSeen, LastSeen, ChangedAt time.Time
}

// LockSnapshotRow contains the latest sampled lock tree for one instance.
type LockSnapshotRow struct {
	TenantID   string
	InstanceID uuid.UUID
	ClusterID  int64
	TS         time.Time
	Tree       []byte
}

// RelationStatsPrune describes a successful, complete relation check: the
// relations it did not report no longer exist (or are no longer visible), so
// their recent rows must not keep showing them as current.
type RelationStatsPrune struct {
	TableName  string
	TenantID   string
	InstanceID uuid.UUID
	Datname    string
	Before     time.Time
	// Present lists every relation the complete sample reported. Only the
	// rows of the other relations are removed.
	Present []RelationKey
}

// RelationKey names one relation of a relation check: Index is the index
// name for index rows, empty for table rows.
type RelationKey struct {
	Schema, Relation, Index string
}

// relationPruneWindow bounds how far back a prune reaches. Every reader of
// "current" relation rows looks at most a few hours back, so older rows are
// history and are left alone; the bound also keeps the DELETE inside chunks
// that are not compressed yet (compression starts at 48 hours).
const relationPruneWindow = 24 * time.Hour

// PruneRelationStats removes the recent rows of the relations a complete
// sample no longer reports. It used to delete every older row of the
// database, so the tables, indexes and bloat history of every relation that
// still existed were wiped on each scrape (which left the index advisor with
// no history to judge an unused index by), including the pgstattuple bloat
// measurements, which are not the check's to remove.
func PruneRelationStats(ctx context.Context, tx pgx.Tx, p RelationStatsPrune) error {
	var match, scope string
	switch p.TableName {
	case "metrics_tables":
		match = "k.s = m.schemaname AND k.r = m.relname"
	case "metrics_indexes":
		match = "k.s = m.schemaname AND k.i = m.indexrelname"
	case "metrics_bloat":
		match = "k.s = m.schemaname AND k.r = m.relname AND k.i = m.indexrelname"
		scope = " AND m.method = 'estimate'"
	default:
		return fmt.Errorf("unknown relation stats table %q", p.TableName)
	}
	schemas := make([]string, len(p.Present))
	relations := make([]string, len(p.Present))
	indexes := make([]string, len(p.Present))
	for i, k := range p.Present {
		schemas[i], relations[i], indexes[i] = k.Schema, k.Relation, k.Index
	}
	query := "DELETE FROM " + p.TableName + " m WHERE m.tenant_id=$1 AND m.instance_id=$2 AND m.datname=$3 AND m.ts < $4 AND m.ts >= $5" + scope +
		" AND NOT EXISTS (SELECT 1 FROM unnest($6::text[], $7::text[], $8::text[]) AS k(s, r, i) WHERE " + match + ")"
	if _, err := tx.Exec(ctx, query, p.TenantID, p.InstanceID, p.Datname, p.Before, p.Before.Add(-relationPruneWindow), schemas, relations, indexes); err != nil {
		return fmt.Errorf("prune %s: %w", p.TableName, err)
	}
	return nil
}

func (r TableStatRow) args() []any {
	return []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.Schemaname, r.Relname, r.SeqScan, r.SeqTupRead, r.IdxScan, r.IdxTupFetch, r.NTupIns, r.NTupUpd, r.NTupDel, r.NTupHotUpd, r.NLiveTup, r.NDeadTup, r.NModSinceAnalyze, r.DeadTupleRatio, r.LastVacuumAgeSeconds, r.LastVacuum, r.LastAutovacuum, r.LastAnalyze, r.LastAutoanalyze, r.AutovacuumCount, r.AutoanalyzeCount, r.Relpages, r.RelTuples, r.RelfrozenXIDAge, r.TotalBytes, r.TableBytes, r.ToastBytes}
}
func (r IndexStatRow) args() []any {
	return []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.Schemaname, r.Relname, r.IndexRelname, r.IdxScan, r.IdxTupRead, r.IdxTupFetch, r.IdxBlksRead, r.IdxBlksHit, r.IndexBytes, r.IsUnique, r.IsPrimary, r.IsValid, r.DefHash}
}
func (r BloatRow) args() []any {
	return []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.Schemaname, r.Relname, r.IndexRelname, r.ObjectKind, r.Method, r.RealBytes, r.ExpectedBytes, r.BloatBytes, r.BloatRatio}
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
var statementColumns = []string{"ts", "tenant_id", "cluster_id", "instance_id", "datname", "queryid", "calls_rate", "exec_time_rate_ms", "rows_rate", "shared_blks_hit_rate", "shared_blks_read_rate", "wal_bytes_rate", "temp_blks_written_rate", "sample_seconds"}

func (r StatementRow) args() []any {
	return []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.QueryID, r.CallsRate, r.ExecTimeRateMs, r.RowsRate, r.SharedBlksHitRate, r.SharedBlksReadRate, r.WALBytesRate, r.TempBlksWrittenRate, r.SampleSeconds}
}

func WriteStatements(ctx context.Context, tx pgx.Tx, rows []StatementRow) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([][]any, 0, len(rows))
	for _, r := range rows {
		args = append(args, r.args())
	}
	return writeTypedRows(ctx, tx, "metrics_statements", statementColumns, args)
}

// writeTypedRows inserts rows into a typed metrics table, ignoring duplicate
// samples: a batch of INSERTs for a small envelope, COPY through a temporary
// table for a large one.
func writeTypedRows(ctx context.Context, tx pgx.Tx, table string, columns []string, rows [][]any) error {
	columnList := strings.Join(columns, ", ")
	if len(rows) < 500 {
		placeholders := make([]string, len(columns))
		for i := range columns {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}
		query := "INSERT INTO " + table + " (" + columnList + ") VALUES (" + strings.Join(placeholders, ",") + ") ON CONFLICT DO NOTHING"
		batch := &pgx.Batch{}
		for _, r := range rows {
			batch.Queue(query, r...)
		}
		br := tx.SendBatch(ctx, batch)
		defer func() {
			_ = br.Close()
		}()
		for i := range rows {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("write %s batch row %d: %w", table, i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("close %s batch: %w", table, err)
		}
		return nil
	}
	tmp := "tmp_" + table
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE "+tmp+" (LIKE "+table+" INCLUDING DEFAULTS) ON COMMIT DROP"); err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{tmp}, columns, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("copy %s: %w", table, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO "+table+" ("+columnList+") SELECT "+columnList+" FROM "+tmp+" ON CONFLICT DO NOTHING"); err != nil {
		return fmt.Errorf("insert from %s: %w", tmp, err)
	}
	if _, err := tx.Exec(ctx, "DROP TABLE IF EXISTS "+tmp); err != nil {
		return fmt.Errorf("drop %s: %w", tmp, err)
	}
	return nil
}

func writeRows(ctx context.Context, tx pgx.Tx, query string, args [][]any) error {
	if len(args) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, a := range args {
		b.Queue(query, a...)
	}
	br := tx.SendBatch(ctx, b)
	defer func() { _ = br.Close() }()
	for i := range args {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("write batch row %d: %w", i, err)
		}
	}
	return br.Close()
}

func WriteTableStats(ctx context.Context, tx pgx.Tx, rows []TableStatRow) error {
	args := make([][]any, len(rows))
	for i := range rows {
		args[i] = rows[i].args()
	}
	return writeRows(ctx, tx, `INSERT INTO metrics_tables (ts,tenant_id,cluster_id,instance_id,datname,schemaname,relname,seq_scan,seq_tup_read,idx_scan,idx_tup_fetch,n_tup_ins,n_tup_upd,n_tup_del,n_tup_hot_upd,n_live_tup,n_dead_tup,n_mod_since_analyze,dead_tuple_ratio,last_vacuum_age_seconds,last_vacuum,last_autovacuum,last_analyze,last_autoanalyze,autovacuum_count,autoanalyze_count,relpages,reltuples,relfrozenxid_age,total_bytes,table_bytes,toast_bytes) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32) ON CONFLICT DO NOTHING`, args)
}

func WriteIndexStats(ctx context.Context, tx pgx.Tx, rows []IndexStatRow) error {
	args := make([][]any, len(rows))
	for i := range rows {
		args[i] = rows[i].args()
	}
	return writeRows(ctx, tx, `INSERT INTO metrics_indexes (ts,tenant_id,cluster_id,instance_id,datname,schemaname,relname,indexrelname,idx_scan,idx_tup_read,idx_tup_fetch,idx_blks_read,idx_blks_hit,index_bytes,is_unique,is_primary,is_valid,def_hash) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) ON CONFLICT DO NOTHING`, args)
}

func WriteBloat(ctx context.Context, tx pgx.Tx, rows []BloatRow) error {
	args := make([][]any, len(rows))
	for i := range rows {
		args[i] = rows[i].args()
	}
	return writeRows(ctx, tx, `INSERT INTO metrics_bloat (ts,tenant_id,cluster_id,instance_id,datname,schemaname,relname,indexrelname,object_kind,method,real_bytes,expected_bytes,bloat_bytes,bloat_ratio) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, args)
}

// WriteQueryPlan stores a plan shape once. Re-capturing the same shape is a
// successful no-op, while a changed hash creates the next history row.
func WriteQueryPlan(ctx context.Context, tx pgx.Tx, r QueryPlanRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO query_plans (tenant_id, instance_id, cluster_id, datname, queryid, plan_hash, analyzed, captured_at, plan) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (tenant_id, instance_id, datname, queryid, plan_hash, analyzed) DO NOTHING`, r.TenantID, r.InstanceID, r.ClusterID, r.Datname, r.QueryID, r.PlanHash, r.Analyzed, r.CapturedAt, r.Plan)
	if err != nil {
		return fmt.Errorf("write query plan: %w", err)
	}
	return nil
}

func WriteObjectFacts(ctx context.Context, tx pgx.Tx, rows []ObjectFactRow) error {
	args := make([][]any, 0, len(rows))
	for _, r := range rows {
		labels, err := json.Marshal(r.Labels)
		if err != nil {
			return fmt.Errorf("marshal fact labels: %w", err)
		}
		args = append(args, []any{r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.Kind, r.Key, labels, r.ValueText, r.ValueJSON, r.FirstSeen, r.LastSeen, r.ChangedAt})
	}
	return writeRows(ctx, tx, `INSERT INTO object_facts (tenant_id,cluster_id,instance_id,datname,kind,key,labels,value_text,value_json,first_seen,last_seen,changed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (tenant_id,instance_id,datname,kind,key) DO UPDATE SET last_seen=EXCLUDED.last_seen,labels=EXCLUDED.labels,value_text=EXCLUDED.value_text,value_json=EXCLUDED.value_json,changed_at=CASE WHEN object_facts.value_text IS DISTINCT FROM EXCLUDED.value_text OR object_facts.value_json IS DISTINCT FROM EXCLUDED.value_json THEN EXCLUDED.last_seen ELSE object_facts.changed_at END`, args)
}

// ObjectFactPrune describes a complete result of a check whose fact kinds it
// replaces (wire.ReplacedFactKinds): the facts of those kinds it did not
// report for its database are gone.
type ObjectFactPrune struct {
	TenantID   string
	InstanceID uuid.UUID
	Datname    string
	Kinds      []string
	Before     time.Time
	// Present lists every fact of Kinds the result reported. They stay even
	// when a result of the same envelope with an older timestamp was
	// written after them and set last_seen back.
	Present []FactKey
}

// FactKey names one fact of a database.
type FactKey struct {
	Kind, Key string
}

// PruneObjectFacts removes the facts a complete result no longer reports.
// Facts seen after the result (a newer result written earlier) stay.
func PruneObjectFacts(ctx context.Context, tx pgx.Tx, p ObjectFactPrune) error {
	kinds := make([]string, len(p.Present))
	keys := make([]string, len(p.Present))
	for i, k := range p.Present {
		kinds[i], keys[i] = k.Kind, k.Key
	}
	_, err := tx.Exec(ctx, `DELETE FROM object_facts f
		WHERE f.tenant_id=$1 AND f.instance_id=$2 AND f.datname=$3 AND f.kind = ANY($4::text[]) AND f.last_seen < $5
		  AND NOT EXISTS (SELECT 1 FROM unnest($6::text[], $7::text[]) AS p(kind, key) WHERE p.kind = f.kind AND p.key = f.key)`,
		p.TenantID, p.InstanceID, p.Datname, p.Kinds, p.Before, kinds, keys)
	if err != nil {
		return fmt.Errorf("prune object facts: %w", err)
	}
	return nil
}

// DeleteCheckSkipFacts removes capability-skip observations after a check has
// become runnable again. Skip facts are instance-scoped even when the check's
// normal results are database-scoped, so one successful result clears the
// stale degraded state for the whole instance.
func DeleteCheckSkipFacts(ctx context.Context, tx pgx.Tx, tenantID string, instanceID uuid.UUID, check string) error {
	_, err := tx.Exec(ctx, `DELETE FROM object_facts WHERE tenant_id=$1 AND instance_id=$2 AND kind='check_skip' AND key=$3`, tenantID, instanceID, check)
	if err != nil {
		return fmt.Errorf("delete check skip fact: %w", err)
	}
	return nil
}

// DeleteCheckPartialFact removes the check_partial fact of one check and
// database once a complete result has arrived for them.
func DeleteCheckPartialFact(ctx context.Context, tx pgx.Tx, tenantID string, instanceID uuid.UUID, datname, check string) error {
	_, err := tx.Exec(ctx, `DELETE FROM object_facts WHERE tenant_id=$1 AND instance_id=$2 AND datname=$3 AND kind='check_partial' AND key=$4`, tenantID, instanceID, datname, check)
	if err != nil {
		return fmt.Errorf("delete check partial fact: %w", err)
	}
	return nil
}

// WriteLockSnapshots keeps only the newest tree per instance and removes trees
// older than the fifteen-minute freshness window for each touched instance.
func WriteLockSnapshots(ctx context.Context, tx pgx.Tx, rows []LockSnapshotRow) error {
	for _, r := range rows {
		if _, err := tx.Exec(ctx, `DELETE FROM lock_snapshots WHERE tenant_id=$1 AND instance_id=$2 AND ts < $3::timestamptz - interval '15 minutes'`, r.TenantID, r.InstanceID, r.TS); err != nil {
			return fmt.Errorf("prune lock snapshot: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO lock_snapshots (tenant_id,instance_id,cluster_id,ts,tree) VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (tenant_id,instance_id) DO UPDATE SET cluster_id=EXCLUDED.cluster_id,ts=EXCLUDED.ts,tree=EXCLUDED.tree WHERE EXCLUDED.ts > lock_snapshots.ts`, r.TenantID, r.InstanceID, r.ClusterID, r.TS, r.Tree); err != nil {
			return fmt.Errorf("write lock snapshot: %w", err)
		}
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
var replicationColumns = []string{"ts", "tenant_id", "cluster_id", "instance_id", "upstream_id", "slot_name", "edge_type", "sync_state", "write_lag_bytes", "flush_lag_bytes", "replay_lag_bytes", "write_lag_sec", "flush_lag_sec", "replay_lag_sec", "slot_active", "slot_wal_status", "slot_retained_bytes", "source", "slot_type", "slot_safe_wal_size_bytes"}

func (r ReplicationRow) args() []any {
	edgeType := r.EdgeType
	if edgeType == "" {
		edgeType = "streaming"
	}
	var source *string
	if r.Source != "" {
		v := r.Source
		source = &v
	}
	return []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.UpstreamID, r.SlotName, edgeType, r.SyncState, r.WriteLagBytes, r.FlushLagBytes, r.ReplayLagBytes, r.WriteLagSec, r.FlushLagSec, r.ReplayLagSec, r.SlotActive, r.SlotWalStatus, r.SlotRetainedBytes, source, r.SlotType, r.SlotSafeWALSizeBytes}
}

func WriteReplication(ctx context.Context, tx pgx.Tx, rows []ReplicationRow) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([][]any, 0, len(rows))
	for _, r := range rows {
		args = append(args, r.args())
	}
	return writeTypedRows(ctx, tx, "metrics_replication", replicationColumns, args)
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

func (w *Writer) WriteTableStats(ctx context.Context, tx pgx.Tx, rows []TableStatRow) error {
	return WriteTableStats(ctx, tx, rows)
}
func (w *Writer) WriteIndexStats(ctx context.Context, tx pgx.Tx, rows []IndexStatRow) error {
	return WriteIndexStats(ctx, tx, rows)
}
func (w *Writer) WriteBloat(ctx context.Context, tx pgx.Tx, rows []BloatRow) error {
	return WriteBloat(ctx, tx, rows)
}
func (w *Writer) WriteObjectFacts(ctx context.Context, tx pgx.Tx, rows []ObjectFactRow) error {
	return WriteObjectFacts(ctx, tx, rows)
}
