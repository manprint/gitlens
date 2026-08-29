# Phase 8 — Command channel and query plans

> **Intent:** add the only path in pglens that executes something on a monitored
> instance on request — query plans, session cancellation, exact bloat — with
> at-most-once semantics, three independent gates on the dangerous operation, and
> an audit row for every execution.
> **Shippable alone?** yes — with no command enqueued nothing happens, and with
> every per-instance gate closed by default the dangerous executors are
> unreachable.
> **Preconditions:** phase 2 `DONE` (the `facts` transport carries plans);
> phase 4 `DONE` for the exact-bloat executor to have somewhere to write.

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

## Security model, stated before any code

This is the one part of pglens that is not read-only, so the rules are strict and
they are all testable.

1. **Agents poll; the server never connects to an agent** (plan 002 D20). The
   agent already authenticates to the server with its bootstrap token; the
   command endpoints reuse exactly that authentication and add no second scheme.
2. **An agent may only fetch commands for its own `agent_id`**, and may only
   submit a result for a command it holds a valid claim token for.
3. **Three independent gates on `EXPLAIN ANALYZE`** (plan 002 D7): permission
   tier T1, the per-instance `allow_explain_analyze` flag in the agent config,
   and an explicit `"analyze": true` in the request. Any one closed means the
   command is **rejected by the agent, audited, and never executed** — the
   rejection is a command outcome, not an HTTP status, because the agent holds
   the authoritative tier and flags.
4. **Cancel and terminate need tier T2 and the per-instance `allow_signal`
   flag** (plan 002 D8).
5. **Every execution writes an audit row** before the result is stored, naming
   the command, the requester, the instance, and the outcome. Invariant I-1.
6. **At-most-once.** A command is claimed atomically. A claim that is never
   completed expires; an expired command is never claimed again. Invariant I-7.
7. **No command carries free-form SQL.** The `explain` executor takes a
   `queryid` and looks the text up in `query_texts`, or takes a statement that
   the server has already stored. It never accepts a statement string from the
   caller. This is what stops the command channel from being a remote SQL
   console.

---

## Sub-phases

### 8.1 Migration `0010_commands.sql`

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/store/migrations/0010_commands.sql` (new)
- **Change:**

  ```sql
  CREATE TABLE IF NOT EXISTS commands (
    command_id   uuid        PRIMARY KEY,
    tenant_id    text        NOT NULL DEFAULT 'default',
    agent_id     text        NOT NULL,
    instance_id  uuid        NOT NULL,
    cluster_id   bigint      NOT NULL,
    kind         text        NOT NULL CHECK (kind IN ('explain','cancel','terminate','pgstattuple')),
    args         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    state        text        NOT NULL CHECK (state IN ('pending','claimed','done','failed','expired')),
    requested_by text        NOT NULL DEFAULT 'api',
    claim_token  uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    claimed_at   timestamptz,
    finished_at  timestamptz,
    expires_at   timestamptz NOT NULL,
    result       jsonb,
    error        text
  );
  CREATE INDEX IF NOT EXISTS commands_queue_idx
    ON commands (tenant_id, agent_id, state, created_at)
    WHERE state = 'pending';
  CREATE INDEX IF NOT EXISTS commands_instance_idx
    ON commands (tenant_id, instance_id, created_at DESC);

  -- Append-only. Nothing in the codebase updates or deletes from this table.
  CREATE TABLE IF NOT EXISTS command_audit (
    audit_id     bigserial   PRIMARY KEY,
    tenant_id    text        NOT NULL DEFAULT 'default',
    command_id   uuid        NOT NULL,
    instance_id  uuid        NOT NULL,
    kind         text        NOT NULL,
    args         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    requested_by text        NOT NULL,
    executed_at  timestamptz NOT NULL DEFAULT now(),
    outcome      text        NOT NULL CHECK (outcome IN ('ok','error','rejected')),
    detail       text
  );
  CREATE INDEX IF NOT EXISTS command_audit_instance_idx
    ON command_audit (tenant_id, instance_id, executed_at DESC);

  CREATE TABLE IF NOT EXISTS query_plans (
    plan_id     bigserial   PRIMARY KEY,
    tenant_id   text        NOT NULL DEFAULT 'default',
    instance_id uuid        NOT NULL,
    cluster_id  bigint      NOT NULL,
    datname     text        NOT NULL,
    queryid     bigint,
    plan_hash   text        NOT NULL,
    analyzed    boolean     NOT NULL DEFAULT false,
    captured_at timestamptz NOT NULL,
    plan        jsonb       NOT NULL
  );
  CREATE UNIQUE INDEX IF NOT EXISTS query_plans_dedup_idx
    ON query_plans (tenant_id, instance_id, datname, queryid, plan_hash, analyzed);
  CREATE INDEX IF NOT EXISTS query_plans_lookup_idx
    ON query_plans (tenant_id, instance_id, queryid, captured_at DESC);
  ```

  The unique index on `query_plans` is the whole point of plan history: capturing
  the same plan twice stores nothing new, so the table holds one row per
  **distinct** plan and `captured_at` marks when that shape first appeared. Plan
  comparison then reduces to "how many rows exist for this queryid", which is
  cheap and needs no diffing engine.

- **Unit tests:** `TestMigrations_0010_ContainsCommands`.
- **e2e tests:** `INT-CMD-001` — migrations apply and the three tables exist with
  their indexes.
- **Done:** gates green + closed in `STATE.md`.

### 8.2 Command types and gate evaluation

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — security model; review gate
- **Files:** `internal/command/doc.go` (new), `internal/command/types.go` (new),
  `internal/command/gate.go` (new), `internal/command/gate_test.go` (new)
- **Change:**

  ```go
  package command

  type Kind string
  const (
      KindExplain      Kind = "explain"
      KindCancel       Kind = "cancel"
      KindTerminate    Kind = "terminate"
      KindPgstattuple  Kind = "pgstattuple"
  )

  // Args is the union of every command's arguments. Only the fields relevant
  // to the kind may be set; Validate rejects the rest, so a caller cannot
  // smuggle a field past the executor.
  type Args struct {
      QueryID   *int64 `json:"queryid,omitempty"`
      Datname   string `json:"datname,omitempty"`
      Analyze   bool   `json:"analyze,omitempty"`
      PID       *int   `json:"pid,omitempty"`
      Schema    string `json:"schema,omitempty"`
      Relation  string `json:"relation,omitempty"`
  }

  func (a Args) Validate(k Kind) error

  // Gates is what the agent knows about one target's permissions.
  type Gates struct {
      Tier                pgtype.PermTier
      AllowExplainAnalyze bool
      AllowSignal         bool
      HasPgstattuple      bool
  }

  // Allowed reports whether the command may run, and why not when it may not.
  // The reason string is returned to the caller verbatim, so it must name the
  // closed gate precisely and must never leak configuration beyond that.
  func Allowed(k Kind, a Args, g Gates) (bool, string)
  ```

  `Allowed` implements the table:

  | Kind | Requires |
  |------|----------|
  | `explain` without `analyze` | tier ≥ T1 |
  | `explain` with `analyze` | tier ≥ T1 **and** `AllowExplainAnalyze` **and** `Args.Analyze` |
  | `cancel` | tier ≥ T2 **and** `AllowSignal` |
  | `terminate` | tier ≥ T2 **and** `AllowSignal` |
  | `pgstattuple` | tier ≥ T1 **and** `HasPgstattuple` |

  `Validate` rejects: `explain` without `QueryID`; `explain` with `PID` set;
  `cancel` or `terminate` without `PID`; `cancel` or `terminate` with `QueryID`
  or `Analyze` set; `pgstattuple` without both `Schema` and `Relation`;
  any kind with a `Datname` containing a character outside
  `[A-Za-z0-9_$]` — identifiers reach SQL and must be constrained before they
  get there, even though the executor also quotes them.

  > **Defence in depth is the rule here, not belt and braces.** `Validate`
  > constrains the identifier, the executor quotes it with
  > `pgx.Identifier{...}.Sanitize()`, and neither one is allowed to be removed
  > because the other exists.

- **Unit tests:** an exhaustive table for `Allowed` covering every kind against
  every combination of the four gate fields — write it as a generated table, not
  by hand, and assert the count of allowed combinations, so an accidentally
  widened gate fails the test.
  Plus `TestValidate_ExplainRequiresQueryID`,
  `TestValidate_ExplainRejectsPID`,
  `TestValidate_SignalRequiresPID`,
  `TestValidate_SignalRejectsAnalyze`,
  `TestValidate_PgstattupleRequiresSchemaAndRelation`,
  `TestValidate_RejectsDatnameWithQuote`,
  `TestValidate_RejectsDatnameWithSemicolon`,
  `TestAllowed_ReasonNamesTheClosedGate`.
- **e2e tests:** covered by `INT-CMD-006`.
- **Done:** gates green + closed in `STATE.md`.

### 8.3 Server-side queue and endpoints

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/api_commands.go` (new),
  `internal/server/commands.go` (new),
  `internal/server/api_commands_test.go` (new),
  `internal/server/api_commands_integration_test.go` (new),
  `internal/server/http.go` (modified — `NewRouter` at `internal/server/http.go:11`;
  register the routes the way `API.RegisterRoutes` does at
  `internal/server/api.go:46`)
- **Change:** four endpoints.

  `POST /api/v1/instances/{id}/commands` — the caller's endpoint. Body:
  `{"kind": "...", "args": {...}}`. The server validates `Args`, resolves the
  instance's `agent_id` from the inventory, sets `expires_at = now +
  PGLENS_COMMAND_TTL` (default 5 minutes), inserts with `state = 'pending'` and
  returns `202` with `{"command_id": "..."}`. It does **not** evaluate the
  gates — the agent holds the authoritative tier and flags, and duplicating that
  knowledge server-side would let the two drift. The gate rejection comes back
  as a `rejected` result, and the audit row records it.

  `GET /api/v1/commands/{id}` — state, result and error.

  `GET /api/v1/agents/{agent_id}/commands?wait=25s` — **agent authentication
  only**, and only for its own `agent_id`; a mismatch is `403`, never `404`,
  because an agent asking for another agent's queue is a configuration error
  worth surfacing. It long-polls: return immediately with any claimable command,
  otherwise wait up to `wait` (capped at 30 s) and return `204` when nothing
  arrives. Implement the wait as a poll every 500 ms rather than with
  `LISTEN`/`NOTIFY`; the queue is low-volume, and a polling loop has no
  connection-lifetime failure mode to get wrong.

  `POST /api/v1/commands/{id}/result` — agent authentication only. Body carries
  the `claim_token`, an `outcome` and either a `result` or an `error`. A missing
  or mismatched `claim_token` is `409` and the row is left untouched. On success
  it writes the audit row and the result in one transaction.

- **Unit tests:** `TestEnqueue_ValidatesArgs`,
  `TestEnqueue_SetsExpiry`,
  `TestEnqueue_UnknownInstanceIs404`,
  `TestPoll_RejectsForeignAgentWith403`,
  `TestPoll_CapsWaitAt30s`,
  `TestPoll_ReturnsNoContentOnTimeout`,
  `TestResult_MismatchedClaimTokenIsConflict`,
  `TestResult_WritesAuditRow`.
- **e2e tests:** `INT-CMD-002` — enqueue, poll, submit a result, read it back
  through `GET /api/v1/commands/{id}`.
  `INT-CMD-003` — a second agent polling for another agent's id gets `403` and
  the command stays `pending`.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 8.4 Atomic claim and expiry

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — this is invariant I-7; review gate
- **Files:** `internal/server/commands.go` (modified),
  `internal/server/commands_integration_test.go` (new)
- **Change:** the claim is one statement, and it is the only place a command
  changes from `pending` to `claimed`:

  ```sql
  UPDATE commands
  SET state = 'claimed', claimed_at = now(), claim_token = gen_random_uuid()
  WHERE command_id = (
    SELECT command_id FROM commands
    WHERE tenant_id = $1 AND agent_id = $2 AND state = 'pending' AND expires_at > now()
    ORDER BY created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
  )
  RETURNING command_id, kind, args, claim_token, instance_id, cluster_id
  ```

  `FOR UPDATE SKIP LOCKED` is what makes two concurrent polls take two different
  commands rather than blocking or double-claiming. The `expires_at > now()`
  predicate is what stops an expired command from ever being claimed.

  Expiry runs in the same loop that the alert engine uses, or on each poll —
  choose the poll, because it needs no second leader:

  ```sql
  UPDATE commands SET state = 'expired', finished_at = now()
  WHERE tenant_id = $1 AND state IN ('pending','claimed') AND expires_at <= now()
  ```

  A `claimed` command whose agent died is expired by the same statement, which
  is why `claimed` is included. The result endpoint then rejects its late result
  with `409`, and the audit row records the attempt — a late result is
  information, not noise.

- **Unit tests:** none beyond the integration tests; this is a concurrency
  property and a mock cannot demonstrate it.
- **e2e tests:** `INT-CMD-004` — ten goroutines poll concurrently for one agent
  with five pending commands; exactly five are claimed, each by one caller, and
  no command is claimed twice.
  `INT-CMD-005` — a command past `expires_at` is never returned by a poll and
  becomes `expired`; submitting a result for it returns `409` and writes an
  audit row with `outcome = 'rejected'`.
- **Done:** both tests green, each run with `-count=5` to shake out ordering
  luck + closed in `STATE.md`.

### 8.5 Agent-side dispatcher

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/agent/command.go` (new),
  `internal/agent/command_test.go` (new),
  `internal/agent/config.go` (modified — `ChecksConfig` at
  `internal/agent/config.go:59`; add the `commands` block beside it),
  `cmd/pglens-agent/run.go` (modified — wire the dispatcher where the scheduler
  is started),
  `deploy/agent.example.yaml` (modified)
- **Change:** a goroutine per agent — not per target — that long-polls the
  server, dispatches to an executor by kind, and posts the result back.

  ```go
  type Executor interface {
      Kind() command.Kind
      Execute(ctx context.Context, t check.Target, a command.Args) (json.RawMessage, error)
  }

  type Dispatcher struct { /* executors by kind, targets by instance id */ }
  func (d *Dispatcher) Run(ctx context.Context)
  ```

  The loop:
  1. Poll with `wait=25s`. A `204` means loop again immediately. A transport
     error backs off `1s`, `2s`, `4s`, capped at 30 s, and resets on success.
  2. Resolve the target from `instance_id`. An unknown instance is a `rejected`
     result naming that the agent does not monitor it.
  3. Build `command.Gates` from the target's tier, its config flags and its
     extension set, and call `command.Allowed`. A closed gate posts a `rejected`
     result with the reason and executes nothing.
  4. Execute with a context deadline of 60 s, held on a **dedicated connection**
     obtained through `t.ConnFor(ctx, args.Datname)` and released immediately
     afterwards. Never reuse the shared check connection: an `EXPLAIN ANALYZE`
     lasting a minute would starve every scheduled check on that instance.
  5. Post the result with the claim token.

  Config additions:

  ```yaml
  commands:
    enabled: true            # default true; false disables polling entirely
  targets:
    - name: primary
      allow_explain_analyze: false   # default false
      allow_signal: false            # default false
  ```

  > **`commands.enabled: false` must stop the agent polling at all**, so an
  > operator who wants a strictly read-only agent has one switch rather than
  > three. Document it in 8.11.

- **Unit tests:** `TestDispatcher_UnknownInstanceIsRejected`,
  `TestDispatcher_ClosedGateRejectsWithoutExecuting` (the executor is a stub
  that fails the test if called),
  `TestDispatcher_BackoffOnTransportError`,
  `TestDispatcher_BackoffResetsOnSuccess`,
  `TestDispatcher_UsesDedicatedConnection`,
  `TestDispatcher_DisabledDoesNotPoll`,
  `TestDispatcher_ExecutionDeadlineIs60s`.
- **e2e tests:** covered by `SYS-CMD-001` in phase 9.
- **Done:** gates green + closed in `STATE.md`.

### 8.6 The `explain` executor

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — this executes SQL on a production database;
  review gate
- **Files:** `internal/agent/exec_explain.go` (new),
  `internal/agent/exec_explain_test.go` (new),
  `internal/agent/exec_explain_integration_test.go` (new)
- **Change:** the executor takes a `queryid` and never a statement.

  1. Look the statement text up in `pg_stat_statements` on the target, by
     `queryid`, in the database named by `Args.Datname`:
     `SELECT query FROM pg_stat_statements WHERE queryid = $1 LIMIT 1`. Not
     found means a `rejected` result saying the queryid is unknown on this
     instance — statements are evicted, and that is a normal outcome, not an
     error.
  2. Refuse to explain a statement that is not a `SELECT`, `INSERT`, `UPDATE`,
     `DELETE`, `MERGE`, `VALUES` or `WITH`. Determine that from the first
     keyword after stripping leading comments and whitespace. A `DO`, `CALL`,
     `CREATE` or anything unrecognised is rejected. `EXPLAIN` on a utility
     statement is not useful, and this is one more wall between the channel and
     arbitrary execution.
  3. The statement text from `pg_stat_statements` contains `$1`-style
     placeholders. Run it inside a transaction that is **always rolled back**,
     with `PREPARE`-free execution: issue
     `EXPLAIN (FORMAT JSON, VERBOSE, COSTS, BUFFERS <analyze>) <text>` directly.
     Placeholders make the statement unpreparable in some cases; when the server
     rejects it for that reason, return a `rejected` result explaining that the
     normalised statement cannot be explained, rather than attempting to
     substitute values. Substituting invented parameter values would produce a
     plan for a query nobody ran.
  4. `BUFFERS` is only meaningful with `ANALYZE`; include it only then.
  5. When `Args.Analyze` is true, and only after `command.Allowed` said yes:
     - open an explicit transaction,
     - `SET LOCAL statement_timeout = '30s'`,
     - run `EXPLAIN (ANALYZE, FORMAT JSON, VERBOSE, COSTS, BUFFERS) <text>`,
     - **`ROLLBACK` unconditionally**, in a `defer`, including on panic.
     The rollback is what makes `ANALYZE` on a mutating statement survivable. It
     does not make it free — the work still happened — and the README says so.
  6. Compute `plan_hash` as the SHA-256 of the plan JSON with every
     execution-specific field removed: `Actual Rows`, `Actual Total Time`,
     `Actual Startup Time`, `Actual Loops`, and every `Buffers` key. Two runs of
     the same plan shape must hash identically, or the history table fills with
     duplicates.
  7. Return the plan JSON and the hash as the command result.

- **Unit tests:** `TestExplain_RejectsUtilityStatement` (table over `CREATE`,
  `DROP`, `CALL`, `DO`, `VACUUM`),
  `TestExplain_AcceptsDMLAndWith`,
  `TestExplain_StripsLeadingCommentsBeforeKeyword`,
  `TestExplain_UnknownQueryIDIsRejected`,
  `TestExplain_BuffersOnlyWithAnalyze`,
  `TestExplain_PlanHashIgnoresActualFields`,
  `TestExplain_PlanHashDiffersOnDifferentShape`,
  `TestExplain_AnalyzeAlwaysRollsBack` (assert the rollback is issued even when
  the statement fails).
- **e2e tests:** `INT-CMD-006` — against a real container, an `explain` on a
  known `queryid` returns a plan whose root node type is what the query implies.
  With `allow_explain_analyze: false`, the same command with `analyze: true` is
  rejected and the audit row says which gate was closed.
  `INT-CMD-007` — `EXPLAIN ANALYZE` on an `UPDATE` leaves the row count
  unchanged, proving the rollback.
- **Done:** gates green + `INT-CMD-007` green + closed in `STATE.md`.

### 8.7 The `cancel` and `terminate` executors

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/agent/exec_signal.go` (new),
  `internal/agent/exec_signal_test.go` (new),
  `internal/agent/exec_signal_integration_test.go` (new)
- **Change:** `pg_cancel_backend($1)` and `pg_terminate_backend($1)`, tier T2 and
  `allow_signal` both required.

  Before signalling, confirm the pid is a client backend on this instance:
  `SELECT backend_type FROM pg_stat_activity WHERE pid = $1`. A pid that is not
  present, or whose `backend_type` is not `client backend`, is rejected. This
  stops a stale pid from a previous incident being used to signal a background
  worker or an autovacuum worker.

  Refuse to signal the agent's own backends: exclude any backend whose
  `application_name` starts with `pglens-agent/`. Cancelling the monitor from
  the monitor is a loop nobody wants to debug at three in the morning.

  The result carries what the signal function returned and the
  `application_name` and `usename` of the signalled backend, so the audit row
  says who was interrupted.

- **Unit tests:** `TestSignal_RejectsUnknownPID`,
  `TestSignal_RejectsNonClientBackend`,
  `TestSignal_RefusesOwnBackends`,
  `TestSignal_ResultCarriesTargetIdentity`.
- **e2e tests:** `INT-CMD-008` — start a long `pg_sleep` in a second session,
  cancel it through the executor, and assert the session is still connected but
  the statement ended; then terminate it and assert the connection is gone.
- **Done:** gates green + closed in `STATE.md`.

### 8.8 The `pgstattuple` executor

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/agent/exec_pgstattuple.go` (new),
  `internal/agent/exec_pgstattuple_test.go` (new),
  `internal/agent/exec_pgstattuple_integration_test.go` (new)
- **Change:** exact bloat on request, only where the extension already exists
  (plan 002 D6, R4).

  Gate on `t.HasExtension("pgstattuple")`; when absent, reject with a reason
  naming the extension and stating that pglens does not install it. Never
  attempt `CREATE EXTENSION` — the hard constraint of plan 001 is that no
  monitored instance needs one, and the moment the agent creates one that
  constraint is gone.

  ```sql
  SELECT table_len, tuple_count, tuple_len, dead_tuple_count, dead_tuple_len,
         free_space, free_percent
  FROM pgstattuple($1::regclass)
  ```

  Build the argument as `pgx.Identifier{schema, relation}.Sanitize()` and pass it
  as a parameter; never interpolate. Set `statement_timeout` to 300 s — this is a
  full relation scan and it must be allowed to finish or be cancelled cleanly,
  not left to run unbounded.

  The result is returned to the server, which writes it into `metrics_bloat`
  with `method = 'pgstattuple'`. That is why the `method` column exists: the
  exact figure sits beside the estimate for the same relation, and the API can
  show both without either overwriting the other.

- **Unit tests:** `TestPgstattuple_RejectsWithoutExtension`,
  `TestPgstattuple_SanitizesIdentifier`,
  `TestPgstattuple_TimeoutIs300s`,
  `TestPgstattuple_NeverIssuesCreateExtension` (assert on the recorded statement
  list, not on intent).
- **e2e tests:** `INT-CMD-009` — on a container where `pgstattuple` is
  installed, the exact result lands in `metrics_bloat` with
  `method = 'pgstattuple'` and the estimate row for the same relation is still
  present.
  `INT-CMD-010` — on a container without the extension, the command is rejected
  and no `CREATE EXTENSION` was attempted.
- **Done:** gates green + closed in `STATE.md`.

### 8.9 Plan storage and history API

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/api_plans.go` (new),
  `internal/store/write.go` (modified — follow the row-type + writer shape of
  `WriteMetrics` at `internal/store/write.go:158`),
  `internal/server/api_plans_test.go` (new),
  `internal/server/api_plans_integration_test.go` (new),
  `internal/server/http.go` (modified — `NewRouter` at `internal/server/http.go:11`)
- **Change:** when an `explain` command completes, the result handler writes a
  `query_plans` row with `ON CONFLICT DO NOTHING` on the dedup index, so
  re-capturing an unchanged plan is a no-op.

  `GET /api/v1/plans?instance_id=&queryid=&datname=&limit=` returns the distinct
  plans for that query, newest first, each with `plan_hash`, `captured_at`,
  `analyzed` and the plan JSON. Because the table holds one row per distinct
  shape, the response **is** the plan history: two rows mean the plan changed
  once, and `captured_at` says when the new shape first appeared.

  Add a derived field `"changed": true` on every row after the first, and
  `"total_shapes": <n>` at the top level, so a client does not have to infer the
  story from the row count.

- **Unit tests:** `TestPlansAPI_OrdersNewestFirst`,
  `TestPlansAPI_MarksChangedAfterFirst`,
  `TestPlansAPI_TotalShapes`,
  `TestPlansAPI_RequiresQueryID`,
  `TestWriteQueryPlan_DedupOnHash`.
- **e2e tests:** `INT-PLAN-001` — capture the same plan twice, assert one row.
  `INT-PLAN-002` — capture, then force a different plan with
  `SET enable_seqscan = off` on a fresh command, assert two rows and
  `total_shapes = 2`.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 8.10 Audit trail verification

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `internal/server/api_audit.go` (new),
  `internal/server/commands_integration_test.go` (extended)
- **Change:** add `GET /api/v1/instances/{id}/command-audit` returning the audit
  rows for an instance, newest first, with a `limit` capped at 500. There is no
  delete endpoint and no update path — the table is append-only by design, and
  the absence of a mutation endpoint is part of the guarantee.

  Then assert the guarantee: **every** terminal command state writes exactly one
  audit row. Drive one command of each kind and each outcome through the whole
  path and count the rows.

- **Unit tests:** `TestAuditAPI_LimitCapped`,
  `TestAuditAPI_NoMutationRoutesRegistered` (walk the chi router and assert no
  `POST`, `PUT`, `PATCH` or `DELETE` route exists under the audit path).
- **e2e tests:** `INT-CMD-011` — one audit row per command for the outcomes
  `ok`, `error`, `rejected` and for an expired command's late result; four
  commands, four rows, with the right `outcome` on each.
- **Done:** gates green + closed in `STATE.md`.

### 8.11 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** update these sections for what this phase actually made usable:
  - **On-demand operations** — a new section: what the command channel is, that
    the agent polls and the server never connects inward, the four command kinds
    and what each needs, and that everything is audited and readable.
  - **Configuration** — `commands.enabled`, `targets[].allow_explain_analyze`,
    `targets[].allow_signal`, `PGLENS_COMMAND_TTL`, all with defaults, and a
    clear statement that the two per-target flags default to **false**.
  - **Usage** — `curl` to enqueue an `explain`, poll its state, and read the
    plan history; a `curl` for the audit trail.
  - **Security** — the three gates on `EXPLAIN ANALYZE` spelled out, that
    `EXPLAIN ANALYZE` **executes the query** and is rolled back but not free,
    that commands never carry SQL from the caller, and that setting
    `commands.enabled: false` makes the agent strictly read-only.
  - **Limitations** — exact bloat requires `pgstattuple` to be already
    installed; pglens never installs an extension. Plans are captured on request
    only; there is no automatic plan capture. A normalised statement with
    placeholders may not be explainable, and pglens reports that rather than
    substituting values.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the `curl` examples were executed and produced the
  documented output.
- **Done:** an operator can enable and use on-demand `EXPLAIN` from the README
  alone and can see, in one place, exactly what pglens is permitted to execute;
  gates green; closed in `STATE.md` with the §11 docs row for phase 8 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `make test-integration`,
  `go test -race ./internal/agent/... ./internal/server/...`
- **Coverage:** `make coverage-gate`; `internal/command` at or above 95 %, since
  it is the gate logic
- **Regression guard:** `SYS-PERM-001` must still pass — an agent at tier T0
  with both flags false must behave exactly as it did before this phase.
- **README:** the three gates and the read-only switch are documented.

## Phase done criterion

With `allow_explain_analyze` false, an `explain` with `analyze: true` is
rejected and audited; with it true and tier T1, the plan is captured, rolled
back, hashed and stored once. Ten concurrent polls over five pending commands
claim each command exactly once. An expired command is never claimed and its
late result is rejected with an audit row. `INT-CMD-001` to `INT-CMD-011` and
`INT-PLAN-001`/`002` are green. README.md reflects this phase's shipped
behavior, and `STATE.md` §11 shows phase 8 `DONE` with every sub-phase closed.
