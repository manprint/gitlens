//go:build integration

package server

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	sharedPool      *pgxpool.Pool
	sharedContainer testcontainers.Container
	sharedOnce      sync.Once
	sharedErr       error
)

func getSharedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	sharedOnce.Do(func() {
		ctx := context.Background()
		c, err := postgres.Run(ctx, "postgres:17-alpine",
			postgres.WithDatabase("test"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("postgres"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2)),
		)
		if err != nil {
			sharedErr = fmt.Errorf("start postgres: %w", err)
			return
		}
		sharedContainer = c
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			sharedErr = err
			return
		}
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			sharedErr = err
			return
		}
		// create schema
		schema := `
CREATE TABLE IF NOT EXISTS tenants (tenant_id text PRIMARY KEY, name text NOT NULL);
INSERT INTO tenants VALUES ('default','Default') ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS agents (agent_id uuid PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', agent_version text, first_seen timestamptz NOT NULL DEFAULT now(), last_seen timestamptz NOT NULL DEFAULT now(), revoked_at timestamptz);
CREATE TABLE IF NOT EXISTS clusters (tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, name text, id_source text NOT NULL CHECK (id_source IN ('system_identifier','manual')), first_seen timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, cluster_id));
CREATE TABLE IF NOT EXISTS instances (instance_id uuid PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, agent_id uuid NOT NULL, addr text NOT NULL, port int NOT NULL, pg_version int NOT NULL, role text NOT NULL CHECK (role IN ('primary','standby','unknown')), perm_tier text NOT NULL, first_seen timestamptz NOT NULL DEFAULT now(), last_seen timestamptz NOT NULL DEFAULT now(), FOREIGN KEY (tenant_id, cluster_id) REFERENCES clusters(tenant_id, cluster_id));
CREATE INDEX IF NOT EXISTS instances_addr_idx ON instances (tenant_id, cluster_id, addr, port);
CREATE TABLE IF NOT EXISTS databases (instance_id uuid NOT NULL REFERENCES instances ON DELETE CASCADE, datname text NOT NULL, monitored bool NOT NULL, skip_reason text, last_seen timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (instance_id, datname));
CREATE TABLE IF NOT EXISTS query_texts (tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, datname text NOT NULL, queryid bigint NOT NULL, pg_major int NOT NULL, query_text text NOT NULL, first_seen timestamptz NOT NULL DEFAULT now(), last_seen timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, cluster_id, datname, queryid, pg_major));
CREATE TABLE IF NOT EXISTS events (event_id bigserial PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', ts timestamptz NOT NULL, type text NOT NULL, cluster_id bigint, instance_id uuid, payload jsonb NOT NULL DEFAULT '{}'::jsonb);
CREATE INDEX IF NOT EXISTS events_lookup_idx ON events (tenant_id, cluster_id, ts DESC);
CREATE TABLE IF NOT EXISTS metrics (ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL DEFAULT '', metric text NOT NULL, labels jsonb NOT NULL DEFAULT '{}'::jsonb, series_id bigint NOT NULL, value double precision NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS metrics_dedup_idx ON metrics (series_id, ts);
CREATE TABLE IF NOT EXISTS metrics_statements (ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL, queryid bigint NOT NULL, calls_rate double precision, exec_time_rate_ms double precision, rows_rate double precision, shared_blks_hit_rate double precision, shared_blks_read_rate double precision, wal_bytes_rate double precision);
CREATE UNIQUE INDEX IF NOT EXISTS metrics_statements_dedup_idx ON metrics_statements (instance_id, datname, queryid, ts);
CREATE TABLE IF NOT EXISTS metrics_ash (ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL DEFAULT '', wait_event_type text NOT NULL, wait_event text NOT NULL, state text NOT NULL, queryid bigint, samples int NOT NULL, window_seconds int NOT NULL, window_ticks int NOT NULL DEFAULT 0);
CREATE UNIQUE INDEX IF NOT EXISTS metrics_ash_dedup_idx ON metrics_ash (instance_id, datname, wait_event_type, wait_event, state, COALESCE(queryid,0), ts);
CREATE TABLE IF NOT EXISTS metrics_replication (ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, instance_id uuid NOT NULL, upstream_id uuid, slot_name text NOT NULL DEFAULT '', edge_type text NOT NULL DEFAULT 'streaming', sync_state text, write_lag_bytes bigint, flush_lag_bytes bigint, replay_lag_bytes bigint, write_lag_sec double precision, flush_lag_sec double precision, replay_lag_sec double precision, slot_active bool, slot_wal_status text, slot_retained_bytes bigint);
CREATE UNIQUE INDEX IF NOT EXISTS metrics_replication_dedup_idx ON metrics_replication (instance_id, COALESCE(upstream_id,'00000000-0000-0000-0000-000000000000'::uuid), slot_name, ts);
CREATE TABLE IF NOT EXISTS topology_edges (tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, from_instance uuid NOT NULL, to_instance uuid NOT NULL, edge_type text NOT NULL, sync_state text, confidence text NOT NULL DEFAULT 'high' CHECK (confidence IN ('high','low')), updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, cluster_id, from_instance, to_instance));
`
		if _, err := pool.Exec(ctx, schema); err != nil {
			sharedErr = fmt.Errorf("create schema: %w", err)
			pool.Close()
			return
		}
		sharedPool = pool
	})
	if sharedErr != nil {
		t.Fatalf("getSharedPool: %v", sharedErr)
	}
	if sharedPool == nil {
		t.Fatal("shared pool not initialized")
	}
	return sharedPool
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedPool != nil {
		sharedPool.Close()
	}
	if sharedContainer != nil {
		_ = sharedContainer.Terminate(context.Background())
	}
	os.Exit(code)
}
