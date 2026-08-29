package advisor

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	return s, nil
}
