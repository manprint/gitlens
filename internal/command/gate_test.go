package command

import (
	"strings"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestAllowed_ExhaustiveGateMatrix(t *testing.T) {
	type bools struct {
		explain, signal, extension bool
	}
	tiers := []pgtype.PermTier{pgtype.TierReadOnly, pgtype.TierExplain, pgtype.TierSignal, pgtype.TierExtension}
	scenarios := []struct {
		kind    Kind
		analyze bool
	}{
		{KindExplain, false}, {KindExplain, true},
		{KindCancel, false}, {KindTerminate, false}, {KindPgstattuple, false},
	}
	allowed := 0
	for _, scenario := range scenarios {
		for _, tier := range tiers {
			for _, flags := range []bools{
				{false, false, false}, {false, false, true},
				{false, true, false}, {false, true, true},
				{true, false, false}, {true, false, true},
				{true, true, false}, {true, true, true},
			} {
				ok, reason := Allowed(scenario.kind, Args{Analyze: scenario.analyze}, Gates{
					Tier: tier, AllowExplainAnalyze: flags.explain,
					AllowSignal: flags.signal, HasPgstattuple: flags.extension,
				})
				if ok {
					allowed++
				}
				if !ok {
					require.NotEmpty(t, reason, "%s tier %s analyze=%t flags=%+v", scenario.kind, tier, scenario.analyze, flags)
				}
			}
		}
	}
	// explain: 24 non-analyze + 12 analyze; cancel and terminate: 8 each;
	// pgstattuple: 12.
	require.Equal(t, 64, allowed)
}

func TestValidate_ExplainRequiresQueryID(t *testing.T) {
	err := (Args{}).Validate(KindExplain)
	require.ErrorContains(t, err, "queryid")
}

func TestValidate_ExplainRejectsPID(t *testing.T) {
	pid := 42
	err := (Args{QueryID: int64Ptr(1), PID: &pid}).Validate(KindExplain)
	require.ErrorContains(t, err, "pid")
}

func TestValidate_RejectsUnknownKind(t *testing.T) {
	err := (Args{}).Validate(Kind("vacuum"))
	require.ErrorContains(t, err, "unknown command kind")
}

func TestValidate_ExplainRejectsRelationArguments(t *testing.T) {
	err := (Args{QueryID: int64Ptr(1), Schema: "public"}).Validate(KindExplain)
	require.ErrorContains(t, err, "schema and relation")
}

func TestValidate_SignalRequiresPID(t *testing.T) {
	err := (Args{}).Validate(KindCancel)
	require.ErrorContains(t, err, "pid")
}

func TestValidate_SignalRejectsAnalyze(t *testing.T) {
	pid := 42
	err := (Args{PID: &pid, Analyze: true}).Validate(KindTerminate)
	require.ErrorContains(t, err, "analyze")
}

func TestValidate_SignalRejectsQueryID(t *testing.T) {
	err := (Args{PID: intPtr(42), QueryID: int64Ptr(1)}).Validate(KindCancel)
	require.ErrorContains(t, err, "rejects queryid")
}

func TestValidate_SignalRejectsUnrelatedArguments(t *testing.T) {
	err := (Args{PID: intPtr(42), Schema: "public"}).Validate(KindTerminate)
	require.ErrorContains(t, err, "unrelated arguments")
}

func TestValidate_PgstattupleRequiresSchemaAndRelation(t *testing.T) {
	require.Error(t, (Args{Schema: "public"}).Validate(KindPgstattuple))
	require.Error(t, (Args{Relation: "orders"}).Validate(KindPgstattuple))
}

func TestValidate_PgstattupleRejectsUnrelatedArguments(t *testing.T) {
	err := (Args{Schema: "public", Relation: "orders", QueryID: int64Ptr(1)}).Validate(KindPgstattuple)
	require.ErrorContains(t, err, "unrelated arguments")
}

func TestValidate_AcceptsValidArguments(t *testing.T) {
	require.NoError(t, (Args{QueryID: int64Ptr(1), Datname: "app_db", Analyze: true}).Validate(KindExplain))
	require.NoError(t, (Args{PID: intPtr(42)}).Validate(KindCancel))
	require.NoError(t, (Args{Schema: "public", Relation: "orders"}).Validate(KindPgstattuple))
}

func TestValidate_RejectsDatnameWithQuote(t *testing.T) {
	err := (Args{QueryID: int64Ptr(1), Datname: `app"db`}).Validate(KindExplain)
	require.ErrorContains(t, err, "datname")
}

func TestValidate_RejectsDatnameWithSemicolon(t *testing.T) {
	err := (Args{QueryID: int64Ptr(1), Datname: "app;drop"}).Validate(KindExplain)
	require.ErrorContains(t, err, "datname")
}

func TestAllowed_ReasonNamesTheClosedGate(t *testing.T) {
	cases := []struct {
		name  string
		kind  Kind
		args  Args
		gates Gates
		want  string
	}{
		{"tier explain", KindExplain, Args{}, Gates{Tier: pgtype.TierReadOnly}, "T1"},
		{"analyze flag", KindExplain, Args{Analyze: true}, Gates{Tier: pgtype.TierExplain}, "EXPLAIN ANALYZE"},
		{"tier signal", KindCancel, Args{}, Gates{Tier: pgtype.TierExplain}, "T2"},
		{"signal flag", KindTerminate, Args{}, Gates{Tier: pgtype.TierSignal}, "signal"},
		{"extension flag", KindPgstattuple, Args{}, Gates{Tier: pgtype.TierExplain}, "pgstattuple"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := Allowed(tc.kind, tc.args, tc.gates)
			require.False(t, ok)
			require.True(t, strings.Contains(strings.ToLower(reason), strings.ToLower(tc.want)), reason)
		})
	}
}

func TestAllowed_AcceptsOpenGatesAndRejectsUnknownKind(t *testing.T) {
	ok, reason := Allowed(KindExplain, Args{}, Gates{Tier: pgtype.TierExplain})
	require.True(t, ok)
	require.Empty(t, reason)

	ok, reason = Allowed(Kind("vacuum"), Args{}, Gates{Tier: pgtype.TierSignal})
	require.False(t, ok)
	require.Equal(t, "unknown command kind", reason)
}

func int64Ptr(v int64) *int64 { return &v }

func intPtr(v int) *int { return &v }
