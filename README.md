# pglens

pglens is a self-hostable monitoring system for fleets of PostgreSQL instances, focused on replication and wait-event analysis. It is designed for DBAs and platform teams running self-hosted or managed PostgreSQL.

## Project status

The repository ships the agent, server, durable ingest, replication/topology
views, ASH, alerts, advisor findings, on-demand command gates, deployment
artefacts, and the Go and web verification suites. `pglens-server` also serves
the real web interface: Fleet, Cluster, Instance, Wait-event analysis, Queries,
Locks, Advisor, Alerts, and Settings.

For the product boundary and the behaviour that is intentionally not promised,
see [Product limits](docs/LIMITS.md).

The releases are alpha prereleases, published as multi-arch images on ghcr.io
with an SBOM and build provenance. Every one of them goes through the whole of
`ci.yml` first: format, lint and `go vet` under all three build tags, supply-chain
verification (`go mod verify` plus `govulncheck`), unit tests under `-race
-shuffle=on` with per-package coverage floors, ten repeated shuffled race
rounds over the concurrency-sensitive packages, a container image build with a
`--version` smoke test, and the integration matrix across PostgreSQL 15 to 18 in
both `vanilla` and `rds-like` permission profiles. The E2E and Playwright
acceptance suites run nightly and on every pull request into `main`.

## Requirements

- Go 1.26.1 or later to build. `go.mod` pins `toolchain go1.26.6`, so a
  compatible toolchain is fetched automatically; the pin is what keeps the
  standard library clear of the advisories `make vuln` checks for.
- Node 20.19+ or 22.12+ and pnpm 11.24.0, only to build the web interface from
  source. CI and the container images build on Node 24.
- Docker and Docker Compose for the test suites and for the server's storage
- PostgreSQL 15 to 18 as monitoring targets
- PostgreSQL 17 with TimescaleDB 2.29 for the server's own storage (see Running the server)

## Building

```sh
make build
```

This produces `bin/pglens-agent` and `bin/pglens-server`.

To build the web interface into the assets served by `pglens-server`:

```sh
make web-build
```

Running `make build` alone produces a binary with the committed placeholder
page; run `make web-build` first when the built interface is required.

`make web-build` rewrites `internal/webui/dist/index.html` — the one file of the
built bundle that is tracked, because a source checkout has to serve *something*
— to point at that build's content-hashed assets, which are not tracked. That
leaves the file dirty in `git status`. Restore the placeholder before
committing:

```sh
git checkout -- internal/webui/dist/index.html   # or: make clean
```

```sh
make build-images          # builds ghcr.io/manprint/pglens-agent:dev and ...-server:dev
make build-images-multiarch # linux/amd64 + linux/arm64 via docker buildx
```

## Running the checks

One command runs the whole fast gate — Go and web, formatting through coverage
floors:

```sh
make ci-local-unit
```

Frontend only:

```sh
make web-install
make web-lint
make web-typecheck
make web-build
make web-test
make web-coverage-gate
make web-budget
```

These commands cover frontend formatting/linting, TypeScript checking, and the
production asset build. The complete target list, including the integration and
E2E suites, is under [Running the checks](#running-the-checks-1) further down;
the test strategy behind it is in [TESTING.md](TESTING.md).

## Quick start

Three commands from nothing to data (requires Docker and a target PostgreSQL 15+ instance):

```sh
# 1. Create the monitoring role on each target PostgreSQL instance (as superuser)
export PGLENS_DSN='postgres://postgres@<target-host>/postgres'
export PGLENS_MONITORING_PASSWORD='<password>'
psql "$PGLENS_DSN" -v pglens_password="$PGLENS_MONITORING_PASSWORD" \
  -v dbname=postgres -f deploy/sql/monitoring_user.sql

# 2. Configure the agent, enable the web interface, and build the local images
# Keep the defaults for the self-contained example, or edit
# deploy/agent.example.yaml with your target DSN.
export PGLENS_BOOTSTRAP_TOKEN=dev-token
export PGLENS_UI_PASSWORD=dev-password
make build-images
docker compose -f deploy/docker-compose.yml up -d

# 3. Check that data is appearing
curl -fsS localhost:8080/readyz
curl -fsS -H "Authorization: Bearer $PGLENS_BOOTSTRAP_TOKEN" \
  localhost:8080/api/v1/clusters | jq .
```

The server health endpoints return the plain-text body `ok`; the agent's
separate health endpoint at `:9187/healthz` returns JSON status and buffer
details.

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
export PGLENS_DSN='postgres://postgres@<target-host>/postgres'
export PGLENS_MONITORING_PASSWORD='<password>'
psql "$PGLENS_DSN" -v pglens_password="$PGLENS_MONITORING_PASSWORD" \
  -v dbname=postgres -f deploy/sql/monitoring_user.sql
```

The two `GRANT EXECUTE ON FUNCTION pg_control_system()` and `pg_control_checkpoint()` are required because these functions are not covered by `pg_monitor`. Without them the cluster identity falls back to the configured `cluster_name` and a failover can split one cluster into two (see Monitoring a replicated cluster).

The script supports explicit higher tiers:

- `-v tier1=1` — adds `pg_read_all_data` and enables plan-only `EXPLAIN` (T1)
- `-v tier2=1` — adds `pg_signal_backend` and enables cancel/terminate (T2; also pass `tier1=1`)

The script never creates extensions. See [the permission-tier guide](deploy/sql/README.md)
for the complete matrix and downgrade procedure.

## Running the server

**Docker Compose (recommended):**

```sh
docker compose -f deploy/docker-compose.yml up -d
# or the minimal TimescaleDB service
docker compose -f deploy/compose/timescaledb.yml up -d
```

The server applies migrations on startup (forward-only, idempotent) and listens on `:8080`. See `deploy/docker-compose.yml` for the pinned image `timescale/timescaledb:2.29.0-pg17` and the self-contained server/agent stack.

The web interface is served by the server itself on the same address as the API:
`http://<host>:8080/`. Set `PGLENS_UI_PASSWORD` (see the configuration table
below) for the interface to be usable, or set `PGLENS_UI_ENABLED=false` to turn
it off while leaving the API available. `PGLENS_UI_ENABLED=false` disables the
static assets only: the API still requires the agent bearer token or a valid
session cookie, and a server with no `PGLENS_UI_PASSWORD` configured answers
`503 ui_password_not_configured` to browser requests rather than serving them
anonymously. The published container image includes
the web assets. A local binary built with `make build` uses the committed
fallback page; run `make web-build` before `make build` when building the
interface into that binary.

**Binary:**

```sh
PGLENS_DSN=postgres://pglens:pglens@localhost:5432/pglens?sslmode=disable \
PGLENS_BOOTSTRAP_TOKEN=dev-token \
PGLENS_LISTEN=:8080 \
./bin/pglens-server
```

`/healthz` returns 200 once listening; `/readyz` returns 200 when migrations are applied and the pool answers `SELECT 1`. Graceful shutdown on `SIGTERM` with 15s drain.

### Signing in

Open `http://<host>:8080/` in a browser to see the sign-in form. Enter the
password configured through `PGLENS_UI_PASSWORD`; a successful sign-in creates
a session that lasts for `PGLENS_UI_SESSION_TTL`. The session does not survive
a server restart, and signing out clears it.

### Web interface

After signing in, the interface provides shared navigation, a time-range
control, freshness and connection status, and a theme toggle. The pages are:

#### Fleet

Fleet Overview groups monitored instances by cluster and health, showing
firing alerts, replication lag, stale agents, and identity warnings. Missing
lag is shown as `Unknown`, never as zero.

#### Cluster

Cluster Detail brings together topology, replication lag, slot health,
configuration drift, and the event timeline. A failover keeps the same
`cluster_id`, so the cluster remains one continuous story.

#### Instance

Instance Detail scopes metrics by database and makes unmonitored databases
visible. It labels measured, stale, unavailable, and truncated data; shows
counter-reset gaps, host-metric availability, pending settings restarts,
redacted `archive_command` values, and relation-budget notices.

#### Wait-event analysis

The Wait-event analysis page charts sampled activity and supports drill-down
from wait-event type to wait event and query. It reports under-sampling, a
disabled ASH check, missing query attribution, and the `other` aggregate
explicitly.

#### Queries

Query Inspector lists statements collected by `pg_stat_statements`. Operators
can request a plan-only `EXPLAIN` at T1; `EXPLAIN ANALYZE` requires confirmation,
T2, and the target's `allow_explain_analyze` policy. Plan history contains only
explicit requests and comparisons stay within one cluster.

#### Locks and activity

Locks and Activity shows the latest blocking-tree sample and session activity,
with clear empty, stale, and unavailable states. At T2, and only when
`allow_signal` permits it, an operator can confirm cancellation or termination
of a target client backend; the result is recorded in command audit.

#### Advisor

Advisor ranks findings and explains whether each rule is open, degraded, muted,
or resolved. Its rule catalogue shows inputs, scope, severity, affected
targets, and the minimum permission tier. Muting requires a reason and expiry.

#### Alerts

Alerts shows firing and suppressed alerts, editable rules where permitted,
silences with their preview, and the fleet-wide event timeline. Suppression
does not resolve an alert; delivery remains configured separately for Slack or
generic webhooks.

#### Settings

Settings lists instances and databases, summarizes permission tiers and
available actions, and exposes the command audit. The audit records requests,
arguments, outcomes, timing, expiry, or rejection; it does not identify an
operator.

The selected time range is reflected in the URL, so a view can be shared as a
link. Keyboard shortcuts are available for the main destinations and filters:

- `g f` — Fleet
- `g a` — Alerts
- `g s` — Settings
- `/` — focus the primary filter
- `?` — open the shortcut sheet

When `PGLENS_UI_PASSWORD` is configured, sign in to obtain a session cookie and
use the authenticated HTTP API examples below. The agent's bearer token also
authenticates API requests. `/healthz`, `/readyz` and `/metrics` remain open for
probes and monitoring.

### Configuration

| Env | Type | Default | Meaning |
|-----|------|---------|---------|
| `PGLENS_DSN` | DSN | — | TimescaleDB connection (required) |
| `PGLENS_LISTEN` | `host:port` | `:8080` | HTTP listen address |
| `PGLENS_BOOTSTRAP_TOKEN` | string | — | shared secret for agent auth (**required**: the server exits at startup if neither this nor the `_FILE` form resolves to a non-empty token) |
| `PGLENS_BOOTSTRAP_TOKEN_FILE` | path | — | file containing the token (trailing newline trimmed); an unreadable or empty file is a startup failure, not a fallback |
| `PGLENS_UI_PASSWORD` | string | unset | shared password that enables browser/API session authentication |
| `PGLENS_UI_PASSWORD_FILE` | path | unset | password file; trailing newline trimmed and takes precedence over the inline value |
| `PGLENS_UI_SESSION_TTL` | duration | `24h` | session-cookie lifetime; accepted range is `5m` to `720h` |
| `PGLENS_UI_ENABLED` | bool | `true` | serves the UI assets; `false` disables the static assets only — the credential gate on the API stays installed, so requests still need the agent token or a session cookie |
| `PGLENS_UI_COOKIE_SECURE` | bool | `auto` | sets cookie `Secure` for TLS/`X-Forwarded-Proto: https`; `true`/`false` force the attribute |
| `PGLENS_ALERT_INTERVAL` | duration | `30s` | alert evaluation interval |
| `PGLENS_ADVISOR_INTERVAL` | duration | `15m` | advisor finding evaluation interval |
| `PGLENS_COMMAND_TTL` | duration | `5m` | lifetime of an on-demand command before expiry |
| `PGLENS_ALERT_SLACK_WEBHOOK_URL` | URL | unset | enables Slack notifications (preferred name) |
| `PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE` | path | unset | reads the Slack URL; takes precedence over the inline URL |
| `PGLENS_SLACK_WEBHOOK_URL[_FILE]` | URL/path | unset | legacy Slack aliases, still accepted |
| `PGLENS_WEBHOOK_URL` | URL | unset | enables generic webhook notifications |

The bootstrap token is a shared secret; this release has no token rotation, no mTLS, and no approval queue. Revocation is supported: `UPDATE agents SET revoked_at = now()` makes the next push return 401.

## On-demand operations

On-demand commands use a pull channel: the server queues a command and the
agent polls for work. The server never opens an inbound PostgreSQL connection
to an agent target. Supported commands are `explain`, `cancel`, `terminate`,
and `pgstattuple`; every request, claim, result, expiry, rejection, and error
is recorded in the command audit stream.

The pull channel is available by default, but each target is deny-by-default
for mutating or potentially expensive operations:
`targets[].allow_explain_analyze` and `targets[].allow_signal` default to
`false`. Set `commands.enabled: false` for a strictly read-only agent.

```sh
INSTANCE_ID=11111111-1111-1111-1111-111111111111

# Run the HTTP API sign-in block once first; these commands reuse cookies.txt.

# Queue EXPLAIN (the command carries queryid and options, never SQL text).
COMMAND_ID=$(curl -s -b cookies.txt -X POST "http://localhost:8080/api/v1/instances/$INSTANCE_ID/commands" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"explain","args":{"queryid":1234,"datname":"app","analyze":false}}' \
  | jq -r .command_id)

# Poll state/result, then inspect the persisted plan history and audit trail.
curl -s -b cookies.txt "http://localhost:8080/api/v1/commands/$COMMAND_ID" | jq .
curl -s -b cookies.txt "http://localhost:8080/api/v1/plans?instance_id=$INSTANCE_ID&queryid=1234&datname=app" | jq .
curl -s -b cookies.txt "http://localhost:8080/api/v1/instances/$INSTANCE_ID/command-audit" | jq .
```

`explain` resolves the query through `pg_stat_statements`. Plan-only
`EXPLAIN` requires permission tier T1; `EXPLAIN ANALYZE` additionally
requires T2, `allow_explain_analyze: true` for the target, and explicit
confirmation in the web interface. ANALYZE runs inside a transaction that is
rolled back afterward, but it still consumes database resources and may
observe locks or invoke PostgreSQL-permitted side effects.
`cancel` and `terminate` require T2 and `allow_signal: true`, and only target
client backends. `pgstattuple` requires the `pgstattuple` extension to already
be installed and a permitted relation; pglens never installs extensions.
Command results are immutable, and expired commands reject late results.

Security and operational boundaries:

- With `commands.enabled: false` the agent remains strictly read-only; it
  does not poll or execute on-demand commands.
- The server validates authentication, envelope shape, command state and expiry;
  the agent is authoritative for capability, target-policy and execution gates.
- `EXPLAIN ANALYZE` executes the selected statement inside a transaction that
  is rolled back, but it still consumes database resources and can observe
  locks or invoke side effects that PostgreSQL permits during execution.
- Query text is never sent in a command payload. Plan history stores only
  explicitly requested plans, using a normalized query hash and JSON plan;
  placeholders may prevent a useful plan for parameter-sensitive statements.
- Plan-history comparison is valid only within the same cluster; the UI does
  not compare plans from different clusters.

## Alerting

The server evaluates ten built-in Tier 0 rules: `agent_down`, `instance_unreachable`, `check_failing`, `no_primary_in_cluster`, `agent_buffer_full`, `clock_skew`, `cardinality_budget_exceeded`, `failover_detected`, `split_brain_detected`, and `slot_inactive`. Tier 0 rules are always enabled. Tier 1 rules are editable through the alert-rules endpoint.

Four of those rules watch the collector itself rather than PostgreSQL, and the
series they compare are derived at ingest instead of being scraped from a
database. They are written to `metrics` like any other gauge, per instance, on
every push — so they also carry the zero sample that resolves the alert:

| Metric | Rule | Produced by |
|--------|------|-------------|
| `pglens_check_error_rate` | `check_failing` | server, fraction of the push's checks that reported an error |
| `pglens_cardinality_truncated_rate` | `cardinality_budget_exceeded` | server, fraction of the push's results truncated by a cardinality budget |
| `pglens_agent_clock_skew_seconds` | `clock_skew` | server, unsigned difference between the push's `sent_at` and the receive time |
| `pglens_samples_dropped_rate` | `agent_buffer_full` | agent, samples per second lost to a full disk buffer since the previous push |

The two cluster-scoped Tier 1 replication rules are aggregated across the
cluster's standbys, not per standby: `replica.all_standbys_lagging` compares
the *smallest* replay lag among them (so it fires only when every standby is
above the threshold), and `replica.no_sync_standby` counts the standbys whose
`sync_state` is `sync` or `quorum`. Each opens one alert per cluster.

Configure Slack or a generic webhook with the variables above. A silence suppresses notification while the alert remains visible and continues to be evaluated.

Alerts are persisted and evaluated even when no delivery channel is configured.
Delivery remains limited to Slack via `PGLENS_ALERT_SLACK_WEBHOOK_URL` or
`PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE` (with the accepted legacy
`PGLENS_SLACK_WEBHOOK_URL` aliases) and generic webhooks via
`PGLENS_WEBHOOK_URL`.

Run the HTTP API sign-in block once before these examples; reuse its
`cookies.txt` for every protected request.

```sh
curl -s -b cookies.txt http://localhost:8080/api/v1/alerts | jq .
curl -s -b cookies.txt -X POST http://localhost:8080/api/v1/silences \
  -H 'Content-Type: application/json' \
  -d '{"matchers":[{"name":"severity","value":"warning"}],"reason":"maintenance","starts_at":"2030-08-29T10:00:00Z","ends_at":"2030-08-29T11:00:00Z"}'
curl -s -b cookies.txt http://localhost:8080/api/v1/alert-rules | jq .
curl -s -b cookies.txt -X DELETE http://localhost:8080/api/v1/silences/<silence-id>
```

An alert listing contains objects such as `{"alert_key":"agent_down/...","state":"firing","severity":"critical","cluster_id":"7381927364512345678","suppressed":false}`. The API returns cluster identifiers as strings and timestamps in RFC 3339 format.

Alerting delivers only to Slack and generic webhooks. Email and PagerDuty are not supported.

## Advisor findings

Advisor findings are durable, ranked statements about the current state of an
instance. Unlike alerts, they are not one-time events: `open` means the rule is
currently firing, `degraded` means a required metric, check, permission tier or
host view is unavailable, `muted` means an operator has temporarily hidden the
finding, and `resolved` means a later pass no longer reproduced it. Muting does
not delete the finding; the next pass restores its real state after the mute
expires or is removed.

Run the HTTP API sign-in block once before these examples; reuse its
`cookies.txt` for every protected request.

```sh
curl -s -b cookies.txt 'http://localhost:8080/api/v1/findings?severity=critical&limit=100' | jq .
curl -s -b cookies.txt 'http://localhost:8080/api/v1/advisor/rules' | jq .
curl -s -b cookies.txt -X POST http://localhost:8080/api/v1/findings/<finding-id>/mute \
  -H 'Content-Type: application/json' \
  -d '{"reason":"accepted risk","until":"2030-08-29T11:00:00Z"}' | jq .
```

The findings listing returns a JSON array, for example:

```json
[{"finding_id":"query.slow_mean/11111111-1111-1111-1111-111111111111","rule_id":"query.slow_mean","severity":"warning","state":"open","scope":"instance","title":"High mean query latency"}]
```

Mute returns the changed state and expiry; remove it with
`DELETE /api/v1/findings/<finding-id>/mute` (which returns `204`):

```json
{"finding_id":"query.slow_mean/11111111-1111-1111-1111-111111111111","state":"muted","muted_until":"2030-08-29T11:00:00Z","mute_reason":"accepted risk"}
```

The rule catalogue is available from `/api/v1/advisor/rules`; it is the
authoritative list of rule IDs, severity, scope, required inputs and minimum
permission tier. Findings are based on collected statistics, not query plans,
so index recommendations are candidates rather than certainties. Rules that
need seven days of history remain degraded until that history exists, and
host-memory rules are unavailable for remote instances.

The catalogue response is a JSON array, for example:

```json
[{"id":"query.slow_mean","severity":"warning","scope":"instance","needs":["Statements"],"min_tier":"T0"}]
```

Findings are based on collected statistics, while index recommendations remain
candidates for review. The Advisor page described above exposes the same ranked
results and their freshness state.

### Advisor rule catalogue

| Rule ID | Severity | Meaning |
|---|---|---|
| `query.slow_mean` | warning | A frequently executed query has a high mean execution time. |
| `query.total_time_share` | warning | One query consumes a disproportionate share of execution time. |
| `query.regression` | critical | A query is materially slower than its seven-day baseline. |
| `query.temp_bytes_high` | warning | A query writes unusually large temporary volumes per call. |
| `query.cache_miss_high` | info | A high-volume query has an excessive shared-buffer read ratio. |
| `index.unused` | warning | A large non-primary index has no recorded usage over seven days. |
| `index.duplicate` | warning | Multiple indexes share the same definition on one table. |
| `index.redundant_prefix` | info | A shorter non-unique index is a prefix of another index. |
| `index.invalid` | critical | An invalid index is still present and maintained on writes. |
| `index.bloat_high` | warning | An index exceeds the configured bloat ratio and size thresholds. |
| `index.divergence` | warning | An index definition is absent from a sibling cluster member. |
| `table.dead_tuples_high` | warning | Dead tuples exceed the ratio and count thresholds. |
| `table.never_autovacuumed` | warning | A large table has no recorded vacuum or autovacuum. |
| `table.autoanalyze_stale` | info | Table modifications exceed the threshold since the last analyze. |
| `table.wraparound_risk` | critical | A relation's frozen transaction ID age is dangerously high. |
| `table.bloat_high` | warning | A table exceeds the configured bloat ratio and size thresholds. |
| `table.seq_scan_heavy` | info | A large table is dominated by sequential scans and may need review. |
| `vacuum.starvation` | critical | Too many bloated tables coincide with all autovacuum workers being busy. |
| `vacuum.disabled` | critical | Autovacuum is disabled. |
| `config.work_mem_oversized` | warning | The work_mem and connection budget can exceed usable memory. |
| `config.work_mem_low` | info | work_mem is low while queries are writing temporary data. |
| `config.shared_buffers_low` | warning | shared_buffers is below the recommended share of usable memory. |
| `config.shared_buffers_high` | warning | shared_buffers consumes an excessive share of usable memory. |
| `config.effective_cache_size_mismatch` | info | effective_cache_size is far outside the usable-memory range. |
| `config.maintenance_work_mem_low` | info | maintenance_work_mem is low for a large host. |
| `config.max_connections_high` | warning | Many configured connections appear unnecessary without a pooler. |
| `config.track_io_timing_off` | info | I/O timing collection is disabled. |
| `config.checkpoints_too_frequent` | warning | Requested checkpoints are occurring too frequently. |
| `config.wal_keep_size_low` | warning | Replication lag exceeds wal_keep_size without slot protection. |
| `config.fsync_off` | critical | fsync is disabled and committed data may be lost after a power failure. |
| `config.full_page_writes_off` | critical | Full-page writes are disabled, reducing crash-recovery protection. |
| `config.drift` | warning | A shared configuration setting differs between cluster members. |
| `conn.saturation` | warning | Connection usage is above the saturation threshold. |
| `conn.idle_share_high` | info | More than 70% of connections are idle. |
| `archive.disabled` | info | WAL archiving is disabled. |
| `archive.failing` | critical | Archive failures dominate the observed archive attempts. |
| `archive.stalled` | critical | The last successful archive is older than the allowed interval. |
| `backup.no_basebackup_seen` | warning | No recent base backup has been observed in the available history. |
| `backup.no_strategy` | critical | Neither archiving nor a visible base-backup strategy is configured. |

## Running the agent

The agent collects from each configured target, buffers samples to disk, and pushes to the server every 15 seconds (default). It stores its identity on disk and must run with a persistent `/var/lib/pglens` directory — without it, each container restart creates a new instance ID and duplicates the instance.

### Container (Docker)

```sh
docker run -d --name pglens-agent \
  -v agent-data:/var/lib/pglens \
  -v "$PWD/deploy/agent.example.yaml":/etc/pglens/agent.yaml:ro \
  -v /proc:/host/proc:ro -v /sys:/host/sys:ro \
  -e PGLENS_SERVER_URL=http://pglens-server:8080 \
  -e PGLENS_BOOTSTRAP_TOKEN=dev-token \
  ghcr.io/manprint/pglens-agent:dev run --config /etc/pglens/agent.yaml
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
      - ./deploy/agent.example.yaml:/etc/pglens/agent.yaml:ro
      - /proc:/host/proc:ro              # read-only host introspection
      - /sys:/host/sys:ro                # read-only host introspection
    environment:
      PGLENS_SERVER_URL: http://pglens-server:8080
      PGLENS_BOOTSTRAP_TOKEN: dev-token
      PGLENS_CONFIG: /etc/pglens/agent.yaml
      HOST_PROC: /host/proc
      HOST_SYS: /host/sys
      # OR mount the config file: 
      # PGLENS_CONFIG: /etc/pglens/agent.yaml
    healthcheck:
      test: ["CMD", "/usr/local/bin/pglens-agent", "--healthcheck"]
      interval: 5s
      timeout: 5s
      retries: 20

volumes:
  agent-data:                           # named volume persists across restarts
```

The proc/sys mounts are read-only; they are preferred to running the agent
container with `--privileged`.

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

The instance `settings` check records curated or operator-changed PostgreSQL
GUCs and exposes normalized byte/time gauges for advisory rules. For safety,
`archive_command` facts retain only the first command token; any remaining
arguments are emitted as `[redacted]` because they may contain credentials.

Collected checks include `locks` (10s, instance scope, Tier 0), which reports
sampled blocking trees and bounded wait-event gauges. The `activity` check also
reports connection use, per-database counts, state age, prepared transactions,
and frozen-XID age; per-application counts are opt-in.

Maintenance checks include `table_stats` and `index_stats` (5m, database scope,
Tier 0), `vacuum_progress` (30s, instance scope, Tier 0), and `bloat_estimate`
(6h, database scope, Tier 0). Relation reporting is bounded by a top-N budget
shared per instance across databases; table and index defaults are 50.

Durability checks are `settings` (1h, instance/Tier 0), `wal` (15s,
instance/Tier 0), `checkpointer` (30s, instance/Tier 0), `io` (30s,
instance/Tier 0, PostgreSQL 16+), and `archiver` (60s, instance/Tier 0).

Annotated `deploy/agent.example.yaml`:

```yaml
# Server connection (required)
server:
  url: http://pglens-server:8080     # string: HTTP URL to pglens server (required)
  token: dev-token                   # string: bootstrap token (use token_file instead for a file secret)
  token_file: /run/secrets/pglens    # string: path to file containing token; used when token is empty
  
# Identity persistence (required)
identity_path: /var/lib/pglens/identity.json  # string: path to identity file (must be on persistent mount)

# Host metrics (optional; enabled by default)
host:
  enabled: true
  interval: 30s
  proc_path: /proc                 # defaults to $HOST_PROC, then /proc
  sys_path: /sys                    # defaults to $HOST_SYS, then /sys

# Push scheduling (optional)
push_interval: 15s                   # duration: time between envelope deliveries (default 15s)

# On-demand operations (optional; disabled when false)
commands:
  enabled: true

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
    host_local: true                 # optional; otherwise inferred from localhost/loopback or Unix socket DSNs
    cluster_name: pg-prod-eu         # string: fallback cluster identity if pg_control_system() unavailable
    allow_explain_analyze: false     # permit EXPLAIN ANALYZE for this target (default false)
    allow_signal: false              # permit cancel/terminate for this target (default false)
    databases:
      include: ["app_.*"]            # string array: regex patterns to include (default empty = all)
      exclude: ["^template\\d$", "^rdsadmin$", "^azure_.*$"]
                                     # string array: regex patterns to exclude (default excludes templates and managed dbs)
      max: 10                        # integer: max databases per instance to monitor (default 10; max 10)

# Per-check configuration overrides (optional)
checks:
  instance_info:    { interval: 60s }        # General instance metadata (mandatory)
  activity:        { interval: 10s, by_application: false } # Session activity
  locks:           { interval: 10s }        # Blocking tree (instance scope, T0)
  table_stats:     { interval: 5m, top_n: 50 }
  index_stats:     { interval: 5m, top_n: 50 }
  vacuum_progress: { interval: 30s }
  bloat_estimate:  { interval: 6h, top_n: 50 }
  settings:         { interval: 1h }
  wal:              { interval: 15s }
  checkpointer:     { interval: 30s }
  io:               { interval: 30s }        # PostgreSQL 16+
  archiver:         { interval: 60s }
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
- Size format: `512MiB`, `1GiB`, `512MB` or `512B` (binary/decimal units are parsed by the agent)

Relation endpoints are available at `/api/v1/instances/{id}/tables`,
`/indexes`, and `/bloat`; each response includes `truncated` when the relation
budget limits the result. For example: `curl -s -b cookies.txt localhost:8080/api/v1/instances/11111111-1111-1111-1111-111111111111/tables | jq .`.

Host metrics for a local target are available at:

```sh
curl -s -b cookies.txt http://localhost:8080/api/v1/instances/<instance-id>/host | jq .
```

The local response includes `available: true`, `source` (`host`, `cgroup_v1`,
or `cgroup_v2`) and only fields that were measurable. A remote target returns
`{"instance_id":"…","available":false,"reason":"target is not local to any agent"}`;
it never returns zeros for unavailable host metrics.

Settings and cluster drift are available with:

```sh
curl -s -b cookies.txt 'localhost:8080/api/v1/instances/<instance-id>/settings' | jq .
curl -s -b cookies.txt 'localhost:8080/api/v1/instances/<instance-id>/settings?changed_since=2026-08-28T00:00:00Z' | jq .
curl -s -b cookies.txt 'localhost:8080/api/v1/clusters/<cluster-id>/settings-drift' | jq .
```

The settings response contains `name`, `value`, source, context, pending-restart
and observation timestamps. Drift returns only differing settings with their
instance IDs, roles and values.

**Environment overrides** (take precedence over the config file's own value):
- `PGLENS_SERVER_URL=http://...` — overrides `server.url`
- `PGLENS_BOOTSTRAP_TOKEN=...` — overrides `server.token`
- `PGLENS_BOOTSTRAP_TOKEN_FILE=/path/to/token` — overrides `server.token_file`
- `PGLENS_IDENTITY_PATH=/path/to/identity.json` — overrides `identity_path`
- `PGLENS_CONFIG=/path/to/agent.yaml` — path to the config file itself (default `/etc/pglens/agent.yaml`); the `--config` flag on `pglens-agent run` takes precedence over this
- `HOST_PROC=/host/proc` and `HOST_SYS=/host/sys` — override the host collector's proc/sys roots (use with read-only container mounts)
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
  - `T1 (pg_monitor + pg_read_all_stats + pg_read_all_data)` — enables plan-only `EXPLAIN` in the UI
  - `T2 (T1 + pg_signal_backend)` — enables query cancellation and termination
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

Every `/api/v1` endpoint except `POST /api/v1/session` requires a session
cookie or an agent bearer token; `/healthz`, `/readyz` and `/metrics` are the
unauthenticated operational endpoints.

Authenticate once before running the protected examples below; the cookie jar
authenticates every subsequent request:

```sh
# sign in once; the cookie jar authenticates the examples below
curl -s -c cookies.txt -X POST localhost:8080/api/v1/session \
  -H 'Content-Type: application/json' -d '{"password":"'"$PGLENS_UI_PASSWORD"'"}'
```

For scripts, `-H "Authorization: Bearer $PGLENS_BOOTSTRAP_TOKEN"` is the
equivalent credential and can replace `-b cookies.txt` on protected requests.

All responses carry `cluster_id` as a **decimal string** — `uint64` exceeds IEEE-754 exact integer range and would be rounded by `jq` or any JS consumer. Missing intervals are `null`, never `0` or interpolated.

### `GET /api/v1/clusters`

```sh
curl -s -b cookies.txt localhost:8080/api/v1/clusters | jq '.[0]'
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
curl -s -b cookies.txt localhost:8080/api/v1/clusters/7381927364512345678/topology | jq .
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
curl -s -b cookies.txt "localhost:8080/api/v1/clusters/7381927364512345678/replication?from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z" | jq .
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
curl -s -b cookies.txt localhost:8080/api/v1/instances/11111111-1111-1111-1111-111111111111 | jq .
```

Returns the instance plus its databases with `monitored`, `skip_reason`, and `databases_not_monitored` count.

### `GET /api/v1/locks`

```sh
curl -s -b cookies.txt "localhost:8080/api/v1/locks?instance_id=11111111-1111-1111-1111-111111111111" | jq .
```

Returns the latest sampled blocking tree. An instance with no stored tree still
returns 200: `{"instance_id":"…","sampled_at":null,"stale":true,"nodes":[]}`.

### `GET /api/v1/instances/{id}/activity`

```sh
curl -s -b cookies.txt "localhost:8080/api/v1/instances/11111111-1111-1111-1111-111111111111/activity" | jq .
```

The response groups recent activity metrics, including database connection
counts and age gauges, for example `{"stale":false,"metrics":{"pg_connections_by_database":[{"value":3,"labels":{"datname":"app"}}]}}`.

### `GET /api/v1/metrics/query`

```sh
curl -s -b cookies.txt "localhost:8080/api/v1/metrics/query?metric=pg_backends&instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z&step=60s" | jq .
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
curl -s -b cookies.txt "localhost:8080/api/v1/events?cluster_id=7381927364512345678&type=failover_detected&limit=10" | jq .
```

Newest first, `limit` capped at 1000.

### `GET /api/v1/statements`

```sh
curl -s -b cookies.txt "localhost:8080/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111&database=app&from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z&order_by=total_exec_time&limit=20" | jq .
```

Returns top queries joined to `query_texts`, with `truncated` and `comparable_scope: "cluster"` (`queryid` is comparable only within one cluster, not across clusters or major versions).

### `GET /api/v1/ash` and `/api/v1/ash/top`

Grouped by wait event:

```sh
curl -s -b cookies.txt "localhost:8080/api/v1/ash?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T00:30:00Z&group_by=wait_event_type" | jq .
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
curl -s -b cookies.txt "localhost:8080/api/v1/ash?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T00:30:00Z&group_by=queryid" | jq .
```

Returns `queryid` grouped rows; use `/api/v1/ash/top` for queries joined to their text:

```sh
curl -s -b cookies.txt "localhost:8080/api/v1/ash/top?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T00:30:00Z" | jq .
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
- `pglens_check_ok_total` — counter, check scrapes that completed without an error (by `check` name). The pair is what makes a broken check distinguishable from a briefly unreachable target: a check that has errored *and never once succeeded* produced no data at all, while a check that errored during a failover and succeeded on either side of it is behaving correctly.
- `pglens_samples_too_old_total` — counter, samples rejected for exceeding max age (12 hours)
- `pglens_cardinality_truncated_total` — counter, envelopes where cardinality budgets caused truncation
- `pglens_agent_clock_skew_seconds` — gauge, most recently observed agent/server clock skew (useful for diagnosing timestamp misalignment)

`pglens_ingest_rejected_total` currently emits four reasons:
`decompressed_too_large` (a gzipped push that expands past 256 MiB),
`inventory_error`, `pipeline_error`, and `invalid_fact`.

```sh
curl -s localhost:8080/metrics | head -20
```

Use this endpoint to monitor pglens itself — feed it into Prometheus, Datadog, or your favorite metrics backend. The E2E suite scrapes it too: `pglens_series_total` backs invariant I-8 (cardinality within budget), and `pglens_check_error_total` together with `pglens_check_ok_total` backs the "no unexpected check errors" invariant at the end of every scenario.

### `GET /healthz`, `/readyz`

```sh
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz
```

The complete machine-readable API contract is [`api/openapi.yaml`](api/openapi.yaml),
with an offline rendered reference at [`docs/api.md`](docs/api.md). The OpenAPI
document is authoritative for parameters, defaults, response shapes, and error
codes; regenerate the reference with `make api-docs`.

### Endpoint index

The API is authenticated with the configured bearer token where applicable.
The complete route surface is:

```text
GET    /api/v1/clusters
GET    /api/v1/clusters/{id}/topology
GET    /api/v1/clusters/{id}/replication
GET    /api/v1/clusters/{id}/settings-drift
GET    /api/v1/instances
GET    /api/v1/instances/{id}
GET    /api/v1/instances/{id}/activity|databases|host|settings|tables|indexes|bloat|command-audit
GET    /api/v1/locks
GET    /api/v1/metrics/query
GET    /api/v1/events
GET    /api/v1/statements
GET    /api/v1/ash|ash/top
POST   /api/v1/push
GET    /api/v1/plans
POST   /api/v1/instances/{id}/commands
GET    /api/v1/commands/{id}
GET    /api/v1/agents/{agent_id}/commands
POST   /api/v1/commands/{id}/result
GET    /api/v1/alerts|alerts/{alert_key}
GET    /api/v1/alert-rules
PUT    /api/v1/alert-rules/{rule_id}
GET    /api/v1/silences
POST   /api/v1/silences
DELETE /api/v1/silences/{id}
GET    /api/v1/findings|findings/{finding-id}
POST   /api/v1/findings/{finding-id}/mute
DELETE /api/v1/findings/{finding-id}/mute
GET    /api/v1/advisor/rules
```

The agent-only command poll and result routes are listed for operators
debugging the pull channel; normal users enqueue commands through the instance
route and read them through the command status route.

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
curl -s -b cookies.txt localhost:8080/api/v1/clusters | jq '.[] | {name, primary, standby_count, max_replay_lag_seconds, health}'
```

**Topology graph and failover history:**
```sh
curl -s -b cookies.txt localhost:8080/api/v1/clusters/7381927364512345678/topology | jq .
```

**Replication lag over time (e.g., last hour):**
```sh
curl -s -b cookies.txt "localhost:8080/api/v1/clusters/7381927364512345678/replication?from=$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)&to=$(date -u +%Y-%m-%dT%H:%M:%SZ)" | jq '.edges[] | select(.metric=="replay_lag_sec")'
```

The `cluster_id` returned by these endpoints is byte-identical before and after a failover or promote, proving invariant I-1 is honored.

## Wait-event analysis

What it answers: where the database is spending its time, right now and historically. It samples `pg_stat_activity` once per second, aggregates into 10-second windows, and stores the sample counts by wait event. No extension or restart required — it works on managed PostgreSQL including Amazon RDS.

`compute_query_id = on` is strongly recommended. Without it, samples cannot be attributed to a query and ASH loses much of its value. The server logs a warning once if `compute_query_id` is off.

Query by wait event type:

```sh
curl -s -b cookies.txt "localhost:8080/api/v1/ash?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z&group_by=wait_event_type" | jq .
```

Query by query (requires `compute_query_id = on`):

```sh
curl -s -b cookies.txt "localhost:8080/api/v1/ash/top?instance_id=11111111-1111-1111-1111-111111111111&from=2026-08-27T00:00:00Z&to=2026-08-27T01:00:00Z" | jq .
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
make vet-tags         # go vet with no tags, -tags=integration, and -tags=e2e
make test             # unit tests with -race -shuffle=on
make stress           # repeat the concurrency-sensitive packages, new shuffle seed per round
make coverage-gate    # enforce per-package and global coverage floors
make tidy-check       # go.mod/go.sum are tidy, and every module verifies
make vuln             # govulncheck against the pinned toolchain and dependencies
make api-docs         # regenerate the offline HTTP API reference from OpenAPI
make test-integration # integration tests against real PostgreSQL (requires Docker)
make test-e2e         # E2E smoke subset (requires Docker, ~10m)
make test-e2e-full    # full E2E suite (requires Docker, ~60m)
make test-e2e-full-evidence # full E2E suite with a durable log and captured exit status (requires Docker, ~60m)
make test-ui-e2e      # Playwright UI acceptance against a real stack (requires Docker)

make ci-local-unit     # everything CI runs in its fast lane, Go and web
make ci-local-security # tidy-check + vuln
make ci-local          # the fast lane, then security, then the full L2 matrix
```

`make vuln` needs `govulncheck` on `PATH`
(`go install golang.org/x/vuln/cmd/govulncheck@latest`); `make lint` needs
`golangci-lint`. Both targets say so rather than silently passing when the tool
is missing.

### What CI runs

| Workflow | Trigger | Jobs |
| --- | --- | --- |
| `ci.yml` | pull request, push to `main`, manual, and reused by the release workflow | web, format/lint/vet-tags, supply chain (`tidy-check` + `govulncheck`), unit + coverage, repeated race stress (not on pull requests), container image build and `--version` smoke, integration across PostgreSQL 15–18 × `vanilla`/`rds-like`, and a `CI gate` job that aggregates them |
| `e2e.yml` | pull request to `main`, nightly, manual | the full E2E suite and the Playwright UI acceptance suite, each across `AGENT_MODE` `container` and `binary` |
| `release-alpha.yml` | a `v*-alpha.*` tag, manual | the whole of `ci.yml`, then multi-arch images pushed to ghcr.io with SBOM and provenance, a `--version` smoke test against the published images, and a GitHub prerelease |

`CI gate` is the single check to require in branch protection: it fails if any
job failed or was cancelled, so a job added to `ci.yml` is covered without
touching the branch rule.

### Test levels

- **L1 — Unit** (`make test`): fast, isolated Go tests for pure logic and component behavior; no Docker or external PostgreSQL is required.
- **L2 — Integration** (`make test-integration`): agent, wire and storage tests against real PostgreSQL versions; requires Docker and takes roughly 15 minutes.
- **L3 — E2E system** (`make test-e2e` or `make test-e2e-full`): operator-style multi-container scenarios and failure recovery; requires Docker, with the smoke subset taking roughly 10 minutes and the full suite roughly 60 minutes.
- **L4 — Frontend component** (`make web-test`, `make web-coverage-gate`): Vitest and MSW against the React components and API layer, including contract validation of `api/openapi.yaml` through the generated types. Floors are in `scripts/coverage_gate_ui.sh` and the rationale is in `TESTING.md` §6.
- **L5 — Frontend E2E** (`make test-ui-e2e`): Playwright driving a real browser against a real stack, sharing L3's scenario library. The `SYS-UI-*` acceptance scenarios live here. See `TESTING.md` §7.

Performance and profiling work — workload benchmarks, resource-budget
investigation — is an explicitly scoped exercise rather than a numbered level or
a correctness gate.

**L3 E2E tests**

L3 requires Docker and the following host ports:
- Port `8080` (server HTTP)
- Ports `5432+` (PostgreSQL instances, typically 5432-5435)
- Port `8474` (Toxiproxy fault injection)

`make build-images` must precede any L3 run. Each test uses a unique compose project name and dynamically allocated ports, so concurrent runs and cleanup are safe.

L2 tests inside one container run serially by design (`postgres.Restore()` needs exclusive access); parallelism comes from the version matrix only. `make test-e2e` uses `-count=1` (cached E2E pass is a lie).

**Invariants**

Every L3 scenario ends with `AssertInvariants`, which checks the properties no
individual scenario was looking for: no duplicate `(series_id, ts)`, no negative
rates, no orphaned `instance_id`, no `cluster_id_changed` event, per-instance
series count within budget, and no unexpected `pglens_check_error_total`. The
last two read the server's own `/metrics`; the parsing and assertions behind
them live in `test/harness/metrics.go` with no build tag, so `make test` covers
them.

Goroutine leaks are asserted in-process instead: `internal/leaktest` snapshots
the live goroutines, and every component with a `Start`/`Stop` pair has a
lifecycle test that proves `Stop` actually stops it.

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
export PGLENS_DSN='postgres://postgres@<target-host>/postgres'
export PGLENS_MONITORING_PASSWORD='<password>'
psql "$PGLENS_DSN" -v pglens_password="$PGLENS_MONITORING_PASSWORD" \
  -v dbname=postgres -f deploy/sql/monitoring_user.sql
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

The authoritative, numbered list is [docs/LIMITS.md](docs/LIMITS.md). The
following is a short operational summary.

The server accepts agents speaking protocol version 1 or 2. An agent newer than
the server is rejected, so upgrade the server before upgrading agents.

**Agent:**
- **No token rotation or mTLS** — the bootstrap token is a shared secret; revocation only (no key rotation, no mutual TLS, no approval queue, single-tenant)
- **An unclean agent shutdown may lose the tail of the current buffer segment** — samples within the current segment are fsynced only at segment roll (every ~8 MiB), not per record. A graceful shutdown (SIGTERM with signal handler) flushes the active segment before exit; forceful termination (SIGKILL) skips that flush. The trade-off is necessary: per-record fsync would make monitoring overhead unacceptable for busy instances.
- **Maximum 10 databases per instance by default** — if you have more databases, increase `targets[].databases.max` in the config or use the `include` filter. Unmonitored databases are reported with `skip_reason=db_budget` to make this visible (not a silent degradation).
- **Oversized envelopes (>32 MiB) are dropped** — the agent does not split envelopes. The server answers 413, the agent drops that envelope instead of retrying it forever, and the drop is counted into `pglens_samples_dropped_rate`, the metric behind the `agent_buffer_full` alert rule. A gzipped envelope that expands past 256 MiB is rejected the same way, and is additionally visible server-side as `pglens_ingest_rejected_total{reason="decompressed_too_large"}`.

- **Buffer retention drops are labelled, not silent** — records rolled off by the disk buffer's size or age policy are counted per reason (`size_limit`, `age_limit`) in the buffer's own stats and folded into `pglens_samples_dropped_rate`. A buffer that is discarding data always says so.

**Deployment:**
- **An agent container without a persistent volume duplicates its instances on restart** — `/var/lib/pglens` must be a persistent volume (not ephemeral). Without it, every container restart creates a new instance ID, causing `duplicate_instance_suspected` events and fragmenting instance history. The compose file and systemd unit handle this correctly; Docker `--rm` breaks it.
- **Host metrics are local-only** — they are attached only to targets declared or inferred as local to the agent. Remote targets report host metrics as unavailable, never as zero. In a container with a memory limit, the reported total is the cgroup limit rather than the machine's memory. Per-device IOPS, disk latency and network metrics are not collected.

**Server and replication:**
- PostgreSQL 15 to 18 only; 13 and 14 are not supported (EOL)
- Only streaming replication is supported; logical, Patroni, and Aurora topologies are not detected
- Raw retention is 30 days with no rollups; compression after 48h (`buffer 6h < sample_age 12h < compress_after 48h`)
- **Every in-memory structure has a fixed ceiling and drops data past it** — 32 MiB per push on the wire and 256 MiB after gzip, 64 KiB of request headers, 2-minute read/write/idle timeouts, delta and topology state evicted one hour after an instance's last sample, and at most 4 096 live sessions. None of these are configurable; see [limit 13](docs/LIMITS.md#13-every-in-memory-structure-is-bounded-and-a-bound-that-trips-drops-data) for what each one trades away.

**Contention:**
- The lock view is sampled every 10 seconds, not live; a contention episode shorter than the interval can be missed entirely
- Query text in a lock tree is truncated to 2 048 bytes
- Deadlocks are reported as a counter and a rate; identifying the statements involved in a specific deadlock requires PostgreSQL log analysis, which pglens does not do

**Space and maintenance:**
- Bloat is a statistical estimate, not a measurement; exact figures require `pgstattuple` on demand where already installed
- Exact bloat commands require a pre-installed `pgstattuple` extension; pglens never creates it automatically
- Plan history is captured only when an `explain` command is requested; it is not an automatic query sampler
- Normalized query placeholders can make a plan less representative for parameter-sensitive workloads; inspect the returned hash and metadata
- Relations under 1 MiB and never-analysed relations are not estimated
- Only top-N relations per instance are listed; the rest are counted as truncated

**Configuration and durability:**
- `archive_command` facts retain only the first token and redact remaining arguments because they may contain credentials
- I/O timing is zero unless `track_io_timing` is enabled; `pg_io_timing_enabled` reports which case applies
- `pg_stat_io` and the `io` check are unavailable below PostgreSQL 16
- Archiver metrics report whether WAL archiving is working; pglens does not verify restoreability and does not integrate with pgBackRest, Barman or WAL-G

**Wait-event analysis (ASH):**
- **Statistical sampling, not exact tracing.** ASH samples once per second, not continuously. Queries shorter than approximately 1 second are under-represented in results. This is the same fundamental trade-off made by Oracle ASH and AWS Performance Insights — acceptable for identifying where the database spends time over hours or days, not suitable for microsecond-level analysis.
- **Fewer than 60 samples is not statistically meaningful.** When a requested time range contains fewer than 60 total samples across all wait events, the API response includes a `warning` field to alert you that results may be unreliable. This is a built-in guard against drawing conclusions from too-small a sample set.
- **At most 100 distinct wait keys per 10-second window.** When more than 100 unique combinations of (database, wait_event_type, wait_event, state, query) appear in a single 10-second window, the top 99 by sample count are kept individually and the remainder is folded into an `other` bucket. The total sample count is always conserved exactly (never underestimated), making this a safe operation for producing aggregate statistics.

**Web interface:**
- **One shared password, no user accounts or roles.** Browser and API sessions use the deployment's shared `PGLENS_UI_PASSWORD`; the interface does not provide per-user identity, roles, or a per-user audit trail.
- **Sessions are lost on restart.** Operators must sign in again after the server restarts; the session is not a durable credential.
- **No pooler view.** pglens does not display pooler queues, pool sizes, or pooler health; inspect a pooler separately when one sits in front of PostgreSQL.
- **Polling bounds freshness.** The interface refreshes on a polling cadence rather than a streaming connection, so displayed data can be as old as the applicable poll interval and storage delay.
- **Responsive minimum only.** The interface is designed for desktop operations and has no dedicated mobile layout beyond a responsive minimum.

## Contributing

Build, lint, and test commands are documented above. Before opening a change,
read [CONTRIBUTING.md](CONTRIBUTING.md) and keep the implementation, tests, and
operator documentation in sync.

## Licence

Apache License 2.0. See [LICENSE](LICENSE).
