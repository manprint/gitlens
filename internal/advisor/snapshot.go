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
	// lockSampleFresh is how old the latest lock sample may be for the lock
	// rules to read it. The locks check samples every ten seconds.
	lockSampleFresh = 2 * time.Minute
	// settingBatchSlack separates the settings of the latest scrape, which
	// all carry that scrape's timestamp, from facts no scrape has refreshed
	// since: a setting put back to its default stops being shipped, and its
	// fact used to keep the old value forever.
	settingBatchSlack = time.Minute
	// pgBlockSize converts pg_stat_statements block counters to bytes.
	pgBlockSize = 8192
)

// Fact is one object_facts row. Snapshot.Facts keys a fact of an instance
// (Datname empty) by its key, and a fact of a database by
// databaseFactKey(Datname, Key): the same object name exists in several
// databases.
type Fact struct {
	Key       string
	Datname   string
	ValueText string
	// Value is value_json, when it is an object.
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
	// ScanTracked reports that the agent read last_idx_scan for this index
	// (PostgreSQL 16 and later), so LastScan and StatsReset are meaningful.
	ScanTracked bool
	// LastScan is the last scan since StatsReset, nil when the index was not
	// scanned since then. StatsReset is nil when the reset time is unknown.
	LastScan   *time.Time
	StatsReset *time.Time
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
	// IndexScanned names the indexes this sibling has scanned: those with a
	// last scan since its statistics reset, and those with a scan anywhere in
	// its metrics history. A standby's reads count only on the standby, so
	// the primary needs this before it calls an index unused.
	IndexScanned map[string]bool
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

// LockTreeSample is the latest lock tree the locks check stored for the
// instance. It holds only what the lock rules read: no query text.
type LockTreeSample struct {
	SampledAt time.Time
	Nodes     []LockTreeNode
}

// LockTreeNode is one session of a lock sample. WaitDuration is the seconds
// the session has waited for the lock it waits on, measured on the server at
// the sample; it is zero for a session that waits on none.
type LockTreeNode struct {
	PID          int32
	BlockedBy    []int32
	WaitDuration float64
}

type Snapshot struct {
	Now        time.Time
	ClusterID  int64
	InstanceID uuid.UUID
	Role       pgtype.Role
	PGVersion  pgtype.PGVersion
	PermTier   pgtype.PermTier
	// FirstSeen is when monitoring of the instance began; zero if unknown.
	FirstSeen   time.Time
	Metrics     map[string]map[string]float64
	Facts       map[string]map[string]Fact
	Tables      []TableStat
	Indexes     []IndexStat
	Bloat       []BloatStat
	Statements  []StatementStat
	Host        HostInfo
	Siblings    []SiblingInfo
	Replication *ReplicationInfo
	// LockTree is the latest lock sample; nil when the lock tree could not be
	// read, and empty (zero SampledAt) when no sample was ever stored.
	LockTree      *LockTreeSample
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
	if err := loadLockTree(ctx, pool, s); err != nil && !isOptionalSnapshotError(err) {
		return nil, err
	}
	return s, nil
}

// loadLockTree reads the newest lock tree stored for the instance. A tree that
// does not decode is not an empty sample: it is recorded as skipped, so the
// lock rules degrade rather than read no wait where there may be one.
func loadLockTree(ctx context.Context, pool snapshotDB, s *Snapshot) error {
	var ts time.Time
	var raw []byte
	err := pool.QueryRow(ctx, `SELECT ts, tree FROM lock_snapshots WHERE instance_id=$1 ORDER BY ts DESC LIMIT 1`, s.InstanceID).Scan(&ts, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		s.LockTree = &LockTreeSample{}
		return nil
	}
	if err != nil {
		return err
	}
	sample, parseErr := parseLockTree(ts, raw)
	if parseErr != nil {
		s.SkippedChecks["LockTree"] = "lock tree unreadable: " + parseErr.Error()
	} else {
		s.LockTree = sample
	}
	return nil
}

// parseLockTree reads the fields the lock rules use from a stored tree.
func parseLockTree(ts time.Time, raw []byte) (*LockTreeSample, error) {
	var tree struct {
		Nodes []struct {
			PID          int32    `json:"pid"`
			BlockedBy    []int32  `json:"blocked_by"`
			WaitDuration *float64 `json:"wait_duration"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	sample := &LockTreeSample{SampledAt: ts, Nodes: make([]LockTreeNode, 0, len(tree.Nodes))}
	for _, n := range tree.Nodes {
		node := LockTreeNode{PID: n.PID, BlockedBy: n.BlockedBy}
		if n.WaitDuration != nil {
			node.WaitDuration = *n.WaitDuration
		}
		sample.Nodes = append(sample.Nodes, node)
	}
	return sample, nil
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
	attachIndexDefs(s)
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
	object                    map[string]any
	observed                  time.Time
}

func loadFacts(ctx context.Context, pool snapshotDB, s *Snapshot) error {
	rows, err := pool.Query(ctx, `SELECT kind, key, labels, COALESCE(value_text, ''), last_seen, datname, value_json
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
		var rawLabels, rawValue []byte
		if err := rows.Scan(&f.kind, &f.key, &rawLabels, &f.value, &f.observed, &f.datname, &rawValue); err != nil {
			return err
		}
		f.labels = map[string]string{}
		if len(rawLabels) > 0 {
			_ = json.Unmarshal(rawLabels, &f.labels)
		}
		if len(rawValue) > 0 {
			_ = json.Unmarshal(rawValue, &f.object)
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
		if f.kind == "index_def" || f.datname != "" {
			key = databaseFactKey(f.datname, f.key)
		}
		s.Facts[f.kind][key] = Fact{Key: f.key, Datname: f.datname, ValueText: f.value, Value: f.object, Labels: f.labels, ObservedAt: f.observed}
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
func indexHashKey(datname, name string) string { return databaseFactKey(datname, name) }

// databaseFactKey is the Snapshot.Facts key of a fact reported for one
// database.
func databaseFactKey(datname, key string) string { return datname + "\x00" + key }

type siblingFactRow struct {
	id                                 uuid.UUID
	role, kind, key, value, hash, ctxt *string
	lastScan                           *string
	datname                            *string
	observed                           *time.Time
}

// loadSiblings reads the other members of the cluster that reported in the
// last hour: a decommissioned instance used to stay a sibling forever, and
// its last settings were compared as drift.
func loadSiblings(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time) error {
	rows, err := pool.Query(ctx, `SELECT i.instance_id, i.role, f.kind, f.key,
		COALESCE(f.value_text, ''), COALESCE(f.labels->>'def_hash', ''), COALESCE(f.labels->>'context', ''),
		COALESCE(f.labels->>'last_idx_scan', ''), f.datname, f.last_seen
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
		if err := rows.Scan(&f.id, &f.role, &f.kind, &f.key, &f.value, &f.hash, &f.ctxt, &f.lastScan, &f.datname, &f.observed); err != nil {
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
			sib = &SiblingInfo{InstanceID: f.id, Settings: map[string]string{}, SettingContexts: map[string]string{}, IndexHashes: map[string]string{}, IndexScanned: map[string]bool{}}
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
			datname := ""
			if f.datname != nil {
				datname = *f.datname
			}
			if f.hash != nil {
				sib.IndexHashes[indexHashKey(datname, *f.key)] = *f.hash
			}
			// An empty label is no scan since the reset, not an unknown one.
			if f.lastScan != nil && *f.lastScan != "" {
				sib.IndexScanned[indexHashKey(datname, *f.key)] = true
			}
		case "backup":
			if f.observed != nil && (sib.BasebackupSeenAt == nil || f.observed.After(*sib.BasebackupSeenAt)) {
				seen := *f.observed
				sib.BasebackupSeenAt = &seen
			}
		}
	}
	if err := loadSiblingScans(ctx, pool, s, now, byID); err != nil {
		return err
	}
	s.Siblings = make([]SiblingInfo, 0, len(byID))
	for _, sibling := range byID {
		s.Siblings = append(s.Siblings, *sibling)
	}
	// A stable order keeps the rules that walk the siblings deterministic.
	sort.Slice(s.Siblings, func(i, j int) bool { return s.Siblings[i].InstanceID.String() < s.Siblings[j].InstanceID.String() })
	return nil
}

// loadSiblingScans marks the indexes each active sibling scanned at any point
// in its retained metrics history. It is the only evidence for a sibling on
// PostgreSQL 15, which cannot report last_idx_scan, and it still counts a scan
// that predates the statistics reset the fact reports.
func loadSiblingScans(ctx context.Context, pool snapshotDB, s *Snapshot, now time.Time, byID map[uuid.UUID]*SiblingInfo) error {
	if len(byID) == 0 {
		return nil
	}
	rows, err := pool.Query(ctx, `SELECT m.instance_id, m.datname, m.schemaname, m.indexrelname
		FROM metrics_indexes m
		WHERE m.tenant_id='default' AND m.idx_scan > 0
		  AND m.instance_id IN (SELECT i.instance_id FROM instances i
		        WHERE i.cluster_id=$1 AND i.instance_id<>$2 AND i.last_seen >= $3)
		GROUP BY m.instance_id, m.datname, m.schemaname, m.indexrelname`, s.ClusterID, s.InstanceID, now.Add(-siblingActiveWindow))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var datname, schema, index string
		if err := rows.Scan(&id, &datname, &schema, &index); err != nil {
			return err
		}
		if sib := byID[id]; sib != nil {
			sib.IndexScanned[indexHashKey(datname, schema+"."+index)] = true
		}
	}
	return rows.Err()
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
	// Two rankings of 500 each: the relations with the most dead tuples, and
	// the relations with the oldest relfrozenxid. Dead tuples alone dropped
	// the append-only tables, which have none, and they are the tables at
	// risk of wraparound.
	rows, err := pool.Query(ctx, `WITH latest AS (
			SELECT DISTINCT ON (datname, schemaname, relname)
				datname, schemaname, relname,
				COALESCE(n_live_tup, 0)::float8 AS n_live_tup, COALESCE(n_dead_tup, 0)::float8 AS n_dead_tup,
				COALESCE(n_mod_since_analyze, 0)::float8 AS n_mod_since_analyze, last_autovacuum, last_vacuum,
				COALESCE(relfrozenxid_age, 0)::float8 AS relfrozenxid_age, COALESCE(seq_scan, 0)::float8 AS seq_scan,
				COALESCE(idx_scan, 0)::float8 AS idx_scan
			FROM metrics_tables
			WHERE instance_id=$1 AND ts >= $2
			ORDER BY datname, schemaname, relname, ts DESC
		), ranked AS (
			SELECT latest.*,
				row_number() OVER (ORDER BY n_dead_tup DESC, datname, schemaname, relname) AS by_dead,
				row_number() OVER (ORDER BY relfrozenxid_age DESC, datname, schemaname, relname) AS by_age
			FROM latest
		)
		SELECT datname, schemaname, relname, n_live_tup, n_dead_tup,
			n_mod_since_analyze, last_autovacuum, last_vacuum, relfrozenxid_age, seq_scan, idx_scan
		FROM ranked
		WHERE by_dead <= 500 OR by_age <= 500
		ORDER BY n_dead_tup DESC, datname, schemaname, relname`, s.InstanceID, now.Add(-recentWindow))
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

// attachIndexDefs gives each index of the window its definition and hash from
// the index_def facts that loadFacts has already read. They used to be read a
// second time, with a query of their own, on every pass.
func attachIndexDefs(s *Snapshot) {
	defs := s.Facts["index_def"]
	for i := range s.Indexes {
		if def, ok := defs[indexHashKey(s.Indexes[i].Datname, s.Indexes[i].Name)]; ok {
			s.Indexes[i].IndexDef = def.ValueText
			s.Indexes[i].DefHash = def.Labels["def_hash"]
			s.Indexes[i].ScanTracked, s.Indexes[i].LastScan, s.Indexes[i].StatsReset = scanLabels(def.Labels)
		}
	}
}

// scanLabels reads the last scan and the statistics reset the agent puts on an
// index_def fact. An absent last_idx_scan label means the agent cannot read
// the column (PostgreSQL 15), so the scan is untracked. An empty label means
// none: no scan since the reset, or no reset known. A label that does not
// parse is treated as untracked rather than as a never-scanned index.
func scanLabels(labels map[string]string) (tracked bool, lastScan, reset *time.Time) {
	last, ok := labels["last_idx_scan"]
	if !ok {
		return false, nil, nil
	}
	lastScan, lastOK := parseScanLabel(last)
	reset, resetOK := parseScanLabel(labels["stats_reset"])
	if !lastOK || !resetOK {
		return false, nil, nil
	}
	return true, lastScan, reset
}

func parseScanLabel(value string) (*time.Time, bool) {
	if value == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, false
	}
	return &parsed, true
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
