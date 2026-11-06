//go:build integration

package main

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const intImage = "postgres:18"

func pgFlags(name string) []string {
	return []string{"postgres",
		"-c", "cluster_name=" + name,
		"-c", "wal_level=replica", "-c", "max_wal_senders=10", "-c", "max_replication_slots=10",
		"-c", "hot_standby=on", "-c", "hot_standby_feedback=on", "-c", "wal_log_hints=on",
		"-c", "fsync=off",
	}
}

const primaryInit = `#!/bin/bash
set -e
psql -v ON_ERROR_STOP=1 -U postgres -d postgres <<'SQL'
CREATE ROLE replicator REPLICATION LOGIN PASSWORD 'repl';
ALTER SYSTEM SET wal_keep_size = '256MB';
SQL
echo "host replication replicator all scram-sha-256" >> "$PGDATA/pg_hba.conf"
`

func standbyEntrypoint(name, slot string) string {
	return fmt.Sprintf(`set -e
if [ ! -f "$PGDATA/PG_VERSION" ]; then
  until pg_isready -h odoo-db -U postgres -d postgres >/dev/null 2>&1; do sleep 1; done
  until PGPASSWORD=repl pg_basebackup -h odoo-db -U replicator -D "$PGDATA" -R -X stream -C -S %[2]s; do
    rm -rf "$PGDATA"/*; sleep 1
  done
  echo "primary_conninfo = 'host=odoo-db port=5432 user=replicator password=repl application_name=%[1]s'" >> "$PGDATA/postgresql.auto.conf"
  chmod 700 "$PGDATA"; chown -R postgres:postgres "$PGDATA"
fi
exec gosu postgres %[3]s`, name, slot, strings.Join(pgFlags(name), " "))
}

type intNode struct {
	cfg nodeConfig
	c   testcontainers.Container
	dsn string
}

func startNode(ctx context.Context, t *testing.T, netName, name, slot string, primary bool) intNode {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:          intImage,
		ExposedPorts:   []string{"5432/tcp"},
		Env:            map[string]string{"POSTGRES_PASSWORD": "test", "PGDATA": "/var/lib/postgresql/data"},
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {name}},
	}
	if primary {
		req.Cmd = pgFlags(name)
		req.Files = []testcontainers.ContainerFile{{Reader: strings.NewReader(primaryInit), ContainerFilePath: "/docker-entrypoint-initdb.d/10-repl.sh", FileMode: 0o755}}
		req.WaitingFor = wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(120 * time.Second)
	} else {
		req.Entrypoint = []string{"bash", "-c", standbyEntrypoint(name, slot)}
		req.WaitingFor = wait.ForLog("started streaming WAL").WithStartupTimeout(180 * time.Second)
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(context.WithoutCancel(ctx)) })
	host, err := c.Host(ctx)
	require.NoError(t, err)
	port, err := c.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	addr := net.JoinHostPort(host, port.Port())
	return intNode{cfg: nodeConfig{Name: name, Addr: addr, Slot: slot}, c: c,
		dsn: fmt.Sprintf("postgres://postgres:test@%s/postgres?sslmode=disable", addr)}
}

func queryVia(ctx context.Context, addr, sql string, dst ...any) error {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := pgx.Connect(cctx, fmt.Sprintf("postgres://postgres:test@%s/postgres?sslmode=disable&connect_timeout=2", addr))
	if err != nil {
		return err
	}
	defer closeConn(ctx, conn)
	if len(dst) == 0 {
		_, err = conn.Exec(cctx, sql)
		return err
	}
	return conn.QueryRow(cctx, sql).Scan(dst...)
}

// TestHACTLINT001: kill the primary; hactl promotes within 15 s, writes go
// through the RW endpoint, the RO endpoint serves the remaining standby on
// the new timeline, and the old primary is never auto-rejoined.
func TestHACTLINT001_FailoverOnRealContainers(t *testing.T) {
	ctx := context.Background()
	nw, err := network.New(ctx, network.WithLabels(map[string]string{"test": "pglens-hactl"}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = nw.Remove(context.Background()) })

	n1 := startNode(ctx, t, nw.Name, "odoo-db", "ha_1", true)
	n2 := startNode(ctx, t, nw.Name, "pg-ha-2", "ha_2", false)
	n3 := startNode(ctx, t, nw.Name, "pg-ha-3", "ha_3", false)

	ops := pgxOps{user: "postgres", password: "test", dbname: "postgres", replUser: "replicator", replPassword: "repl", replPort: 5432}
	c := newController([]nodeConfig{n1.cfg, n2.cfg, n3.cfg}, ops, stateStore{path: t.TempDir() + "/hactl.json"}, persisted{}, time.Now, t.Logf)
	rw, ro := newProxy(c.routeRW), newProxy(c.routeRO)
	c.onRWChange = rw.closeAll
	rwAddr, roAddr := startProxy(t, rw), startProxy(t, ro)
	startLoop := func() func() {
		lctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			tk := time.NewTicker(time.Second)
			defer tk.Stop()
			for {
				c.tick(lctx)
				select {
				case <-lctx.Done():
					return
				case <-tk.C:
				}
			}
		}()
		return func() { cancel(); <-done }
	}
	stopLoop := startLoop()
	t.Cleanup(func() { stopLoop() })

	require.Eventually(t, func() bool { return c.status().Leader == "odoo-db" }, 30*time.Second, 200*time.Millisecond)
	var streaming int
	require.Eventually(t, func() bool {
		return queryVia(ctx, n1.cfg.Addr, `SELECT count(*) FROM pg_stat_replication
			WHERE state = 'streaming' AND application_name IN ('pg-ha-2','pg-ha-3')`, &streaming) == nil && streaming == 2
	}, 60*time.Second, 500*time.Millisecond, "both standbys stream under their target names")
	require.NoError(t, queryVia(ctx, rwAddr, "CREATE TABLE t (id int)"))
	require.NoError(t, queryVia(ctx, rwAddr, "INSERT INTO t SELECT generate_series(1, 100)"))

	out, err := exec.Command("docker", "kill", n1.c.GetContainerID()).CombinedOutput()
	require.NoError(t, err, string(out))
	killed := time.Now()

	var leader string
	require.Eventually(t, func() bool {
		leader = c.status().Leader
		return leader != "" && leader != "odoo-db"
	}, 15*time.Second, 100*time.Millisecond, "promotion within 15 s of the kill")
	t.Logf("promoted %s after %s", leader, time.Since(killed).Round(time.Millisecond))
	other := "pg-ha-2"
	if leader == "pg-ha-2" {
		other = "pg-ha-3"
	}

	require.Eventually(t, func() bool {
		return queryVia(ctx, rwAddr, "INSERT INTO t VALUES (101)") == nil
	}, 10*time.Second, 200*time.Millisecond, "writes through the RW endpoint succeed")
	var name string
	var inRecovery bool
	var count int
	require.NoError(t, queryVia(ctx, rwAddr, "SELECT current_setting('cluster_name'), pg_is_in_recovery(), (SELECT count(*) FROM t)", &name, &inRecovery, &count))
	require.Equal(t, leader, name)
	require.False(t, inRecovery)
	require.Equal(t, 101, count)

	require.Eventually(t, func() bool {
		st := c.status()
		for _, n := range st.Nodes {
			if n.Name == other {
				return n.Role == "standby" && n.Timeline == 2 && n.Upstream == leader
			}
		}
		return false
	}, 30*time.Second, 200*time.Millisecond, "the remaining standby follows the new timeline")
	require.Eventually(t, func() bool {
		var rows int
		return queryVia(ctx, roAddr, "SELECT current_setting('cluster_name'), pg_is_in_recovery(), (SELECT count(*) FROM t)", &name, &inRecovery, &rows) == nil &&
			name == other && inRecovery && rows == 101
	}, 30*time.Second, 200*time.Millisecond, "the RO endpoint serves the remaining standby with the new row")

	require.NoError(t, n1.c.Start(ctx))
	host, err := n1.c.Host(ctx)
	require.NoError(t, err)
	port, err := n1.c.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	stopLoop()
	n1.cfg.Addr = net.JoinHostPort(host, port.Port())
	c.nodes[0].Addr = n1.cfg.Addr
	stopLoop = startLoop()
	require.Eventually(t, func() bool {
		for _, n := range c.status().Nodes {
			if n.Name == "odoo-db" {
				return n.Role == "diverged_or_stale" && n.Timeline == 1
			}
		}
		return false
	}, 60*time.Second, 500*time.Millisecond, "the old primary comes back writable on timeline 1: diverged_or_stale")
	for range 5 {
		require.NoError(t, queryVia(ctx, rwAddr, "SELECT current_setting('cluster_name')", &name))
		require.Equal(t, leader, name, "never routed to the old primary")
	}
	var rec bool
	require.NoError(t, queryVia(ctx, n1.cfg.Addr, "SELECT pg_is_in_recovery()", &rec))
	require.False(t, rec, "never auto-rejoined")
}
