//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/test/pgtest"
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
		result, err := NewPgstattupleExecutor().Execute(ctx, mgr, pgstattupleArgs("public", "pglens_pgstattuple"))
		require.NoError(t, err)
		var decoded pgstattupleResult
		require.NoError(t, json.Unmarshal(result, &decoded))
		require.Equal(t, int64(0), decoded.TupleCount)
		require.Equal(t, "public", decoded.Schema)
		require.Equal(t, "postgres", decoded.Datname, "the result names the database the scan ran in")
	})
}

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

// INT-CMD-011: extensions are per database. The scan runs where the relation
// lives, so the extension installed there is the one that counts, not the
// maintenance database's, which is all the target's capability list covers.
func TestINTCMD011_PgstattupleChecksTheRelationsDatabase(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		const datname = "pglens_int_cmd_011"
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "DROP EXTENSION IF EXISTS pgstattuple")
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+datname+" WITH (FORCE)")
		})
		other := pg.Pool(t, datname, pgtest.RoleSuperuser) // creates the database
		_, err = other.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgstattuple")
		require.NoError(t, err)
		_, err = other.Exec(ctx, "CREATE TABLE IF NOT EXISTS public.measured (id integer)")
		require.NoError(t, err)

		mgr, err := NewManager(ctx, "pgstattuple-per-database", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		require.False(t, mgr.HasExtension("pgstattuple"), "the maintenance database has no pgstattuple")

		result, err := NewPgstattupleExecutor().Execute(ctx, mgr, command.Args{Schema: "public", Relation: "measured", Datname: datname})
		require.NoError(t, err)
		var decoded pgstattupleResult
		require.NoError(t, json.Unmarshal(result, &decoded))
		require.Equal(t, datname, decoded.Datname)

		_, err = NewPgstattupleExecutor().Execute(ctx, mgr, pgstattupleArgs("public", "items"))
		var rejected *RejectedError
		require.ErrorAs(t, err, &rejected)
		require.Contains(t, rejected.Error(), `not installed in database "postgres"`)
	})
}

// INT-CMD-027: the server files every pgstattuple result as a table's bloat,
// and pgstattuple also reads an index. The index is refused against the real
// catalog before it is scanned, and the table beside it is still measured.
func TestINTCMD027_PgstattupleRefusesIndexes(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		defer admin.Close()
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgstattuple")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE TABLE IF NOT EXISTS public.pglens_pgstattuple_kind (id integer PRIMARY KEY)")
		require.NoError(t, err)
		defer func() { _, _ = admin.Exec(ctx, "DROP TABLE IF EXISTS public.pglens_pgstattuple_kind") }()

		mgr, err := NewManager(ctx, "pgstattuple-kind-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		_, err = NewPgstattupleExecutor().Execute(ctx, mgr, pgstattupleArgs("public", "pglens_pgstattuple_kind_pkey"))
		var rejected *RejectedError
		require.ErrorAs(t, err, &rejected)
		require.Contains(t, rejected.Error(), `relkind "i"`)

		result, err := NewPgstattupleExecutor().Execute(ctx, mgr, pgstattupleArgs("public", "pglens_pgstattuple_kind"))
		require.NoError(t, err)
		var decoded pgstattupleResult
		require.NoError(t, json.Unmarshal(result, &decoded))
		require.Equal(t, "pglens_pgstattuple_kind", decoded.Relation)
	})
}

// INT-CMD-028: a database name may hold a space, a dot or a hyphen, and the
// command must reach that database. The name is a connection parameter and
// never SQL text, so the arguments are valid as they are sent.
func TestINTCMD028_PgstattupleReachesADatabaseWithAnyName(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		const datname = "pglens int-cmd.028"
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		defer admin.Close()
		_, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{datname}.Sanitize())
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{datname}.Sanitize()+" WITH (FORCE)")
		})
		cfg, err := pgxpool.ParseConfig(pg.DSN("postgres", pgtest.RoleSuperuser))
		require.NoError(t, err)
		cfg.ConnConfig.Database = datname
		other, err := pgxpool.NewWithConfig(ctx, cfg)
		require.NoError(t, err)
		defer other.Close()
		_, err = other.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgstattuple")
		require.NoError(t, err)
		_, err = other.Exec(ctx, "CREATE TABLE public.measured (id integer)")
		require.NoError(t, err)

		args := command.Args{Schema: "public", Relation: "measured", Datname: datname}
		require.NoError(t, args.Validate(command.KindPgstattuple))
		mgr, err := NewManager(ctx, "pgstattuple-punctuated-name", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		result, err := NewPgstattupleExecutor().Execute(ctx, mgr, args)
		require.NoError(t, err)
		var decoded pgstattupleResult
		require.NoError(t, json.Unmarshal(result, &decoded))
		require.Equal(t, datname, decoded.Datname)
	})
}
