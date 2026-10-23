package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/identity"
	"github.com/manprint/pglens/internal/pgtype"
)

func TestManager_QueryCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		readAll  bool
		signal   bool
		exts     []string
		failAt   string // the probe that fails: "read", "signal" or "ext"
		wantTier pgtype.PermTier
	}{
		{name: "T0 without the optional roles", exts: []string{}, wantTier: pgtype.TierReadOnly},
		{name: "T1 with extension", readAll: true, exts: []string{"pg_stat_statements"}, wantTier: pgtype.TierExplain},
		{name: "T2 with extension", readAll: true, signal: true, exts: []string{"pgstattuple", "pg_stat_statements"}, wantTier: pgtype.TierSignal},
		{name: "a failed role probe is an error", failAt: "read"},
		{name: "a failed signal probe is an error", failAt: "signal"},
		{name: "a failed extension query is an error", failAt: "ext"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewConn()
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, mock.Close(context.Background()))
				require.NoError(t, mock.ExpectationsWereMet())
			})

			// A probe that fails ends the read there, so the probes after it
			// are not expected at all.
			switch tt.failAt {
			case "read":
				mock.ExpectQuery("pg_read_all_data").WillReturnError(errors.New("capability probe failed"))
			case "signal":
				mock.ExpectQuery("pg_read_all_data").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(tt.readAll))
				mock.ExpectQuery("pg_signal_backend").WillReturnError(errors.New("capability probe failed"))
			case "ext":
				mock.ExpectQuery("pg_read_all_data").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(tt.readAll))
				mock.ExpectQuery("pg_signal_backend").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(tt.signal))
				mock.ExpectQuery("SELECT extname").WillReturnError(errors.New("extension probe failed"))
			default:
				mock.ExpectQuery("pg_read_all_data").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(tt.readAll))
				mock.ExpectQuery("pg_signal_backend").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(tt.signal))
				rows := pgxmock.NewRows([]string{"extname"})
				for _, ext := range tt.exts {
					rows.AddRow(ext)
				}
				mock.ExpectQuery("SELECT extname").WillReturnRows(rows)
			}
			mock.ExpectClose()

			m := &Manager{}
			gotTier, gotExts, err := m.queryCapabilities(context.Background(), mock)
			if tt.failAt != "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantTier, gotTier)
			for _, ext := range tt.exts {
				require.True(t, gotExts[ext])
			}
		})
	}
}

// TestQueryClusterIdentity pins the three outcomes of the identity read: the
// system_identifier, the manual id for a denied read, and an error for any
// other failure. A transient failure must never turn into a manual id, since
// the manual id is then kept for the life of the process.
func TestQueryClusterIdentity(t *testing.T) {
	const systemID uint64 = 7381927364512345678
	tests := []struct {
		name       string
		err        error
		wantID     pgtype.ClusterID
		wantSource pgtype.IDSource
		wantErr    bool
	}{
		{name: "system identifier", wantID: pgtype.ClusterID(systemID), wantSource: pgtype.IDSourceSystemIdentifier},
		{name: "permission denied falls back to the manual id", err: &pgconn.PgError{Code: "42501"}, wantID: pgtype.ManualClusterID("shared-cluster"), wantSource: pgtype.IDSourceManual},
		{name: "connection reset is returned, not cached", err: errors.New("connection reset by peer"), wantErr: true},
		{name: "statement timeout is returned, not cached", err: &pgconn.PgError{Code: "57014"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewConn()
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, mock.Close(context.Background()))
				require.NoError(t, mock.ExpectationsWereMet())
			})

			expectation := mock.ExpectQuery("pg_control_system")
			if tt.err != nil {
				expectation.WillReturnError(tt.err)
			} else {
				expectation.WillReturnRows(pgxmock.NewRows([]string{"system_identifier"}).AddRow(systemID))
			}
			mock.ExpectClose()

			gotID, gotSource, err := queryClusterIdentity(context.Background(), mock, "shared-cluster")
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, pgtype.ClusterID(0), gotID)
				require.Empty(t, gotSource)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantID, gotID)
			require.Equal(t, tt.wantSource, gotSource)
		})
	}
}

// TestManager_RefreshCapabilities_KeepsTheSnapshotWhenAQueryFails: a transient
// error in a refresh must leave the tier and extensions as they were. The
// schedule and the command gate were built from them, and a read-only fallback
// would rebuild the one and refuse the other.
func TestManager_RefreshCapabilities_KeepsTheSnapshotWhenAQueryFails(t *testing.T) {
	mock, err := pgxmock.NewConn()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.Close(context.Background()))
		require.NoError(t, mock.ExpectationsWereMet())
	})
	mock.ExpectQuery("pg_read_all_data").WillReturnError(errors.New("connection reset"))
	mock.ExpectClose()

	m := &Manager{target: &poolHolder{}}
	m.target.cache.initialized = true
	m.target.cache.permTier = pgtype.TierSignal
	m.target.cache.extensions = map[string]bool{"pg_stat_statements": true}

	require.Error(t, m.refreshCapabilities(context.Background(), mock))
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	require.Equal(t, pgtype.TierSignal, m.target.cache.permTier)
	require.True(t, m.target.cache.extensions["pg_stat_statements"])
}

func TestManager_RefreshCapabilities_ConnectionError(t *testing.T) {
	m, err := NewManager(context.Background(), "refresh-capabilities", "postgres://user:pass@127.0.0.1:1/nonexistent?connect_timeout=1", DefaultConnOptions(), nil)
	require.NoError(t, err)
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	require.Error(t, m.RefreshCapabilities(ctx))
	cancel()

	m.target.cache.initialized = true
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	require.Error(t, m.RefreshCapabilities(ctx))
	cancel()
}

func TestManager_AddrPort_ParsedFromDSN(t *testing.T) {
	m := &Manager{target: &poolHolder{dsn: "postgres://user:pass@dbhost:6543/postgres?sslmode=disable"}}
	if got := m.Addr(); got != "dbhost" {
		t.Errorf("Addr() = %q, want %q", got, "dbhost")
	}
	if got := m.Port(); got != 6543 {
		t.Errorf("Port() = %d, want 6543", got)
	}
}

func TestManager_AddrPort_InvalidDSN(t *testing.T) {
	m := &Manager{target: &poolHolder{dsn: "not a dsn"}}
	if got := m.Addr(); got != "" {
		t.Errorf("Addr() = %q, want empty string on parse error", got)
	}
	if got := m.Port(); got != 0 {
		t.Errorf("Port() = %d, want 0 on parse error", got)
	}
}

// TestManager_AgentID_DoesNotBlockOnUnreachableTarget: the agent id comes
// from the local identity store, so an unreachable database neither delays
// it nor, since the identity stopped depending on the target, zeroes it
// (see TestManager_IdentityDoesNotNeedTheTarget).
func TestManager_AgentID_DoesNotBlockOnUnreachableTarget(t *testing.T) {
	t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(t.TempDir(), "identity.json"))
	ctx := context.Background()
	mgr, err := NewManager(ctx, "unreachable-target", "postgres://user:pass@127.0.0.1:1/nonexistent?connect_timeout=1", DefaultConnOptions(), nil)
	require.NoError(t, err)
	defer mgr.Close()

	done := make(chan uuid.UUID, 1)
	go func() { done <- mgr.AgentID() }()
	select {
	case id := <-done:
		require.NotEqual(t, uuid.Nil, id)
	case <-time.After(15 * time.Second):
		t.Fatal("AgentID() did not return within 15s against an unreachable database")
	}
}

func TestSelection_ExcludeDefaults(t *testing.T) {
	dbs := []dbRow{
		{"postgres", 1000},
		{"template0", 0},
		{"template1", 0},
		{"template2", 0},
		{"rdsadmin", 100},
		{"azure_sys", 50},
		{"app", 2000},
	}
	opts := DefaultConnOptions()
	result := selectDatabases(dbs, opts)

	monitored := map[string]bool{}
	for _, db := range result {
		monitored[db.Name] = db.Monitored
	}

	if monitored["template0"] || monitored["template1"] || monitored["template2"] || monitored["rdsadmin"] || monitored["azure_sys"] {
		t.Error("expected templates/rdsadmin/azure_* to be excluded")
	}
	if !monitored["postgres"] || !monitored["app"] {
		t.Error("expected postgres and app to be monitored")
	}
}

func TestSelection_IncludeFilters(t *testing.T) {
	dbs := []dbRow{
		{"app_prod", 1000},
		{"app_dev", 500},
		{"db_other", 800},
		{"test_app", 200},
	}
	opts := DefaultConnOptions()
	opts.Include = []*regexp.Regexp{regexp.MustCompile(`^app_`)}
	result := selectDatabases(dbs, opts)

	if len(result) != 2 {
		t.Errorf("expected 2 databases, got %d", len(result))
	}
	for _, db := range result {
		if !regexp.MustCompile(`^app_`).MatchString(db.Name) {
			t.Errorf("expected only app_* databases, got %s", db.Name)
		}
	}
}

func TestSelection_BudgetKeepsMostActive(t *testing.T) {
	dbs := []dbRow{
		{"db01", 100},
		{"db02", 200},
		{"db03", 150},
		{"db04", 300},
		{"db05", 50},
		{"db06", 250},
		{"db07", 175},
		{"db08", 75},
		{"db09", 125},
		{"db10", 225},
		{"db11", 160},
		{"db12", 110},
		{"db13", 80},
		{"db14", 190},
		{"db15", 220},
	}
	opts := DefaultConnOptions()
	opts.MaxDatabases = 10
	result := selectDatabases(dbs, opts)

	monitored := 0
	unmonitored := 0
	for _, db := range result {
		if db.Monitored {
			monitored++
		} else {
			if db.SkipReason != "db_budget" {
				t.Errorf("expected skip_reason=db_budget for %s, got %s", db.Name, db.SkipReason)
			}
			unmonitored++
		}
	}
	if monitored != 10 || unmonitored != 5 {
		t.Errorf("expected 10 monitored and 5 unmonitored, got %d and %d", monitored, unmonitored)
	}
}

func TestSelection_Deterministic(t *testing.T) {
	dbs := []dbRow{
		{"db_a", 100},
		{"db_b", 100},
		{"db_c", 100},
	}
	opts := DefaultConnOptions()
	opts.Exclude = nil
	opts.Include = nil

	result1 := selectDatabases(dbs, opts)
	result2 := selectDatabases(dbs, opts)

	order1 := []string{}
	order2 := []string{}
	for _, db := range result1 {
		order1 = append(order1, db.Name)
	}
	for _, db := range result2 {
		order2 = append(order2, db.Name)
	}

	if len(order1) != len(order2) {
		t.Error("orders have different lengths")
	}
	for i := range order1 {
		if order1[i] != order2[i] {
			t.Errorf("non-deterministic: run 1 had %v, run 2 had %v", order1, order2)
		}
	}
}

// TestManager_IdentityDoesNotNeedTheTarget covers an agent starting while its
// targets are down: both identities come from the local identity file, and
// two targets initialising at once must both keep their instance id.
func TestManager_IdentityDoesNotNeedTheTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	t.Setenv("PGLENS_IDENTITY_PATH", path)
	ctx := context.Background()
	var managers []*Manager
	for i, dsn := range []string{
		"postgres://pglens@127.0.0.1:1/postgres?connect_timeout=1",
		"postgres://pglens@127.0.0.1:2/postgres?connect_timeout=1",
	} {
		m, err := NewManager(ctx, fmt.Sprintf("down-%d", i), dsn, DefaultConnOptions(), nil)
		require.NoError(t, err)
		defer m.Close()
		managers = append(managers, m)
	}

	ids := make([]pgtype.InstanceID, len(managers))
	var wg sync.WaitGroup
	for i, m := range managers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i] = m.InstanceID()
		}()
	}
	wg.Wait()

	require.NotEqual(t, uuid.Nil, managers[0].AgentID(), "the dispatcher needs an agent id even with every target down")
	require.Equal(t, managers[0].AgentID(), managers[1].AgentID())
	require.NotEqual(t, uuid.Nil, ids[0])
	require.NotEqual(t, uuid.Nil, ids[1])
	require.NotEqual(t, ids[0], ids[1])
	require.Equal(t, ids[0], managers[0].InstanceID(), "the instance id is stable")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var persisted identity.File
	require.NoError(t, json.Unmarshal(raw, &persisted))
	require.Len(t, persisted.Instances, 2, "neither concurrent allocation may erase the other")
}

// TestManager_ForDatabaseKeepsAPoolBeingAcquired: a caller still waiting to
// acquire from a database's pool is not evicted by a request for another
// database, so it never acquires from a pool that was closed under it. The
// listener accepts connections and never answers, which keeps the first
// acquire waiting until its context expires.
func TestManager_ForDatabaseKeepsAPoolBeingAcquired(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()

	opts := DefaultConnOptions()
	opts.MaxDatabases = 1
	dsn := fmt.Sprintf("postgres://pglens:pglens@%s/postgres?sslmode=disable", ln.Addr().String())
	m, err := NewManager(context.Background(), "test-target", dsn, opts, clock.System())
	require.NoError(t, err)
	defer m.Close()
	// Registered after m.Close, so it runs first: the silent connections must
	// be closed before Close, which waits for the pool's own dial to finish.
	defer func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	firstErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		conn, err := m.ForDatabase(ctx, "postgres")
		if conn != nil {
			conn.Release()
		}
		firstErr <- err
	}()

	// The first call has picked its pool once the map holds it, so the second
	// call runs while the first is still waiting to acquire from it.
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ok := m.dbs["postgres"]
		return ok
	}, time.Second, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err = m.ForDatabase(ctx, "template1")
	require.Error(t, err, "the listener never answers, so the second acquire cannot succeed")

	m.mu.Lock()
	kept := len(m.dbs)
	m.mu.Unlock()
	require.Equal(t, 2, kept, "the pool being acquired from must not be evicted")

	// Its own acquire runs out of time; a closed pool would fail it at once.
	require.ErrorIs(t, <-firstErr, context.DeadlineExceeded)
}

// The agent id is the identity file's, so a target whose DSN no longer
// fingerprints (a PGPORT that does not parse, set after the manager was built)
// still reports it. Deriving it from the target's instance id returned the nil
// id here, and the dispatcher then registered nothing under it.
func TestManager_AgentIDIgnoresTheTargetDSN(t *testing.T) {
	t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(t.TempDir(), "identity.json"))
	mgr, err := NewManager(context.Background(), "hostless", "postgres:///postgres?connect_timeout=1", DefaultConnOptions(), nil)
	require.NoError(t, err)
	defer mgr.Close()

	want := mgr.AgentID()
	require.NotEqual(t, uuid.Nil, want)
	t.Setenv("PGPORT", "not-a-port")
	require.Equal(t, want, mgr.AgentID())
}
