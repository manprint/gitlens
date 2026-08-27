# Phase 4 — Agent: connections, scheduler, buffer, push

> **Intent:** Build the collector half of the chain — connection policy including
> the per-database level, a scheduler with per-check timeouts and a circuit
> breaker, a durable disk buffer, the push client, configuration, and packaging
> as both a binary and a container.
> **Shippable alone?** yes — an agent that collects from real instances and
> pushes to the phase 3 server.
> **Preconditions:** phase 3 DONE.

## State contract (mandatory)

1. Before touching anything: read [STATE.md](STATE.md). If §1 `Status` is `OPEN`,
   finish or revert that unit first (§6 says how far it got). Run the gate
   commands in STATE.md **§3** and check the result against what §1, §7, and §11
   claim; the repo wins, so correct the file when they disagree.
2. **Open the sub-phase in STATE.md §1 before editing any code**: `Type:
   sub-phase`, its `ID`, `Status: OPEN`, `Intent`, `Next action:`, and §6 set to
   `claimed — nothing written yet`.
3. **Close it after the gates are green**: append the §4 ledger row, reset §6 to
   `none — tree consistent`, update §5 §7 §8 §9 §10 and the §11 board, point §1
   at the next unit with `Status: none`, bump the timestamp. When STATE.md §3 has
   WIP commits on, commit the closed sub-phase and put its sha in the §4 row. A
   sub-phase is not done until this is written.
4. If the session ends mid-sub-phase, leave §1 `OPEN` and write exactly what is
   half-finished into §6 before stopping — plus a `wip(<N.Y>)` commit when WIP
   commits are on.

---

## Sub-phases

### 4.1 Connection manager and the per-database level

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — concurrency and resource ownership. **`agent-1:opus` review gate:** lifecycle and connection budget. An agent that over-connects becomes the cause of the incident it was installed to observe.
- **Files:** `internal/agent/conn.go`, `internal/agent/discovery.go`, `internal/agent/target.go`, and `_test.go` / `it_*_test.go` beside each
- **Change:** implements decision D16 (Q10) and `IDEA.md` §4.2, the level that
  v0.1 of the design left unspecified.

  The arithmetic that drives the design: 50 instances with 8 databases each is
  400 permanent connections used for nothing but monitoring. On a managed
  instance with a modest `max_connections` and a pooler already near capacity,
  that is not overhead — it is an outage.

  > **Forward reference (decision D22):** the default is 3 in this phase and
  > becomes **4** in sub-phase 7.1, when the ASH sampler takes a dedicated
  > connection. That change carries the instruction to update this default,
  > `INT-CONN-001` and the README together, so the two never disagree. Do not
  > pre-emptively set it to 4 here — the fourth connection would be unused and
  > `INT-CONN-001` would stop proving anything.

  ```go
  type ConnOptions struct {
  	MaxConnsPerInstance int           // default 3: 1 shared + 2 rotating (see D22)
  	MaxDatabases        int           // default 10
  	IdleTimeout         time.Duration // default 5m
  	Include, Exclude    []*regexp.Regexp
  }

  type Manager struct{ /* per-target shared pool + LRU of per-database pools */ }

  // Shared returns the long-lived connection on the maintenance database. Most
  // checks use only this.
  func (m *Manager) Shared(ctx context.Context) (*pgxpool.Conn, error)

  // ForDatabase returns a connection to datname, opening one on demand and
  // closing it after IdleTimeout. Blocks up to ctx deadline when the budget is
  // exhausted; never exceeds MaxConnsPerInstance.
  func (m *Manager) ForDatabase(ctx context.Context, datname string) (*pgxpool.Conn, error)
  ```

  Database discovery, run every 5 minutes:
  ```sql
  SELECT d.datname, COALESCE(s.xact_commit, 0) AS xact_commit
    FROM pg_database d
    LEFT JOIN pg_stat_database s ON s.datname = d.datname
   WHERE d.datallowconn AND NOT d.datistemplate
   ORDER BY 2 DESC;
  ```
  Selection rules, in order:
  1. drop names matching `Exclude` (default `^template\d$`, `^rdsadmin$`, `^azure_.*$`)
  2. when `Include` is non-empty, keep only matches
  3. if more than `MaxDatabases` remain, keep the most active by `xact_commit`
     and mark the rest `monitored=false` with `skip_reason="db_budget"`

  **Degradation must be visible.** The unmonitored databases are reported in the
  envelope with their reason so the API can say "5 databases not monitored"
  rather than implying full coverage. Silently covering 10 of 15 databases while
  presenting a complete-looking dashboard is the failure mode this rule exists
  to prevent.

  Every connection sets `application_name = 'pglens/<check>'`, so the monitoring
  sessions are identifiable in `pg_stat_activity` and excludable by an operator.
  Every session applies `check.ApplySessionLimits` from sub-phase 2.3 —
  `statement_timeout` per check, `lock_timeout = 1s`, and
  `idle_in_transaction_session_timeout = 30s`. The agent must never be the
  session that blocks on a lock (invariant I-6).
- **Unit tests:** `TestSelection_ExcludeDefaults` — templates and `rdsadmin` are dropped. `TestSelection_IncludeFilters` — only matching names survive. `TestSelection_BudgetKeepsMostActive` — 15 databases with `MaxDatabases` 10 keeps the 10 highest `xact_commit`, and the 5 dropped carry `skip_reason="db_budget"`. `TestSelection_Deterministic` — ties broken by name so two runs agree.
- **Integration tests:** `INT-CONN-001` — with `MaxConnsPerInstance` 3, driving 8 databases concurrently never exceeds 3 backends, asserted by querying `pg_stat_activity` from a separate connection while the load runs. **Invariant I-6.** `INT-CONN-002` — an idle per-database connection is closed after `IdleTimeout` (fake clock plus a real pool; assert the backend disappears). `INT-CONN-003` — `application_name` is `pglens/<check>` on every session the agent opens. `INT-CONN-004` — a database created after startup is discovered within one discovery interval; one dropped stops being scraped without producing an error. `INT-CONN-005` — with 15 databases and `MaxDatabases` 10, the envelope reports 5 entries with `monitored=false` and `skip_reason="db_budget"`.
- **e2e tests:** `SYS-DB-001` (budget visible through the API), `SYS-DB-002`, `SYS-DB-003` in phase 5.
- **Done:** `make test-integration` green; `INT-CONN-001` proves the connection ceiling; closed in `STATE.md`.

### 4.2 Scheduler, timeouts and circuit breaker

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — the agent's concurrency core.
- **Files:** `internal/agent/scheduler.go`, `internal/agent/breaker.go`, and `_test.go` beside each
- **Change:**
  - one scheduling entry per `(target, check, database?)`; entries run on a
    bounded worker pool (default `min(8, GOMAXPROCS)`), so a slow instance cannot
    starve the others
  - the interval is `policy.Interval` when the server supplied one, otherwise
    `check.DefaultInterval()` (decision D7). The server sends none in this plan;
    the plumbing exists so adding it later is additive
  - **initial jitter** of up to one interval per entry, so a restart does not
    stampede every check at once. `--testing.no-jitter` disables it and is
    required by the E2E harness for determinism
  - **per-check timeouts, never one global value.** The context deadline is
    `check.Timeout()`, and `ApplySessionLimits` sets the matching
    `statement_timeout`. `IDEA.md` §4.4 explains why a uniform 2s is wrong: a
    `locks` check during a lock storm is slow precisely when its output matters
    most, and a global timeout throws away the one measurement worth having
  - **circuit breaker on error, never on load.** After 3 consecutive failures an
    entry is suspended with exponential backoff (1m, 2m, 4m, capped at 15m) and
    a single `check_circuit_open` log line; one success closes it. Emits
    `check_error_total{check,reason}` and `check_skipped{check,reason}`

  > **Explicitly not implemented: adaptive sampling.** `IDEA.md` v0.1 proposed
  > reducing frequency when the database is under load. That is backwards — it
  > removes resolution exactly during the incident, producing a hole in the
  > graphs at the only moment anyone will look at them. Checks are instead
  > dimensioned to be affordable at full load, and anything that is not
  > affordable is on-demand rather than scheduled. Do not add a load-based
  > throttle; if one appears in a later diff, this paragraph is the reason to
  > reject it.

  - graceful shutdown: on `SIGTERM`, stop scheduling, let in-flight scrapes
    finish within 10s, flush the buffer, exit 0
- **Unit tests:** all with the fake clock from sub-phase 1.2; no test may call `time.Sleep`. `TestScheduler_RunsAtInterval` — a 10s check runs exactly 6 times over `Advance(60s)`. `TestScheduler_JitterWithinBounds` — with jitter on, first runs are spread across one interval and never beyond it; with `--testing.no-jitter` every entry fires at t0. `TestScheduler_PerCheckTimeout` — a check exceeding its timeout is cancelled at its own deadline, and a slower check with a longer timeout is not affected. `TestBreaker_OpensAfterThree` — two failures do not open it, the third does. `TestBreaker_Backoff` — the sequence is 1m, 2m, 4m, 8m, 15m, 15m. `TestBreaker_ClosesOnSuccess` — one success resets the counter and the backoff. `TestScheduler_SlowTargetDoesNotStarveOthers` — one target blocked for 30s does not delay another target's checks beyond one interval. `TestScheduler_GracefulShutdown` — in-flight scrapes complete and the run loop returns. `TestScheduler_Concurrent` — clean under `-race` with 50 entries.
- **Integration tests:** `INT-SCHED-001` — against a real container, a check whose statement exceeds its timeout returns a cancellation error and the backend is gone from `pg_stat_activity` within a second, proving the server-side `statement_timeout` really fired rather than the client merely giving up.
- **e2e tests:** `SYS-NET-002`, `SYS-NET-003`, `SYS-LOAD-002` in phase 5.
- **Done:** `make test` green under `-race -shuffle=on`; `grep -rn "time.Sleep" internal/agent/*_test.go` returns nothing; closed in `STATE.md`.

### 4.3 Durable disk buffer

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — durability. **`agent-1:opus` review gate:** crash behavior and the drop policy; a buffer that fills the host disk takes down the database it was watching.
- **Files:** `internal/agent/buffer/buffer.go`, `internal/agent/buffer/segment.go`, and `_test.go` beside each
- **Change:** implements `IDEA.md` §4.8, which v0.1 left unspecified.
  - append-only segments `NNNNNNNN.seg` in the buffer directory, rolled at 8 MiB
  - each record is length-prefixed with a CRC32C; on read, **a truncated or
    corrupt trailing record is skipped and counted**, not treated as fatal. A
    crash mid-write must cost the last record, never the whole buffer
  - `fsync` on segment roll, not per record. The trade is stated in the README:
    an unclean shutdown may lose the tail of the current segment. For metrics
    that is the right trade; for anything else it would not be
  - limits: `max_size` 512 MiB, `max_age` 6h (decision D19). On breach, **delete
    the oldest segment** — recent metrics are worth more than old ones — and add
    the record count to `agent_samples_dropped_total`
  - **disk full**: stop writing, keep scraping and keep serving health, raise
    `buffer_full`, and log once per minute rather than per record. The agent must
    degrade, never crash and never expand until the host disk is gone
    (invariant I-7)
  - counters: `agent_buffer_bytes`, `agent_buffer_segments`,
    `agent_samples_dropped_total{reason}`, `agent_buffer_corrupt_records_total`
  ```go
  type Buffer struct{ /* dir, opts, mu, segments */ }

  func Open(dir string, opts Options) (*Buffer, error)
  func (b *Buffer) Append(env []byte) error
  // Next returns the oldest unacknowledged envelope. Ack removes it only after
  // the server confirmed receipt, so a crash between send and ack costs a
  // duplicate delivery, never a loss. Duplicates are harmless because ingest is
  // idempotent (invariant I-3, enforced by the dedup indexes of 3.1).
  func (b *Buffer) Next() ([]byte, AckFunc, error)
  func (b *Buffer) Stats() Stats
  ```
  Choosing at-least-once over at-most-once is deliberate: the storage layer makes
  duplicates free, and there is no way to make losses free.
- **Unit tests:** `TestBuffer_AppendReadRoundTrip` — 1000 records survive close and reopen in order. `TestBuffer_SegmentRoll` — records crossing 8 MiB create a second segment. `TestBuffer_TruncatedTailSkipped` — truncating the last segment mid-record makes `Open` succeed, return every intact record, and increment the corrupt counter. `TestBuffer_CorruptCRCSkipped` — flipping a byte inside a record skips exactly that record. `TestBuffer_MaxSizeDropsOldest` — exceeding `max_size` deletes the oldest segment and the newest records survive. `TestBuffer_MaxAgeDropsOldest` — same with the fake clock. `TestBuffer_DiskFull` — with a write path that returns `ENOSPC`, `Append` returns an error, does not panic, sets `buffer_full`, and a later successful write clears it. `TestBuffer_AckRemoves` — an unacked envelope is redelivered after reopen; an acked one is not. `TestBuffer_Concurrent` — concurrent `Append` and `Next` clean under `-race`.
- **Integration tests:** none — the buffer touches only the filesystem and is fully covered at L1. `SYS-AGENT-003` proves it against a real full tmpfs.
- **e2e tests:** `SYS-NET-001` (10-minute outage replays with no duplicates and no gaps), `SYS-AGENT-003` (tmpfs full).
- **Done:** `make test` green under `-race`; the truncated-tail and disk-full tests pass; closed in `STATE.md`.

### 4.4 Push client, retry and clock skew

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — network client and failure semantics.
- **Files:** `internal/agent/pusher.go`, `internal/agent/pusher_test.go`
- **Change:**
  - build a `wire.Envelope` per push cycle (default every 15s) from the results
    accumulated since the last one, `gzip` it, `POST` to `/api/v1/push` with the
    bearer token
  - retry with exponential backoff and full jitter, capped at 60s, indefinitely.
    A push that cannot be delivered goes to the buffer and is retried from there;
    the collection loop never blocks on the network
  - **response handling, by status:**

    | Status | Action |
    |--------|--------|
    | `202` | ack the buffered envelope |
    | `400` protocol version | **stop collecting**, log once at error naming the required version, set health to `incompatible`. A silent version mismatch would produce a fleet that looks monitored and is not |
    | `401` revoked | **stop collecting**, log once at error, set health to `revoked`, keep the process alive so an operator sees the state. Do not exit: a container that exits is restarted forever and the reason scrolls away |
    | `401` bad token | same as above with health `unauthorized` |
    | `413` | drop the envelope, count it, log; splitting oversized envelopes is out of scope for this plan |
    | `5xx`, timeout, connection refused | keep in buffer, back off, retry |

  - **clock skew**: read the `Date` header of every response, compute
    `skew = server_time - local_time`, expose `agent_clock_skew_seconds`, and log
    a warning above 30s. Skew that nobody measures produces graphs nobody can
    explain
  - `/healthz` on the agent (default `:9187`) returns 200 with a JSON body
    carrying state, last successful push, buffer stats and skew; 503 in the
    `revoked`, `unauthorized` and `incompatible` states
- **Unit tests:** against an `httptest.Server` and the fake clock. `TestPusher_RetriesOn5xx` — three 500s then a 202 results in one successful delivery and one ack. `TestPusher_BackoffCapped` — the delay never exceeds 60s over 20 attempts, and jitter keeps two runs from being identical. `TestPusher_StopsOnRevoked` — after a 401 with `agent revoked`, no further request is made and health reports `revoked`. `TestPusher_StopsOnProtocolMismatch` — after a 400 naming version 2, no further request is made. `TestPusher_AcksOnlyOn202` — a non-202 leaves the envelope unacked and it is redelivered. `TestPusher_ClockSkew` — a `Date` header 5 minutes ahead yields a skew near 300s. `TestPusher_NeverBlocksCollection` — with a server that never responds, the collection loop keeps producing results and the buffer grows.
- **Integration tests:** `INT-PUSH-001` — against a real phase 3 server, an envelope is delivered, acked and visible through `/api/v1/clusters`. `INT-PUSH-002` — after revoking the agent in the database, the next push returns 401 and the agent stops; asserted by no new rows and a 503 on agent health.
- **e2e tests:** `SYS-NET-001`, `SYS-AGENT-004` (skew), `SYS-AGENT-005` (revocation).
- **Done:** `make test` and `make test-integration` green; a revoked agent provably stops without exiting; closed in `STATE.md`.

### 4.5 Configuration and the `check` subcommand

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — user-facing surface.
- **Files:** `internal/agent/config.go`, `cmd/pglens-agent/main.go`, `deploy/agent.example.yaml`, and `_test.go`
- **Change:**
  1. Configuration file, validated strictly — an unknown key is an error, not a
     shrug, because a silently ignored typo in a monitoring config is how an
     instance goes unwatched for a month:
     ```yaml
     server:
       url: http://pglens-server:8080
       token_file: /run/secrets/pglens_token   # or token: <literal>
     identity_path: /var/lib/pglens/identity.json
     push_interval: 15s
     buffer:
       path: /var/lib/pglens/buffer
       max_size: 512MiB
       max_age: 6h
     targets:
       - name: pg-app
         dsn: postgres://pglens@pg-app:5432/postgres?sslmode=require
         cluster_name: pg-prod-eu      # fallback when pg_control_system() is denied
         databases:
           include: ["app_.*"]
           exclude: ["^template\\d$"]
           max: 10
     checks:
       activity:        { interval: 10s }
       database_stats:  { interval: 30s }
       stat_statements: { interval: 60s, top_n: 50 }
     ```
     Environment overrides: `PGLENS_SERVER_URL`, `PGLENS_BOOTSTRAP_TOKEN`,
     `PGLENS_BOOTSTRAP_TOKEN_FILE`. **The DSN password is never logged and never
     appears in an error**; errors identify a target by `name` or fingerprint.
  2. `pglens-agent check --dsn <dsn>` — the friction remover for the "setup under
     five minutes" claim. Connects once and prints:
     ```
     pglens agent check
       server version   : 16.10  (server_version_num 160010) — supported
       role             : primary
       system_identifier: 7381927364512345678
       permission tier  : T0 (pg_monitor)
       extensions       : pg_stat_statements=yes  pg_buffercache=no
       databases        : 12 found, 10 monitored (2 skipped: db_budget)

     checks
       instance_info    enabled
       activity         enabled
       database_stats   enabled
       stat_statements  enabled
       replication_*    disabled  — requires role=primary or standby with a upstream

     warnings
       - identity path /var/lib/pglens is not on a persistent mount:
         instance ids will change on restart and instances will be duplicated
     ```
     Exit code 0 when every mandatory check is enabled, 1 otherwise, so it is
     usable in a provisioning script.
  3. The persistent-mount warning is produced by reading `/proc/self/mountinfo`
     and checking whether the identity directory resolves to a mount separate
     from the container root. This is the single most common containerized
     misconfiguration and it directly causes `SYS-AGENT-002`.
- **Unit tests:** `TestConfig_UnknownKeyIsAnError`. `TestConfig_Defaults` — omitted fields take documented defaults. `TestConfig_EnvOverrides` — env beats file. `TestConfig_TokenFileRead` — trailing newline stripped; unreadable file is a clear error. `TestConfig_DSNNeverLogged` — rendering a config to its string form and to every error path contains no password. `TestConfig_Validation` — empty `targets`, a malformed DSN, a bad duration and a negative `max` are each rejected with a message naming the field. `TestMountWarning` — with a fabricated `mountinfo`, the warning fires for a root-filesystem path and not for a mounted one.
- **Integration tests:** `INT-CFG-001` — `pglens-agent check` against a real container prints the correct version, role and tier, and exits 0; against a T0 role lacking the `pg_control_*` grants it still exits 0 but reports the manual fallback and a warning.
- **e2e tests:** used as a diagnostic in every phase 5 scenario failure dump.
- **Done:** `make test` and `make test-integration` green; `pglens-agent check` output matches the shape above; no password appears in any log at debug level; closed in `STATE.md`.

### 4.6 Packaging: binary and container

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — the deployment surface both `AGENT_MODE` values in phase 5 depend on.
- **Files:** `Dockerfile.agent`, `Dockerfile.server`, `deploy/compose/docker-compose.yml`, `deploy/systemd/pglens-agent.service`, `Makefile`
- **Change:**
  1. Multi-stage Dockerfiles producing static binaries on a minimal base
     (`gcr.io/distroless/static-debian12:nonroot`), running as **non-root**, with
     `VOLUME /var/lib/pglens` declared on the agent image and
     `HEALTHCHECK` wired to each component's health endpoint.
  2. `make build-images` builds both with the version stamped via `-ldflags`, and
     `make build-images-multiarch` uses `docker buildx` for `linux/amd64` and
     `linux/arm64`. Phase 5 depends on `build-images`, so add it now.
  3. `deploy/compose/docker-compose.yml` — a working production-shaped stack:
     `timescaledb` (pinned `timescale/timescaledb:2.29.0-pg17`, decision D3),
     `pglens-server`, and one `pglens-agent` with the identity volume,
     `/proc:/host/proc:ro` and `/sys:/host/sys:ro`.

     > The named volume on `/var/lib/pglens` is **not optional**. Without it the
     > agent regenerates its identity on every container recreation and every
     > instance is duplicated. The compose file carries a comment saying so, and
     > `SYS-AGENT-002` in phase 5 is the test that keeps the comment honest.

  4. `deploy/systemd/pglens-agent.service` with `StateDirectory=pglens`,
     `DynamicUser=yes`, `ProtectSystem=strict`, `NoNewPrivileges=yes`, and
     `Restart=on-failure`.
- **Unit tests:** none (packaging).
- **Integration tests:** `INT-PKG-001` — `make build-images` succeeds and `docker run --rm ghcr.io/manprint/pglens-agent:dev --version` prints the stamped version. `INT-PKG-002` — the agent image runs as a non-root uid, asserted with `docker run --rm --entrypoint id ghcr.io/manprint/pglens-agent:dev`.
- **e2e tests:** the whole of phase 5 runs against these images.
- **Done:** `make build-images` produces both images; `docker compose -f deploy/compose/docker-compose.yml up -d` brings up a stack where the agent reaches the server and `/api/v1/clusters` returns one cluster; `docker compose down -v` cleans up; closed in `STATE.md`.

### 4.7 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation; `agent-1:opus` reads it on the final phase.
- **Files:** `README.md`
- **Change:** this is the phase that makes the product usable end to end for the
  first time. Add and update:
  - **Quick start** — the three commands that take a reader from nothing to data:
    run `deploy/sql/monitoring_user.sql`, start the compose stack, `curl
    /api/v1/clusters`. Show the real output
  - **Running the agent** — both supported forms, binary with systemd and
    container, with a complete `docker run` and the compose fragment. **State
    explicitly that `/var/lib/pglens` must be a persistent volume and what breaks
    without it** (instance ids change on restart and instances are duplicated)
  - **Agent configuration** — the annotated YAML from `deploy/agent.example.yaml`,
    every key with its type and default, and the environment overrides
  - **`pglens-agent check`** — what it prints, how to read it, its exit codes,
    and that it is the first thing to run when something is not appearing
  - **Troubleshooting** — the failure modes an operator will actually hit: no
    data appearing (run `check`), `permission denied` on `pg_control_system`
    (run the grant), agent reporting `revoked`, databases missing because of the
    budget, buffer filling because the server is unreachable
  - **Known limits** — update with: no token rotation or mTLS; an unclean agent
    shutdown may lose the tail of the current buffer segment; a maximum of 10
    databases per instance by default; oversized envelopes are dropped rather
    than split
- **Unit tests:** none (documentation).
- **e2e tests:** none — every command in the Quick start was executed from a clean checkout and produced the documented output.
- **Done:** a reader following only the README goes from a clean machine to a working stack showing one cluster; the persistent-volume requirement is stated where a reader will see it before running the container, not in a footnote; no package or type name appears in the file; all gates green; closed in `STATE.md` with the §11 docs row for phase 4 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` and `make test-integration`
- **Coverage:** `make coverage-gate`
- **Build:** `make build-images` produces both images
- **Regression guard:** phases 0 to 3 gates still green; `INT-CHECK-015` and `INT-PIPE-002` still pass
- **README:** updated with the quick start, agent configuration and troubleshooting, free of implementation detail

## Phase done criterion

`docker compose -f deploy/compose/docker-compose.yml up -d` produces a stack in
which the agent discovers its databases, respects the connection ceiling
(`INT-CONN-001`), buffers through a server outage and replays without duplicates,
stops cleanly when revoked, and reports its own state on `/healthz`.
`pglens-agent check` prints an accurate readiness report including the
persistent-mount warning. README.md reflects this phase's shipped behavior, and
`STATE.md` §11 shows phase 4 `DONE` with every sub-phase closed.
