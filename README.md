# pglens

pglens is a self-hostable monitoring system for fleets of PostgreSQL instances, focused on replication and wait-event analysis. It is designed for DBAs and platform teams running self-hosted or managed PostgreSQL.

## Project status

This project is pre-alpha. The foundations (types, clock, identity, delta, cardinality, wire envelope, server schema) are implemented and tested. The agent (connections, scheduler, buffer, push), L3 harness, replication topology, and ASH are scaffolded as stubs and will be completed in the next iteration (see `docs/plans/001_plan-Foundations/`). The current release builds two binaries (`pglens-agent` and `pglens-server`); the agent requires implementation to collect, the server accepts pushes on `/api/v1/push`.

**Phase 5 (L3 E2E test harness)** ships test infrastructure and scenarios only — no new user-visible product behavior is added in this phase. It provides the foundation for proving system-level correctness through controlled failure injection, workload generation, and global invariant checking.

## Requirements

- Go 1.26 or later to build
- Docker and Docker Compose for the test suites and for the server's storage
- PostgreSQL 15 to 18 as monitoring targets
- PostgreSQL 17 with TimescaleDB 2.29 for the server's own storage (see Running the server)

## Building

```sh
make build
```

This produces `bin/pglens-agent` and `bin/pglens-server`.

```sh
make build-images          # builds ghcr.io/manprint/pglens-agent:dev and ...-server:dev
make build-images-multiarch # linux/amd64 + linux/arm64 via docker buildx
```

## Quick start

Three commands from nothing to data (requires Docker and a target PostgreSQL 15+ instance):

```sh
# 1. Create the monitoring role on each target PostgreSQL instance (as superuser)
psql -U postgres -h <target-host> -v pw="'<password>'" -f deploy/sql/monitoring_user.sql

# 2. Configure the agent and start the stack
# Edit deploy/agent.example.yaml with your target DSN, or use environment variables:
PGLENS_SERVER_URL=http://localhost:8080 \
PGLENS_BOOTSTRAP_TOKEN=dev-token \
docker compose -f deploy/compose/docker-compose.yml up -d

# 3. Check that data is appearing
curl -s localhost:8080/healthz | jq .
curl -s localhost:8080/api/v1/clusters | jq .
```

Output from `healthz`:
```json
{
  "state": "ok",
  "last_successful_push": "2026-08-27T02:00:00Z",
  "buffer": {"bytes_used": 1234567, "segments": 2, "samples_dropped": 0},
  "clock_skew_seconds": 0.5
}
```

Output from `/api/v1/clusters` (first cluster):
```json
{
  "cluster_id": "7381927364512345678",
  "name": "pg-prod-eu",
  "id_source": "system_identifier",
  "primary": "11111111-1111-1111-1111-111111111111",
  "instance_count": 1,
  "health": "ok",
  "standby_count": 0,
  "max_replay_lag_seconds": null,
  "topology": [],
  "instances": [
    {
      "instance_id": "11111111-1111-1111-1111-111111111111",
      "addr": "pg-app", "port": 5432,
      "role": "primary", "pg_version": 170011,
      "perm_tier": "T0", "last_seen": "2026-08-27T02:00:00Z", "up": true
    }
  ]
}
```

The agent discovers its databases, respects the connection ceiling (default 3 per instance), buffers through outages, and reports on `localhost:8080`. See Running the agent and Agent configuration below.

## Setting up the monitoring role

The monitoring user is created by `deploy/sql/monitoring_user.sql`. It creates a role `pglens` with `pg_monitor` and the two grants required for a stable cluster identity.

Run it as a superuser (or as `rds_superuser` on Amazon RDS):

```sh
psql -v pw="'<password>'" -f deploy/sql/monitoring_user.sql
```

The two `GRANT EXECUTE ON FUNCTION pg_control_system()` and `pg_control_checkpoint()` are required because these functions are not covered by `pg_monitor`. Without them the cluster identity falls back to the configured `cluster_name` and a failover can split one cluster into two (see Monitoring a replicated cluster).

The script leaves two optional grants commented out:

- `GRANT pg_read_all_data TO pglens` — enables `EXPLAIN` without `ANALYZE` (tier T1)
- `GRANT pg_signal_backend TO pglens` — enables cancelling queries from the UI (tier T2)

Uncomment them only if you need those features.

## Running the server

**Docker Compose (recommended):**

```sh
docker compose -f deploy/compose/docker-compose.yml up -d
# or the minimal TimescaleDB service
docker compose -f deploy/compose/timescaledb.yml up -d
```

The server applies migrations on startup (forward-only, idempotent) and listens on `:8080`. See `deploy/compose/docker-compose.yml` for the pinned image `timescale/timescaledb:2.29.0-pg17`.

**Binary:**

```sh
PGLENS_DSN=postgres://pglens:pglens@localhost:5432/pglens?sslmode=disable \
PGLENS_BOOTSTRAP_TOKEN=dev-token \
PGLENS_LISTEN=:8080 \
./bin/pglens-server
```

`/healthz` returns 200 once listening; `/readyz` returns 200 when migrations are applied and the pool answers `SELECT 1`. Graceful shutdown on `SIGTERM` with 15s drain.

### Configuration

| Env | Type | Default | Meaning |
|-----|------|---------|---------|
| `PGLENS_DSN` | DSN | — | TimescaleDB connection (required) |
| `PGLENS_LISTEN` | `host:port` | `:8080` | HTTP listen address |
| `PGLENS_BOOTSTRAP_TOKEN` | string | — | shared secret for agent auth |
| `PGLENS_BOOTSTRAP_TOKEN_FILE` | path | — | file containing the token (trailing newline trimmed) |
| `PGLENS_ALERT_INTERVAL` | duration | `30s` | alert evaluation interval |
| `PGLENS_SLACK_WEBHOOK_URL` | URL | unset | enables Slack notifications |
| `PGLENS_SLACK_WEBHOOK_URL_FILE` | path | unset | reads the Slack URL; takes precedence over the inline URL |
| `PGLENS_WEBHOOK_URL` | URL | unset | enables generic webhook notifications |

The bootstrap token is a shared secret; this release has no token rotation, no mTLS, and no approval queue. Revocation is supported: `UPDATE agents SET revoked_at = now()` makes the next push return 401.

## Alerting

The server evaluates ten built-in Tier 0 rules: `agent_down`, `instance_unreachable`, `check_failing`, `no_primary_in_cluster`, `agent_buffer_full`, `clock_skew`, `cardinality_budget_exceeded`, `failover_detected`, `split_brain_detected`, and `slot_inactive`. Tier 0 rules are always enabled. Tier 1 rules are editable through the alert-rules endpoint.

Configure Slack or a generic webhook with the variables above. A silence suppresses notification while the alert remains visible and continues to be evaluated.

```sh
curl -s http://localhost:8080/api/v1/alerts | jq .
curl -s -X POST http://localhost:8080/api/v1/silences \
  -H 'Content-Type: application/json' \
  -d '{"matchers":[{"name":"severity","value":"warning"}],"reason":"maintenance","starts_at":"2026-08-29T10:00:00Z","ends_at":"2026-08-29T11:00:00Z"}'
curl -s http://localhost:8080/api/v1/alert-rules | jq .
curl -s -X DELETE http://localhost:8080/api/v1/silences/<silence-id>
```

An alert listing contains objects such as `{"alert_key":"agent_down/...","state":"firing","severity":"critical","cluster_id":"7381927364512345678","suppressed":false}`. The API returns cluster identifiers as strings and timestamps in RFC 3339 format.

Alerting delivers only to Slack and generic webhooks. Email and PagerDuty are not supported.

## Running the agent

The agent collects from each configured target, buffers samples to disk, and pushes to the server every 15 seconds (default). It stores its identity on disk and must run with a persistent `/var/lib/pglens` directory — without it, each container restart creates a new instance ID and duplicates the instance.

### Container (Docker)

```sh
docker run -d --name pglens-agent \
  -v agent-data:/var/lib/pglens \
  -v /proc:/host/proc:ro -v /sys:/host/sys:ro \
  -e PGLENS_SERVER_URL=http://pglens-server:8080 \
  -e PGLENS_BOOTSTRAP_TOKEN=dev-token \
  ghcr.io/manprint/pglens-agent:dev --config /etc/pglens/agent.yaml
```

**Persistent volume explanation:** The `-v agent-data:/var/lib/pglens` flag mounts a named volume at `/var/lib/pglens`. This directory holds `identity.json`, which contains the agent's instance UUID. **Without this persistent mount:**
- The agent generates a new UUID on every container start
- Each restart is reported as a different instance, leading to `duplicate_instance_suspected` events
- Instance and replication data appear fragmented across multiple "instances" with the same `addr:port`

**Always mount the volume** in production. Even in development, use a named volume rather than `--rm` to preserve identity across restarts.

### Docker Compose

Compose fragment for use in `docker-compose.yml`:

```yaml
services:
  # ... pglens-server, timescaledb, ...

  pglens-agent:
    image: ghcr.io/manprint/pglens-agent:dev
    depends_on: [pglens-server]
    volumes:
      - agent-data:/var/lib/pglens      # REQUIRED: persistent volume for identity.json
      - /proc:/host/proc:ro              # optional: for better host introspection
      - /sys:/host/sys:ro                # optional: for better host introspection
    environment:
      PGLENS_SERVER_URL: http://pglens-server:8080
      PGLENS_BOOTSTRAP_TOKEN: dev-token
      # OR mount the config file: 
      # PGLENS_CONFIG: /etc/pglens/agent.yaml
    healthcheck:
      test: ["CMD", "/usr/local/bin/pglens-agent", "check", "--dsn", "postgres://..."]
      interval: 30s
      timeout: 10s
      retries: 3

volumes:
  agent-data:                           # named volume persists across restarts
```

### Binary with systemd

Install and run as a system service on Linux:

```sh
# Install the binary
sudo cp ./bin/pglens-agent /usr/local/bin/pglens-agent

# Create the config
sudo vi /etc/pglens/agent.yaml

# Install the systemd unit
sudo cp deploy/systemd/pglens-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable pglens-agent
sudo systemctl start pglens-agent
```

The systemd unit (`deploy/systemd/pglens-agent.service`) provides:
- `StateDirectory=pglens` — creates `/var/lib/pglens` with proper permissions for the `pglens` user
- `DynamicUser=yes` — runs as a non-root dynamically allocated user (no manual account creation needed)
- `ProtectSystem=strict` — filesystem read-only outside `/var/lib/pglens` and `/var/tmp`
- `NoNewPrivileges=yes` — prevents privilege escalation
- `Restart=on-failure` — automatic restart if the service crashes

Check status:
```sh
sudo systemctl status pglens-agent
sudo journalctl -u pglens-agent -n 50  # last 50 lines of logs
```

### Agent configuration

Annotated `deploy/agent.example.yaml`:

```yaml
# Server connection (required)
server:
  url: http://pglens-server:8080     # string: HTTP URL to pglens server (required)
  token: dev-token                   # string: bootstrap token (required unless token_file is set)
  token_file: /run/secrets/pglens    # string: path to file containing token; overrides token field
  
# Identity persistence (required)
identity_path: /var/lib/pglens/identity.json  # string: path to identity file (must be on persistent mount)

# Push scheduling (optional)
push_interval: 15s                   # duration: time between envelope deliveries (default 15s)

# Disk buffer configuration (required)
buffer:
  path: /var/lib/pglens/buffer       # string: directory for buffer segments (required)
  max_size: 512MiB                   # size: maximum buffer size before deleting oldest segments (default 512MiB)
  max_age: 6h                        # duration: maximum segment age before deletion (default 6h)

# Monitoring targets (required, at least one)
targets:
  - name: pg-app                     # string: identifier for this target (required)
    dsn: postgres://pglens@pg-app:5432/postgres?sslmode=require
                                     # string: PostgreSQL connection DSN (required; password never logged)
    cluster_name: pg-prod-eu         # string: fallback cluster identity if pg_control_system() unavailable
    databases:
      include: ["app_.*"]            # string array: regex patterns to include (default empty = all)
      exclude: ["^template\\d$", "^rdsadmin$", "^azure_.*$"]
                                     # string array: regex patterns to exclude (default excludes templates and managed dbs)
      max: 10                        # integer: max databases per instance to monitor (default 10; max 10)

# Per-check configuration overrides (optional)
checks:
  instance_info:    { interval: 60s }        # General instance metadata (mandatory)
  activity:        { interval: 10s }        # Session activity and lock info
  database_stats:  { interval: 30s }        # Per-database counters and size
  stat_statements: { interval: 60s, top_n: 50 }  # Top N queries (requires pg_stat_statements)
  ash:             { interval: 1s }         # Activity sampling (ASH); lower for sensitive instances
  replication_streaming:   { interval: 10s }  # Replication lag from primary
  replication_receiver:    { interval: 10s }  # Replication status on standby
  replication_slots:       { interval: 15s }  # Replication slot status
```

**Configuration rules:**
- Unknown keys are rejected (strict YAML validation) — a typo silently leaves an instance unwatched; the parser catches this
- `server.url` and either `server.token` or `server.token_file` are required
- `identity_path` must be on a persistent mount (not container root); use `pglens-agent check` to verify
- `targets` must be non-empty; each target requires `name` and `dsn`
- Duration format: `10s`, `1m`, `1h` (Go time.ParseDuration syntax)
- Size format: `512MiB`, `1GiB` (parsed by Go's time/humanize)

**Environment overrides** (take precedence over the config file's own value):
- `PGLENS_SERVER_URL=http://...` — overrides `server.url`
- `PGLENS_BOOTSTRAP_TOKEN=...` — overrides `server.token`
- `PGLENS_BOOTSTRAP_TOKEN_FILE=/path/to/token` — overrides `server.token_file`
- `PGLENS_IDENTITY_PATH=/path/to/identity.json` — overrides `identity_path`
- `PGLENS_CONFIG=/path/to/agent.yaml` — path to the config file itself (default `/etc/pglens/agent.yaml`); the `--config` flag on `pglens-agent run` takes precedence over this
- `PGLENS_HEALTHZ_LISTEN=:9187` — bind address for the agent's own `/healthz` endpoint (default `:9187`); change this if running more than one agent on the same host

**Security notes:**
- The DSN password is never logged at any level
- Unknown keys in YAML cause a validation error, not a silent skip
- The `identity_path` must be on a persistent volume; without it, each container restart creates a new instance ID

### `pglens-agent check`

Diagnostic command to verify database connectivity and readiness for collection. Run this first when debugging why data is not appearing. Exit code 0 when every mandatory check is enabled, 1 otherwise (usable in provisioning scripts).

```sh
./bin/pglens-agent check --dsn postgres://pglens:password@host:5432/postgres
```

Sample output from a fully configured PostgreSQL 17 instance with `pg_monitor`:

```
pglens agent check
  server version   : 17.11  (server_version_num 170011) — supported
  role             : primary
  system_identifier: 7678730985106710562
  permission tier  : T0 (pg_monitor)
  extensions       : pg_stat_statements=no  pg_buffercache=no
  databases        : 1 found, 1 monitored

checks
  instance_info      enabled
  activity           enabled
  ash                enabled
  database_stats     enabled
  replication_receiver disabled  — role primary not allowed
  replication_slots  enabled
  replication_streaming enabled
  stat_statements    disabled  — missing extension pg_stat_statements

warnings
  - identity path /var/lib/pglens/identity.json is not on a persistent mount:
        instance ids will change on restart and instances will be duplicated
```

**How to read the output:**

- **server version** — PostgreSQL version; `— NOT SUPPORTED` means it's older than 15 (EOL versions 13, 14 not supported)
- **role** — `primary`, `standby`, or `read-only replica`; determines which replication checks run
- **system_identifier** — stable cluster identity; if it says `N/A (requires pg_control_system privilege)`, you need the grant from `monitoring_user.sql`
- **permission tier**:
  - `T0 (pg_monitor)` — minimum, enables all core monitoring
  - `T1 (pg_monitor + pg_read_all_data)` — enables `EXPLAIN` without `ANALYZE` in the UI
  - `T2 (pg_monitor + pg_read_all_data + pg_signal_backend)` — enables query cancellation
- **extensions** — `pg_stat_statements=yes` is required for the `stat_statements` check; `pg_buffercache` is optional
- **databases** — number of databases found and how many will be monitored; if some are skipped due to `db_budget`, the reason is shown

**Mandatory checks** (must be enabled for exit code 0):
- `instance_info` — general metadata
- `activity` — session activity
- `database_stats` — per-database counters

**Exit codes:**
- `0` — all mandatory checks enabled; safe to deploy this target
- `1` — one or more mandatory checks disabled; fix the permission tier or configuration before deploying

**Warnings:**
- The persistent-mount warning indicates that `/var/lib/pglens` is not on a persistent volume. This is the #1 container misconfiguration and causes instance duplication on restart. Always mount the identity directory on a persistent volume (e.g., `docker run -v agent-data:/var/lib/pglens ...` or in compose: `volumes: ["agent-data:/var/lib/pglens"]`).

## HTTP API

All responses carry `cluster_id` as a **decimal string** — `uint64` exceeds IEEE-754 exact integer range and would be rounded by `jq` or any JS consumer. Missing intervals are `null`, never `0` or interpolated.

### `GET /api/v1/clusters`

```sh
curl -s localhost:8080/api/v1/clusters | jq '.[0]'
```

```json
{
  "cluster_id": "7381927364512345678",
  "name": "pg-prod-eu",
  "id_source": "system_identifier",
  "primary": "11111111-1111-1111-1111-111111111111",
  "instance_count": 2,
  "health": "ok",
  "standby_count": 1,
  "sync_standby_count": 0,
  "max_replay_lag_seconds": 0.12,
  "topology": [
    {
      "from": "11111111-1111-1111-1111-111111111111",
      "to": "22222222-2222-2222-2222-222222222222",
      "type": "streaming",
      "sync_state": "async",
      "confidence": "high"
    }
  ],
  "instances": [
    {
      "instance_id": "11111111-1111-1111-1111-111111111111",
      "addr": "pg-primary",
      "port": 5432,
      "role": "primary",
      "pg_version": 170006,
      "perm_tier": "T0",
      "last_seen": "2026-08-27T02:00:00Z",
      "up": true
    },
    {
      "instance_id": "22222222-2222-2222-2222-222222222222",
      "addr": "pg-standby",
      "port": 5432,
      "role": "standby",
      "pg_version": 170006,
      "perm_tier": "T0",
      "last_seen": "2026-08-27T02:00:00Z",
      "up": true
    }
  ]
}
```

**Fields explained:**
- `health` is one of:
  - `ok` — primary exists, all instances up, no replication lag
  - `degraded` — primary exists but at least one instance is down OR any replica has measurable replay lag (`max_replay_lag_seconds > 0`)
  - `critical` — no primary found in the cluster, **or more than one** (split-brain)
- `standby_count` — number of instances in standby role (omitted if zero)
- `sync_standby_count` — number of synchronous replicas per `sync_state` (omitted if zero)
- `max_replay_lag_seconds` — largest replay lag from all active standby replicas; omitted if no lag data or all are caught up
- `topology` — replication graph edges (omitted if empty); see below
- Low-confidence edges (those not matched to a known instance) carry `"note": "Endpoint could not be resolved; check connectivity"`

### `GET /api/v1/clusters/{id}/topology`

```sh
curl -s localhost:8080/api/v1/clusters/7381927364512345678/topology | jq .
```

```json
{
  "cluster_id": "7381927364512345678",
  "topology": [
    {
      "from": "11111111-1111-1111-1111-111111111111",
      "to": "22222222-2222-2222-2222-222222222222",
      "type": "streaming",
      "sync_state": "async",
      "confidence": "high"
    }
  ],
  "events": [
    {
      "event_id": 1,
      "ts": "2026-08-27T01:45:23Z",
      "type": "failover_detected",
      "cluster_id": "7381927364512345678",
      "instance_id": "22222222-2222-2222-2222-222222222222",
      "payload": {
        "old_primary": "11111111-1111-1111-1111-111111111111",
        "new_primary": "22222222-2222-2222-2222-222222222222"
      }
    }
  ]
}
```

**Topology edges:**
- `from`, `to` — instance IDs (UUIDs as strings)
- `type` — replication type; currently always `streaming` for streaming replication
- `sync_state` — `sync` (synchronous replica) or `async` (asynchronous); null if not yet known
- `confidence` — `high` (matched to a known instance) or `low` (upstream IP/port could not be resolved to a known instance); low-confidence edges carry a `note` field

**Events:**
- Listed here are `failover_detected` events only (see Events section for the complete event taxonomy)

### `GET /api/v1/clusters/{id}/replication`

```sh
curl -s "localhost:8080/api/v1/clusters/7381927364512345678/replication?from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z" | jq .
```

```json
{
  "cluster_id": "7381927364512345678",
  "edges": [
    {
      "from": "11111111-1111-1111-1111-111111111111",
      "to": "22222222-2222-2222-2222-222222222222",
      "metric": "replay_lag_sec",
      "sync_state": "async",
      "series": [
        {"ts": "2026-08-27T00:00:00Z", "value": 0.0},
        {"ts": "2026-08-27T00:01:00Z", "value": null},
        {"ts": "2026-08-27T00:02:00Z", "value": 0.05}
      ]
    },
    {
      "from": "11111111-1111-1111-1111-111111111111",
      "to": "22222222-2222-2222-2222-222222222222",
      "metric": "write_lag_sec",
      "sync_state": "async",
      "series": [
        {"ts": "2026-08-27T00:00:00Z", "value": 0.001},
        {"ts": "2026-08-27T00:01:00Z", "value": null},
        {"ts": "2026-08-27T00:02:00Z", "value": 0.002}
      ]
    }
  ]
}
```

**Time range query parameters:** `from` and `to` are required, in RFC3339 format (e.g., `2026-08-27T00:00:00Z`). Missing intervals are `null`, never `0` or interpolated. The response includes three series per edge: `write_lag_sec`, `flush_lag_sec`, and `replay_lag_sec` (bytes counterparts are available in metrics but shown here in seconds for readability).

### `GET /api/v1/instances/{id}`

```sh
curl -s localhost:8080/api/v1/instances/11111111-1111-1111-1111-111111111111 | jq .
```

Returns the instance plus its databases with `monitored`, `skip_reason`, and `databases_not_monitored` count.

### `GET /api/v1/metrics/query`

```sh
curl -s "localhost:8080/api/v1/metrics/query?metric=pg_backends&instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z&step=60s" | jq .
```

```json
{
  "series": [
    {"ts": "2026-08-27T00:00:00Z", "value": 12},
    {"ts": "2026-08-27T00:01:00Z", "value": null},
    {"ts": "2026-08-27T00:02:00Z", "value": 15}
  ]
}
```

A counter reset yields `null` for that bucket, never a negative or a spike above 10x the pre-reset rate. `step` producing >10000 points returns 422.

### `GET /api/v1/events`

```sh
curl -s "localhost:8080/api/v1/events?cluster_id=7381927364512345678&type=failover_detected&limit=10" | jq .
```

Newest first, `limit` capped at 1000.

### `GET /api/v1/statements`

```sh
curl -s "localhost:8080/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111&database=app&from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z&order_by=total_exec_time&limit=20" | jq .
```

Returns top queries joined to `query_texts`, with `truncated` and `comparable_scope: "cluster"` (`queryid` is comparable only within one cluster, not across clusters or major versions).

### `GET /api/v1/ash` and `/api/v1/ash/top`

Grouped by wait event:

```sh
curl -s "localhost:8080/api/v1/ash?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T00:30:00Z&group_by=wait_event_type" | jq .
```

```json
{
  "resolution_seconds": 1,
  "statistical": true,
  "buckets": [
    {
      "ts": "2026-08-27T00:00:00Z",
      "wait_event_type": "CPU",
      "samples": 42,
      "ticks": 10,
      "avg_active_sessions": 4.2
    },
    {
      "ts": "2026-08-27T00:00:00Z",
      "wait_event_type": "Lock",
      "samples": 8,
      "ticks": 10,
      "avg_active_sessions": 0.8
    }
  ]
}
```

Grouped by query (requires `compute_query_id = on`):

```sh
curl -s "localhost:8080/api/v1/ash?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T00:30:00Z&group_by=queryid" | jq .
```

Returns `queryid` grouped rows; use `/api/v1/ash/top` for queries joined to their text:

```sh
curl -s "localhost:8080/api/v1/ash/top?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T00:30:00Z" | jq .
```

```json
{
  "resolution_seconds": 1,
  "statistical": true,
  "entries": [
    {
      "queryid": 1234567890,
      "query_text": "SELECT * FROM users WHERE id = $1",
      "samples": 45,
      "ticks": 10,
      "avg_active_sessions": 4.5
    }
  ]
}
```

Notes: `avg_active_sessions = samples / ticks`; when `ticks == 0`, `avg_active_sessions` is `null` (never division by zero). When `ash.enabled: false` (in agent configuration), the response is `{"enabled": false}` rather than an ambiguous empty result. Fewer than 60 total samples in the range returns a `"warning"` field.

### `POST /api/v1/push`

Agent ingest. `401` when the agent is revoked; `400` for unknown `protocol_version` (body names supported version) or stale `sent_at` (>12h); `413` for body >32 MiB.

### `GET /metrics`

Prometheus-format metrics exposition (port 8080). Exposes server-side monitoring metrics:
- `pglens_up` — gauge, 1 if an agent is currently reachable, 0 otherwise (by `instance_id`)
- `pglens_agent_last_seen_seconds` — gauge, unix timestamp of the last accepted push (by `instance_id`)
- `pglens_series_total` — gauge, distinct metric series tracked per instance (used to monitor cardinality budget compliance)
- `pglens_ingest_envelopes_total` — counter, envelopes accepted by `/api/v1/push` (by `result`)
- `pglens_ingest_rejected_total` — counter, envelopes rejected by `/api/v1/push` (by `reason`)
- `pglens_check_error_total` — counter, check scrapes that reported an error (by `check` name)
- `pglens_samples_too_old_total` — counter, samples rejected for exceeding max age (12 hours)
- `pglens_cardinality_truncated_total` — counter, envelopes where cardinality budgets caused truncation
- `pglens_agent_clock_skew_seconds` — gauge, most recently observed agent/server clock skew (useful for diagnosing timestamp misalignment)

```sh
curl -s localhost:8080/metrics | head -20
```

Use this endpoint to monitor pglens itself — feed it into Prometheus, Datadog, or your favorite metrics backend.

### `GET /healthz`, `/readyz`

```sh
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz
```

## Monitoring a replicated cluster

**Setup:** Point the agent at each instance (primary and all standbys) in your configuration. Instances are grouped into a cluster automatically by `system_identifier`, which is read from `pg_control_system()` during each agent check. No manual clustering is needed.

### How cluster identity is determined

The agent reports a `system_identifier` from each instance, which is stable across:
- Failover (promote a standby to primary)
- Rename or reconfiguration of the server
- IP address changes
- Restart of the database

This stable identifier is how pglens proves that a promoted standby is the same cluster, not a new one — the `cluster_id` remains byte-identical across the failover event. **Invariant I-1 of the project:** a cluster identity never changes, even after a promote.

### The `pg_control_system()` grant

The `system_identifier` is not covered by `pg_monitor` and requires an explicit grant:

```sql
GRANT EXECUTE ON FUNCTION pg_control_system() TO pglens;
```

This is applied by `deploy/sql/monitoring_user.sql`. **Without it, the agent cannot read `system_identifier` and falls back to the configured `cluster_name` as the identity.** If you then promote a standby, the primary and standby will report different `cluster_name` values, causing the system to see them as two separate clusters — `cluster_id` will appear to change.

**Best practice:** always include the `pg_control_system()` grant. If you cannot grant it (e.g., on some managed PostgreSQL services), explicitly set `cluster_name` to the same value on all instances in the cluster and ensure it survives failover.

### What the agent does

On each target instance, the agent runs:

1. **Replication streaming check** (primary only) — every 10 seconds
   - Reports write, flush, and replay lag (in bytes and seconds) for each connected standby
   - Shows standby state and sync mode (synchronous or asynchronous)
   - Empty result if no standbys are connected; lag values are `null` until the standby has replayed at least once

2. **Replication receiver check** (standby only) — every 10 seconds
   - Reports the connection status to the primary (`streaming`, `catchup`, `disconnected`)
   - Shows the sender's host and port
   - Reports replay lag; reads `0` on an idle primary (no lag) rather than drifting upward

3. **Replication slots check** (both primary and standby) — every 15 seconds
   - Reports each slot's state, WAL status, and how many bytes of WAL are being retained
   - Detects inactive slots (standby crashed or disconnected) and growing retention

These checks are only emitted when applicable (e.g., replication streaming only runs on the primary), so you will see a `replication_*` section under `checks` that says `disabled` when a check is not applicable to the instance's current role. After `pg_ctl promote`, a standby becomes a primary and the replication checks automatically switch roles.

### Viewing replication state

**Cluster overview:**
```sh
curl -s localhost:8080/api/v1/clusters | jq '.[] | {name, primary, standby_count, max_replay_lag_seconds, health}'
```

**Topology graph and failover history:**
```sh
curl -s localhost:8080/api/v1/clusters/7381927364512345678/topology | jq .
```

**Replication lag over time (e.g., last hour):**
```sh
curl -s "localhost:8080/api/v1/clusters/7381927364512345678/replication?from=$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)&to=$(date -u +%Y-%m-%dT%H:%M:%SZ)" | jq '.edges[] | select(.metric=="replay_lag_sec")'
```

The `cluster_id` returned by these endpoints is byte-identical before and after a failover or promote, proving invariant I-1 is honored.

## Wait-event analysis

What it answers: where the database is spending its time, right now and historically. It samples `pg_stat_activity` once per second, aggregates into 10-second windows, and stores the sample counts by wait event. No extension or restart required — it works on managed PostgreSQL including Amazon RDS.

`compute_query_id = on` is strongly recommended. Without it, samples cannot be attributed to a query and ASH loses much of its value. The server logs a warning once if `compute_query_id` is off.

Query by wait event type:

```sh
curl -s "localhost:8080/api/v1/ash?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z&group_by=wait_event_type" | jq .
```

Query by query (requires `compute_query_id = on`):

```sh
curl -s "localhost:8080/api/v1/ash/top?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z" | jq .
```

Configuration: `ash.interval` (default `1s`). Lower the interval for sensitive instances. See Agent configuration above for `checks.ash.interval`.

## Events

Events are the system's way of notifying you of changes in replication topology and cluster health. Access them via `GET /api/v1/events?cluster_id=...&type=...`.

| Type | What it means | What to check |
|------|---------------|---------------|
| **Failover & topology** | |
| `failover_detected` | a standby became primary while another instance held primary within the last 120 seconds | Payload has `old_primary` and `new_primary` instance IDs; check if this was planned (e.g., a controlled switchover) or unplanned (e.g., primary crash requiring failover) |
| `split_brain_detected` | two or more instances are reporting primary role in the same cluster, outside the 30-second grace window after a failover | This is a serious condition: network partition or a manual promote without stopping the old primary. Investigate which instance is the authoritative primary and demote the other. |
| `orphan_standby` | a standby's upstream cannot be resolved for more than 60 seconds | Primary is down but the standby has not been promoted. Either the primary is recovering, or you need to promote the standby and reconnect it. |
| `role_change` | any instance changed role (except failovers, which emit `failover_detected` instead) | Expected during planned promotion or recovery. Check the instance's new role via `/api/v1/clusters`. |
| **Replication slots** | |
| `slot_inactive` | a replication slot has been inactive (not actively feeding a standby) for the configured window | Standby is down or disconnected. Check if the standby needs to be restarted or reconnected. |
| `slot_retained_bytes` | a replication slot is retaining an unusual amount of WAL | Standby is lagging or disconnected; WAL is accumulating. Monitor disk usage; if it grows unbounded, the primary can run out of disk. |
| **Cluster health** | |
| `no_primary_in_cluster` | no instance in the cluster is reporting primary role for more than 60 seconds | Critical: the cluster has no leader. Investigate why all instances went down or why none are reporting as primary. Promote a standby if needed. |
| **Other** | |
| `counter_reset_detected` | a counter (e.g., transaction or connection count) went backwards or `pg_stat_reset()` was called | Expected after `pg_stat_reset()` or an unexpected instance restart; not usually a sign of a problem. |
| `agent_down` | the agent has not reported for more than 3× its check interval | Agent container crashed, network unreachable, or server unreachable. Check agent logs and connectivity. |
| `agent_up` | the agent resumed reporting after being down | Recovery; check that the instance is operating normally. |
| `check_circuit_open` | 3 consecutive failures of a single check (e.g., permission denied on a replication query) | Permission issue, query timeout, or transient database problem. Check agent logs and the instance's error log. |
| `duplicate_instance_suspected` | the agent is generating a different `instance_id` for the same `addr:port` on restart | The agent lost its persistent identity. Verify the container has a persistent volume at `/var/lib/pglens` (in Docker) or the systemd service's `StateDirectory` (in systemd). Without persistence, the agent regenerates its identity on restart and instances are duplicated. |
| `cluster_id_changed` | an envelope from an agent claimed a different `cluster_id` for the same `system_identifier` | This should never happen under normal circumstances. It indicates a violation of invariant I-1 (cluster identity must never change). The system rejects such envelopes. If you see this, review the agent logs and the instance's `system_identifier`. |

## Running the checks

```sh
make fmt-check        # verify formatting (gofmt -l must be empty)
make lint             # run golangci-lint
make test             # unit tests with -race -shuffle=on
make coverage-gate    # enforce per-package and global coverage floors
make test-integration # integration tests against real PostgreSQL (requires Docker)
make test-e2e         # E2E smoke subset (requires Docker, ~10m)
make test-e2e-full    # full E2E suite (requires Docker, ~60m)
```

**L3 E2E tests** (L1 unit, L2 integration, L3 E2E system)

L3 requires Docker and the following host ports:
- Port `8080` (server HTTP)
- Ports `5432+` (PostgreSQL instances, typically 5432-5435)
- Port `8474` (Toxiproxy fault injection)

`make build-images` must precede any L3 run. Each test uses a unique compose project name and dynamically allocated ports, so concurrent runs and cleanup are safe.

L2 tests inside one container run serially by design (`postgres.Restore()` needs exclusive access); parallelism comes from the version matrix only. `make test-e2e` uses `-count=1` (cached E2E pass is a lie).

## Troubleshooting

**No data appearing in the API**

First, run the diagnostic check:
```sh
./bin/pglens-agent check --dsn postgres://pglens:password@host:5432/postgres
```

If the check exits 0 and all mandatory checks show `enabled`, the target is configured correctly. The issue is either:
1. Agent not running — verify container is up (`docker ps`) or systemd service is active (`systemctl status pglens-agent`)
2. Agent not connecting to server — check network connectivity and `PGLENS_SERVER_URL`
3. Token mismatch — ensure `PGLENS_BOOTSTRAP_TOKEN` on agent matches the server's token
4. Server not accepting pushes — check server logs for errors on `/api/v1/push`

**`permission denied` on `pg_control_system()`**

The monitoring role needs an explicit grant that `pg_monitor` does not cover. As superuser on the target:

```sql
GRANT EXECUTE ON FUNCTION pg_control_system() TO pglens;
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens;
```

Or re-run the setup script:
```sh
psql -U postgres -v pw="'mypassword'" -f deploy/sql/monitoring_user.sql
```

Without this grant, the agent falls back to `cluster_name` for cluster identity, and a failover can split the cluster into two in the UI.

**Agent reports `revoked` and stops collecting**

The agent has been explicitly revoked in the server. Check the database:

```sql
SELECT agent_id, revoked_at FROM agents WHERE revoked_at IS NOT NULL;
```

To re-enroll:
```sql
UPDATE agents SET revoked_at = NULL WHERE agent_id = '...';
```

The agent will resume after the next health check (default every push interval, ~15 seconds). Do not exit the agent container/service — the `revoked` state is visible in logs and health checks; restarting just delays visibility.

**Ingest returns HTTP 400 for an unsupported protocol**

A response such as `{"error":"unsupported protocol_version 3, supported range 1-2"}` means the agent is newer than the server. Upgrade the server first, then retry the agent.

**Databases missing or showing `skip_reason=db_budget`**

By default, the agent monitors the 10 most active databases per instance (measured by `xact_commit`). If you have more databases:

Option 1: Increase the budget in the config:
```yaml
targets:
  - name: my-target
    databases:
      max: 20                    # raise from default 10
```

Option 2: Monitor specific databases only:
```yaml
targets:
  - name: my-target
    databases:
      include: ["^app_.*", "^analytics_.*"]  # only these databases
```

Run `pglens-agent check --dsn <dsn>` to see which databases will be monitored and which are skipped.

**Buffer filling / `agent_buffer_bytes` metric keeps growing**

The agent's disk buffer grows when samples cannot be delivered to the server. Check:

1. Server connectivity:
   ```sh
   curl -s http://server:8080/healthz
   ```
   If this fails, the agent cannot reach the server.

2. Agent configuration:
   ```sh
   # Verify the URL
   grep PGLENS_SERVER_URL /path/to/config
   # Verify the token is correct
   curl -s -H "Authorization: Bearer <token>" http://server:8080/healthz
   ```

3. Firewall/network:
   - Agent container: check DNS resolution and network connectivity to server
   - Binary deployment: check firewall rules and routing

After the server becomes reachable, the buffer is replayed automatically (in order, with duplicates absorbed by the server's dedup index). Samples are never lost; buffer overflow after 512 MiB only deletes the oldest (least recent) segments.

**Agent container restarting or marked as down**

1. Check the logs:
   ```sh
   docker logs pglens-agent      # last 100 lines
   docker logs -f pglens-agent   # follow logs
   ```

2. Check the health endpoint:
   ```sh
   curl -s localhost:9187/healthz | jq .
   ```
   Health returns 503 in the `revoked`, `unauthorized`, or `incompatible` states. Otherwise it returns 200 with buffer stats and clock skew.

3. Verify the persistent volume is mounted:
   ```sh
   docker inspect pglens-agent | grep -A 5 Mounts
   # Look for: "Source": "agent-data", "Destination": "/var/lib/pglens"
   ```
   If the volume is missing, every restart will duplicate the instance.

**Multiple instances with same `addr:port` appearing**

The agent did not persist its identity across a restart. Verify:

1. The persistent volume is mounted:
   ```sh
   docker run -v agent-data:/var/lib/pglens ...     # correct
   docker run --rm ...                              # wrong: no persistence
   ```

2. The volume is not being wiped:
   ```sh
   docker volume inspect agent-data
   # Check "Mountpoint" exists on disk
   ```

3. Run the check to see the warning:
   ```sh
   ./bin/pglens-agent check --dsn <dsn>
   # Should show: "identity path /var/lib/pglens is not on a persistent mount"
   ```

To fix: mount the persistent volume and restart. Old duplicate instances will age out (last_seen >3× interval) and trigger `agent_down` events.

## Known limits

The server accepts agents speaking protocol version 1 or 2. An agent newer than
the server is rejected, so upgrade the server before upgrading agents.

**Agent:**
- **No token rotation or mTLS** — the bootstrap token is a shared secret; revocation only (no key rotation, no mutual TLS, no approval queue, single-tenant)
- **An unclean agent shutdown may lose the tail of the current buffer segment** — samples within the current segment are fsynced only at segment roll (every ~8 MiB), not per record. A graceful shutdown (SIGTERM with signal handler) flushes the active segment before exit; forceful termination (SIGKILL) skips that flush. The trade-off is necessary: per-record fsync would make monitoring overhead unacceptable for busy instances.
- **Maximum 10 databases per instance by default** — if you have more databases, increase `targets[].databases.max` in the config or use the `include` filter. Unmonitored databases are reported with `skip_reason=db_budget` to make this visible (not a silent degradation).
- **Oversized envelopes (>32 MiB) are dropped** — the agent does not split envelopes; they are counted in `agent_samples_dropped_total{reason=envelope_oversized}` and logged once per minute

**Deployment:**
- **An agent container without a persistent volume duplicates its instances on restart** — `/var/lib/pglens` must be a persistent volume (not ephemeral). Without it, every container restart creates a new instance ID, causing `duplicate_instance_suspected` events and fragmenting instance history. The compose file and systemd unit handle this correctly; Docker `--rm` breaks it.

**Server and replication:**
- PostgreSQL 15 to 18 only; 13 and 14 are not supported (EOL)
- Only streaming replication is supported; logical, Patroni, and Aurora topologies are not detected
- Raw retention is 30 days with no rollups; compression after 48h (`buffer 6h < sample_age 12h < compress_after 48h`)

**Wait-event analysis (ASH):**
- **Statistical sampling, not exact tracing.** ASH samples once per second, not continuously. Queries shorter than approximately 1 second are under-represented in results. This is the same fundamental trade-off made by Oracle ASH and AWS Performance Insights — acceptable for identifying where the database spends time over hours or days, not suitable for microsecond-level analysis.
- **Fewer than 60 samples is not statistically meaningful.** When a requested time range contains fewer than 60 total samples across all wait events, the API response includes a `warning` field to alert you that results may be unreliable. This is a built-in guard against drawing conclusions from too-small a sample set.
- **At most 100 distinct wait keys per 10-second window.** When more than 100 unique combinations of (database, wait_event_type, wait_event, state, query) appear in a single 10-second window, the top 99 by sample count are kept individually and the remainder is folded into an `other` bucket. The total sample count is always conserved exactly (never underestimated), making this a safe operation for producing aggregate statistics.

## Licence

Apache License 2.0. See [LICENSE](LICENSE).
