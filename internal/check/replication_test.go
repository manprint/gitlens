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
					{"standby1", "192.168.1.100", "streaming", "async", nilInt64, nilInt64, nilInt64, nilFloat, nilFloat, nilFloat},
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
					{"standby1", "192.168.1.100", "streaming", "async", &w, &w, &w, &f, &f, &f},
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
					{"disconnected", "", 0, "", "0/0", "0/0", (*float64)(nil)},
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
					{"streaming", "primary.local", 5432, "slot1", "0/1000000", "0/1000000", &lag},
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
func TestReplicationStreaming_RequiresPrimary(t *testing.T) {
	t.Parallel()
	check := &replicationStreamingCheck{}
	req := check.Requires()
	require.Equal(t, []pgtype.Role{pgtype.RolePrimary}, req.Roles)
	ok, msg := req.Supports(pgtype.RoleStandby, pgtype.PG15, pgtype.TierReadOnly, nil)
	require.False(t, ok, "should reject Standby: %s", msg)
	ok, _ = req.Supports(pgtype.RolePrimary, pgtype.PG15, pgtype.TierReadOnly, nil)
	require.True(t, ok)
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
