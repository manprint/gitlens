-- store.Migrate() itself bootstraps this table before checking which
-- migrations are pending (it has to, in order to make that check at all on a
-- database that has never been migrated) — IF NOT EXISTS here keeps this
-- migration correct standalone without colliding with that bootstrap step.
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    int PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tenants (
  tenant_id text PRIMARY KEY,
  name      text NOT NULL
);
INSERT INTO tenants VALUES ('default', 'Default') ON CONFLICT DO NOTHING;

CREATE TABLE agents (
  agent_id     uuid PRIMARY KEY,
  tenant_id    text NOT NULL DEFAULT 'default' REFERENCES tenants,
  agent_version text,
  first_seen   timestamptz NOT NULL DEFAULT now(),
  last_seen    timestamptz NOT NULL DEFAULT now(),
  revoked_at   timestamptz
);

CREATE TABLE clusters (
  tenant_id  text   NOT NULL DEFAULT 'default' REFERENCES tenants,
  cluster_id bigint NOT NULL,   -- uint64 bit pattern; see clusterid.go
  name       text,
  id_source  text   NOT NULL CHECK (id_source IN ('system_identifier','manual')),
  first_seen timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, cluster_id)
);

CREATE TABLE instances (
  instance_id uuid PRIMARY KEY,
  tenant_id   text   NOT NULL DEFAULT 'default',
  cluster_id  bigint NOT NULL,   -- uint64 bit pattern
  agent_id    uuid   NOT NULL REFERENCES agents,
  addr        text   NOT NULL,
  port        int    NOT NULL,
  pg_version  int    NOT NULL,
  role        text   NOT NULL CHECK (role IN ('primary','standby','unknown')),
  perm_tier   text   NOT NULL,
  first_seen  timestamptz NOT NULL DEFAULT now(),
  last_seen   timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (tenant_id, cluster_id) REFERENCES clusters (tenant_id, cluster_id)
);
-- Detects the duplicate-instance case of SYS-AGENT-002: an agent that lost
-- its identity file re-registers the same address under a new UUID.
CREATE INDEX instances_addr_idx ON instances (tenant_id, cluster_id, addr, port);

CREATE TABLE databases (
  instance_id uuid NOT NULL REFERENCES instances ON DELETE CASCADE,
  datname     text NOT NULL,
  monitored   bool NOT NULL,
  skip_reason text,
  last_seen   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (instance_id, datname)
);

CREATE TABLE query_texts (
  tenant_id  text   NOT NULL DEFAULT 'default',
  cluster_id bigint NOT NULL,
  datname    text   NOT NULL,
  queryid    bigint NOT NULL,
  pg_major   int    NOT NULL,
  query_text text   NOT NULL,
  first_seen timestamptz NOT NULL DEFAULT now(),
  last_seen  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, cluster_id, datname, queryid, pg_major)
);

CREATE TABLE events (
  event_id    bigserial PRIMARY KEY,
  tenant_id   text        NOT NULL DEFAULT 'default',
  ts          timestamptz NOT NULL,
  type        text        NOT NULL,
  cluster_id  bigint,
  instance_id uuid,
  payload     jsonb       NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX events_lookup_idx ON events (tenant_id, cluster_id, ts DESC);

CREATE TABLE topology_edges (
  tenant_id     text   NOT NULL DEFAULT 'default',
  cluster_id    bigint NOT NULL,
  from_instance uuid   NOT NULL,
  to_instance   uuid   NOT NULL,
  edge_type     text   NOT NULL,
  sync_state    text,
  confidence    text   NOT NULL DEFAULT 'high' CHECK (confidence IN ('high','low')),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, cluster_id, from_instance, to_instance)
);
