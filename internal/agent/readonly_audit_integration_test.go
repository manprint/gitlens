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
UNION ALL SELECT 'trg ' || t.oid || ' ' || t.tgname || ' ' || t.tgenabled::text FROM pg_trigger t
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
		auditAgentOnlyReads(t, pg, "", []string{"trigger", "fk_unindexed", "extension", "sequence", "partitioned_table", "function_tracking", "security_definer_function", "subscription"}, "")
	})
}

// INT-RO-002: INT-RO-001 on PostGIS, with spatial columns for the PostGIS
// checks to read. The PostGIS images install postgis_topology, whose
// aggregates call functions of an extension the EXPLAIN ANALYZE guard does
// not know to be free of effects, so the guard refuses EXPLAIN ANALYZE in
// that database: a refusal, never a write.
func TestINTRO002_AgentOnlyReadsPostGIS(t *testing.T) {
	pgtest.ForEachPostGIS(t, func(t *testing.T, pg *pgtest.PG) {
		auditAgentOnlyReads(t, pg, `
CREATE TABLE ro_audit_places (id int PRIMARY KEY, pt geometry(Point, 4326), area geometry(Polygon));
CREATE INDEX ON ro_audit_places USING gist (pt);
INSERT INTO ro_audit_places SELECT g, ST_SetSRID(ST_MakePoint(g, g), 4326) FROM generate_series(1, 100) g`,
			[]string{"trigger", "fk_unindexed", "extension", "spatial_column", "sequence", "partitioned_table", "function_tracking", "security_definer_function", "subscription"},
			"belongs to extension postgis_topology")
	})
}

// auditAgentOnlyReads runs every check and command against the app database
// with the fixture plus extraFixture, and asserts nothing changed, every
// statement read, and each of the catalog kinds was reported. A non-empty
// analyzeRefusal is the reason the explain guard must give for refusing
// EXPLAIN ANALYZE of a read; an empty one means it must run.
func auditAgentOnlyReads(t *testing.T, pg *pgtest.PG, extraFixture string, kinds []string, analyzeRefusal string) {
	t.Helper()
	{
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		// A subscription that never connects: no publisher, no slot.
		dropSubscription := func() {
			_, _ = admin.Exec(context.Background(), "ALTER SUBSCRIPTION ro_audit_sub SET (slot_name = NONE)")
			_, _ = admin.Exec(context.Background(), "DROP SUBSCRIPTION IF EXISTS ro_audit_sub")
		}
		dropSubscription()
		t.Cleanup(dropSubscription)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP TABLE IF EXISTS ro_audit_child, ro_audit, ro_audit_places, ro_audit_parts; DROP FUNCTION IF EXISTS ro_audit_touch(), ro_audit_definer(); DROP SEQUENCE IF EXISTS ro_audit_fresh, ro_audit_down")
		})
		_, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pgstattuple;
DROP TABLE IF EXISTS ro_audit_child, ro_audit;
DROP FUNCTION IF EXISTS ro_audit_touch(), ro_audit_definer();
CREATE FUNCTION ro_audit_definer() RETURNS integer LANGUAGE plpgsql SECURITY DEFINER AS 'BEGIN RETURN 1; END';
REVOKE EXECUTE ON FUNCTION ro_audit_definer() FROM PUBLIC;
CREATE TABLE ro_audit (id bigserial PRIMARY KEY, v text);
CREATE INDEX ro_audit_v ON ro_audit (v);
INSERT INTO ro_audit (v) SELECT 'v' || g FROM generate_series(1, 2000) g;
DELETE FROM ro_audit WHERE id % 3 = 0;
ANALYZE ro_audit;
CREATE FUNCTION ro_audit_touch() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END';
CREATE TABLE ro_audit_child (id int PRIMARY KEY, audit_id bigint REFERENCES ro_audit (id));
CREATE TRIGGER ro_audit_touch BEFORE INSERT ON ro_audit_child FOR EACH ROW EXECUTE FUNCTION ro_audit_touch();
ALTER TABLE ro_audit_child DISABLE TRIGGER ALL;
DROP SEQUENCE IF EXISTS ro_audit_fresh, ro_audit_down;
DROP TABLE IF EXISTS ro_audit_parts;
CREATE TABLE ro_audit_parts (at date NOT NULL, id int) PARTITION BY RANGE (at);
CREATE TABLE ro_audit_parts_2026 PARTITION OF ro_audit_parts FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE TABLE ro_audit_parts_def PARTITION OF ro_audit_parts DEFAULT;
CREATE INDEX ro_audit_parts_id ON ONLY ro_audit_parts (id);
INSERT INTO ro_audit_parts VALUES ('2030-01-01', 1);
CREATE SEQUENCE ro_audit_fresh;
CREATE SEQUENCE ro_audit_down AS integer INCREMENT -1 MAXVALUE -1 CYCLE;
SELECT setval('ro_audit_down', -5);
ALTER TABLE ro_audit_child ALTER COLUMN id SET DEFAULT nextval('ro_audit_id_seq');
DROP TABLE IF EXISTS ro_audit_places;`+extraFixture)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE SUBSCRIPTION ro_audit_sub CONNECTION 'dbname=app' PUBLICATION ro_audit_pub WITH (connect = false)")
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
		factsByKind := map[string]int{}
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
				factsByKind[f.Kind]++
			}
			ran++
		}
		require.Greater(t, ran, 15)
		// The fixture holds an object of each catalog kind, so each catalog
		// check ran its queries on something.
		for _, kind := range kinds {
			require.Positive(t, factsByKind[kind], "no %s fact", kind)
		}

		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: readID, Datname: "app"})
		require.NoError(t, err)
		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: readID, Datname: "app", Analyze: true})
		if analyzeRefusal == "" {
			require.NoError(t, err)
		} else {
			var rejected *RejectedError
			require.ErrorAs(t, err, &rejected)
			require.ErrorContains(t, err, analyzeRefusal)
		}
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
	}
}
