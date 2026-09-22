package advisor

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

type snapshotDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Windows over which each snapshot section is read. A window must span at
// least one run of the check that feeds it: the settings check runs hourly
// and the bloat estimate every six hours, so a flat 15 minutes left those
// inputs missing most of the time and their findings opened and resolved
// with every pass.
const (
	recentWindow        = 15 * time.Minute
	bloatEstimateWindow = 13 * time.Hour
	pgstattupleWindow   = 7 * 24 * time.Hour
	checkpointWindow    = 24 * time.Hour
	replicationWindow   = 5 * time.Minute
	baselineWindow      = 8 * 24 * time.Hour
	siblingActiveWindow = time.Hour
	// settingBatchSlack separates the settings of the latest scrape, which
	// all carry that scrape's timestamp, from facts no scrape has refreshed
	// since: a setting put back to its default stops being shipped, and its
	// fact used to keep the old value forever.
	settingBatchSlack = time.Minute
	// pgBlockSize converts pg_stat_statements block counters to bytes.
	pgBlockSize = 8192
)

type Fact struct {
	Key        string
	ValueText  string
	Value      map[string]any
	Labels     map[string]string
	ObservedAt time.Time
}

type TableStat struct {
	Datname          string
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
	Datname   string
	Name      string
	TableName string
	DefHash   string
	IndexDef  string
	// IdxScan is the highest scan rate (scans per second) recorded over the
	// index's whole history, so zero means no scan was ever recorded. It
	// used to be the latest rate, which is zero for any index not scanned
	// in the last five minutes.
	IdxScan     float64
	SizeBytes   float64
	HistoryDays float64
	IsPrimary   bool
	IsUnique    bool
	IsValid     bool
}

type BloatStat struct {
	Datname      string
	Name         string
	RelationName string
	// Kind is "table" or "index".
	Kind      string
	Method    string
	Ratio     float64
	SizeBytes float64
}

// StatementStat holds one statement's totals over the recent window. Calls,
// TotalExecTimeMs, TempBytes and the block counters are counts, not the
// per-second rates metrics_statements stores.
type StatementStat struct {
	Datname         string
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
	InstanceID      uuid.UUID
	Role            pgtype.Role
	Settings        map[string]string
	SettingContexts map[string]string
	IndexHashes     map[string]string
	// BasebackupSeenAt is the last time a base backup was seen streaming
	// from this sibling; nil when none ever was.
	BasebackupSeenAt *time.Time
}

// ReplicationInfo describes the standbys streaming from this instance, as
// its own pg_stat_replication and pg_replication_slots report them.
type ReplicationInfo struct {
	Standbys int
	// MaxLagBytes is the WAL the furthest standby has not written yet.
	MaxLagBytes         float64
	ActivePhysicalSlots int
}

type Baseline struct {
	MeanExecTimeByQueryID map[int64]float64
}

type Snapshot struct {
	Now        time.Time
	ClusterID  int64
	InstanceID uuid.UUID
	Role       pgtype.Role
	PGVersion  pgtype.PGVersion
	PermTier   pgtype.PermTier
	// FirstSeen is when monitoring of the instance began; zero if unknown.
	FirstSeen     time.Time
	Metrics       map[string]map[string]float64
	Facts         map[string]map[string]Fact
	Tables        []TableStat
	Indexes       []IndexStat
	Bloat         []BloatStat
	Statements    []StatementStat
	Host          HostInfo
	Siblings      []SiblingInfo
	Replication   *ReplicationInfo
	SkippedChecks map[string]string
	Baseline      *Baseline
	Settings      map[string]string
	// SettingContexts maps a setting to its pg_settings context.
	SettingContexts map[string]string
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
		SkippedChecks:   map[string]string{},
		Settings:        map[string]string{},
		SettingContexts: map[string]string{},
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
	if err := pool.QueryRow(ctx, `SELECT pg_version, perm_tier, first_seen FROM instances WHERE instance_id=$1`, instanceID).Scan(&pgVersion, &tier, &s.FirstSeen); err == nil {
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
	if err := loadCheckpointRatio(ctx, pool, s, now); err != nil && !isOptionalSnapshotError(err) {
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
	if err := loadSiblings(ctx, pool, s, now); err != nil && !isOptionalSnapshotError(err) {
		return nil, err
	}
	if err := loadReplication(ctx, pool, s, now); err != nil && !isOptionalSnapshotError(err) {
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
		metric, labels, value, ts
		FROM metrics
		WHERE tenant_id='default' AND instance_id=$1 AND ts >= $2
		ORDER BY metric, labels, ts DESC`, s.InstanceID, now.Add(-recentWindow))
	if err != nil {
		return err
	}
	defer rows.Close()
	var hostTotal, hostSource float64
	var hasHostTotal, hasHostAvailable, hasHostSource bool
	type backendSample struct {
		state string
		value float64
		ts    time.Time
	}
	var backends []backendSample
	var backendsAt time.Time
	for rows.Next() {
		var name string
		var rawLabels []byte
		var value float64
		var ts time.Time
		if err := rows.Scan(&name, &rawLabels, &value, &ts); err != nil {
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
		if name == "pg_backends" {
			backends = append(backends, backendSample{state: labels["state"], value: value, ts: ts})
			if ts.After(backendsAt) {
				backendsAt = ts
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
	// The idle share of client connections, from the latest activity scrape
	// only: a state that has no session now emits no series, and its older
	// value would otherwise be added to the current ones.
	var idle, total float64
	for _, b := range backends {
		if !b.ts.Equal(backendsAt) {
			continue
		}
		total += b.value
		if b.state == "idle" {
			idle += b.value
		}
	}
	if total > 0 {
		s.Metrics["pg_connections_idle_share"] = map[string]float64{"": idle / total}
	} else if _, ok := s.Metrics["pg_connections_used"]; ok {
		// No client session at all: pg_backends emits no series then.
		s.Metrics["pg_connections_idle_share"] = map[string]float64{"": 0}
	}
	return rows.Err()
}

// loadCheckpointRatio derives the share of requested checkpoints over the
// last day from the checkpointer counters (stored as rates). Both counters
// come from the same scrapes, so the ratio of their summed rates is the
// ratio of their totals.
func loadCheckpointRatio(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	var requested, timed float64
	var samples int64
	if err := pool.QueryRow(ctx, `SELECT
		COALESCE(SUM(value) FILTER (WHERE metric='pg_checkpoints_requested_total'), 0)::float8,
		COALESCE(SUM(value) FILTER (WHERE metric='pg_checkpoints_timed_total'), 0)::float8,
		count(*)
		FROM metrics
		WHERE tenant_id='default' AND instance_id=$1 AND ts >= $2
			AND metric IN ('pg_checkpoints_requested_total', 'pg_checkpoints_timed_total')`,
		s.InstanceID, now.Add(-checkpointWindow)).Scan(&requested, &timed, &samples); err != nil {
		return err
	}
	if samples == 0 {
		return nil
	}
	ratio := 0.0
	if requested+timed > 0 {
		ratio = requested / (requested + timed)
	}
	s.Metrics["pg_checkpoints_requested_ratio"] = map[string]float64{"": ratio}
	return nil
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

// settingUnits converts a pg_settings unit to bytes or seconds, the same
// normalisation the settings check applies to pg_setting_bytes and
// pg_setting_seconds.
var settingUnits = map[string]struct {
	suffix string
	scale  float64
}{
	"B": {"_bytes", 1}, "8kB": {"_bytes", 8192}, "kB": {"_bytes", 1024},
	"MB": {"_bytes", 1 << 20}, "GB": {"_bytes", 1 << 30}, "TB": {"_bytes", 1 << 40},
	"us": {"_seconds", 1e-6}, "ms": {"_seconds", 1e-3}, "s": {"_seconds", 1},
	"min": {"_seconds", 60}, "h": {"_seconds", 3600}, "d": {"_seconds", 86400},
}

// normalizedSetting is a setting's value in bytes or seconds, under the key
// the rules read (work_mem_bytes, archive_timeout_seconds, ...).
func normalizedSetting(name, value, unit string) (string, string, bool) {
	u, ok := settingUnits[unit]
	if !ok {
		return "", "", false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || v < 0 {
		return "", "", false
	}
	return name + u.suffix, strconv.FormatFloat(v*u.scale, 'f', -1, 64), true
}

// latestSettingBatch reports whether a setting fact belongs to the newest
// settings scrape of its instance.
func latestSettingBatch(observed, newest time.Time) bool {
	return !observed.Before(newest.Add(-settingBatchSlack))
}

type factRow struct {
	kind, key, value, datname string
	labels                    map[string]string
	observed                  time.Time
}

func loadFacts(ctx context.Context, pool snapshotDB, s *Snapshot) error {
	rows, err := pool.Query(ctx, `SELECT kind, key, labels, COALESCE(value_text, ''), last_seen, datname
		FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1`, s.InstanceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var facts []factRow
	var newestSetting time.Time
	for rows.Next() {
		var f factRow
		var rawLabels []byte
		if err := rows.Scan(&f.kind, &f.key, &rawLabels, &f.value, &f.observed, &f.datname); err != nil {
			return err
		}
		f.labels = map[string]string{}
		if len(rawLabels) > 0 {
			_ = json.Unmarshal(rawLabels, &f.labels)
		}
		if f.kind == "setting" && f.observed.After(newestSetting) {
			newestSetting = f.observed
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range facts {
		if f.kind == "setting" && !latestSettingBatch(f.observed, newestSetting) {
			continue
		}
		if s.Facts[f.kind] == nil {
			s.Facts[f.kind] = map[string]Fact{}
		}
		key := f.key
		if f.kind == "index_def" {
			key = indexHashKey(f.datname, f.key)
		}
		s.Facts[f.kind][key] = Fact{Key: f.key, ValueText: f.value, Labels: f.labels, ObservedAt: f.observed}
		if f.kind == "setting" {
			s.Settings[f.key] = f.value
			if f.labels["context"] != "" {
				s.SettingContexts[f.key] = f.labels["context"]
			}
			// The fact is always there; pg_setting_bytes and
			// pg_setting_seconds only for an hour after each scrape.
			if key, value, ok := normalizedSetting(f.key, f.value, f.labels["unit"]); ok {
				if _, set := s.Settings[key]; !set {
					s.Settings[key] = value
				}
			}
		}
		if f.kind == "check_skip" {
			s.SkippedChecks[f.key] = f.value
			switch f.key {
			case "stat_statements":
				s.SkippedChecks["Statements"] = f.value
			case "table_stats":
				s.SkippedChecks["Tables"] = f.value
			case "index_stats":
				s.SkippedChecks["Indexes"] = f.value
			case "bloat_estimate":
				s.SkippedChecks["Bloat"] = f.value
			}
		}
	}
	return nil
}

// loadStatements totals each statement over the recent window: every row
// holds per-second rates over sample_seconds (the default stat_statements
// interval for rows written before migration 0014). The rules used to read
// the latest rates as counts, so "at least 10 calls" meant ten calls per
// second, and the temporary bytes were never loaded at all.
func loadStatements(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	const seconds = "COALESCE(sample_seconds, 60)"
	rows, err := pool.Query(ctx, `SELECT datname, queryid,
		COALESCE(SUM(calls_rate * `+seconds+`), 0)::float8,
		COALESCE(SUM(exec_time_rate_ms * `+seconds+`), 0)::float8,
		COALESCE(SUM(temp_blks_written_rate * `+seconds+`), 0)::float8,
		COALESCE(SUM(shared_blks_read_rate * `+seconds+`), 0)::float8,
		COALESCE(SUM(shared_blks_hit_rate * `+seconds+`), 0)::float8
		FROM metrics_statements
		WHERE instance_id=$1 AND ts >= $2
		GROUP BY datname, queryid`, s.InstanceID, now.Add(-recentWindow))
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Statements = make([]StatementStat, 0)
	for rows.Next() {
		var q StatementStat
		var tempBlocks float64
		if err := rows.Scan(&q.Datname, &q.QueryID, &q.Calls, &q.TotalExecTimeMs, &tempBlocks, &q.SharedBlksRead, &q.SharedBlksHit); err != nil {
			return err
		}
		q.TempBytes = tempBlocks * pgBlockSize
		if q.Calls > 0 {
			q.MeanExecTimeMs = q.TotalExecTimeMs / q.Calls
		}
		s.Statements = append(s.Statements, q)
	}
	return rows.Err()
}

// loadBaseline is the median per-sample mean execution time of each
// statement over the previous week. Samples without calls carry no mean: they
// used to count as zero, which dragged the median of an occasional
// statement to zero and hid its regressions.
func loadBaseline(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT queryid,
		percentile_cont(0.5) WITHIN GROUP (ORDER BY exec_time_rate_ms / calls_rate)
		FROM metrics_statements
		WHERE instance_id=$1 AND ts >= $2 AND ts < $3 AND calls_rate > 0 AND exec_time_rate_ms IS NOT NULL
		GROUP BY queryid`, s.InstanceID, now.Add(-baselineWindow), now.Add(-time.Hour))
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

// indexHashKey names an index definition across databases: the same schema
// and index name can exist in several databases of one instance.
func indexHashKey(datname, name string) string { return datname + "\x00" + name }

type siblingFactRow struct {
	id                                 uuid.UUID
	role, kind, key, value, hash, ctxt *string
	datname                            *string
	observed                           *time.Time
}

// loadSiblings reads the other members of the cluster that reported in the
// last hour: a decommissioned instance used to stay a sibling forever, and
// its last settings were compared as drift.
func loadSiblings(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT i.instance_id, i.role, f.kind, f.key,
		COALESCE(f.value_text, ''), COALESCE(f.labels->>'def_hash', ''), COALESCE(f.labels->>'context', ''),
		f.datname, f.last_seen
		FROM instances i
		LEFT JOIN object_facts f ON f.instance_id=i.instance_id AND f.tenant_id='default'
			AND f.kind IN ('setting', 'index_def', 'backup')
		WHERE i.cluster_id=$1 AND i.instance_id<>$2 AND i.last_seen >= $3
		ORDER BY i.instance_id`, s.ClusterID, s.InstanceID, now.Add(-siblingActiveWindow))
	if err != nil {
		return err
	}
	defer rows.Close()
	var facts []siblingFactRow
	newestSetting := map[uuid.UUID]time.Time{}
	for rows.Next() {
		var f siblingFactRow
		if err := rows.Scan(&f.id, &f.role, &f.kind, &f.key, &f.value, &f.hash, &f.ctxt, &f.datname, &f.observed); err != nil {
			return err
		}
		if f.kind != nil && *f.kind == "setting" && f.observed != nil && f.observed.After(newestSetting[f.id]) {
			newestSetting[f.id] = *f.observed
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	byID := map[uuid.UUID]*SiblingInfo{}
	for _, f := range facts {
		sib := byID[f.id]
		if sib == nil {
			sib = &SiblingInfo{InstanceID: f.id, Settings: map[string]string{}, SettingContexts: map[string]string{}, IndexHashes: map[string]string{}}
			if f.role != nil {
				sib.Role = pgtype.Role(*f.role)
			}
			byID[f.id] = sib
		}
		if f.kind == nil || f.key == nil {
			continue
		}
		switch *f.kind {
		case "setting":
			if f.value == nil || (f.observed != nil && !latestSettingBatch(*f.observed, newestSetting[f.id])) {
				continue
			}
			sib.Settings[*f.key] = *f.value
			if f.ctxt != nil && *f.ctxt != "" {
				sib.SettingContexts[*f.key] = *f.ctxt
			}
		case "index_def":
			if f.hash != nil {
				datname := ""
				if f.datname != nil {
					datname = *f.datname
				}
				sib.IndexHashes[indexHashKey(datname, *f.key)] = *f.hash
			}
		case "backup":
			if f.observed != nil && (sib.BasebackupSeenAt == nil || f.observed.After(*sib.BasebackupSeenAt)) {
				seen := *f.observed
				sib.BasebackupSeenAt = &seen
			}
		}
	}
	s.Siblings = make([]SiblingInfo, 0, len(byID))
	for _, sibling := range byID {
		s.Siblings = append(s.Siblings, *sibling)
	}
	// A stable order keeps the rules that walk the siblings deterministic.
	sort.Slice(s.Siblings, func(i, j int) bool { return s.Siblings[i].InstanceID.String() < s.Siblings[j].InstanceID.String() })
	return nil
}

// loadReplication summarises the standbys streaming from this instance and
// the physical slots holding WAL for them, from the latest row of each.
func loadReplication(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	info := &ReplicationInfo{}
	if err := pool.QueryRow(ctx, `WITH latest AS (
			SELECT DISTINCT ON (slot_name, src) src, write_lag_bytes, slot_active, slot_type
			FROM (SELECT slot_name, ts, write_lag_bytes, slot_active, slot_type, `+store.ReplicationSourceSQL+` AS src
			      FROM metrics_replication WHERE instance_id=$1 AND ts >= $2) r
			WHERE src IN ('sender', 'slot')
			ORDER BY slot_name, src, ts DESC
		)
		SELECT count(*) FILTER (WHERE src='sender'),
			COALESCE(max(write_lag_bytes) FILTER (WHERE src='sender'), 0)::float8,
			count(*) FILTER (WHERE src='slot' AND slot_active AND COALESCE(slot_type, 'physical')='physical')
		FROM latest`, s.InstanceID, now.Add(-replicationWindow)).Scan(&info.Standbys, &info.MaxLagBytes, &info.ActivePhysicalSlots); err != nil {
		return err
	}
	s.Replication = info
	return nil
}

func loadTables(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	// One row per relation: the window holds several scrapes, and every
	// relation used to be evaluated once per scrape (vacuum.starvation
	// counted each table three times).
	rows, err := pool.Query(ctx, `SELECT datname, schemaname, relname, n_live_tup, n_dead_tup,
		n_mod_since_analyze, last_autovacuum, last_vacuum, relfrozenxid_age, seq_scan, idx_scan
		FROM (SELECT DISTINCT ON (datname, schemaname, relname)
			datname, schemaname, relname,
			COALESCE(n_live_tup, 0)::float8 AS n_live_tup, COALESCE(n_dead_tup, 0)::float8 AS n_dead_tup,
			COALESCE(n_mod_since_analyze, 0)::float8 AS n_mod_since_analyze, last_autovacuum, last_vacuum,
			COALESCE(relfrozenxid_age, 0)::float8 AS relfrozenxid_age, COALESCE(seq_scan, 0)::float8 AS seq_scan,
			COALESCE(idx_scan, 0)::float8 AS idx_scan
			FROM metrics_tables
			WHERE instance_id=$1 AND ts >= $2
			ORDER BY datname, schemaname, relname, ts DESC) latest
		ORDER BY n_dead_tup DESC
		LIMIT 500`, s.InstanceID, now.Add(-recentWindow))
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Tables = make([]TableStat, 0)
	for rows.Next() {
		var schema, relation string
		var table TableStat
		if err := rows.Scan(&table.Datname, &schema, &relation, &table.NLiveTup, &table.NDeadTup, &table.NModSinceAnalyze, &table.LastAutovacuum, &table.LastVacuum, &table.RelfrozenXIDAge, &table.SeqScanRate, &table.IdxScanRate); err != nil {
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
			COALESCE(index_bytes, 0)::float8 AS index_bytes,
			COALESCE(is_primary, false) AS is_primary, COALESCE(is_unique, false) AS is_unique,
			COALESCE(is_valid, false) AS is_valid, COALESCE(def_hash, '') AS def_hash
		FROM metrics_indexes
		WHERE instance_id=$1 AND ts >= $2
		ORDER BY datname, schemaname, indexrelname, ts DESC
	), history AS (
		SELECT datname, schemaname, indexrelname,
			EXTRACT(EPOCH FROM (MAX(ts)-MIN(ts)))/86400.0 AS history_days,
			MAX(COALESCE(idx_scan, 0)) AS peak_scan_rate
		FROM metrics_indexes
		WHERE instance_id=$1
		GROUP BY datname, schemaname, indexrelname
	)
	SELECT l.datname, l.schemaname, l.relname, l.indexrelname,
		COALESCE(h.peak_scan_rate, 0)::float8, l.index_bytes, l.is_primary, l.is_unique, l.is_valid, l.def_hash,
		COALESCE(h.history_days, 0)::float8
	FROM latest l
	LEFT JOIN history h USING (datname, schemaname, indexrelname)
	ORDER BY l.index_bytes DESC
	LIMIT 500`, s.InstanceID, now.Add(-recentWindow))
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Indexes = make([]IndexStat, 0)
	for rows.Next() {
		var schema, tableName, indexName string
		var index IndexStat
		if err := rows.Scan(&index.Datname, &schema, &tableName, &indexName, &index.IdxScan, &index.SizeBytes, &index.IsPrimary, &index.IsUnique, &index.IsValid, &index.DefHash, &index.HistoryDays); err != nil {
			return err
		}
		index.Name = schema + "." + indexName
		index.TableName = schema + "." + tableName
		s.Indexes = append(s.Indexes, index)
	}
	return rows.Err()
}

func loadIndexFacts(ctx context.Context, pool snapshotDB, s *Snapshot) error {
	rows, err := pool.Query(ctx, `SELECT datname, key, COALESCE(value_text, ''), COALESCE(labels->>'def_hash', '') FROM object_facts WHERE tenant_id='default' AND instance_id=$1 AND kind='index_def'`, s.InstanceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type indexDef struct{ definition, hash string }
	defs := make(map[string]indexDef)
	for rows.Next() {
		var datname, key, definition, hash string
		if err := rows.Scan(&datname, &key, &definition, &hash); err != nil {
			return err
		}
		defs[indexHashKey(datname, key)] = indexDef{definition: definition, hash: hash}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range s.Indexes {
		if def, ok := defs[indexHashKey(s.Indexes[i].Datname, s.Indexes[i].Name)]; ok {
			s.Indexes[i].IndexDef = def.definition
			s.Indexes[i].DefHash = def.hash
		}
	}
	return nil
}

// loadBloat keeps the latest measurement of each relation, preferring an
// exact pgstattuple one from the last week to the estimate, which runs
// every six hours and was never inside the old 15-minute window for long.
func loadBloat(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT datname, schemaname, relname, indexrelname, object_kind, real_bytes, bloat_ratio, method
		FROM (SELECT DISTINCT ON (datname, schemaname, relname, indexrelname, object_kind)
			datname, schemaname, relname, indexrelname, object_kind,
			COALESCE(real_bytes, 0)::float8 AS real_bytes, COALESCE(bloat_ratio, 0)::float8 AS bloat_ratio,
			COALESCE(method, 'estimate') AS method
			FROM metrics_bloat
			WHERE instance_id=$1 AND ts >= $2 AND (method = 'pgstattuple' OR ts >= $3)
			ORDER BY datname, schemaname, relname, indexrelname, object_kind,
				CASE WHEN method='pgstattuple' THEN 0 ELSE 1 END, ts DESC) latest
		ORDER BY real_bytes * bloat_ratio DESC
		LIMIT 500`, s.InstanceID, now.Add(-pgstattupleWindow), now.Add(-bloatEstimateWindow))
	if err != nil {
		return err
	}
	defer rows.Close()
	s.Bloat = make([]BloatStat, 0)
	for rows.Next() {
		var b BloatStat
		var schema, relname, indexname string
		if err := rows.Scan(&b.Datname, &schema, &relname, &indexname, &b.Kind, &b.SizeBytes, &b.Ratio, &b.Method); err != nil {
			return err
		}
		b.Name = schema + "." + relname
		b.RelationName = b.Name
		if b.Kind == "index" && indexname != "" {
			b.Name = schema + "." + indexname
		}
		s.Bloat = append(s.Bloat, b)
	}
	return rows.Err()
}
