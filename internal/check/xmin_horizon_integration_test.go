//go:build integration

package check

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/redact"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// findXminHolder returns the holder of a kind with the identity, or nil.
func findXminHolder(holders []xminHolder, kind, identity string) *xminHolder {
	for i := range holders {
		if holders[i].HolderKind == kind && holders[i].Identity == identity {
			return &holders[i]
		}
	}
	return nil
}

// TestINTXMIN001_RepeatableReadSessionHoldsTheXmin: a session that holds a
// snapshot in REPEATABLE READ keeps its backend_xmin, which the activity check
// reads as a session holder by pid. The agent's own session is never named.
func TestINTXMIN001_RepeatableReadSessionHoldsTheXmin(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		holder, err := pool.Acquire(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = holder.Exec(context.Background(), "ROLLBACK")
			holder.Release()
		})
		var holderPID int
		require.NoError(t, holder.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID))
		_, err = holder.Exec(ctx, "BEGIN ISOLATION LEVEL REPEATABLE READ")
		require.NoError(t, err)
		var one int
		require.NoError(t, holder.QueryRow(ctx, "SELECT 1").Scan(&one)) // takes the snapshot

		// The scrape releases the connection it is given, so the agent's own
		// session is read from it before the scrape and the holders from another.
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		var ownPID int
		require.NoError(t, conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&ownPID))
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(ctx, target)
		require.NoError(t, err)

		probe, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer probe.Release()
		holders, err := queryXminHolders(ctx, probe)
		require.NoError(t, err)
		session := findXminHolder(holders, "session", strconv.Itoa(holderPID))
		require.NotNil(t, session, "the repeatable-read session is not reported as a holder")
		require.NotNil(t, session.AgeSeconds, "a session has a transaction start to measure from")
		require.GreaterOrEqual(t, *session.AgeSeconds, 0.0)
		require.GreaterOrEqual(t, session.AgeXIDs, int64(0))
		require.Nil(t, findXminHolder(holders, "session", strconv.Itoa(ownPID)), "the agent's own session is named")

		// The gauge of the session kind is the age of the same holder, read a
		// moment earlier, and the scrape reported it with the other kinds.
		require.GreaterOrEqual(t, metricValue(result, "pg_xmin_horizon_age_xids", map[string]string{"holder_kind": "session"}), 0.0)
		require.LessOrEqual(t, metricValue(result, "pg_xmin_horizon_age_xids", map[string]string{"holder_kind": "session"}), float64(session.AgeXIDs))
	})
}

// TestINTXMIN002_PreparedTransactionHolderIsNamedByGid: a prepared transaction
// holds the xmin until it is committed or rolled back. Its gid is shipped, and
// a gid that may hold a credential is withheld. The shared containers leave
// max_prepared_transactions at 0, so this test starts its own container.
func TestINTXMIN002_PreparedTransactionHolderIsNamedByGid(t *testing.T) {
	for _, major := range pgtest.Versions() {
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			pg := pgtest.StartPreparedTransactions(t, major)
			pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()

			// txid_current() gives the transaction an xid before it is prepared.
			prepare := func(gid string) {
				t.Helper()
				_, err := conn.Exec(ctx, "BEGIN")
				require.NoError(t, err)
				_, err = conn.Exec(ctx, "SELECT txid_current()")
				require.NoError(t, err)
				_, err = conn.Exec(ctx, fmt.Sprintf("PREPARE TRANSACTION '%s'", gid))
				require.NoError(t, err)
			}

			prepare("orders import")
			holders, err := queryXminHolders(ctx, conn)
			require.NoError(t, err)
			prepared := findXminHolder(holders, "prepared", "orders import")
			require.NotNil(t, prepared, "the prepared transaction is not reported as a holder")
			require.NotNil(t, prepared.AgeSeconds, "a prepared transaction has a prepare time to measure from")
			require.GreaterOrEqual(t, *prepared.AgeSeconds, 0.0)

			_, err = conn.Exec(ctx, "ROLLBACK PREPARED 'orders import'")
			require.NoError(t, err)

			prepare("import password=hunter2")
			holders, err = queryXminHolders(ctx, conn)
			require.NoError(t, err)
			require.Nil(t, findXminHolder(holders, "prepared", "import password=hunter2"), "the credential gid is shipped")
			require.NotNil(t, findXminHolder(holders, "prepared", redact.Withheld), "the credential gid is not withheld")
			_, err = conn.Exec(ctx, "ROLLBACK PREPARED 'import password=hunter2'")
			require.NoError(t, err)
		})
	}
}

// TestINTXMIN003_LogicalSlotHolderIsNamedByName: a logical slot holds the
// catalog xmin from its creation, and is reported by its name with no start
// time. The slot is dropped when the test ends, since the shared container
// keeps it otherwise.
func TestINTXMIN003_LogicalSlotHolderIsNamedByName(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		const slot = "pglens_xmin_slot"
		admin := adminConn(t, pool)
		_, err := admin.Exec(ctx, "SELECT pg_create_logical_replication_slot($1, 'test_decoding')", slot)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "SELECT pg_drop_replication_slot($1)", slot)
		})

		holders, err := queryXminHolders(ctx, admin)
		require.NoError(t, err)
		logical := findXminHolder(holders, "slot", slot)
		require.NotNil(t, logical, "the logical slot is not reported as a holder")
		require.Nil(t, logical.AgeSeconds, "a slot has no start time")
		require.GreaterOrEqual(t, logical.AgeXIDs, int64(0))
	})
}

// adminConn acquires a connection from pool and releases it at the end of the test.
func adminConn(t *testing.T, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()
	conn, err := pool.Acquire(context.Background())
	require.NoError(t, err)
	t.Cleanup(conn.Release)
	return conn
}
