//go:build integration

package check

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
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
				PID           int32      `json:"pid"`
				BlockedBy     []int32    `json:"blocked_by"`
				WaitEvent     string     `json:"wait_event"`
				BackendStart  *time.Time `json:"backend_start"`
				SessionID     string     `json:"session_id"`
				WaitStartedAt *time.Time `json:"wait_started_at"`
				WaitDuration  *float64   `json:"wait_duration"`
			} `json:"nodes"`
		}
		var holderStart time.Time
		require.NoError(t, pool.QueryRow(ctx, "SELECT backend_start FROM pg_stat_activity WHERE pid = $1", holderPID).Scan(&holderStart))
		require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
		require.Len(t, tree.Nodes, 2)
		blockedNodes := 0
		sawHolder := false
		for _, node := range tree.Nodes {
			require.Equal(t, fmt.Sprintf("%x.%d", node.BackendStart.UnixMicro(), node.PID), node.SessionID, "node %d", node.PID)
			if len(node.BlockedBy) > 0 {
				blockedNodes++
				require.Contains(t, node.BlockedBy, holderPID)
				require.Equal(t, "transactionid", node.WaitEvent)
				// pg_locks.waitstart is set for the waiting lock, and the
				// duration is measured on the server from that moment.
				require.NotNil(t, node.WaitStartedAt, "a blocked node names the start of its wait")
				require.NotNil(t, node.WaitDuration, "a blocked node names how long it has waited")
				require.Greater(t, *node.WaitDuration, 0.0)
			} else {
				require.Nil(t, node.WaitStartedAt, "a session that waits on no lock names no wait")
				require.Nil(t, node.WaitDuration)
			}
			require.NotNil(t, node.BackendStart, "node %d", node.PID)
			if node.PID == holderPID {
				sawHolder = true
				require.Equal(t, holderStart.UnixMicro(), node.BackendStart.UnixMicro(), "the holder carries the start time of its session")
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
// shown, and a text that is cut at a character boundary is withheld too. Only
// the sessions of the blocking tree carry text, so each statement waits on a
// lock the test holds and is therefore a node; the session list carries none.
func TestINTLOCK007_SessionTextTheServerCutIsWithheld(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE TABLE IF NOT EXISTS pglens_cut_probe (id integer)")
		require.NoError(t, err)
		holder, err := admin.Acquire(ctx)
		require.NoError(t, err)
		defer holder.Release()
		tx, err := holder.Begin(ctx)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, "LOCK TABLE pglens_cut_probe IN ACCESS EXCLUSIVE MODE")
		require.NoError(t, err)
		// IF false keeps the EXECUTE from running, so no role is needed. The
		// keyword sits past the default 1024-byte cut. PERFORM reads the locked
		// table, so the block waits on the holder.
		credential := "DO $$ DECLARE p text := 'hunter2'; BEGIN /*" + strings.Repeat("x", 2000) + "*/ IF false THEN EXECUTE 'ALTER ROLE u PASSWORD ' || quote_literal(p); END IF; PERFORM 1 FROM pglens_cut_probe; END $$"
		// The text is 1026 bytes and the multi-byte character starts at byte
		// 1022, so the server clips it to 1022 bytes, short of the cut size.
		boundary := "SELECT '" + strings.Repeat("x", 1014) + "€' FROM pglens_cut_probe"
		plain := "SELECT 'plain text marker' FROM pglens_cut_probe"
		statements := []string{credential, boundary, plain}
		pids := make([]int32, len(statements))
		var waiters sync.WaitGroup
		var conns []*pgxpool.Conn
		defer func() {
			_ = tx.Rollback(ctx)
			waiters.Wait()
			for _, conn := range conns {
				conn.Release()
			}
		}()
		for i, query := range statements {
			conn, err := admin.Acquire(ctx)
			require.NoError(t, err)
			conns = append(conns, conn)
			require.NoError(t, conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pids[i]))
			waiters.Add(1)
			go func(conn *pgxpool.Conn, query string) {
				defer waiters.Done()
				_, _ = conn.Exec(ctx, "SET lock_timeout = '30s'")
				_, _ = conn.Exec(ctx, query)
			}(conn, query)
		}
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		var result Result
		deadline := time.Now().Add(10 * time.Second)
		for {
			result, err = (&locksCheck{}).Scrape(ctx, target)
			require.NoError(t, err)
			if metricValue(result, "pg_blocked_sessions", nil) == float64(len(statements)) || time.Now().After(deadline) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		require.Equal(t, float64(len(statements)), metricValue(result, "pg_blocked_sessions", nil))
		require.NotContains(t, string(result.Facts[0].ValueJSON), "hunter2")
		var tree struct {
			Nodes []struct {
				PID   int32  `json:"pid"`
				Query string `json:"query"`
			} `json:"nodes"`
			Sessions []struct {
				PID   int32  `json:"pid"`
				Query string `json:"query"`
			} `json:"sessions"`
		}
		require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
		queries := map[int32]string{}
		for _, n := range tree.Nodes {
			queries[n.PID] = n.Query
		}
		require.Equal(t, withheldQuery, queries[pids[0]])
		require.Equal(t, withheldQuery, queries[pids[1]])
		require.Equal(t, plain, queries[pids[2]])
		for _, sess := range tree.Sessions {
			require.Empty(t, sess.Query, "the session list carries no text, session %d", sess.PID)
		}
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
