//go:build integration

package pgtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHarness_INT_HARNESS_001(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", 0)
		var one int
		err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&one)
		require.NoError(t, err)
		require.Equal(t, 1, one)
		require.Equal(t, pg.Major, pg.Version.Major())
		// Write a row
		_, err = pool.Exec(context.Background(), "INSERT INTO test_kv(k,v) VALUES('harness','test')")
		require.NoError(t, err)
	})
}

func TestHarness_RestoreIsolation(t *testing.T) {
	// Verify that after previous test, the table is clean
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", 0)
		var count int
		err := pool.QueryRow(context.Background(), "SELECT count(*) FROM test_kv").Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count, "Restore should have cleaned up previous test data")
	})
}
