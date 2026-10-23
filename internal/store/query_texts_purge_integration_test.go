//go:build integration

package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Statement texts shipped before the agent redacted them stay in query_texts
// until the migration that purges credential texts runs. Re-running it, as a
// restart after the version row is missing would, removes the secret and keeps
// the texts without one.
func TestINTSTORE_0017_PurgesStoredCredentialTexts(t *testing.T) {
	pool := getStoreTestPool(t)
	ctx := context.Background()
	const cluster = int64(970017)

	_, err := pool.Exec(ctx, `DELETE FROM query_texts WHERE cluster_id = $1`, cluster)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text) VALUES
		('default', $1, 'app', 1, 17, 'ALTER ROLE tmp_secret PASSWORD ''hunter2'''),
		('default', $1, 'app', 2, 17, 'SELECT 1'),
		('default', $1, 'app', 3, 17, 'CREATE USER MAPPING FOR u SERVER s OPTIONS (user ''bob'', password ''sekret'')'),
		('default', $1, 'app', 4, 17, 'ALTER SUBSCRIPTION s CONNECTION ''postgresql://rep:abc@primary:5432/app''')`, cluster)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version = 17`)
	require.NoError(t, err)
	require.NoError(t, Migrate(ctx, pool))

	rows, err := pool.Query(ctx, `SELECT queryid, query_text FROM query_texts WHERE cluster_id = $1 ORDER BY queryid`, cluster)
	require.NoError(t, err)
	defer rows.Close()
	kept := map[int64]string{}
	for rows.Next() {
		var id int64
		var text string
		require.NoError(t, rows.Scan(&id, &text))
		kept[id] = text
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[int64]string{2: "SELECT 1"}, kept)
	_, err = pool.Exec(ctx, `DELETE FROM query_texts WHERE cluster_id = $1`, cluster)
	require.NoError(t, err)
}
