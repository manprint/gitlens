//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTCMD006_ExplainKnownQueryIDReturnsPlan(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		mgr, err := NewManager(ctx, "explain-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		conn, err := mgr.ForDatabase(ctx, "postgres")
		require.NoError(t, err)
		defer conn.Release()
		_, err = conn.Exec(ctx, "SELECT current_database()")
		require.NoError(t, err)
		var queryID int64
		require.NoError(t, conn.QueryRow(ctx, "SELECT queryid FROM pg_stat_statements WHERE query = 'SELECT current_database()' ORDER BY calls DESC LIMIT 1").Scan(&queryID))

		result, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "postgres"})
		require.NoError(t, err)
		var decoded struct {
			Plan []struct {
				Plan map[string]any `json:"Plan"`
			} `json:"plan"`
			PlanHash string `json:"plan_hash"`
		}
		require.NoError(t, json.Unmarshal(result, &decoded))
		require.NotEmpty(t, decoded.PlanHash)
		require.NotEmpty(t, decoded.Plan)
		require.Equal(t, "Result", decoded.Plan[0].Plan["Node Type"])
	})
}

func TestINTCMD007_ExplainAnalyzeUpdateRollsBack(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		mgr, err := NewManager(ctx, "explain-update-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		table := fmt.Sprintf("pglens_explain_%d", pg.Major)
		conn, err := mgr.ForDatabase(ctx, "postgres")
		require.NoError(t, err)
		defer conn.Release()
		_, err = conn.Exec(ctx, "CREATE TEMP TABLE "+table+" (value int)")
		require.NoError(t, err)
		_, err = conn.Exec(ctx, "INSERT INTO "+table+" VALUES (1)")
		require.NoError(t, err)
		update := "UPDATE " + table + " SET value = value + value"
		_, err = conn.Exec(ctx, update)
		require.NoError(t, err)
		_, err = conn.Exec(ctx, "UPDATE "+table+" SET value = 1")
		require.NoError(t, err)
		var queryID int64
		require.NoError(t, conn.QueryRow(ctx, "SELECT queryid FROM pg_stat_statements WHERE query = $1 LIMIT 1", update).Scan(&queryID))

		// Keep the temporary table on the same dedicated connection used by the
		// executor; the wrapper prevents the executor's immediate Release from
		// invalidating the connection before the post-rollback assertion.
		target := &fixedConnectionTarget{Target: mgr, conn: nonReleasingConn{Conn: conn}}
		result, err := NewExplainExecutor().Execute(ctx, target, command.Args{QueryID: &queryID, Datname: "postgres", Analyze: true})
		require.NoError(t, err)
		require.NotEmpty(t, result)
		var value int
		require.NoError(t, conn.QueryRow(ctx, "SELECT value FROM "+table).Scan(&value))
		require.Equal(t, 1, value)
	})
}

type fixedConnectionTarget struct {
	check.Target
	conn check.Conn
}

func (t *fixedConnectionTarget) ConnFor(context.Context, string) (check.Conn, error) {
	return t.conn, nil
}

type nonReleasingConn struct{ check.Conn }

func (nonReleasingConn) Release() {}
