package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/ash"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/watch"
	"github.com/manprint/pglens/internal/wire"
)

// valueRows is a cursor over fixed rows, scanned by destination type.
type valueRows struct {
	rows [][]any
	idx  int
}

func (r *valueRows) Close()                                       {}
func (r *valueRows) Err() error                                   { return nil }
func (r *valueRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *valueRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *valueRows) Values() ([]any, error)                       { return nil, nil }
func (r *valueRows) RawValues() [][]byte                          { return nil }
func (r *valueRows) Conn() *pgx.Conn                              { return nil }
func (r *valueRows) TypeMap() *pgtype.Map                         { return pgtype.NewMap() }
func (r *valueRows) Next() bool                                   { return r.idx < len(r.rows) }

func (r *valueRows) Scan(dest ...any) error {
	row := r.rows[r.idx]
	r.idx++
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = row[i].(string)
		case **int64:
			*p, _ = row[i].(*int64)
		case *[]byte:
			*p = row[i].([]byte)
		}
	}
	return nil
}

type funcQuerier func(sql string) ([][]any, error)

func (f funcQuerier) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	rows, err := f(sql)
	if err != nil {
		return nil, err
	}
	return &valueRows{rows: rows}, nil
}

func TestLockWaiting(t *testing.T) {
	require.False(t, lockWaiting(nil))
	require.False(t, lockWaiting([]ash.SampleRow{{WaitEventType: "IO"}, {}}))
	require.True(t, lockWaiting([]ash.SampleRow{{WaitEventType: "IO"}, {WaitEventType: "Lock"}}))
}

func TestIncidentResult(t *testing.T) {
	_, ok := incidentResult(nil, time.Now())
	require.False(t, ok)

	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	res, ok := incidentResult([]watch.Incident{
		{ID: "deadlock-1-10", Type: watch.TypeDeadlock, Relation: "public.orders", CTID: "(0,1)"},
		{ID: "long_query-2-20", Type: watch.TypeLongQuery},
	}, now)
	require.True(t, ok)
	require.Equal(t, "ash", res.Check)
	require.Equal(t, now, res.TS)
	require.Len(t, res.Facts, 2)
	f := res.Facts[0]
	require.Equal(t, watch.FactKind, f.Kind)
	require.Equal(t, "deadlock-1-10", f.Key)
	require.Equal(t, map[string]string{"type": watch.TypeDeadlock}, f.Labels)
	require.NoError(t, f.Validate())
	var back watch.Incident
	require.NoError(t, json.Unmarshal(f.ValueJSON, &back))
	require.Equal(t, "(0,1)", back.CTID)
}

func TestStartWatchDisabled(t *testing.T) {
	off := false
	require.False(t, startWatch(context.Background(), nil, nil, nil, "t", agent.CheckConfig{Enabled: &off}, clock.System(), &sync.Mutex{}, map[string][]wire.Result{}))
}

// The watcher runs on the sampler's ticks: a tick that samples a lock wait
// makes it read pg_locks, and what it captures is queued for the next push.
func TestStartWatchQueuesIncidentFromSamplerTick(t *testing.T) {
	clk := clock.NewFake(time.Now())
	sampler := ash.NewSampler(funcQuerier(func(string) ([][]any, error) {
		return [][]any{{"app", "active", "Lock", "transactionid", (*int64)(nil)}}, nil
	}), clk, nil)

	lockJSON, err := json.Marshal([]watch.Session{
		{PID: 1, Datname: "app", State: "idle in transaction", XactSeconds: 30},
		{PID: 2, Datname: "app", State: "active", BlockedBy: []int32{1},
			Wait: &watch.LockRef{LockType: "relation", Mode: "AccessExclusiveLock", Relation: "public.orders", WaitSeconds: 2}},
	})
	require.NoError(t, err)
	var failing atomic.Bool
	probes := funcQuerier(func(sql string) ([][]any, error) {
		if failing.Load() {
			return nil, errors.New("probe failed")
		}
		if strings.Contains(sql, "pg_blocking_pids") {
			return [][]any{{lockJSON}}, nil
		}
		return [][]any{{[]byte("[]")}}, nil
	})

	var mu sync.Mutex
	pending := map[string][]wire.Result{}
	require.True(t, startWatch(context.Background(), sampler, probes, nil, "db1", agent.CheckConfig{}, clk, &mu, pending))
	require.NoError(t, sampler.Start(context.Background()))
	defer sampler.Stop()

	queued := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(pending["db1"])
	}
	require.Eventually(t, func() bool {
		clk.Advance(time.Second)
		return queued() > 0
	}, 3*time.Second, 5*time.Millisecond)

	mu.Lock()
	res := pending["db1"][0]
	mu.Unlock()
	require.Equal(t, "ash", res.Check)
	require.Equal(t, watch.TypeLockWait, res.Facts[0].Labels["type"])
	require.Contains(t, string(res.Facts[0].ValueJSON), `"relation":"public.orders"`)

	// A failing probe, then a working one, are each logged once and do not stop the sampler.
	failing.Store(true)
	before := sampler.TicksMissed()
	for i := 0; i < 3; i++ {
		clk.Advance(time.Second)
		time.Sleep(5 * time.Millisecond)
	}
	failing.Store(false)
	for i := 0; i < 3; i++ {
		clk.Advance(time.Second)
		time.Sleep(5 * time.Millisecond)
	}
	require.Equal(t, before, sampler.TicksMissed(), "a watch failure is not an ASH tick failure")
}
