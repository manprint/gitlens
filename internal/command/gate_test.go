package command

import (
	"strings"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestAllowed_ExhaustiveGateMatrix(t *testing.T) {
	type bools struct {
		explain, signal bool
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
				{false, false}, {false, true}, {true, false}, {true, true},
			} {
				ok, reason := Allowed(scenario.kind, Args{Analyze: scenario.analyze}, Gates{
					Tier: tier, AllowExplainAnalyze: flags.explain, AllowSignal: flags.signal,
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
	// explain: 12 non-analyze + 6 analyze; cancel and terminate: 4 each;
	// pgstattuple: 12 (its extension is the executor's check).
	require.Equal(t, 38, allowed)
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

// PostgreSQL accepts these names, and a name never reaches SQL text, so they
// are valid targets. The old character allowlist refused them as unsafe.
func TestValidate_AcceptsDatnamesPostgreSQLAccepts(t *testing.T) {
	for _, datname := range []string{"my-app.prod", "my app", "città", "app_1$x", `app"db`, "app;drop"} {
		require.NoError(t, (Args{QueryID: int64Ptr(1), Datname: datname}).Validate(KindExplain), datname)
	}
}

// The length is in bytes, as PostgreSQL counts it: 32 two-byte letters are
// 64 bytes and too long, though they are only 32 characters.
func TestValidate_RejectsDatnameLongerThanPostgreSQLAllows(t *testing.T) {
	require.NoError(t, (Args{QueryID: int64Ptr(1), Datname: strings.Repeat("a", 63)}).Validate(KindExplain))
	err := (Args{QueryID: int64Ptr(1), Datname: strings.Repeat("a", 64)}).Validate(KindExplain)
	require.ErrorContains(t, err, "longer than 63 bytes")
	err = (Args{QueryID: int64Ptr(1), Datname: strings.Repeat("ì", 32)}).Validate(KindExplain)
	require.ErrorContains(t, err, "longer than 63 bytes")
}

// The command store keeps the arguments as jsonb, which cannot hold a NUL.
func TestValidate_RejectsDatnameWithNULByte(t *testing.T) {
	err := (Args{QueryID: int64Ptr(1), Datname: "app\x00db"}).Validate(KindExplain)
	require.ErrorContains(t, err, "NUL byte")
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
		{"tier pgstattuple", KindPgstattuple, Args{}, Gates{Tier: pgtype.TierReadOnly}, "T1"},
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
