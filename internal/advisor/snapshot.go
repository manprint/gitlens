package advisor

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/pgtype"
)

type snapshotDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

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
func LoadSnapshot(ctx context.Context, pool snapshotDB, instanceID uuid.UUID, now time.Time) (*Snapshot, error) {
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
	var pgVersion int
	var tier string
	if err := pool.QueryRow(ctx, `SELECT pg_version, perm_tier FROM instances WHERE instance_id=$1`, instanceID).Scan(&pgVersion, &tier); err == nil {
		s.PGVersion = pgtype.PGVersion(pgVersion)
		if parsed, parseErr := pgtype.ParsePermTier(tier); parseErr == nil {
			s.PermTier = parsed
		}
	} else if !isOptionalSnapshotError(err) {
		return nil, err
	}
	if err := loadMetrics(ctx, pool, s, now); err != nil && !isOptionalSnapshotError(err) {
		return nil, err
	}
	if err := loadFacts(ctx, pool, s); err != nil && !isOptionalSnapshotError(err) {
		return nil, err
	}
	if err := loadRelationSnapshot(ctx, pool, s, now); err != nil {
		return nil, err
	}
	if err := loadStatements(ctx, pool, s, now); err != nil && !isOptionalSnapshotError(err) {
		return nil, err
	}
	if err := loadBaseline(ctx, pool, s, now); err != nil && !isOptionalSnapshotError(err) {
		return nil, err
	}
	if err := loadSiblings(ctx, pool, s); err != nil && !isOptionalSnapshotError(err) {
		return nil, err
	}
	return s, nil
}

// loadRelationSnapshot is deliberately a small, fixed-query loader. Relation
// rules share these bounded samples instead of issuing one query per rule.
// Older isolated advisor fixtures may not have migration 0007 installed; in
// that case the relation inputs remain unavailable and the engine degrades the
// affected rules as designed.
func loadRelationSnapshot(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	if err := loadTables(ctx, pool, s, now); err != nil {
		if !isOptionalSnapshotError(err) {
			return err
		}
	}
	if err := loadIndexes(ctx, pool, s, now); err != nil {
		if !isOptionalSnapshotError(err) {
			return err
		}
	}
	if err := loadIndexFacts(ctx, pool, s); err != nil {
		if !isOptionalSnapshotError(err) {
			return err
		}
	}
	if err := loadBloat(ctx, pool, s, now); err != nil {
		if !isOptionalSnapshotError(err) {
			return err
		}
	}
	return nil
}

func isOptionalSnapshotError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "42P01" || pgErr.Code == "42703"
}

func loadMetrics(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT DISTINCT ON (metric, labels)
		metric, labels, value
		FROM metrics
		WHERE tenant_id='default' AND instance_id=$1 AND ts >= $2
		ORDER BY metric, labels, ts DESC`, s.InstanceID, now.Add(-15*time.Minute))
	if err != nil {
		return err
	}
	defer rows.Close()
	var hostTotal, hostSource float64
	var hasHostTotal, hasHostAvailable, hasHostSource bool
	for rows.Next() {
		var name string
		var rawLabels []byte
		var value float64
		if err := rows.Scan(&name, &rawLabels, &value); err != nil {
			return err
		}
		labels := map[string]string{}
		if len(rawLabels) > 0 {
			_ = json.Unmarshal(rawLabels, &labels)
		}
		canonical := pgtype.CanonicalLabels(labels)
		if s.Metrics[name] == nil {
			s.Metrics[name] = map[string]float64{}
		}
		s.Metrics[name][canonical] = value
		if canonical == "" {
			switch name {
			case "host_mem_total_bytes":
				hostTotal, hasHostTotal = value, true
			case "host_mem_available_bytes":
				hasHostAvailable = true
			case "host_metrics_source":
				hostSource, hasHostSource = value, true
			}
		}
		if name == "pg_setting_bytes" || name == "pg_setting_seconds" {
			if setting := labels["name"]; setting != "" {
				suffix := "_bytes"
				if name == "pg_setting_seconds" {
					suffix = "_seconds"
				}
				s.Settings[setting+suffix] = strconv.FormatFloat(value, 'f', -1, 64)
			}
		}
	}
	if hasHostTotal && hasHostAvailable && hostTotal > 0 {
		s.Host = HostInfo{
			Available:  true,
			Source:     hostSourceName(hostSource),
			TotalBytes: hostTotal,
		}
		if hasHostSource && (int(hostSource) == 1 || int(hostSource) == 2) {
			s.Host.CgroupLimitBytes = hostTotal
		}
	}
	return rows.Err()
}

func hostSourceName(value float64) string {
	switch int(value) {
	case 1:
		return "cgroup_v1"
	case 2:
		return "cgroup_v2"
	default:
		return "host"
	}
}

func loadFacts(ctx context.Context, pool snapshotDB, s *Snapshot) error {
	rows, err := pool.Query(ctx, `SELECT kind, key, labels, COALESCE(value_text, ''), last_seen
		FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1`, s.InstanceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, key, value string
		var rawLabels []byte
		var observed time.Time
		if err := rows.Scan(&kind, &key, &rawLabels, &value, &observed); err != nil {
			return err
		}
		labels := map[string]string{}
		if len(rawLabels) > 0 {
			_ = json.Unmarshal(rawLabels, &labels)
		}
		if s.Facts[kind] == nil {
			s.Facts[kind] = map[string]Fact{}
		}
		s.Facts[kind][key] = Fact{Key: key, ValueText: value, ObservedAt: observed}
		if kind == "setting" {
			s.Settings[key] = value
		}
		if kind == "check_skip" {
			s.SkippedChecks[key] = value
			switch key {
			case "stat_statements":
				s.SkippedChecks["Statements"] = value
			case "table_stats":
				s.SkippedChecks["Tables"] = value
			case "index_stats":
				s.SkippedChecks["Indexes"] = value
			case "bloat_estimate":
				s.SkippedChecks["Bloat"] = value
			}
		}
	}
	return rows.Err()
}

func loadStatements(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT DISTINCT ON (datname, queryid)
		datname, queryid, COALESCE(calls_rate, 0), COALESCE(exec_time_rate_ms, 0),
		COALESCE(shared_blks_read_rate, 0), COALESCE(shared_blks_hit_rate, 0)
		FROM metrics_statements
		WHERE instance_id=$1 AND ts >= $2
		ORDER BY datname, queryid, ts DESC`, s.InstanceID, now.Add(-15*time.Minute))
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Statements = make([]StatementStat, 0)
	for rows.Next() {
		var datname string
		var q StatementStat
		var exec, reads, hits float64
		if err := rows.Scan(&datname, &q.QueryID, &q.Calls, &exec, &reads, &hits); err != nil {
			return err
		}
		q.TotalExecTimeMs = exec
		if q.Calls > 0 {
			q.MeanExecTimeMs = exec / q.Calls
		}
		q.SharedBlksRead = reads
		q.SharedBlksHit = hits
		s.Statements = append(s.Statements, q)
	}
	return rows.Err()
}

func loadBaseline(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT queryid,
		percentile_cont(0.5) WITHIN GROUP (ORDER BY CASE WHEN calls_rate > 0 THEN exec_time_rate_ms / calls_rate ELSE 0 END)
		FROM metrics_statements
		WHERE instance_id=$1 AND ts >= $2 AND ts < $3
		GROUP BY queryid`, s.InstanceID, now.Add(-8*24*time.Hour), now.Add(-time.Hour))
	if err != nil {
		return err
	}
	defer rows.Close()
	baseline := &Baseline{MeanExecTimeByQueryID: map[int64]float64{}}
	for rows.Next() {
		var queryID int64
		var mean float64
		if err := rows.Scan(&queryID, &mean); err != nil {
			return err
		}
		baseline.MeanExecTimeByQueryID[queryID] = mean
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.Baseline = baseline
	return nil
}

func loadSiblings(ctx context.Context, pool snapshotDB, s *Snapshot) error {
	rows, err := pool.Query(ctx, `SELECT i.instance_id, i.role, f.kind, f.key,
		COALESCE(f.value_text, ''), COALESCE(f.labels->>'def_hash', '')
		FROM instances i
		LEFT JOIN object_facts f ON f.instance_id=i.instance_id AND f.tenant_id='default'
			AND f.kind IN ('setting', 'index_def')
		WHERE i.cluster_id=$1 AND i.instance_id<>$2
		ORDER BY i.instance_id`, s.ClusterID, s.InstanceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[uuid.UUID]*SiblingInfo{}
	for rows.Next() {
		var id uuid.UUID
		var role, kind, key, value, hash *string
		if err := rows.Scan(&id, &role, &kind, &key, &value, &hash); err != nil {
			return err
		}
		sib := byID[id]
		if sib == nil {
			sib = &SiblingInfo{InstanceID: id, Settings: map[string]string{}, IndexHashes: map[string]string{}}
			if role != nil {
				sib.Role = pgtype.Role(*role)
			}
			byID[id] = sib
		}
		if kind == nil || key == nil {
			continue
		}
		if *kind == "setting" && value != nil {
			sib.Settings[*key] = *value
		}
		if *kind == "index_def" && hash != nil {
			sib.IndexHashes[*key] = *hash
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.Siblings = make([]SiblingInfo, 0, len(byID))
	for _, sibling := range byID {
		s.Siblings = append(s.Siblings, *sibling)
	}
	return nil
}

func loadTables(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
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

func loadIndexes(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
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

func loadIndexFacts(ctx context.Context, pool snapshotDB, s *Snapshot) error {
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

func loadBloat(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
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
