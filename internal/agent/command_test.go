package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/pgtype"
)

type dispatcherTarget struct {
	tier         pgtype.PermTier
	pgstattuple  bool
	connForCalls int
	conn         check.Conn
}

func (t *dispatcherTarget) InstanceID() pgtype.InstanceID { return uuid.New() }
func (t *dispatcherTarget) ClusterID() pgtype.ClusterID   { return 1 }
func (t *dispatcherTarget) Role() pgtype.Role             { return pgtype.RolePrimary }
func (t *dispatcherTarget) PGVersion() pgtype.PGVersion   { return 170000 }
func (t *dispatcherTarget) PermTier() pgtype.PermTier     { return t.tier }
func (t *dispatcherTarget) HasExtension(name string) bool {
	return name == "pgstattuple" && t.pgstattuple
}
func (t *dispatcherTarget) Conn(context.Context) (check.Conn, error) {
	if t.conn != nil {
		return t.conn, nil
	}
	return &dispatcherConn{}, nil
}
func (t *dispatcherTarget) ConnFor(context.Context, string) (check.Conn, error) {
	t.connForCalls++
	if t.conn != nil {
		return t.conn, nil
	}
	return &dispatcherConn{}, nil
}
func (t *dispatcherTarget) Database() string   { return "primary" }
func (t *dispatcherTarget) Clock() clock.Clock { return clock.System() }

type dispatcherConn struct{}

func (c *dispatcherConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *dispatcherConn) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
func (c *dispatcherConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (c *dispatcherConn) Release() {}

type dispatcherExecutor struct {
	kind       command.Kind
	executed   int
	deadline   time.Duration
	useConnFor bool
}

func (e *dispatcherExecutor) Kind() command.Kind { return e.kind }
func (e *dispatcherExecutor) Execute(ctx context.Context, target check.Target, args command.Args) (json.RawMessage, error) {
	e.executed++
	if deadline, ok := ctx.Deadline(); ok {
		e.deadline = time.Until(deadline)
	}
	if e.useConnFor {
		conn, err := target.ConnFor(ctx, args.Datname)
		if err != nil {
			return nil, err
		}
		conn.Release()
	}
	return json.RawMessage(`{"ok":true}`), nil
}

// oversizeExecutor returns a result larger than the server accepts.
type oversizeExecutor struct{}

func (oversizeExecutor) Kind() command.Kind { return command.KindExplain }
func (oversizeExecutor) Execute(context.Context, check.Target, command.Args) (json.RawMessage, error) {
	return json.RawMessage(`{"plan":"` + strings.Repeat("x", command.MaxResultBytes) + `","plan_hash":"h"}`), nil
}

func polledFor(instanceID uuid.UUID, kind command.Kind, args command.Args) polledCommand {
	return polledCommand{CommandID: uuid.New(), Kind: kind, Args: args, InstanceID: instanceID, ClaimToken: uuid.New()}
}

func TestDispatcher_UnknownInstanceIsRejected(t *testing.T) {
	agentID := uuid.New()
	commandID := uuid.New()
	claimToken := uuid.New()
	var got commandResult
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Contains(t, r.URL.Path, commandID.String())
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	d := NewDispatcher(server.URL, "token", agentID, true)
	err := d.dispatch(context.Background(), polledCommand{CommandID: commandID, ClaimToken: claimToken, InstanceID: uuid.New(), Kind: command.KindCancel, Args: command.Args{PID: intPtr(42)}})
	require.NoError(t, err)
	require.Equal(t, claimToken, got.ClaimToken)
	require.Equal(t, "rejected", got.Outcome)
	require.Contains(t, got.Error, "does not monitor")
}

// refreshFailingTarget reports a capability refresh that cannot read the
// database, the way a connection that cannot be acquired does.
type refreshFailingTarget struct {
	dispatcherTarget
}

func (t *refreshFailingTarget) RefreshCapabilities(context.Context) error {
	return errors.New("connection refused")
}

// A failed refresh keeps the cached tier, and that tier may be T2 after the
// grant was revoked. The command must not run on it.
func TestDispatcher_DoesNotRunOnAStaleTierWhenRefreshFails(t *testing.T) {
	agentID, instanceID := uuid.New(), uuid.New()
	var got commandResult
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	d := NewDispatcher(server.URL, "token", agentID, true)
	d.AddTarget(instanceID, &refreshFailingTarget{dispatcherTarget: dispatcherTarget{tier: pgtype.TierSignal}}, false, true)
	executor := &dispatcherExecutor{kind: command.KindCancel}
	d.AddExecutor(executor)
	err := d.dispatch(context.Background(), polledFor(instanceID, command.KindCancel, command.Args{PID: intPtr(42)}))
	require.NoError(t, err)
	require.Zero(t, executor.executed)
	require.Equal(t, "error", got.Outcome)
	require.Contains(t, got.Error, "could not be refreshed")
}

func TestDispatcher_ClosedGateRejectsWithoutExecuting(t *testing.T) {
	agentID, instanceID := uuid.New(), uuid.New()
	server := resultCaptureServer(t, agentID, nil)
	defer server.Close()
	d := NewDispatcher(server.URL, "token", agentID, true)
	target := &dispatcherTarget{tier: pgtype.TierReadOnly}
	d.AddTarget(instanceID, target, false, false)
	executor := &dispatcherExecutor{kind: command.KindExplain}
	d.AddExecutor(executor)
	err := d.dispatch(context.Background(), polledFor(instanceID, command.KindExplain, command.Args{QueryID: int64Ptr(7)}))
	require.NoError(t, err)
	require.Zero(t, executor.executed)
}

func TestDispatcher_BackoffOnTransportError(t *testing.T) {
	d := NewDispatcher("http://127.0.0.1:1", "token", uuid.New(), true)
	var delays []time.Duration
	d.sleep = func(context.Context, time.Duration) error {
		delays = append(delays, time.Duration(len(delays)+1)*time.Second)
		if len(delays) == 3 {
			return context.Canceled
		}
		return nil
	}
	d.Run(context.Background())
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}, delays)
}

func TestDispatcher_BackoffResetsOnSuccess(t *testing.T) {
	var mu sync.Mutex
	responses := []int{http.StatusInternalServerError, http.StatusNoContent, http.StatusInternalServerError}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if len(responses) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		code := responses[0]
		responses = responses[1:]
		w.WriteHeader(code)
	}))
	defer server.Close()
	d := NewDispatcher(server.URL, "token", uuid.New(), true)
	var delays []time.Duration
	d.sleep = func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		if len(delays) == 2 {
			return context.Canceled
		}
		return nil
	}
	d.Run(context.Background())
	require.Equal(t, []time.Duration{time.Second, time.Second}, delays)
}

func TestDispatcher_UsesDedicatedConnection(t *testing.T) {
	agentID, instanceID := uuid.New(), uuid.New()
	server := resultCaptureServer(t, agentID, nil)
	defer server.Close()
	d := NewDispatcher(server.URL, "token", agentID, true)
	target := &dispatcherTarget{tier: pgtype.TierExplain}
	d.AddTarget(instanceID, target, false, false)
	executor := &dispatcherExecutor{kind: command.KindExplain, useConnFor: true}
	d.AddExecutor(executor)
	err := d.dispatch(context.Background(), polledFor(instanceID, command.KindExplain, command.Args{QueryID: int64Ptr(7)}))
	require.NoError(t, err)
	require.Equal(t, 1, target.connForCalls)
}

// The server refuses a body over command.MaxResultBytes, which reads as a
// result that never arrived. The agent posts such a result as an error that
// names the size, under the same claim token, so the audit records the cause.
func TestDispatcher_PostsAnOversizeResultAsAnError(t *testing.T) {
	agentID, instanceID, claimToken := uuid.New(), uuid.New(), uuid.New()
	var got commandResult
	var size int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		size = len(body)
		require.NoError(t, json.Unmarshal(body, &got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	d := NewDispatcher(server.URL, "token", agentID, true)
	d.AddTarget(instanceID, &dispatcherTarget{tier: pgtype.TierExplain}, false, false)
	d.AddExecutor(oversizeExecutor{})
	err := d.dispatch(context.Background(), polledCommand{CommandID: uuid.New(), ClaimToken: claimToken, InstanceID: instanceID, Kind: command.KindExplain, Args: command.Args{QueryID: int64Ptr(7)}})
	require.NoError(t, err)
	require.LessOrEqual(t, size, command.MaxResultBytes)
	require.Equal(t, claimToken, got.ClaimToken)
	require.Equal(t, "error", got.Outcome)
	require.Contains(t, got.Error, "byte limit")
	require.Empty(t, got.Result)
}

func TestDispatcher_DisabledDoesNotPoll(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { polls++; w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	d := NewDispatcher(server.URL, "token", uuid.New(), false)
	d.Run(context.Background())
	require.Zero(t, polls)
}

func TestDispatcher_ExecutionDeadlineIs60s(t *testing.T) {
	agentID, instanceID := uuid.New(), uuid.New()
	server := resultCaptureServer(t, agentID, nil)
	defer server.Close()
	d := NewDispatcher(server.URL, "token", agentID, true)
	target := &dispatcherTarget{tier: pgtype.TierExplain}
	d.AddTarget(instanceID, target, false, false)
	executor := &dispatcherExecutor{kind: command.KindExplain}
	d.AddExecutor(executor)
	err := d.dispatch(context.Background(), polledFor(instanceID, command.KindExplain, command.Args{QueryID: int64Ptr(7)}))
	require.NoError(t, err)
	require.InDelta(t, float64(commandExecutionLimit), float64(executor.deadline), float64(250*time.Millisecond))
}

func resultCaptureServer(t *testing.T, agentID uuid.UUID, pollBody any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if pollBody == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(pollBody)
			return
		}
		require.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		require.Equal(t, agentID.String(), r.Header.Get(commandAgentIDHeader))
		w.WriteHeader(http.StatusNoContent)
	}))
}

func int64Ptr(v int64) *int64 { return &v }
func intPtr(v int) *int       { return &v }
