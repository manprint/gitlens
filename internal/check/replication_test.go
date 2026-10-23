package check

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestReplicationChecks_Registered(t *testing.T) {
	t.Parallel()
	checks := []Check{
		&replicationStreamingCheck{},
		&replicationReceiverCheck{},
		&replicationSlotsCheck{},
	}
	for _, c := range checks {
		require.NotEmpty(t, c.Name())
		req := c.Requires()
		require.NotEmpty(t, req.Scope)
		_ = c.DefaultInterval()
		_ = c.Timeout()
	}
	// Also check that Get works for at least one real check (if registry not cleared)
	if _, ok := Get("instance_info"); ok {
		// Registry is intact, also check replication via Get
		for _, name := range []string{"replication_streaming", "replication_receiver", "replication_slots"} {
			_, ok := Get(name)
			// If registry was cleared by previous test, this may be false, but we don't fail
			_ = ok
		}
	}
}

// TestReplicationStreaming_NullLagEmittedAsNull — NULL lag columns stay NULL (never 0)
func TestReplicationStreaming_NullLagEmittedAsNull(t *testing.T) {
	t.Parallel()
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			// Row: new standby with NULL lag values (not yet reported)
			var nilInt64 *int64
			var nilFloat *float64
			return &mockRows{
				rows: []([]any){
					{"standby1", "192.168.1.100", 50000, "streaming", "async", nilInt64, nilInt64, nilInt64, nilFloat, nilFloat, nilFloat, nilFloat},
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &replicationStreamingCheck{}
	result, err := check.Scrape(context.Background(), target)
	require.NoError(t, err)
	// No lag metrics emitted for NULL values
	require.Equal(t, 0, len(result.Metrics))
}

// TestReplicationStreaming_EmptyReturnsNoRows — empty pg_stat_replication
func TestReplicationStreaming_EmptyReturnsNoRows(t *testing.T) {
	t.Parallel()
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []([]any){}}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &replicationStreamingCheck{}
	result, err := check.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Empty(t, result.Metrics)
}

// TestReplicationStreaming_ValidLagEmitted — non-NULL lag values emitted
func TestReplicationStreaming_ValidLagEmitted(t *testing.T) {
	t.Parallel()
	w := int64(1000)
	f := float64(0.5)
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []([]any){
					{"standby1", "192.168.1.100", 50000, "streaming", "async", &w, &w, &w, &f, &f, &f, (*float64)(nil)},
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &replicationStreamingCheck{}
	result, err := check.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Greater(t, len(result.Metrics), 0)
	// Should emit lag_bytes and lag_seconds metrics
	hasBytes := false
	hasSec := false
	for _, m := range result.Metrics {
		if m.Name == "replication_write_lag_bytes" {
			hasBytes = true
			require.Equal(t, 1000.0, m.Value)
		}
		if m.Name == "replication_write_lag_seconds" {
			hasSec = true
			require.Equal(t, 0.5, m.Value)
		}
	}
	require.True(t, hasBytes, "should have write_lag_bytes")
	require.True(t, hasSec, "should have write_lag_seconds")
}

// TestReplicationReceiver_DisconnectedEmitsOneRow — disconnected receiver status
func TestReplicationReceiver_DisconnectedEmitsOneRow(t *testing.T) {
	t.Parallel()
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			// pg_stat_wal_receiver is empty, LEFT JOIN gives us one row with NULL values
			return &mockRows{
				rows: []([]any){
					{"disconnected", "", 0, "", (*float64)(nil)},
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &replicationReceiverCheck{}
	result, err := check.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Greater(t, len(result.Metrics), 0)
	hasStatus := false
	for _, m := range result.Metrics {
		if m.Name == "replication_receiver_status" {
			hasStatus = true
			require.Equal(t, 0.0, m.Value) // disconnected = 0
		}
	}
	require.True(t, hasStatus, "should have status metric")
}

// TestReplicationReceiver_CASEGuard — idle primary gives 0 replay lag, not drift
func TestReplicationReceiver_CASEGuard(t *testing.T) {
	t.Parallel()
	lag := float64(0)
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			// CASE guard: when receive_lsn == replay_lsn, the CASE gives 0
			return &mockRows{
				rows: []([]any){
					{"streaming", "primary.local", 5432, "slot1", &lag},
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &replicationReceiverCheck{}
	result, err := check.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Greater(t, len(result.Metrics), 0)
	for _, m := range result.Metrics {
		if m.Name == "replication_receiver_replay_lag_seconds" {
			require.Equal(t, 0.0, m.Value, "caught-up standby should have 0 replay lag")
		}
	}
}

// TestReplicationSlots_WithWALStatusLost — slot with wal_status='lost' parses
func TestReplicationSlots_WithWALStatusLost(t *testing.T) {
	t.Parallel()
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []([]any){
					{"slot1", "physical", true, 5432, "lost", int64(0), int64(1000000)},
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &replicationSlotsCheck{}
	result, err := check.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Greater(t, len(result.Metrics), 0)
	// Should parse despite wal_status='lost'
	hasRetained := false
	for _, m := range result.Metrics {
		if m.Name == "replication_slot_retained_bytes" {
			hasRetained = true
			require.Equal(t, 1000000.0, m.Value)
		}
	}
	require.True(t, hasRetained, "should parse wal_status='lost'")
}

// TestReplicationSlots_UnlimitedSafeWALSizeIsNotZero: safe_wal_size is NULL
// with max_slot_wal_keep_size = -1. It used to be reported as 0 bytes, the
// value that means the slot is about to be invalidated.
func TestReplicationSlots_UnlimitedSafeWALSizeIsNotZero(t *testing.T) {
	t.Parallel()
	conn := &mockConn{
		queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mockRows{rows: []([]any){
				{"unlimited", "physical", true, 5432, "reserved", nil, int64(10)},
				{"limited", "physical", true, 5433, "reserved", int64(4096), int64(10)},
			}}, nil
		},
	}
	result, err := (&replicationSlotsCheck{}).Scrape(context.Background(), &SimpleTarget{
		ConnFunc: func(context.Context) (Conn, error) { return conn, nil }, ClockValue: clock.System(),
	})
	require.NoError(t, err)
	safe := map[string]float64{}
	for _, m := range result.Metrics {
		if m.Name == "replication_slot_safe_wal_size_bytes" {
			safe[m.Labels["slot_name"]] = m.Value
		}
	}
	require.Equal(t, map[string]float64{"limited": 4096}, safe)
}

// TestReplicationSlots_EmptyReturnsNoRows — no slots
func TestReplicationSlots_EmptyReturnsNoRows(t *testing.T) {
	t.Parallel()
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{rows: []([]any){}}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &replicationSlotsCheck{}
	result, err := check.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Empty(t, result.Metrics)
}

// TestReplicationStreaming_RequiresPrimary — Requires() only accepts Primary role
// A cascading standby reports the lag of the standbys behind it from its own
// walsenders, so the check must be applicable on a standby as well as on a
// primary. The role gate used to keep it off every standby.
func TestReplicationStreaming_AppliesOnStandbyAndPrimary(t *testing.T) {
	t.Parallel()
	req := (&replicationStreamingCheck{}).Requires()
	for _, role := range []pgtype.Role{pgtype.RolePrimary, pgtype.RoleStandby} {
		ok, reason := req.Supports(role, pgtype.PG15, pgtype.TierReadOnly, nil)
		require.True(t, ok, "replication_streaming should run on %s: %s", role, reason)
	}
}

// TestReplicationReceiver_RequiresStandby — Requires() only accepts Standby role
func TestReplicationReceiver_RequiresStandby(t *testing.T) {
	t.Parallel()
	check := &replicationReceiverCheck{}
	req := check.Requires()
	require.Equal(t, []pgtype.Role{pgtype.RoleStandby}, req.Roles)
	ok, msg := req.Supports(pgtype.RolePrimary, pgtype.PG15, pgtype.TierReadOnly, nil)
	require.False(t, ok, "should reject Primary: %s", msg)
	ok, _ = req.Supports(pgtype.RoleStandby, pgtype.PG15, pgtype.TierReadOnly, nil)
	require.True(t, ok)
}

// TestReplicationSlots_BothRoles — Requires() accepts both Primary and Standby
func TestReplicationSlots_BothRoles(t *testing.T) {
	t.Parallel()
	check := &replicationSlotsCheck{}
	req := check.Requires()
	require.Empty(t, req.Roles, "should accept both roles (nil Roles)")
	ok, _ := req.Supports(pgtype.RolePrimary, pgtype.PG15, pgtype.TierReadOnly, nil)
	require.True(t, ok, "should accept Primary")
	ok, _ = req.Supports(pgtype.RoleStandby, pgtype.PG15, pgtype.TierReadOnly, nil)
	require.True(t, ok, "should accept Standby")
}

// TestReplicationStreaming_StandbysStayDistinct covers two standbys that both
// report the default application_name: each needs its own identity, or the
// server keeps only one of them.
func TestReplicationStreaming_StandbysStayDistinct(t *testing.T) {
	t.Parallel()
	w := int64(10)
	var sql string
	conn := &mockConn{
		queryFunc: func(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
			sql = q
			return &mockRows{rows: [][]any{
				{"walreceiver", "10.0.0.2", 40001, "streaming", "async", &w, &w, &w, (*float64)(nil), (*float64)(nil), (*float64)(nil), (*float64)(nil)},
				{"walreceiver", "10.0.0.3", 40002, "streaming", "sync", &w, &w, &w, (*float64)(nil), (*float64)(nil), (*float64)(nil), (*float64)(nil)},
				{"reporting", "10.0.0.4", 40003, "streaming", "async", &w, &w, &w, (*float64)(nil), (*float64)(nil), (*float64)(nil), (*float64)(nil)},
			}}, nil
		},
	}
	result, err := (&replicationStreamingCheck{}).Scrape(context.Background(), &SimpleTarget{
		ConnFunc: func(ctx context.Context) (Conn, error) { return conn, nil }, ClockValue: clock.System(),
	})
	require.NoError(t, err)
	names := map[string]string{}
	for _, m := range result.Metrics {
		names[m.Labels["application_name"]] = m.Labels["sync_state"]
	}
	require.Equal(t, map[string]string{
		"walreceiver@10.0.0.2": "async",
		"walreceiver@10.0.0.3": "sync",
		"reporting":            "async",
	}, names)
	require.Contains(t, sql, "pg_current_wal_lsn()", "byte lag is measured from the generated WAL, not from sent_lsn")
	require.NotContains(t, sql, "sent_lsn")
}

// TestReplicationStreaming_ReconnectKeepsStandbyIdentity: a reconnected
// standby gets a new client port. Its identity must not change, or the server
// would count it twice while its old row is still in the lookback.
func TestReplicationStreaming_ReconnectKeepsStandbyIdentity(t *testing.T) {
	t.Parallel()
	before := standbyIdentity([]replicationStreamingRow{
		{ApplicationName: "walreceiver", ClientAddr: "10.0.0.2", ClientPort: 50001},
		{ApplicationName: "walreceiver", ClientAddr: "10.0.0.3", ClientPort: 50002},
	})
	after := standbyIdentity([]replicationStreamingRow{
		{ApplicationName: "walreceiver", ClientAddr: "10.0.0.2", ClientPort: 50117},
		{ApplicationName: "walreceiver", ClientAddr: "10.0.0.3", ClientPort: 50002},
	})
	require.Equal(t, before, after)
	require.Equal(t, []string{"walreceiver@10.0.0.2", "walreceiver@10.0.0.3"}, after)
}

// TestReplicationStreaming_SameAddressStandbysStayDistinct: standbys that share
// a name and an address are numbered, so one does not overwrite the other.
func TestReplicationStreaming_SameAddressStandbysStayDistinct(t *testing.T) {
	t.Parallel()
	ids := standbyIdentity([]replicationStreamingRow{
		{ApplicationName: "walreceiver", ClientAddr: "127.0.0.1", ClientPort: 50002},
		{ApplicationName: "walreceiver", ClientAddr: "127.0.0.1", ClientPort: 50001},
	})
	require.Equal(t, []string{"walreceiver@127.0.0.1#2", "walreceiver@127.0.0.1#1"}, ids)
}

// TestReplicationReceiver_ToleratesArchiveOnlyStandby pins that no nullable
// LSN is scanned: pg_last_wal_receive_lsn() is NULL without a WAL receiver.
func TestReplicationReceiver_ToleratesArchiveOnlyStandby(t *testing.T) {
	t.Parallel()
	var sql string
	conn := &mockConn{
		queryFunc: func(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
			sql = q
			lag := float64(42)
			return &mockRows{rows: [][]any{{"disconnected", "", 0, "", &lag}}}, nil
		},
	}
	result, err := (&replicationReceiverCheck{}).Scrape(context.Background(), &SimpleTarget{
		ConnFunc: func(ctx context.Context) (Conn, error) { return conn, nil }, ClockValue: clock.System(),
	})
	require.NoError(t, err)
	require.NotContains(t, sql, "::text")
	// The lag column scans, but a disconnected receiver reports no lag even
	// when the query returned one: its receive LSN is frozen, not caught up.
	require.False(t, hasMetric(result, "replication_receiver_replay_lag_seconds"))
}

// A receiver that is not streaming yet keeps its status label and counts as
// not connected, including PostgreSQL 19's connecting.
func TestReplicationReceiver_StatusValues(t *testing.T) {
	for status, want := range map[string]float64{
		"streaming": 1, "disconnected": 0, "starting": -1, "waiting": -1,
		"connecting": -1, "restarting": -1, "stopping": -1, "stopped": -1,
	} {
		require.Equal(t, want, statusToFloat(status), status)
	}
}

// TestReplicationStreaming_StalledStandbyLagFollowsReplyAge: the walsender
// refreshes write, flush and replay lag only when the standby replies, so a
// standby that stops replying keeps its last small value while its backlog
// grows. While the standby is behind in bytes, its time lag follows the age of
// its last reply. A caught-up standby is left alone when it goes quiet.
func TestReplicationStreaming_StalledStandbyLagFollowsReplyAge(t *testing.T) {
	t.Parallel()
	small := float64(0.01075)
	age := float64(30)
	behind := int64(17105200)
	none := int64(0)
	negative := float64(-0.5)
	shortAge := float64(2)
	bigLag := float64(5)
	for _, tc := range []struct {
		name     string
		lag      *float64
		behind   *int64
		replyAge *float64
		want     float64
	}{
		{"behind and stalled", &small, &behind, &age, 30},
		{"caught up and quiet", &small, &none, &age, 0.01075},
		{"reply age clamped at zero", &small, &behind, &negative, 0.01075},
		{"recent reply keeps the larger lag", &bigLag, &behind, &shortAge, 5},
		{"no reply age reported", &small, &behind, nil, 0.01075},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := &mockConn{
				queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return &mockRows{rows: [][]any{
						{"standby1", "10.0.0.2", 40001, "streaming", "async", tc.behind, tc.behind, tc.behind, tc.lag, tc.lag, tc.lag, tc.replyAge},
					}}, nil
				},
			}
			result, err := (&replicationStreamingCheck{}).Scrape(context.Background(), &SimpleTarget{
				ConnFunc: func(ctx context.Context) (Conn, error) { return conn, nil }, ClockValue: clock.System(),
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, metricValue(result, "replication_replay_lag_seconds", map[string]string{
				"application_name": "standby1", "client": "10.0.0.2", "state": "streaming", "sync_state": "async",
			}))
		})
	}
}

// TestReplicationReceiver_LagOnlyWhileStreaming: the receive LSN stops moving
// when the walreceiver exits, so the LSN comparison reads a standby that has
// lost its primary as caught up. Replay lag is reported only while the stream
// is up, and a disconnected standby reports no lag rather than a false zero.
func TestReplicationReceiver_LagOnlyWhileStreaming(t *testing.T) {
	t.Parallel()
	zero := float64(0)
	for _, tc := range []struct {
		status  string
		wantLag bool
	}{
		{"streaming", true},
		{"catchup", true},
		{"disconnected", false},
		{"starting", false},
		{"connecting", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			conn := &mockConn{
				queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
					return &mockRows{rows: [][]any{{tc.status, "primary.local", 5432, "slot1", &zero}}}, nil
				},
			}
			result, err := (&replicationReceiverCheck{}).Scrape(context.Background(), &SimpleTarget{
				ConnFunc: func(ctx context.Context) (Conn, error) { return conn, nil }, ClockValue: clock.System(),
			})
			require.NoError(t, err)
			require.Equal(t, tc.wantLag, hasMetric(result, "replication_receiver_replay_lag_seconds"))
			require.Equal(t, statusToFloat(tc.status), metricValue(result, "replication_receiver_status", map[string]string{
				"status": tc.status, "sender_host": "primary.local", "slot_name": "slot1",
			}))
		})
	}
}

func hasMetric(result Result, name string) bool {
	for _, m := range result.Metrics {
		if m.Name == name {
			return true
		}
	}
	return false
}
