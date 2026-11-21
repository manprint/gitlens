//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/test/pgtest"
)

// INT-WATCH-010: a lock taken in a database other than the watcher's is named
// through the agent's connection to that database; an unknown database or
// OID leaves it unnamed, and an index is named by its table.
func TestINTWATCH010_ResolverNamesRelationOfOtherDatabase(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg.Exec(t, `DROP TABLE IF EXISTS "Watch Res"; CREATE TABLE "Watch Res" (id int PRIMARY KEY)`)
		t.Cleanup(func() { pg.Exec(t, `DROP TABLE IF EXISTS "Watch Res"`) })
		var oid, idx int64
		require.NoError(t, pg.Pool(t, "app", pgtest.RoleSuperuser).QueryRow(ctx, `SELECT '"Watch Res"'::regclass::oid::bigint, '"Watch Res_pkey"'::regclass::oid::bigint`).Scan(&oid, &idx))

		mgr, err := agent.NewManager(ctx, "watch-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)
		defer mgr.Close()
		resolve := relationResolver(mgr)

		got := resolve(ctx, "app", []uint32{uint32(oid), uint32(idx), 1})
		require.Equal(t, map[uint32]string{uint32(oid): `public."Watch Res"`, uint32(idx): `public."Watch Res"`}, got)
		require.Nil(t, resolve(ctx, "no_such_db", []uint32{uint32(oid)}))
	})
}
