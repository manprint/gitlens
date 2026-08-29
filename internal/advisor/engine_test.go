package advisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

type engineTestStore struct {
	ids                                        []uuid.UUID
	findings                                   []Finding
	ranRules                                   []string
	seen                                       []string
	resolved                                   bool
	purged                                     bool
	activeErr, upsertErr, resolveErr, purgeErr error
}

func (s *engineTestStore) ActiveInstances(context.Context, time.Time) ([]uuid.UUID, error) {
	return append([]uuid.UUID(nil), s.ids...), s.activeErr
}
func (s *engineTestStore) UpsertFinding(_ context.Context, f Finding, _ time.Time) error {
	s.findings = append(s.findings, f)
	return s.upsertErr
}
func (s *engineTestStore) ResolveAbsent(_ context.Context, _ uuid.UUID, rules, seen []string, _ time.Time) error {
	s.ranRules, s.seen, s.resolved = append([]string(nil), rules...), append([]string(nil), seen...), true
	return s.resolveErr
}
func (s *engineTestStore) PurgeResolved(context.Context, time.Time) error {
	s.purged = true
	return s.purgeErr
}

type engineTestRule struct {
	id       string
	needs    []string
	minTier  pgtype.PermTier
	severity Severity
	scope    Scope
	eval     func(*Snapshot) []Finding
}

func (r engineTestRule) ID() string                     { return r.id }
func (r engineTestRule) Severity() Severity             { return r.severity }
func (r engineTestRule) Scope() Scope                   { return r.scope }
func (r engineTestRule) Needs() []string                { return r.needs }
func (r engineTestRule) MinTier() pgtype.PermTier       { return r.minTier }
func (r engineTestRule) Evaluate(s *Snapshot) []Finding { return r.eval(s) }

func runWithRules(t *testing.T, rules ...Rule) (*Engine, *engineTestStore, uuid.UUID) {
	t.Helper()
	registryMu.Lock()
	old := registry
	registry = make(map[string]Rule, len(rules))
	for _, r := range rules {
		registry[r.ID()] = r
	}
	registryMu.Unlock()
	t.Cleanup(func() { registryMu.Lock(); registry = old; registryMu.Unlock() })
	now := time.Unix(100, 0)
	id := uuid.New()
	store := &engineTestStore{ids: []uuid.UUID{id}}
	e := NewEngineWithStore(nil, clock.NewFake(now), time.Minute, store)
	e.leaderOverride = func(context.Context) (bool, error) { return true, nil }
	e.load = func(context.Context, uuid.UUID, time.Time) (*Snapshot, error) {
		return &Snapshot{Now: now, InstanceID: id, PermTier: pgtype.TierReadOnly, Metrics: map[string]map[string]float64{}, Settings: map[string]string{}}, nil
	}
	return e, store, id
}

func TestRun_FollowerDoesNothing(t *testing.T) {
	e, store, _ := runWithRules(t)
	e.leaderOverride = func(context.Context) (bool, error) { return false, nil }
	require.NoError(t, e.Run(context.Background()))
	require.Empty(t, store.findings)
	require.False(t, store.resolved)
}

func TestRun_DegradesForTierAndMissingInput(t *testing.T) {
	tier := engineTestRule{id: "tier", minTier: pgtype.TierExplain, severity: SeverityWarning, scope: ScopeInstance, eval: func(*Snapshot) []Finding { t.Fatal("must not evaluate"); return nil }}
	missing := engineTestRule{id: "missing", needs: []string{"Tables"}, severity: SeverityInfo, scope: ScopeRelation, eval: func(*Snapshot) []Finding { return nil }}
	e, store, _ := runWithRules(t, tier, missing)
	require.NoError(t, e.Run(context.Background()))
	require.Len(t, store.findings, 2)
	byRule := map[string]Finding{}
	for _, f := range store.findings {
		byRule[f.RuleID] = f
	}
	require.Equal(t, StateDegraded, byRule["tier"].State)
	require.Contains(t, byRule["tier"].DegradedReason, "permission tier")
	require.Contains(t, byRule["missing"].DegradedReason, "Tables")
}

func TestRun_PanicDoesNotStopOrResolvePanickedRule(t *testing.T) {
	panicRule := engineTestRule{id: "panic", severity: SeverityWarning, scope: ScopeInstance, eval: func(*Snapshot) []Finding { panic("boom") }}
	okRule := engineTestRule{id: "ok", severity: SeverityInfo, scope: ScopeInstance, eval: func(*Snapshot) []Finding {
		return []Finding{{RuleID: "ok", State: StateOpen, Scope: ScopeInstance, Title: "ok"}}
	}}
	e, store, _ := runWithRules(t, panicRule, okRule)
	require.NoError(t, e.Run(context.Background()))
	require.Len(t, store.findings, 2)
	require.NotContains(t, store.ranRules, "panic")
	require.Contains(t, store.ranRules, "ok")
}

func TestRun_ResolvesOnlyRulesThatRanForThisInstance(t *testing.T) {
	rule := engineTestRule{id: "ok", severity: SeverityInfo, scope: ScopeInstance, eval: func(*Snapshot) []Finding { return nil }}
	e, store, _ := runWithRules(t, rule)
	require.NoError(t, e.Run(context.Background()))
	require.True(t, store.resolved)
	require.Equal(t, []string{"ok"}, store.ranRules)
	require.Empty(t, store.seen)
	require.True(t, store.purged)
}

func TestRun_LoadErrorStopsPass(t *testing.T) {
	e, store, _ := runWithRules(t)
	e.load = func(context.Context, uuid.UUID, time.Time) (*Snapshot, error) { return nil, errors.New("load failed") }
	require.Error(t, e.Run(context.Background()))
	require.Empty(t, store.findings)
}

func TestRun_PropagatesStoreErrors(t *testing.T) {
	rule := engineTestRule{id: "ok", severity: SeverityInfo, scope: ScopeInstance, eval: func(*Snapshot) []Finding {
		return []Finding{{RuleID: "ok", State: StateOpen, Scope: ScopeInstance, Title: "ok"}}
	}}
	e, store, _ := runWithRules(t, rule)
	store.activeErr = errors.New("active")
	require.ErrorContains(t, e.Run(context.Background()), "active")
	store.activeErr = nil
	store.upsertErr = errors.New("upsert")
	require.ErrorContains(t, e.Run(context.Background()), "upsert")
	store.upsertErr = nil
	store.resolveErr = errors.New("resolve")
	require.ErrorContains(t, e.Run(context.Background()), "resolve")
	store.resolveErr = nil
	store.purgeErr = errors.New("purge")
	require.ErrorContains(t, e.Run(context.Background()), "purge")
}

func TestRun_LeaderErrorAndNilStore(t *testing.T) {
	e, _, _ := runWithRules(t)
	e.leaderOverride = func(context.Context) (bool, error) { return false, errors.New("leader") }
	require.ErrorContains(t, e.Run(context.Background()), "leader")
	require.NoError(t, NewEngine(nil, nil, time.Minute).Run(context.Background()))
	store := &engineTestStore{}
	e = NewEngineWithStore(nil, nil, 0, store)
	require.NoError(t, e.Run(context.Background()))
	require.True(t, store.purged)
}

func TestEngine_StartStopIsIdempotent(t *testing.T) {
	e := NewEngineWithStore(nil, clock.NewFake(time.Unix(0, 0)), time.Hour, nil)
	ctx, cancel := context.WithCancel(context.Background())
	e.Start(ctx)
	e.Start(ctx)
	e.Stop()
	e.Stop()
	cancel()
}

func TestMissingSnapshotInput_AllKinds(t *testing.T) {
	s := &Snapshot{Metrics: map[string]map[string]float64{"pg_ok": {}}, Host: HostInfo{}}
	for _, need := range []string{"Statements", "Indexes", "Tables", "Bloat", "Siblings", "Baseline", "host", "settings", "pg_missing"} {
		require.True(t, missingSnapshotInput(s, need), need)
	}
	require.False(t, missingSnapshotInput(s, "unknown"))
	s.Statements = []StatementStat{}
	s.Indexes, s.Tables, s.Bloat, s.Siblings, s.Baseline = []IndexStat{}, []TableStat{}, []BloatStat{}, []SiblingInfo{}, &Baseline{}
	s.Host.Available = false
	s.Settings = map[string]string{}
	require.True(t, missingSnapshotInput(s, "host"))
	s.Host.Available = true
	require.False(t, missingSnapshotInput(s, "Statements"))
	require.False(t, missingSnapshotInput(s, "Indexes"))
	require.False(t, missingSnapshotInput(s, "Tables"))
	require.False(t, missingSnapshotInput(s, "Bloat"))
	require.False(t, missingSnapshotInput(s, "Siblings"))
	require.False(t, missingSnapshotInput(s, "Baseline"))
	require.False(t, missingSnapshotInput(s, "settings"))
}

func TestAdvisorInterval_DefaultAndConfigured(t *testing.T) {
	t.Setenv("PGLENS_ADVISOR_INTERVAL", "2s")
	e := NewEngine(nil, nil, 0)
	require.Equal(t, 2*time.Second, e.interval)
	t.Setenv("PGLENS_ADVISOR_INTERVAL", "3")
	require.Equal(t, 3*time.Second, advisorInterval())
	t.Setenv("PGLENS_ADVISOR_INTERVAL", "not-duration")
	require.Equal(t, defaultAdvisorInterval, advisorInterval())
}

func TestFindingID_UsesStableSubjectFallbacks(t *testing.T) {
	id := uuid.New()
	cluster := int64(42)
	require.Equal(t, "r/object", (Finding{RuleID: "r", ObjectName: "object"}).ID())
	require.Equal(t, "r/db", (Finding{RuleID: "r", Datname: "db"}).ID())
	require.Equal(t, "r/"+id.String(), (Finding{RuleID: "r", InstanceID: &id}).ID())
	require.Equal(t, "r/cluster:42", (Finding{RuleID: "r", ClusterID: &cluster}).ID())
	cluster = 0
	require.Equal(t, "r/cluster:0", (Finding{RuleID: "r", ClusterID: &cluster}).ID())
	require.Equal(t, "r/", (Finding{RuleID: "r"}).ID())
}

func TestSnapshotMetricLookup(t *testing.T) {
	s := &Snapshot{Metrics: map[string]map[string]float64{"x": {"": 1.5}}}
	v, ok := s.Metric("x", nil)
	require.True(t, ok)
	require.Equal(t, 1.5, v)
	_, ok = s.Metric("missing", nil)
	require.False(t, ok)
	_, ok = s.Metric("x", map[string]string{"label": "value"})
	require.False(t, ok)
}

func TestStoreHelpers(t *testing.T) {
	require.Nil(t, nullIfEmpty(""))
	require.Equal(t, "value", nullIfEmpty("value"))
	require.Equal(t, "*advisor.PgStore", (&PgStore{}).String())
}
