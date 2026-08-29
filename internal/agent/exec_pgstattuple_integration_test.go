//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTCMD009_PgstattupleReturnsExactResult(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		defer admin.Close()
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgstattuple")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE TABLE IF NOT EXISTS public.pglens_pgstattuple (id integer)")
		require.NoError(t, err)
		defer func() { _, _ = admin.Exec(ctx, "DROP TABLE IF EXISTS public.pglens_pgstattuple") }()

		mgr, err := NewManager(ctx, "pgstattuple-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		result, err := NewPgstattupleExecutor().Execute(ctx, &extensionTarget{Target: mgr}, pgstattupleArgs("public", "pglens_pgstattuple"))
		require.NoError(t, err)
		var decoded pgstattupleResult
		require.NoError(t, json.Unmarshal(result, &decoded))
		require.Equal(t, int64(0), decoded.TupleCount)
		require.Equal(t, "public", decoded.Schema)
	})
}

type extensionTarget struct{ check.Target }

func (*extensionTarget) HasExtension(string) bool { return true }

func TestINTCMD010_PgstattupleRejectsWithoutExtension(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		mgr, err := NewManager(ctx, "pgstattuple-absent-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		if mgr.HasExtension("pgstattuple") {
			t.Skip("fixture already has pgstattuple installed")
		}
		_, err = NewPgstattupleExecutor().Execute(ctx, mgr, pgstattupleArgs("public", "items"))
		var rejected *RejectedError
		require.ErrorAs(t, err, &rejected)
		require.Contains(t, rejected.Error(), "does not install")
	})
}
