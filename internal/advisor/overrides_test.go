package advisor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/require"
)

// overridesTestStore adds the disabled-rule resolution to the engine test store.
type overridesTestStore struct {
	*engineTestStore
	disabledCalls [][]string
	disabledErr   error
}

func (s *overridesTestStore) ResolveDisabled(_ context.Context, ruleIDs []string, _ time.Time) error {
	s.disabledCalls = append(s.disabledCalls, append([]string(nil), ruleIDs...))
	return s.disabledErr
}

// runWithOverrides is runWithRules with a store that resolves disabled rules
// and an overrides source answering *overrides (or err).
func runWithOverrides(t *testing.T, overrides *map[string]bool, err *error, rules ...Rule) (*Engine, *overridesTestStore) {
	t.Helper()
	e, base, _ := runWithRules(t, rules...)
	st := &overridesTestStore{engineTestStore: base}
	e.store = st
	e.SetOverridesSource(func(context.Context) (map[string]bool, error) {
		if err != nil && *err != nil {
			return nil, *err
		}
		return *overrides, nil
	})
	return e, st
}

func countingRule(id string, calls *atomic.Int32) engineTestRule {
	return engineTestRule{id: id, severity: SeverityWarning, scope: ScopeInstance, eval: func(*Snapshot) []Finding {
		calls.Add(1)
		return []Finding{{RuleID: id, State: StateOpen, Title: id, Detail: id, ObjectName: "obj"}}
	}}
}

// ADVO-002: a disabled rule is not evaluated and does not count as having run,
// so ResolveAbsent leaves its findings to the disabled-rule resolution; the
// other rules run as before.
func TestADVO002_DisabledRuleSkipped(t *testing.T) {
	var offCalls, onCalls atomic.Int32
	overrides := map[string]bool{"rule.off": false, "rule.on": true}
	e, st := runWithOverrides(t, &overrides, nil, countingRule("rule.off", &offCalls), countingRule("rule.on", &onCalls))

	require.NoError(t, e.Run(context.Background()))
	require.Zero(t, offCalls.Load(), "a disabled rule must not be evaluated")
	require.Equal(t, int32(1), onCalls.Load())
	require.Equal(t, []string{"rule.on"}, st.ranRules, "a disabled rule must not count as having run")
	for _, f := range st.findings {
		require.NotEqual(t, "rule.off", f.RuleID)
	}
}

// ADVO-002: without a source (nil) no rule is disabled and no resolution runs.
func TestADVO002_NilSourceDisablesNothing(t *testing.T) {
	var calls atomic.Int32
	e, base, _ := runWithRules(t, countingRule("rule.a", &calls))
	st := &overridesTestStore{engineTestStore: base}
	e.store = st

	require.NoError(t, e.Run(context.Background()))
	require.Equal(t, int32(1), calls.Load())
	require.Empty(t, st.disabledCalls)
}

// ADVO-003: the findings of a disabled rule are resolved at the next
// evaluation, once per pass, through the store's ResolveDisabled.
func TestADVO003_DisabledRuleFindingsResolved(t *testing.T) {
	var calls atomic.Int32
	overrides := map[string]bool{"rule.b": false, "rule.a": false}
	e, st := runWithOverrides(t, &overrides, nil, countingRule("rule.a", &calls), countingRule("rule.b", &calls))

	require.NoError(t, e.Run(context.Background()))
	require.Equal(t, [][]string{{"rule.a", "rule.b"}}, st.disabledCalls)
	require.True(t, st.resolved, "the pass goes on to the other rules")

	st.disabledErr = errors.New("resolve failed")
	err := e.Run(context.Background())
	require.ErrorContains(t, err, "resolve failed")
	require.True(t, st.purged, "a failed resolution must not skip the rest of the pass")
}

// ADVO-003: the SQL closes open, degraded and muted findings of the rules and
// adds resolved_reason rule_disabled to their evidence (R-11.0 A1).
func TestADVO003_PgStoreResolveDisabledSQL(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	store := &PgStore{db: mock}
	now := time.Unix(100, 0).UTC()

	require.Contains(t, resolveDisabledSQL, `state IN ('open','degraded','muted')`)
	require.Contains(t, resolveDisabledSQL, `|| '{"resolved_reason":"rule_disabled"}'::jsonb`)
	require.Contains(t, resolveDisabledSQL, `rule_id = ANY($2)`)
	mock.ExpectExec("UPDATE findings SET state='resolved'").WithArgs(now, []string{"index.unused"}).
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))
	require.NoError(t, store.ResolveDisabled(context.Background(), []string{"index.unused"}, now))
	require.NoError(t, store.ResolveDisabled(context.Background(), nil, now), "no rule: no statement")
	require.NoError(t, mock.ExpectationsWereMet())
}

// ADVO-004: enabling the rule again lets the next evaluation run it, so its
// findings are upserted (reopened) and no longer resolved as disabled.
func TestADVO004_ReenabledRuleReopens(t *testing.T) {
	var calls atomic.Int32
	overrides := map[string]bool{"rule.a": false}
	e, st := runWithOverrides(t, &overrides, nil, countingRule("rule.a", &calls))

	require.NoError(t, e.Run(context.Background()))
	require.Zero(t, calls.Load())
	require.Len(t, st.disabledCalls, 1)

	overrides = map[string]bool{}
	st.findings = nil
	require.NoError(t, e.Run(context.Background()))
	require.Equal(t, int32(1), calls.Load())
	require.Len(t, st.findings, 1)
	require.Equal(t, "rule.a", st.findings[0].RuleID)
	require.Len(t, st.disabledCalls, 1, "an enabled rule is not resolved as disabled")
	require.Equal(t, []string{"rule.a"}, st.ranRules)
}

// ADVO-004: a failed read of the overrides keeps the previous answer, so a
// passing database error neither re-enables a disabled rule nor disables one.
func TestADVO004_SourceErrorKeepsPreviousSnapshot(t *testing.T) {
	var calls atomic.Int32
	overrides := map[string]bool{"rule.a": false}
	var readErr error
	e, st := runWithOverrides(t, &overrides, &readErr, countingRule("rule.a", &calls))

	require.NoError(t, e.Run(context.Background()))
	readErr = errors.New("overrides unavailable")
	require.NoError(t, e.Run(context.Background()), "an unreadable source does not fail the pass")
	require.Zero(t, calls.Load(), "the rule stays disabled")
	require.Len(t, st.disabledCalls, 2)

	// Before any successful read an error disables nothing: rule.a, disabled
	// by the stored overrides, still runs on a fresh engine.
	var fresh atomic.Int32
	e2, st2 := runWithOverrides(t, &overrides, &readErr, countingRule("rule.a", &fresh))
	require.NoError(t, e2.Run(context.Background()))
	require.Equal(t, int32(1), fresh.Load(), "the error path disables no rule")
	require.Empty(t, st2.disabledCalls)
}

// ADVO-003: an override of a rule that is no longer registered never reaches
// ResolveDisabled, so the findings such a rule left are not resolved as
// disabled; the registered disabled rule still is.
func TestADVO003_StaleOverrideIgnored(t *testing.T) {
	var calls atomic.Int32
	overrides := map[string]bool{"rule.gone": false, "rule.a": false}
	e, st := runWithOverrides(t, &overrides, nil, countingRule("rule.a", &calls))

	require.NoError(t, e.Run(context.Background()))
	require.Zero(t, calls.Load())
	require.Equal(t, [][]string{{"rule.a"}}, st.disabledCalls)
	for _, ids := range st.disabledCalls {
		require.NotContains(t, ids, "rule.gone")
	}

	// Only stale overrides: no resolution at all.
	overrides = map[string]bool{"rule.gone": false}
	require.NoError(t, e.Run(context.Background()))
	require.Len(t, st.disabledCalls, 1, "a stale id alone never reaches ResolveDisabled")
	require.Equal(t, int32(1), calls.Load())
}
