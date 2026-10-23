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
	unseenBefore                               time.Time
	unseenCalls                                int
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
func (s *engineTestStore) ResolveUnseen(_ context.Context, before, _ time.Time) error {
	s.unseenBefore, s.unseenCalls = before, s.unseenCalls+1
	return nil
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

// TestRun_DegradedRuleKeepsItsOpenFindings — a pass in which a rule's input is
// missing says nothing about the findings it reported before. Resolving them
// hid real problems for as long as the input stayed unavailable, and reopened
// them on the next healthy pass.
func TestRun_DegradedRuleKeepsItsOpenFindings(t *testing.T) {
	rule := engineTestRule{id: "vac", needs: []string{"Tables"}, severity: SeverityWarning, scope: ScopeRelation, eval: func(*Snapshot) []Finding {
		return []Finding{{RuleID: "vac", State: StateOpen, Scope: ScopeRelation, Datname: "app", ObjectName: "public.t1", Title: "vacuum"}}
	}}
	e, store, _ := runWithRules(t, rule)
	withTables := true
	e.load = func(_ context.Context, id uuid.UUID, now time.Time) (*Snapshot, error) {
		s := &Snapshot{Now: now, InstanceID: id, PermTier: pgtype.TierReadOnly, Metrics: map[string]map[string]float64{}, Settings: map[string]string{}}
		if withTables {
			s.Tables = []TableStat{}
		} else {
			s.SkippedChecks = map[string]string{"Tables": "table statistics timed out"}
		}
		return s, nil
	}
	ctx := context.Background()

	require.NoError(t, e.Run(ctx))
	require.Len(t, store.findings, 1)
	openID := store.findings[0].ID()
	require.Equal(t, StateOpen, store.findings[0].State)
	require.Contains(t, store.ranRules, "vac")
	require.Contains(t, store.seen, openID)

	withTables = false
	store.findings = nil
	require.NoError(t, e.Run(ctx))
	require.Len(t, store.findings, 1)
	require.Equal(t, StateDegraded, store.findings[0].State)
	degradedID := store.findings[0].ID()
	require.NotContains(t, store.ranRules, "vac", "a degraded rule must not resolve its open findings")

	withTables = true
	store.findings = nil
	require.NoError(t, e.Run(ctx))
	require.Contains(t, store.ranRules, "vac")
	require.Contains(t, store.seen, openID)
	require.NotContains(t, store.seen, degradedID, "the degraded row is resolved once the rule runs again")
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

// TestFindingID_QualifiesEverySubjectByInstance: the findings table is keyed
// by finding ID alone, so the same object reported by two instances must get
// two IDs, and a cluster-scoped finding must not collapse across instances.
func TestFindingID_QualifiesEverySubjectByInstance(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	cluster := int64(42)
	require.Equal(t, "r/"+a.String()+"/object", (Finding{RuleID: "r", ObjectName: "object", InstanceID: &a}).ID())
	require.Equal(t, "r/"+a.String()+"/db/object", (Finding{RuleID: "r", Datname: "db", ObjectName: "object", InstanceID: &a}).ID())
	require.Equal(t, "r/"+a.String()+"/db", (Finding{RuleID: "r", Datname: "db", InstanceID: &a}).ID())
	require.Equal(t, "r/"+a.String(), (Finding{RuleID: "r", ClusterID: &cluster, InstanceID: &a}).ID())
	require.NotEqual(t,
		(Finding{RuleID: "r", ObjectName: "object", InstanceID: &a}).ID(),
		(Finding{RuleID: "r", ObjectName: "object", InstanceID: &b}).ID())
	require.NotEqual(t,
		(Finding{RuleID: "r", ClusterID: &cluster, InstanceID: &a}).ID(),
		(Finding{RuleID: "r", ClusterID: &cluster, InstanceID: &b}).ID())
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

// TestRun_OneFailingInstanceDoesNotStopTheOthers — the pass used to abort at
// the first instance whose snapshot could not be loaded, so a single
// unreachable or mis-permissioned instance stopped advisor findings for every
// instance after it in the (stable) list and skipped the retention purge.
func TestRun_OneFailingInstanceDoesNotStopTheOthers(t *testing.T) {
	rule := engineTestRule{id: "ok", severity: SeverityInfo, scope: ScopeInstance, eval: func(s *Snapshot) []Finding {
		return []Finding{{RuleID: "ok", State: StateOpen, Scope: ScopeInstance, Title: "ok", ObjectName: s.InstanceID.String()}}
	}}
	e, store, first := runWithRules(t, rule)
	second := uuid.New()
	store.ids = []uuid.UUID{first, second}
	now := time.Unix(100, 0)
	e.load = func(_ context.Context, id uuid.UUID, _ time.Time) (*Snapshot, error) {
		if id == first {
			return nil, errors.New("snapshot unavailable")
		}
		return &Snapshot{Now: now, InstanceID: id, PermTier: pgtype.TierReadOnly, Metrics: map[string]map[string]float64{}, Settings: map[string]string{}}, nil
	}

	err := e.Run(context.Background())
	require.ErrorContains(t, err, "load advisor snapshot")
	require.ErrorContains(t, err, first.String())
	require.Len(t, store.findings, 1, "the healthy instance must still be evaluated")
	require.Equal(t, second.String(), store.findings[0].ObjectName)
	require.True(t, store.purged, "the retention purge must still run")
}

// TestRun_SlowInstanceDoesNotStopThePass: an instance that exceeds its step
// budget fails alone. The instances after it are still evaluated and the purge
// still runs. A deadline over the whole pass used to expire on the slow
// instance and stop the pass there, every run.
func TestRun_SlowInstanceDoesNotStopThePass(t *testing.T) {
	rule := engineTestRule{id: "ok", severity: SeverityInfo, scope: ScopeInstance, eval: func(s *Snapshot) []Finding {
		return []Finding{{RuleID: "ok", State: StateOpen, Scope: ScopeInstance, Title: "ok", ObjectName: s.InstanceID.String()}}
	}}
	e, store, slow := runWithRules(t, rule)
	healthy := uuid.New()
	store.ids = []uuid.UUID{slow, healthy}
	e.stepBudget = 50 * time.Millisecond
	now := time.Unix(100, 0)
	e.load = func(ctx context.Context, id uuid.UUID, _ time.Time) (*Snapshot, error) {
		if id == slow {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &Snapshot{Now: now, InstanceID: id, PermTier: pgtype.TierReadOnly, Metrics: map[string]map[string]float64{}, Settings: map[string]string{}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.ErrorContains(t, e.Run(ctx), slow.String())
	require.Len(t, store.findings, 1, "the healthy instance after the slow one must be evaluated")
	require.Equal(t, healthy.String(), store.findings[0].ObjectName)
	require.True(t, store.purged, "the retention purge must still run")
}

// TestRun_StopEndsThePassBetweenInstances: Stop during a pass ends it before the
// next instance, and the purge is left for the next pass. Stop waits for the
// pass in progress, so it must not wait for the whole fleet.
func TestRun_StopEndsThePassBetweenInstances(t *testing.T) {
	rule := engineTestRule{id: "ok", severity: SeverityInfo, scope: ScopeInstance, eval: func(s *Snapshot) []Finding {
		return []Finding{{RuleID: "ok", State: StateOpen, Scope: ScopeInstance, Title: "ok", ObjectName: s.InstanceID.String()}}
	}}
	e, store, _ := runWithRules(t, rule)
	store.ids = []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	now := time.Unix(100, 0)
	e.load = func(_ context.Context, id uuid.UUID, _ time.Time) (*Snapshot, error) {
		e.Stop()
		return &Snapshot{Now: now, InstanceID: id, PermTier: pgtype.TierReadOnly, Metrics: map[string]map[string]float64{}, Settings: map[string]string{}}, nil
	}

	require.NoError(t, e.Run(context.Background()))
	require.Len(t, store.findings, 1, "only the instance in progress when Stop was called is evaluated")
	require.False(t, store.purged)
}

// TestRun_PassTimeIsNotBoundedByOneStep: the instances of a pass may take
// together far longer than one step's budget, since each has its own. Every
// instance is evaluated in the pass that lists it.
func TestRun_PassTimeIsNotBoundedByOneStep(t *testing.T) {
	rule := engineTestRule{id: "ok", severity: SeverityInfo, scope: ScopeInstance, eval: func(s *Snapshot) []Finding {
		return []Finding{{RuleID: "ok", State: StateOpen, Scope: ScopeInstance, Title: "ok", ObjectName: s.InstanceID.String()}}
	}}
	e, store, _ := runWithRules(t, rule)
	store.ids = nil
	for i := 0; i < 10; i++ {
		store.ids = append(store.ids, uuid.New())
	}
	e.stepBudget = 50 * time.Millisecond
	now := time.Unix(100, 0)
	e.load = func(ctx context.Context, id uuid.UUID, _ time.Time) (*Snapshot, error) {
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &Snapshot{Now: now, InstanceID: id, PermTier: pgtype.TierReadOnly, Metrics: map[string]map[string]float64{}, Settings: map[string]string{}}, nil
	}

	require.NoError(t, e.Run(context.Background()))
	require.Len(t, store.findings, 10)
	require.True(t, store.purged)
}

// TestRun_ResolvesFindingsOfInstancesGoneQuiet: a pass sweeps the findings of
// the instances not seen within the grace period, which the per-instance pass
// never reaches.
func TestRun_ResolvesFindingsOfInstancesGoneQuiet(t *testing.T) {
	e, store, _ := runWithRules(t)
	require.NoError(t, e.Run(context.Background()))
	require.Equal(t, 1, store.unseenCalls)
	require.Equal(t, time.Unix(100, 0).Add(-unseenInstanceGrace), store.unseenBefore)
}

// TestRun_UpsertFailureStillResolvesAbsentFindings — aborting on a failed
// write skipped ResolveAbsent, so findings that had gone away were never
// resolved and stayed on the dashboard forever.
func TestRun_UpsertFailureStillResolvesAbsentFindings(t *testing.T) {
	rule := engineTestRule{id: "ok", severity: SeverityInfo, scope: ScopeInstance, eval: func(*Snapshot) []Finding {
		return []Finding{{RuleID: "ok", State: StateOpen, Scope: ScopeInstance, Title: "ok"}}
	}}
	e, store, id := runWithRules(t, rule)
	store.upsertErr = errors.New("upsert failed")

	require.ErrorContains(t, e.Run(context.Background()), "persist finding")
	require.True(t, store.resolved, "ResolveAbsent must still run")
	require.Equal(t, []string{"ok"}, store.ranRules)
	// The finding that failed to write must still count as seen, or the next
	// ResolveAbsent would resolve a condition that is in fact still open.
	require.Equal(t, []string{"ok/" + id.String()}, store.seen)
	require.True(t, store.purged)
}
