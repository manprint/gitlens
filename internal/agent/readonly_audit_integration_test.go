//go:build integration

package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// readKeywords are the statements a session that only reads may send.
var readKeywords = map[string]bool{
	"SELECT": true, "WITH": true, "VALUES": true, "TABLE": true, "SHOW": true,
	"EXPLAIN": true, "SET": true, "RESET": true, "BEGIN": true, "START": true,
	"COMMIT": true, "ROLLBACK": true,
}

// dataModifying finds a write inside a statement that starts like a read: a
// data-modifying CTE or EXPLAIN of a write.
var dataModifying = regexp.MustCompile(`(?i)\b(INSERT\s+INTO|UPDATE\s+\S+\s+SET|DELETE\s+FROM|MERGE\s+INTO|TRUNCATE|FOR\s+(NO\s+KEY\s+)?UPDATE|FOR\s+(KEY\s+)?SHARE|nextval|setval|lo_create|lo_import|pg_stat_reset\w*|pg_stat_statements_reset)\b`)

// databaseFingerprint renders, as text, everything in the database and the
// shared catalogs a write would change: objects and their privileges, roles,
// settings, extensions, sequences, large objects, statistics resets and the
// rows of every user table.
func databaseFingerprint(t *testing.T, admin *pgxpool.Pool) []string {
	t.Helper()
	ctx := context.Background()
	rows, err := admin.Query(ctx, `
SELECT 'rel ' || c.oid || ' ' || c.relname || ' ' || c.relkind::text || ' ' || c.relowner || ' ' || COALESCE(c.relacl::text, '')
  FROM pg_class c
UNION ALL SELECT 'proc ' || p.oid || ' ' || p.proname || ' ' || COALESCE(p.proacl::text, '') FROM pg_proc p
UNION ALL SELECT 'nsp ' || n.oid || ' ' || n.nspname || ' ' || COALESCE(n.nspacl::text, '') FROM pg_namespace n
UNION ALL SELECT 'lo ' || m.oid FROM pg_largeobject_metadata m
UNION ALL SELECT 'role ' || a.rolname || ' ' || a.rolsuper || a.rolcreatedb || a.rolcreaterole || a.rolreplication || a.rolbypassrls
  FROM pg_authid a
UNION ALL SELECT 'member ' || m.roleid || ' ' || m.member FROM pg_auth_members m
UNION ALL SELECT 'setting ' || s.setdatabase || ' ' || s.setrole || ' ' || s.setconfig::text FROM pg_db_role_setting s
UNION ALL SELECT 'db ' || d.datname || ' ' || COALESCE(d.datacl::text, '') FROM pg_database d
UNION ALL SELECT 'ext ' || e.extname || ' ' || e.extversion FROM pg_extension e
UNION ALL SELECT 'seq ' || s.schemaname || '.' || s.sequencename || ' ' || COALESCE(s.last_value::text, '') FROM pg_sequences s
UNION ALL SELECT 'reset db ' || COALESCE(d.datname, '') || ' ' || COALESCE(d.stats_reset::text, '') FROM pg_stat_database d
UNION ALL SELECT 'reset bgwriter ' || COALESCE(stats_reset::text, '') FROM pg_stat_bgwriter
UNION ALL SELECT 'reset archiver ' || COALESCE(stats_reset::text, '') FROM pg_stat_archiver
UNION ALL SELECT 'reset statements ' || COALESCE(stats_reset::text, '') FROM public.pg_stat_statements_info
ORDER BY 1`)
	require.NoError(t, err)
	fingerprint, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)

	tables, err := admin.Query(ctx, `SELECT format('%I.%I', n.nspname, c.relname)
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE c.relkind IN ('r', 'p', 'm') AND n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
 ORDER BY 1`)
	require.NoError(t, err)
	names, err := pgx.CollectRows(tables, pgx.RowTo[string])
	require.NoError(t, err)
	for _, name := range names {
		var digest string
		require.NoError(t, admin.QueryRow(ctx, fmt.Sprintf(
			"SELECT count(*) || ' ' || COALESCE(md5(string_agg(t::text, ',' ORDER BY t::text)), '') FROM %s t", name)).Scan(&digest))
		fingerprint = append(fingerprint, "rows "+name+" "+digest)
	}
	return fingerprint
}

// INT-RO-001: pglens only reads. Every check the agent can schedule and every
// on-demand command it executes, run at the highest permission tier against
// a database with tables, sequences, indexes and pgstattuple, leaves the
// database and the shared catalogs as they were, and every statement the
// monitoring role sent is one that reads.
func TestINTRO001_AgentOnlyReads(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP TABLE IF EXISTS ro_audit")
		})
		_, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pgstattuple;
DROP TABLE IF EXISTS ro_audit;
CREATE TABLE ro_audit (id bigserial PRIMARY KEY, v text);
CREATE INDEX ro_audit_v ON ro_audit (v);
INSERT INTO ro_audit (v) SELECT 'v' || g FROM generate_series(1, 2000) g;
DELETE FROM ro_audit WHERE id % 3 = 0;
ANALYZE ro_audit`)
		require.NoError(t, err)

		// From here, every statement the monitoring role sends is recorded.
		_, err = admin.Exec(ctx, "SELECT public.pg_stat_statements_reset()")
		require.NoError(t, err)

		// The statements EXPLAIN looks up by queryid, run by the application.
		// Without constants, pg_stat_statements keeps them as written, so
		// EXPLAIN ANALYZE can run them.
		app, err := pgx.Connect(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		const readStatement = "SELECT count(*) FROM ro_audit"
		const writeStatement = "UPDATE ro_audit SET v = v"
		_, err = app.Exec(ctx, readStatement)
		require.NoError(t, err)
		_, err = app.Exec(ctx, writeStatement)
		require.NoError(t, err)
		require.NoError(t, app.Close(ctx))
		queryID := func(statement string) *int64 {
			var id int64
			require.NoError(t, admin.QueryRow(ctx, "SELECT queryid FROM public.pg_stat_statements WHERE query = $1 LIMIT 1", statement).Scan(&id))
			return &id
		}
		readID, writeID := queryID(readStatement), queryID(writeStatement)

		before := databaseFingerprint(t, admin)

		mgr, err := NewManager(ctx, "ro-audit", pg.DSN("app", pgtest.RoleT2), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		require.NoError(t, mgr.RefreshCapabilities(ctx))
		exts := map[string]bool{"pg_stat_statements": mgr.HasExtension("pg_stat_statements")}
		ran := 0
		for _, c := range check.All() {
			req := c.Requires()
			if ok, _ := req.Supports(mgr.Role(), mgr.PGVersion(), mgr.PermTier(), exts); !ok {
				continue
			}
			var target check.Target = mgr
			if req.Scope == check.ScopeDatabase {
				target = databaseScopedTarget{Target: mgr, database: "app"}
			}
			res, err := c.Scrape(ctx, target)
			require.NoError(t, err, c.Name())
			// The agent drops a fact wire.Fact.Validate refuses, so it
			// never reaches the server; and the server replaces a kind's
			// facts on results of the check wire says reports it.
			for _, f := range res.Facts {
				wf := wire.Fact{Kind: f.Kind, Key: f.Key, Labels: f.Labels, ValueText: f.ValueText, ValueJSON: f.ValueJSON}
				require.NoError(t, wf.Validate(), "%s fact %s/%s", c.Name(), f.Kind, f.Key)
				kind, _ := wire.LookupFactKind(f.Kind)
				require.Equal(t, c.Name(), kind.Check, "fact kind %s", f.Kind)
			}
			ran++
		}
		require.Greater(t, ran, 15)

		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: readID, Datname: "app"})
		require.NoError(t, err)
		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: readID, Datname: "app", Analyze: true})
		require.NoError(t, err)
		// Planning an UPDATE needs the UPDATE privilege, which the role
		// does not hold: the refusal says so.
		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: writeID, Datname: "app"})
		require.ErrorContains(t, err, "explaining a UPDATE statement needs the privilege to run it")
		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: writeID, Datname: "app", Analyze: true})
		require.Error(t, err)
		_, err = NewPgstattupleExecutor().Execute(ctx, mgr, command.Args{Schema: "public", Relation: "ro_audit", Datname: "app"})
		require.NoError(t, err)
		_, err = NewPgstattupleExecutor().Execute(ctx, mgr, command.Args{Schema: "public", Relation: "ro_audit_v", Datname: "app"})
		require.NoError(t, err)

		require.Equal(t, before, databaseFingerprint(t, admin))

		rows, err := admin.Query(ctx, `SELECT s.query FROM public.pg_stat_statements s
  JOIN pg_roles r ON r.oid = s.userid WHERE r.rolname = 'pglens_t2'`)
		require.NoError(t, err)
		statements, err := pgx.CollectRows(rows, pgx.RowTo[string])
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		for _, statement := range statements {
			keyword := firstSQLKeyword(statement)
			require.True(t, readKeywords[keyword], "pglens sent a %s statement: %s", keyword, statement)
			if keyword == "EXPLAIN" && !strings.Contains(strings.ToUpper(statement), "ANALYZE") {
				// A plan-only EXPLAIN may name the application's UPDATE; it
				// never executes it.
				continue
			}
			require.False(t, dataModifying.MatchString(statement), "pglens sent a write: %s", statement)
		}
	})
}
