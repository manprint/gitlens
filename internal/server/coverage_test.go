package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

// inventoryRecPool is a mockPool that records every Exec.
type inventoryRecPool struct {
	*mockPool
	execs []recordedExec
}

func (p *inventoryRecPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	p.execs = append(p.execs, recordedExec{sql: sql, args: args})
	return p.mockPool.Exec(ctx, sql, args...)
}

// Begin answers a transaction whose statements run on the recording pool.
func (p *inventoryRecPool) Begin(context.Context) (pgx.Tx, error) {
	return &inventoryRecTx{pool: p}, nil
}

type inventoryRecTx struct {
	mockTx
	pool *inventoryRecPool
}

func (t *inventoryRecTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.pool.Exec(ctx, sql, args...)
}
func (t *inventoryRecTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.pool.Query(ctx, sql, args...)
}
func (t *inventoryRecTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.pool.QueryRow(ctx, sql, args...)
}

// instanceWrite answers the arguments of the instance UPDATE or INSERT.
func (p *inventoryRecPool) instanceWrite(t *testing.T, prefix string) []any {
	t.Helper()
	for _, e := range p.execs {
		if strings.HasPrefix(strings.TrimSpace(e.sql), prefix) {
			return e.args
		}
	}
	t.Fatalf("no %q statement among %d", prefix, len(p.execs))
	return nil
}

// upsertCoverageInstance runs the inventory upsert of inst through the UPDATE
// branch (existing) or the INSERT branch (new) and answers the instance
// statement's last two coverage arguments: agent_started_at and
// check_intervals.
func upsertCoverageInstance(t *testing.T, inst wire.Instance, existing bool) (startedAt, intervals any) {
	t.Helper()
	cid := pgtype.ClusterID(4711)
	inst.ClusterID = cid.String()
	inst.ClusterIDSource = "manual"
	pool := &inventoryRecPool{mockPool: &mockPool{}}
	if existing {
		pool.queryRowVals = []*mockRow{{vals: []any{store.ToDB(cid)}}, {vals: []any{"manual"}}}
	}
	res, err := (&Inventory{pool: pool}).Upsert(context.Background(), wire.Envelope{AgentID: uuid.NewString(), Instances: []wire.Instance{inst}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted, res.Errors)
	if existing {
		args := pool.instanceWrite(t, "UPDATE instances SET pg_version")
		// ..., agent_started_at, check_intervals, instance_id
		return args[len(args)-3], args[len(args)-2]
	}
	args := pool.instanceWrite(t, "INSERT INTO instances")
	return args[len(args)-2], args[len(args)-1]
}

// WIRE-COV-002: a payload of an older agent, without the two fields, decodes
// to nil, and the inventory stores NULL in both columns on either branch.
func TestWIRECOV002_AbsentFieldsStoreNull(t *testing.T) {
	t.Parallel()
	var inst wire.Instance
	require.NoError(t, json.Unmarshal([]byte(`{"instance_id":"`+uuid.NewString()+`","cluster_id":"1","role":"primary"}`), &inst))
	require.Nil(t, inst.AgentStartedAt)
	require.Nil(t, inst.CheckIntervals)
	for _, existing := range []bool{true, false} {
		startedAt, intervals := upsertCoverageInstance(t, inst, existing)
		require.Nil(t, startedAt, "existing=%v", existing)
		require.Nil(t, intervals, "existing=%v", existing)
	}
}

// WIRE-COV-003: the inventory writes both fields on the UPDATE branch and on
// the INSERT branch; an entry that names no check or has no positive
// interval is left out of the stored intervals.
func TestWIRECOV003_UpsertPersistsBothBranches(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	inst := wire.Instance{
		InstanceID: uuid.NewString(), Role: "primary",
		AgentStartedAt: &started,
		CheckIntervals: map[string]int{"settings": 3600, "activity": 15, "": 30, "broken": 0},
	}
	for _, existing := range []bool{true, false} {
		startedAt, intervals := upsertCoverageInstance(t, inst, existing)
		require.Equal(t, &started, startedAt, "existing=%v", existing)
		require.JSONEq(t, `{"activity":15,"settings":3600}`, string(intervals.([]byte)), "existing=%v", existing)
	}
}

// stubCoverage is a coverageStore in memory.
type stubCoverage struct {
	records map[uuid.UUID]coverageRecord
	err     error
}

func (s stubCoverage) InstanceCoverage(_ context.Context, id uuid.UUID) (coverageRecord, bool, error) {
	if s.err != nil {
		return coverageRecord{}, false, s.err
	}
	rec, ok := s.records[id]
	return rec, ok, nil
}

func getCoverage(t *testing.T, api *API, id string) coverageJSON {
	t.Helper()
	rec := retentionRequest(t, api, http.MethodGet, "/api/v1/instances/"+id+"/coverage", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out coverageJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func coverageByCheck(out coverageJSON) map[string]coverageCheckJSON {
	byCheck := map[string]coverageCheckJSON{}
	for _, c := range out.Checks {
		byCheck[c.Check] = c
	}
	return byCheck
}

// COV-001: a fake agent started at 10:00 schedules four checks. One reported
// in this run, one failed in this run, one reported only before the restart
// and one never: reported, failed, pending, pending.
func TestCOV001_Classification(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	started := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	api := &API{coverage: stubCoverage{records: map[uuid.UUID]coverageRecord{id: {
		AgentStartedAt: &started,
		CheckIntervals: map[string]int{"activity": 15, "settings": 3600, "locks": 30, "bloat_estimate": 21600},
		Results: map[string]coverageResult{
			"activity": {First: started.Add(5 * time.Second), Last: started.Add(50 * time.Second)},
			"locks":    {First: started.Add(10 * time.Second), Last: started.Add(40 * time.Second), Failed: true},
			"settings": {First: started.Add(-48 * time.Hour), Last: started.Add(-time.Minute)},
			// A check the agent no longer schedules is not listed.
			"retired": {First: started, Last: started.Add(time.Second)},
		},
	}}}}
	out := getCoverage(t, api, id.String())
	require.Equal(t, id.String(), out.InstanceID)
	require.Equal(t, &started, out.AgentStartedAt)
	require.Equal(t, 4, out.Total)
	require.Equal(t, 1, out.Reported)
	require.Equal(t, []string{"activity", "bloat_estimate", "locks", "settings"}, []string{out.Checks[0].Check, out.Checks[1].Check, out.Checks[2].Check, out.Checks[3].Check})
	byCheck := coverageByCheck(out)
	require.Equal(t, coverageReported, byCheck["activity"].State)
	require.Equal(t, started.Add(5*time.Second), *byCheck["activity"].FirstResultAt)
	require.Equal(t, started.Add(50*time.Second), *byCheck["activity"].LastResultAt)
	require.Nil(t, byCheck["activity"].ExpectedBy)
	require.Equal(t, coverageFailed, byCheck["locks"].State)
	require.Nil(t, byCheck["locks"].ExpectedBy)
	for _, pending := range []string{"settings", "bloat_estimate"} {
		require.Equal(t, coveragePending, byCheck[pending].State, pending)
		require.Nil(t, byCheck[pending].FirstResultAt, pending)
		require.Nil(t, byCheck[pending].LastResultAt, pending)
		require.NotNil(t, byCheck[pending].ExpectedBy, pending)
	}
	require.Equal(t, 3600, *byCheck["settings"].IntervalSeconds)
}

// COV-002: a pending check is expected by the agent start, plus its first-run
// bound (the interval, at most its warm-up cap: 30 s, 120 s or 600 s), plus
// 30 s.
func TestCOV002_ExpectedByArithmetic(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	started := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	intervals := map[string]int{"ten_seconds": 10, "one_minute": 60, "five_minutes": 300, "fifteen_minutes": 900, "one_hour": 3600, "six_hours": 21600}
	api := &API{coverage: stubCoverage{records: map[uuid.UUID]coverageRecord{id: {AgentStartedAt: &started, CheckIntervals: intervals}}}}
	byCheck := coverageByCheck(getCoverage(t, api, id.String()))
	for check, wait := range map[string]time.Duration{
		"ten_seconds": 10 * time.Second, "one_minute": 30 * time.Second, "five_minutes": 120 * time.Second,
		"fifteen_minutes": 120 * time.Second, "one_hour": 600 * time.Second, "six_hours": 600 * time.Second,
	} {
		require.Equal(t, started.Add(wait+30*time.Second), *byCheck[check].ExpectedBy, check)
		require.Equal(t, wait, wire.FirstRunBound(time.Duration(intervals[check])*time.Second), check)
	}
	require.Zero(t, wire.FirstRunBound(0))
}

// COV-003: an unknown instance is a 404, a malformed id a 400, and a store
// failure a 500.
func TestCOV003_UnknownInstance(t *testing.T) {
	t.Parallel()
	api := &API{coverage: stubCoverage{records: map[uuid.UUID]coverageRecord{}}}
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/coverage", ""), http.StatusNotFound, "instance not found")
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, "/api/v1/instances/not-a-uuid/coverage", ""), http.StatusBadRequest, "invalid instance_id")
	requireAPIError(t, retentionRequest(t, NewAPI(nil), http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/coverage", ""), http.StatusNotFound, "instance not found")
	failing := &API{coverage: stubCoverage{err: errors.New("connection refused")}}
	requireAPIError(t, retentionRequest(t, failing, http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/coverage", ""), http.StatusInternalServerError, "coverage query failed")

	// The database store answers not found for an instance without a row.
	_, found, err := pgCoverageStore{pool: &mockPool{}}.InstanceCoverage(context.Background(), uuid.New())
	require.NoError(t, err)
	require.False(t, found)
}

// COV-004: an older agent leaves both columns NULL. The database store reads
// them as unknown, and the endpoint lists the stored results with
// agent_started_at and every expected_by null, the state from the results.
func TestCOV004_OldAgentHasNoExpectation(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	pool := &inventoryRecPool{mockPool: &mockPool{
		queryRowVals: []*mockRow{{vals: []any{nil, nil}}},
		queryResults: []*mockRows{{rows: [][]any{
			{"activity", at, at.Add(time.Minute), false},
			{"locks", at, at.Add(time.Minute), true},
		}}},
	}}
	rec, found, err := pgCoverageStore{pool: pool}.InstanceCoverage(context.Background(), id)
	require.NoError(t, err)
	require.True(t, found)
	require.Nil(t, rec.AgentStartedAt)
	require.Nil(t, rec.CheckIntervals)

	api := &API{coverage: stubCoverage{records: map[uuid.UUID]coverageRecord{id: rec}}}
	raw := retentionRequest(t, api, http.MethodGet, "/api/v1/instances/"+id.String()+"/coverage", "")
	require.Equal(t, http.StatusOK, raw.Code)
	require.Contains(t, raw.Body.String(), `"agent_started_at":null`)
	out := getCoverage(t, api, id.String())
	require.Nil(t, out.AgentStartedAt)
	require.Equal(t, 2, out.Total)
	require.Equal(t, 1, out.Reported)
	byCheck := coverageByCheck(out)
	require.Equal(t, coverageReported, byCheck["activity"].State)
	require.Equal(t, coverageFailed, byCheck["locks"].State)
	for _, c := range out.Checks {
		require.Nil(t, c.ExpectedBy, c.Check)
		require.Nil(t, c.IntervalSeconds, c.Check)
	}

	// Without results either, nothing is listed.
	empty := buildCoverage(id, coverageRecord{}, at)
	require.Zero(t, empty.Total)
	require.NotNil(t, empty.Checks)
}

// COV-005: a database-scoped check keeps a row per database. A failure in crm
// stays failed after a later success in app, in the envelope and in the
// store's merge; a database that has not reported in the current run does not
// count, and the merge spans the earliest first and the newest last result.
func TestCOV005_FailureInOneDatabaseStaysFailed(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	start := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	results := map[partialKey]store.CheckResultRow{}
	observeCheckResult(results, id, &start, wire.Result{Check: "table_stats", Database: "crm", TS: start.Add(10 * time.Second), Error: "canceling statement due to statement timeout"})
	observeCheckResult(results, id, &start, wire.Result{Check: "table_stats", Database: "app", TS: start.Add(20 * time.Second)})
	observeCheckResult(results, id, &start, wire.Result{Check: "activity", TS: start.Add(5 * time.Second)})
	rows := sortedCheckResults(results)
	require.Len(t, rows, 3)
	require.Equal(t, []string{"activity/", "table_stats/app", "table_stats/crm"}, []string{rows[0].Check + "/" + rows[0].Datname, rows[1].Check + "/" + rows[1].Datname, rows[2].Check + "/" + rows[2].Datname})
	require.False(t, rows[1].Failed)
	require.True(t, rows[2].Failed, "the later success in app does not hide the failure in crm")

	merged := mergeCheckResults([]coverageResult{
		{First: start.Add(20 * time.Second), Last: start.Add(20 * time.Second)},
		{First: start.Add(10 * time.Second), Last: start.Add(10 * time.Second), Failed: true},
	}, &start)
	require.Equal(t, coverageResult{First: start.Add(10 * time.Second), Last: start.Add(20 * time.Second), Failed: true}, merged)

	// A failure of the previous run in crm does not count once app reported.
	merged = mergeCheckResults([]coverageResult{
		{First: start.Add(20 * time.Second), Last: start.Add(20 * time.Second)},
		{First: start.Add(-time.Hour), Last: start.Add(-time.Minute), Failed: true},
	}, &start)
	require.False(t, merged.Failed)
	require.Equal(t, start.Add(20*time.Second), merged.First)
	// No database reported in the run: the newest row is kept, and is pending.
	merged = mergeCheckResults([]coverageResult{
		{First: start.Add(-2 * time.Hour), Last: start.Add(-time.Hour)},
		{First: start.Add(-time.Hour), Last: start.Add(-time.Minute), Failed: true},
	}, &start)
	require.Equal(t, start.Add(-time.Minute), merged.Last)
	// Without a known start every row counts.
	require.True(t, mergeCheckResults([]coverageResult{{Last: start}, {Last: start.Add(-time.Hour), Failed: true}}, nil).Failed)

	// The store merges the rows it reads, and reads the default tenant only.
	pool := &inventoryRecPool{mockPool: &mockPool{
		queryRowVals: []*mockRow{{vals: []any{&start, []byte(`{"table_stats":300}`)}}},
		queryResults: []*mockRows{{rows: [][]any{
			{"table_stats", start.Add(20 * time.Second), start.Add(20 * time.Second), false},
			{"table_stats", start.Add(10 * time.Second), start.Add(10 * time.Second), true},
		}}},
	}}
	rec, found, err := pgCoverageStore{pool: pool}.InstanceCoverage(context.Background(), id)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, coverageResult{First: start.Add(10 * time.Second), Last: start.Add(20 * time.Second), Failed: true}, rec.Results["table_stats"])
	require.Equal(t, coverageFailed, coverageByCheck(buildCoverage(id, rec, start.Add(time.Minute)))["table_stats"].State)
	require.Contains(t, coverageInstanceSQL, "tenant_id = 'default'")
}

// COV-006: two results of one check in one envelope keep the first as
// first_result_at and the newest as last_result_at, whatever their order; of
// two results at the same time a failure wins.
func TestCOV006_EnvelopeKeepsFirstAndNewest(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	results := map[partialKey]store.CheckResultRow{}
	observeCheckResult(results, id, nil, wire.Result{Check: "activity", TS: at.Add(15 * time.Second), Error: "timeout"})
	observeCheckResult(results, id, nil, wire.Result{Check: "activity", TS: at})
	rows := sortedCheckResults(results)
	require.Len(t, rows, 1)
	require.Equal(t, at, rows[0].FirstTS)
	require.Equal(t, at.Add(15*time.Second), rows[0].LastTS)
	require.True(t, rows[0].Failed, "the newest result failed")

	observeCheckResult(results, id, nil, wire.Result{Check: "activity", TS: at.Add(30 * time.Second)})
	observeCheckResult(results, id, nil, wire.Result{Check: "activity", TS: at.Add(30 * time.Second), Error: "timeout"})
	observeCheckResult(results, id, nil, wire.Result{Check: "activity", TS: at.Add(30 * time.Second)})
	row := sortedCheckResults(results)[0]
	require.Equal(t, at, row.FirstTS)
	require.Equal(t, at.Add(30*time.Second), row.LastTS)
	require.True(t, row.Failed, "a failure wins a tie")
}
