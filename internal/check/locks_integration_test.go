//go:build integration

package check

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTLOCK002_NoContentionEmitsGaugesAndEmptyFact(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(context.Background())
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&locksCheck{}).Scrape(context.Background(), target)
		require.NoError(t, err)
		require.Len(t, result.Facts, 1)
		var tree struct {
			Nodes []json.RawMessage `json:"nodes"`
		}
		require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
		require.Empty(t, tree.Nodes)
		require.Equal(t, 0.0, metricValue(result, "pg_blocked_sessions", nil))
	})
}

func TestINTLOCK001_BlockingSessionIsReported(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS pglens_lock_probe (id integer primary key, value integer)")
		require.NoError(t, err)
		_, err = pool.Exec(ctx, "INSERT INTO pglens_lock_probe VALUES (1, 1) ON CONFLICT (id) DO NOTHING")
		require.NoError(t, err)
		holder, err := pool.Acquire(ctx)
		require.NoError(t, err)
		tx, err := holder.Begin(ctx)
		require.NoError(t, err)
		var holderPID int32
		require.NoError(t, holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID))
		_, err = tx.Exec(ctx, "UPDATE pglens_lock_probe SET value=value+1 WHERE id=1")
		require.NoError(t, err)
		blocked, err := pool.Acquire(ctx)
		require.NoError(t, err)
		ready := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			close(ready)
			_, _ = blocked.Exec(ctx, "SET lock_timeout='8s'")
			_, _ = blocked.Exec(ctx, "UPDATE pglens_lock_probe SET value=value+1 WHERE id=1")
		}()
		<-ready
		t.Cleanup(func() {
			_ = tx.Rollback(ctx)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Log("blocked lock probe did not finish during cleanup")
			}
			blocked.Release()
			holder.Release()
		})
		time.Sleep(100 * time.Millisecond)

		scrapeConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		target := &registryTestTarget{conn: scrapeConn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&locksCheck{}).Scrape(ctx, target)
		require.NoError(t, err)
		require.Equal(t, 1.0, metricValue(result, "pg_blocked_sessions", nil))
		require.NotEmpty(t, result.Facts)
		var tree struct {
			Nodes []struct {
				PID       int32   `json:"pid"`
				BlockedBy []int32 `json:"blocked_by"`
				WaitEvent string  `json:"wait_event"`
			} `json:"nodes"`
		}
		require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
		require.Len(t, tree.Nodes, 2)
		blockedNodes := 0
		sawHolder := false
		for _, node := range tree.Nodes {
			if len(node.BlockedBy) > 0 {
				blockedNodes++
				require.Contains(t, node.BlockedBy, holderPID)
				require.Equal(t, "transactionid", node.WaitEvent)
			}
			if node.PID == holderPID {
				sawHolder = true
			}
		}
		require.Equal(t, 1, blockedNodes)
		require.True(t, sawHolder)
	})
}

func TestINTLOCK003_RelationLockIsReported(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS pglens_relation_lock_probe (id integer)")
		require.NoError(t, err)
		holder, err := pool.Acquire(ctx)
		require.NoError(t, err)
		tx, err := holder.Begin(ctx)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, "LOCK TABLE pglens_relation_lock_probe IN ACCESS EXCLUSIVE MODE")
		require.NoError(t, err)

		blocked, err := pool.Acquire(ctx)
		require.NoError(t, err)
		ready := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			close(ready)
			_, _ = blocked.Exec(ctx, "SET lock_timeout='2s'")
			_, _ = blocked.Exec(ctx, "SELECT count(*) FROM pglens_relation_lock_probe")
		}()
		<-ready
		t.Cleanup(func() {
			_ = tx.Rollback(ctx)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Log("blocked relation probe did not finish during cleanup")
			}
			blocked.Release()
			holder.Release()
		})
		time.Sleep(100 * time.Millisecond)

		scrapeConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		target := &registryTestTarget{conn: scrapeConn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&locksCheck{}).Scrape(ctx, target)
		require.NoError(t, err)
		require.Equal(t, 1.0, metricValue(result, "pg_blocked_sessions", nil))
		var tree struct {
			Nodes []struct {
				BlockedBy []int32 `json:"blocked_by"`
				WaitEvent string  `json:"wait_event"`
			} `json:"nodes"`
		}
		require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
		require.Len(t, tree.Nodes, 2)
		blockedNodes := 0
		for _, node := range tree.Nodes {
			if len(node.BlockedBy) > 0 {
				blockedNodes++
				require.Equal(t, "relation", node.WaitEvent)
			}
		}
		require.Equal(t, 1, blockedNodes)
	})
}

func TestINTLOCK006_LockShapeAcrossVersions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(context.Background())
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&locksCheck{}).Scrape(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, []string{"pg_blocked_sessions", "pg_blocking_sessions", "pg_max_block_age_seconds"}, metricNames(result))
		require.Len(t, result.Facts, 1)
		var tree struct {
			Nodes []json.RawMessage `json:"nodes"`
		}
		require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
		require.Empty(t, tree.Nodes)
	})
}

// A session whose text is longer than track_activity_query_size reaches the
// server with only a prefix, so a credential named before the cut must not be
// shown, and a text that is cut at a character boundary is withheld too.
// Sessions whose text fits keep it.
func TestINTLOCK007_SessionTextTheServerCutIsWithheld(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		// IF false keeps the EXECUTE from running, so no role is needed. The
		// keyword sits past the default 1024-byte cut.
		credential := "DO $$ DECLARE p text := 'hunter2'; BEGIN /*" + strings.Repeat("x", 2000) + "*/ IF false THEN EXECUTE 'ALTER ROLE u PASSWORD ' || quote_literal(p); END IF; END $$"
		// The text is 1026 bytes and the multi-byte character starts at byte
		// 1022, so the server clips it to 1022 bytes, short of the cut size.
		boundary := "SELECT '" + strings.Repeat("x", 1014) + "€'"
		plain := "SELECT 'plain text marker'"
		// The holders stay checked out, so each statement keeps its own session,
		// and the PID is read before the statement runs, since reading it after
		// would replace the session's last statement.
		var pids []int32
		var holders []*pgxpool.Conn
		defer func() {
			for _, holder := range holders {
				holder.Release()
			}
		}()
		for _, query := range []string{credential, boundary, plain} {
			holder, err := admin.Acquire(ctx)
			require.NoError(t, err)
			holders = append(holders, holder)
			var pid int32
			require.NoError(t, holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid))
			_, err = holder.Exec(ctx, query)
			require.NoError(t, err)
			pids = append(pids, pid)
		}
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&locksCheck{}).Scrape(ctx, target)
		require.NoError(t, err)
		require.NotContains(t, string(result.Facts[0].ValueJSON), "hunter2")
		var tree struct {
			Sessions []struct {
				PID   int32  `json:"pid"`
				Query string `json:"query"`
			} `json:"sessions"`
		}
		require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
		queries := map[int32]string{}
		for _, s := range tree.Sessions {
			queries[s.PID] = s.Query
		}
		require.Equal(t, withheldQuery, queries[pids[0]])
		require.Equal(t, withheldQuery, queries[pids[1]])
		require.Equal(t, "SELECT 'plain text marker'", queries[pids[2]])
	})
}

func metricNames(result Result) []string {
	names := make([]string, 0, len(result.Metrics))
	for _, metric := range result.Metrics {
		names = append(names, metric.Name)
	}
	sort.Strings(names)
	return names
}
