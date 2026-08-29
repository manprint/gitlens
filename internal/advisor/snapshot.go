package advisor

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
)

type Fact struct {
	Key        string
	ValueText  string
	Value      map[string]any
	ObservedAt time.Time
}

type TableStat struct {
	Name             string
	NLiveTup         float64
	NDeadTup         float64
	NModSinceAnalyze float64
	LastAutovacuum   *time.Time
	LastVacuum       *time.Time
	RelfrozenXIDAge  float64
	SeqScanRate      float64
	IdxScanRate      float64
}

type IndexStat struct {
	Name        string
	TableName   string
	DefHash     string
	IndexDef    string
	IdxScan     float64
	SizeBytes   float64
	HistoryDays float64
	IsPrimary   bool
	IsUnique    bool
	IsValid     bool
}

type BloatStat struct {
	Name         string
	RelationName string
	Ratio        float64
	SizeBytes    float64
}
type StatementStat struct {
	QueryID         int64
	MeanExecTimeMs  float64
	Calls           float64
	TotalExecTimeMs float64
	TempBytes       float64
	SharedBlksRead  float64
	SharedBlksHit   float64
}

type HostInfo struct {
	Available        bool
	Source           string
	TotalBytes       float64
	CgroupLimitBytes float64
}

type SiblingInfo struct {
	InstanceID  uuid.UUID
	Role        pgtype.Role
	Settings    map[string]string
	IndexHashes map[string]string
}

type Baseline struct {
	MeanExecTimeByQueryID map[int64]float64
}

type Snapshot struct {
	Now           time.Time
	ClusterID     int64
	InstanceID    uuid.UUID
	Role          pgtype.Role
	PGVersion     pgtype.PGVersion
	PermTier      pgtype.PermTier
	Metrics       map[string]map[string]float64
	Facts         map[string]map[string]Fact
	Tables        []TableStat
	Indexes       []IndexStat
	Bloat         []BloatStat
	Statements    []StatementStat
	Host          HostInfo
	Siblings      []SiblingInfo
	SkippedChecks map[string]string
	Baseline      *Baseline
	Settings      map[string]string
}

func (s *Snapshot) Metric(name string, labels map[string]string) (float64, bool) {
	values, ok := s.Metrics[name]
	if !ok {
		return 0, false
	}
	v, ok := values[pgtype.CanonicalLabels(labels)]
	return v, ok
}

// LoadSnapshot obtains the instance identity in one bounded query. Additional
// snapshot sections are populated by the advisor loaders as they are enabled;
// an empty section is intentionally distinguishable from a zero-valued fact.
func LoadSnapshot(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, now time.Time) (*Snapshot, error) {
	s := &Snapshot{
		Now: now, InstanceID: instanceID, PermTier: pgtype.TierReadOnly,
		Metrics: map[string]map[string]float64{}, Facts: map[string]map[string]Fact{},
		SkippedChecks: map[string]string{},
		Settings:      map[string]string{},
	}
	if pool == nil {
		return s, nil
	}
	var role string
	if err := pool.QueryRow(ctx, `SELECT cluster_id, role FROM instances WHERE instance_id=$1`, instanceID).Scan(&s.ClusterID, &role); err != nil {
		return nil, err
	}
	s.Role = pgtype.Role(role)
	if err := loadRelationSnapshot(ctx, pool, s, now); err != nil {
		return nil, err
	}
	return s, nil
}

// loadRelationSnapshot is deliberately a small, fixed-query loader. Relation
// rules share these bounded samples instead of issuing one query per rule.
// Older isolated advisor fixtures may not have migration 0007 installed; in
// that case the relation inputs remain unavailable and the engine degrades the
// affected rules as designed.
func loadRelationSnapshot(ctx context.Context, pool *pgxpool.Pool, s *Snapshot, now time.Time) error {
	if err := loadTables(ctx, pool, s, now); err != nil {
		if isMissingRelationTable(err) {
			return nil
		}
		return err
	}
	if err := loadIndexes(ctx, pool, s, now); err != nil {
		if isMissingRelationTable(err) {
			return nil
		}
		return err
	}
	if err := loadIndexFacts(ctx, pool, s); err != nil {
		if isMissingRelationTable(err) {
			return nil
		}
		return err
	}
	if err := loadBloat(ctx, pool, s, now); err != nil {
		if isMissingRelationTable(err) {
			return nil
		}
		return err
	}
	return nil
}

func isMissingRelationTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

func loadTables(ctx context.Context, pool *pgxpool.Pool, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT schemaname, relname,
		COALESCE(n_live_tup, 0)::float8, COALESCE(n_dead_tup, 0)::float8,
		COALESCE(n_mod_since_analyze, 0)::float8, last_autovacuum, last_vacuum,
		COALESCE(relfrozenxid_age, 0)::float8, COALESCE(seq_scan, 0)::float8,
		COALESCE(idx_scan, 0)::float8
		FROM metrics_tables
		WHERE instance_id=$1 AND ts >= $2
		ORDER BY n_dead_tup DESC NULLS LAST
		LIMIT 500`, s.InstanceID, now.Add(-15*time.Minute))
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Tables = make([]TableStat, 0)
	for rows.Next() {
		var schema, relation string
		var table TableStat
		if err := rows.Scan(&schema, &relation, &table.NLiveTup, &table.NDeadTup, &table.NModSinceAnalyze, &table.LastAutovacuum, &table.LastVacuum, &table.RelfrozenXIDAge, &table.SeqScanRate, &table.IdxScanRate); err != nil {
			return err
		}
		table.Name = schema + "." + relation
		s.Tables = append(s.Tables, table)
	}
	return rows.Err()
}

func loadIndexes(ctx context.Context, pool *pgxpool.Pool, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `WITH latest AS (
		SELECT DISTINCT ON (datname, schemaname, indexrelname)
			datname, schemaname, relname, indexrelname,
			COALESCE(idx_scan, 0)::float8 AS idx_scan, COALESCE(index_bytes, 0)::float8 AS index_bytes,
			COALESCE(is_primary, false) AS is_primary, COALESCE(is_unique, false) AS is_unique,
			COALESCE(is_valid, false) AS is_valid, COALESCE(def_hash, '') AS def_hash
		FROM metrics_indexes
		WHERE instance_id=$1 AND ts >= $2
		ORDER BY datname, schemaname, indexrelname, ts DESC
	), history AS (
		SELECT datname, schemaname, indexrelname,
			EXTRACT(EPOCH FROM (MAX(ts)-MIN(ts)))/86400.0 AS history_days
		FROM metrics_indexes
		WHERE instance_id=$1
		GROUP BY datname, schemaname, indexrelname
	)
	SELECT l.datname, l.schemaname, l.relname, l.indexrelname,
		l.idx_scan, l.index_bytes, l.is_primary, l.is_unique, l.is_valid, l.def_hash,
		COALESCE(h.history_days, 0)::float8
	FROM latest l
	LEFT JOIN history h USING (datname, schemaname, indexrelname)
	ORDER BY l.index_bytes DESC
	LIMIT 500`, s.InstanceID, now.Add(-15*time.Minute))
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Indexes = make([]IndexStat, 0)
	for rows.Next() {
		var datname, schema, tableName, indexName string
		var index IndexStat
		if err := rows.Scan(&datname, &schema, &tableName, &indexName, &index.IdxScan, &index.SizeBytes, &index.IsPrimary, &index.IsUnique, &index.IsValid, &index.DefHash, &index.HistoryDays); err != nil {
			return err
		}
		index.Name = schema + "." + indexName
		index.TableName = schema + "." + tableName
		s.Indexes = append(s.Indexes, index)
	}
	return rows.Err()
}

func loadIndexFacts(ctx context.Context, pool *pgxpool.Pool, s *Snapshot) error {
	rows, err := pool.Query(ctx, `SELECT key, COALESCE(value_text, ''), COALESCE(labels->>'def_hash', '') FROM object_facts WHERE tenant_id='default' AND instance_id=$1 AND kind='index_def'`, s.InstanceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	defs := make(map[string]struct {
		definition string
		hash       string
	})
	for rows.Next() {
		var key, definition, hash string
		if err := rows.Scan(&key, &definition, &hash); err != nil {
			return err
		}
		defs[key] = struct {
			definition string
			hash       string
		}{definition: definition, hash: hash}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range s.Indexes {
		name := s.Indexes[i].Name
		if def, ok := defs[name]; ok {
			s.Indexes[i].IndexDef = def.definition
			s.Indexes[i].DefHash = def.hash
		}
	}
	return nil
}

func loadBloat(ctx context.Context, pool *pgxpool.Pool, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT DISTINCT ON (datname, schemaname, relname, indexrelname, object_kind)
		datname, schemaname, relname, indexrelname, object_kind,
		COALESCE(real_bytes, 0)::float8, COALESCE(bloat_ratio, 0)::float8,
		COALESCE(method, 'estimate')
		FROM metrics_bloat
		WHERE instance_id=$1 AND ts >= $2
		ORDER BY datname, schemaname, relname, indexrelname, object_kind,
			CASE WHEN method='pgstattuple' THEN 0 ELSE 1 END, ts DESC
		LIMIT 500`, s.InstanceID, now.Add(-15*time.Minute))
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Bloat = make([]BloatStat, 0)
	for rows.Next() {
		var b BloatStat
		var datname, schema, relname, indexname, kind, method string
		if err := rows.Scan(&datname, &schema, &relname, &indexname, &kind, &b.SizeBytes, &b.Ratio, &method); err != nil {
			return err
		}
		b.Name = schema + "." + relname
		b.RelationName = b.Name
		if kind == "index" && indexname != "" {
			b.Name = schema + "." + indexname
		}
		s.Bloat = append(s.Bloat, b)
	}
	return rows.Err()
}
