package agent

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
)

func TestManager_QueryCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		readAll    bool
		signal     bool
		exts       []string
		wantTier   pgtype.PermTier
		wantErrors bool
	}{
		{name: "T0 when optional queries fail", wantTier: pgtype.TierReadOnly, wantErrors: true},
		{name: "T1 with extension", readAll: true, exts: []string{"pg_stat_statements"}, wantTier: pgtype.TierExplain},
		{name: "T2 with extension", readAll: true, signal: true, exts: []string{"pgstattuple", "pg_stat_statements"}, wantTier: pgtype.TierSignal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewConn()
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, mock.Close(context.Background()))
				require.NoError(t, mock.ExpectationsWereMet())
			})

			readExpectation := mock.ExpectQuery("pg_read_all_data").WillReturnRows(
				pgxmock.NewRows([]string{"exists"}).AddRow(tt.readAll),
			)
			if tt.wantErrors {
				readExpectation.WillReturnError(errors.New("capability probe failed"))
			}
			signalExpectation := mock.ExpectQuery("pg_signal_backend").WillReturnRows(
				pgxmock.NewRows([]string{"exists"}).AddRow(tt.signal),
			)
			if tt.wantErrors {
				signalExpectation.WillReturnError(errors.New("capability probe failed"))
			}
			if tt.exts == nil {
				mock.ExpectQuery("SELECT extname").WillReturnError(errors.New("extension probe failed"))
			} else {
				rows := pgxmock.NewRows([]string{"extname"})
				for _, ext := range tt.exts {
					rows.AddRow(ext)
				}
				mock.ExpectQuery("SELECT extname").WillReturnRows(rows)
			}
			mock.ExpectClose()

			m := &Manager{}
			gotTier, gotExts := m.queryCapabilities(context.Background(), mock)
			require.Equal(t, tt.wantTier, gotTier)
			for _, ext := range tt.exts {
				require.True(t, gotExts[ext])
			}
		})
	}
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

// TestManager_AgentID_ZeroValueOnConnectFailure: AgentID() (like the
// pre-existing InstanceID()) must degrade to the zero value rather than
// panic or block when ensureCache can't reach the database at all — pgxpool
// connects lazily, so NewManager itself succeeds against a merely-parseable
// DSN; the real connection attempt (and its failure) only happens inside
// ensureCache, which is exactly the path AgentID()/InstanceID() share.
func TestManager_AgentID_ZeroValueOnConnectFailure(t *testing.T) {
	ctx := context.Background()
	mgr, err := NewManager(ctx, "unreachable-target", "postgres://user:pass@127.0.0.1:1/nonexistent?connect_timeout=1", DefaultConnOptions(), nil)
	require.NoError(t, err)
	defer mgr.Close()

	done := make(chan uuid.UUID, 1)
	go func() { done <- mgr.AgentID() }()
	select {
	case id := <-done:
		require.Equal(t, uuid.Nil, id)
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
