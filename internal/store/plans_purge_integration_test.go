//go:build integration

package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Plans and command errors stored before the agent withheld them stay in the
// store until the migration that purges credential plans runs. It removes the
// plans and clears the results with a sign of a credential, replaces the
// errors and audit details that have one, and keeps everything else.
func TestINTSTORE_0021_PurgesStoredCredentialPlans(t *testing.T) {
	pool := getStoreTestPool(t)
	ctx := context.Background()
	instance := uuid.New()
	const cluster = int64(970021)
	const withheld = "the error text is withheld because it may hold a credential"
	t.Cleanup(func() {
		for _, table := range []string{"query_plans", "commands", "command_audit"} {
			_, _ = pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE instance_id = $1", instance)
		}
	})

	_, err := pool.Exec(ctx, `INSERT INTO query_plans (instance_id, cluster_id, datname, queryid, plan_hash, captured_at, plan) VALUES
		($1, $2, 'app', 1, 'h1', now(), '[{"Plan": {"Node Type": "Result", "Output": ["''postgresql://app:hunter2@db/app''::text"]}}]'),
		($1, $2, 'app', 2, 'h2', now(), '[{"Plan": {"Node Type": "Function Scan", "Function Call": "dblink(''host=h password=hunter2''::text)"}}]'),
		($1, $2, 'app', 3, 'h3', now(), '[{"Plan": {"Node Type": "Seq Scan", "Relation Name": "orders", "Output": ["\"Orders\".id"]}}]')`, instance, cluster)
	require.NoError(t, err)
	leaked, kept := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO commands (command_id, agent_id, instance_id, cluster_id, kind, state, expires_at, result, error) VALUES
		($1, 'a', $3, $4, 'explain', 'done', now(), '{"plan": [{"Plan": {"Output": ["''postgresql://app:hunter2@db/app''::text"]}}], "plan_hash": "h1"}', NULL),
		($2, 'a', $3, $4, 'explain', 'failed', now(), NULL, 'ERROR: syntax error at or near "PASSWORD" (SQLSTATE 42601)'),
		(gen_random_uuid(), 'a', $3, $4, 'explain', 'failed', now(), NULL, 'ERROR: canceling statement due to statement timeout (SQLSTATE 57014)')`,
		leaked, kept, instance, cluster)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO command_audit (command_id, instance_id, kind, requested_by, outcome, detail) VALUES
		($1, $2, 'explain', 'api', 'error', 'could not connect: postgresql://app:hunter2@db/app'),
		($1, $2, 'explain', 'api', 'rejected', 'queryid 7 is unknown on this instance')`, kept, instance)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version = 21`)
	require.NoError(t, err)
	require.NoError(t, Migrate(ctx, pool))

	var plans []int64
	rows, err := pool.Query(ctx, `SELECT queryid FROM query_plans WHERE instance_id = $1 ORDER BY queryid`, instance)
	require.NoError(t, err)
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		plans = append(plans, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int64{3}, plans, "only the plan without a sign is kept, quoted identifiers and all")

	var leakedResult *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT result::text FROM commands WHERE command_id = $1`, leaked).Scan(&leakedResult))
	require.Nil(t, leakedResult)
	var errors []string
	rows, err = pool.Query(ctx, `SELECT error FROM commands WHERE instance_id = $1 AND error IS NOT NULL ORDER BY error`, instance)
	require.NoError(t, err)
	for rows.Next() {
		var text string
		require.NoError(t, rows.Scan(&text))
		errors = append(errors, text)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"ERROR: canceling statement due to statement timeout (SQLSTATE 57014)", withheld}, errors)

	var details []string
	rows, err = pool.Query(ctx, `SELECT detail FROM command_audit WHERE instance_id = $1 ORDER BY detail`, instance)
	require.NoError(t, err)
	for rows.Next() {
		var text string
		require.NoError(t, rows.Scan(&text))
		details = append(details, text)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"queryid 7 is unknown on this instance", withheld}, details)
}
