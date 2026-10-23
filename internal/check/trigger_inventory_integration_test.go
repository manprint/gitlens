//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-TRG-001: at T0 the trigger inventory reports user triggers with their
// state and function, a foreign-key trigger only once it is disabled, and a
// partition's inherited trigger only when its state differs from the
// parent's.
func TestINTTRG001_TriggerInventory(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		schema := fmt.Sprintf("pglens_trg_%d", testSuffix())
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
		_, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %[1]s;
REVOKE ALL ON SCHEMA %[1]s FROM PUBLIC;
CREATE FUNCTION %[1]s.touch() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END';
CREATE TABLE %[1]s.parent (id int PRIMARY KEY);
CREATE TABLE %[1]s.child (id int PRIMARY KEY, parent_id int REFERENCES %[1]s.parent (id));
CREATE TRIGGER audit BEFORE INSERT OR UPDATE ON %[1]s.child FOR EACH ROW EXECUTE FUNCTION %[1]s.touch();
CREATE TRIGGER paused AFTER DELETE ON %[1]s.child FOR EACH STATEMENT EXECUTE FUNCTION %[1]s.touch();
ALTER TABLE %[1]s.child DISABLE TRIGGER paused;
CREATE TABLE %[1]s.events (at date NOT NULL) PARTITION BY RANGE (at);
CREATE TABLE %[1]s.events_2026 PARTITION OF %[1]s.events FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE TABLE %[1]s.events_2027 PARTITION OF %[1]s.events FOR VALUES FROM ('2027-01-01') TO ('2028-01-01');
CREATE TRIGGER stamp BEFORE INSERT ON %[1]s.events FOR EACH ROW EXECUTE FUNCTION %[1]s.touch();
ALTER TABLE %[1]s.events_2027 DISABLE TRIGGER stamp`, schema))
		require.NoError(t, err)

		scrape := func() map[string]Fact {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			result, err := (&triggerInventoryCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
			require.NoError(t, err)
			require.False(t, result.Truncated)
			facts := map[string]Fact{}
			for _, f := range result.Facts {
				if f.Labels["schemaname"] == schema {
					facts[f.Labels["relname"]+"."+f.Labels["trigger"]] = f
				}
			}
			return facts
		}

		facts := scrape()
		require.ElementsMatch(t, []string{"child.audit", "child.paused", "events.stamp", "events_2027.stamp"}, keysOf(facts))
		audit := facts["child.audit"]
		require.Equal(t, "enabled", audit.ValueText)
		require.Equal(t, schema+".touch", audit.Labels["function"])
		require.Equal(t, "BEFORE", audit.Labels["timing"])
		require.Equal(t, "INSERT OR UPDATE", audit.Labels["events"])
		require.Equal(t, "ROW", audit.Labels["level"])
		require.Equal(t, "user", audit.Labels["kind"])
		require.Equal(t, schema+".child.audit", audit.Key)
		require.Equal(t, "disabled", facts["child.paused"].ValueText)
		require.Equal(t, "enabled", facts["events.stamp"].ValueText)
		require.Equal(t, "disabled", facts["events_2027.stamp"].ValueText)

		// DISABLE TRIGGER ALL also stops the foreign key's own triggers:
		// the constraint is no longer enforced, and the inventory says so.
		_, err = admin.Exec(ctx, "ALTER TABLE "+schema+".child DISABLE TRIGGER ALL")
		require.NoError(t, err)
		facts = scrape()
		var fkTriggers []Fact
		for key, f := range facts {
			if f.Labels["kind"] == "internal" {
				require.Equal(t, "disabled", f.ValueText, key)
				require.Equal(t, "child_parent_id_fkey", f.Labels["constraint"], key)
				require.Equal(t, "foreign key", f.Labels["constraint_type"], key)
				fkTriggers = append(fkTriggers, f)
			}
		}
		require.NotEmpty(t, fkTriggers)
		require.Equal(t, "disabled", facts["child.audit"].ValueText)
	})
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
