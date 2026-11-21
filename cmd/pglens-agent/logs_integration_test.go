//go:build integration

package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// sqlLogLoop starts a log loop of the SQL source on pg, after granting the
// monitoring role the log functions; pending collects what it emits.
func sqlLogLoop(t *testing.T, pg *pgtest.PG) (*logLoop, *pgxpool.Pool, func() []wire.Result) {
	t.Helper()
	ctx := context.Background()
	super := pg.Pool(t, "app", pgtest.RoleSuperuser)
	mgr, err := agent.NewManager(ctx, "logs-target", pg.DSN("app", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
	require.NoError(t, err)
	t.Cleanup(mgr.Close)
	var user string
	require.NoError(t, mgr.LogQuerier().QueryRow(ctx, "SELECT current_user").Scan(&user))
	// pg_monitor holds pg_current_logfile only from PostgreSQL 16 on.
	for _, fn := range []string{"pg_read_binary_file(text,bigint,bigint,boolean)", "pg_current_logfile(text)"} {
		_, err = super.Exec(ctx, "GRANT EXECUTE ON FUNCTION "+fn+" TO "+pgx.Identifier{user}.Sanitize())
		require.NoError(t, err)
	}

	var mu sync.Mutex
	var pending []wire.Result
	cfg := agent.CheckConfig{Enabled: boolp(true), Source: agent.LogSourceSQL, StatementText: boolp(true)}
	l := &logLoop{
		target: "logs-target", cfg: cfg, state: &logState{path: filepath.Join(t.TempDir(), "pglog-state.json")},
		clk: clock.System(), settings: mgr.ReadLogSettings,
		emit: func(r wire.Result) {
			mu.Lock()
			pending = append(pending, r)
			mu.Unlock()
		},
		logf: t.Logf,
	}
	l.source = newLogSource(mgr.LogQuerier(), cfg, false, l.logDir)
	return l, super, func() []wire.Result {
		mu.Lock()
		defer mu.Unlock()
		return append([]wire.Result(nil), pending...)
	}
}

// logEventsAfterUniqueViolation runs the agent's log loop through the SQL
// source against a server writing destination, causes one unique violation,
// and returns the log_event facts of class error.statement with SQLSTATE
// 23505 the loop shipped.
func logEventsAfterUniqueViolation(t *testing.T, major int, destination string) []wire.Fact {
	t.Helper()
	ctx := context.Background()
	l, super, results := sqlLogLoop(t, pgtest.StartLogging(t, major, destination))

	// The first scrape only marks the end of the log.
	l.scrape(ctx)
	_, err := super.Exec(ctx, "CREATE TABLE u (id int PRIMARY KEY); INSERT INTO u VALUES (1)")
	require.NoError(t, err)
	_, err = super.Exec(ctx, "INSERT INTO u VALUES (1)")
	require.Error(t, err)

	var found []wire.Fact
	require.Eventually(t, func() bool {
		l.scrape(ctx)
		found = found[:0]
		for _, r := range results() {
			for _, f := range r.Facts {
				if f.Kind == logEventKind && f.Labels["sqlstate"] == "23505" && f.Labels["class"] == pglog.ClassErrorStatement {
					found = append(found, f)
				}
			}
		}
		return len(found) > 0
	}, 20*time.Second, 300*time.Millisecond)
	for _, r := range results() {
		require.Empty(t, r.SkipReason, "no coverage note expected: %s", r.SkipReason)
	}
	return found
}

// TestINTPGL002_JSONLogUniqueViolation: PostgreSQL 18 with jsonlog ships
// exactly one log_event for a unique violation.
func TestINTPGL002_JSONLogUniqueViolation(t *testing.T) {
	found := logEventsAfterUniqueViolation(t, 18, pglog.FormatJSON)
	require.Len(t, found, 1)
	require.Equal(t, "app", found[0].Labels["datname"])
	require.Contains(t, string(found[0].ValueJSON), "INSERT INTO u VALUES (1)")
}

// TestINTPGL003_CSVLogUniqueViolation: the same through csvlog on PostgreSQL 15.
func TestINTPGL003_CSVLogUniqueViolation(t *testing.T) {
	found := logEventsAfterUniqueViolation(t, 15, pglog.FormatCSV)
	require.Len(t, found, 1)
	require.Equal(t, "app", found[0].Labels["datname"])
}
