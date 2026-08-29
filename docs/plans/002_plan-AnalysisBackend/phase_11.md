# Phase 10 — Packaging, CI, and final documentation

> **Intent:** make everything the plan built installable, runnable and
> understandable by someone who was not here — CI that runs the whole matrix,
> deploy artefacts that work unedited, the exact SQL grants for each permission
> tier, and an honest statement of what pglens cannot see.
> **Shippable alone?** yes — it changes no product behaviour, only what ships
> around it.
> **Preconditions:** phases 0–9 `DONE`. Documenting a feature before its phase is
> done produces documentation that describes intentions, which is worse than
> none.

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

## The rule for this whole phase

**Every command, snippet, YAML file and SQL script written here must be executed
before it is committed.** Not reviewed — executed. Documentation that was never
run is a guess with good formatting, and this phase exists specifically to
eliminate guesses. Where a snippet cannot be executed in this environment, mark
it explicitly as untested in the text itself rather than presenting it as
verified.

---

## Sub-phases

### 10.1 CI matrix

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — mechanical work
- **Files:** `.github/workflows/ci.yml` (modified),
  `.github/workflows/e2e.yml` (modified or new),
  `Makefile` (modified)
- **Change:** extend the plan 001 workflows to cover what plan 002 added, without
  making the pull-request path slower than a developer will tolerate.

  Three jobs:

  | Job | Trigger | Contents | Budget |
  |-----|---------|----------|--------|
  | `unit` | every push and PR | `make fmt-check`, `make lint`, `make build`, `make test`, `make coverage-gate` | 5 min |
  | `integration` | every push and PR | `make test-integration` against the PostgreSQL version matrix | 20 min |
  | `e2e` | PRs to `main`, plus nightly | `make build-images` then `make test-e2e-full` | 35 min |

  The integration matrix runs **PG 15, 16, 17 and 18**. Every one of them,
  every time: the whole point of the version-sweep sub-phases in phases 3, 4 and
  5 is that a column moved between releases, and a matrix that skips a version to
  save minutes will find that out from a user instead.

  The `e2e` job additionally runs the three acceptance scenarios twice, once with
  `AGENT_MODE=container` and once with `AGENT_MODE=binary`.

  Add a `make ci-local` target that runs the `unit` and `integration` jobs
  exactly as CI runs them, so a developer can reproduce a CI failure without
  reading the workflow YAML.

  Cache the Go build and module cache keyed on `go.sum`. Do **not** cache Docker
  images for the E2E job — a stale image is the single most confusing CI failure
  there is, because the code in the repo and the code under test differ silently.

- **Unit tests:** none (CI configuration).
- **e2e tests:** the workflow is proven by running green on the branch that
  introduces it, including one deliberately failing test pushed and then
  reverted, to confirm the job actually fails rather than reporting green on a
  broken suite. A CI pipeline nobody has seen fail is a CI pipeline nobody has
  tested.
- **Done:** all three jobs green on the branch; the deliberate-failure check
  performed and reverted; gates green + closed in `STATE.md`.

### 10.2 Deploy artefacts

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — mechanical work
- **Files:** `deploy/docker-compose.yml` (modified),
  `deploy/agent.example.yaml` (modified),
  `deploy/server.example.env` (modified or new),
  `deploy/systemd/pglens-agent.service` (new),
  `deploy/systemd/pglens-server.service` (new)
- **Change:** everything a new user needs to run pglens, updated for the ten
  checks, the alert engine, the advisor and the command channel.

  `deploy/docker-compose.yml` — TimescaleDB, one server, one agent, with every
  new environment variable present and commented, not merely defaulted. An
  operator reading the compose file must be able to see that
  `PGLENS_ALERT_SLACK_WEBHOOK_URL` exists without grepping the source.

  `deploy/agent.example.yaml` — every new config key with its default and a
  one-line comment, including the full `checks:` block listing all ten new checks
  with their intervals, and the two per-target gate flags shown **false**, which
  is their default and the value most deployments should keep.

  `deploy/server.example.env` — the server's variables: DSN, listen address,
  alert engine interval, advisor interval, `PGLENS_COMMAND_TTL`, retention and
  compression settings.

  The two systemd units — `Type=simple`, `Restart=on-failure`,
  `RestartSec=5`, `EnvironmentFile=`, and hardening that costs nothing:
  `NoNewPrivileges=true`, `PrivateTmp=true`, `ProtectSystem=strict`,
  `ProtectHome=true`. For the **agent** unit add a comment stating that phase 6
  host metrics need `/proc` and `/sys` readable, and that `ProtectSystem=strict`
  still permits that.

  Then run each of them: `docker compose up` to a healthy stack, and the systemd
  units validated with `systemd-analyze verify`.

- **Unit tests:** none (deployment artefacts).
- **e2e tests:** `SYS-DEPLOY-001` — bring up `deploy/docker-compose.yml`
  **unedited**, wait for health, and assert `GET /api/v1/instances` returns one
  instance with recent data. The example that ships must be the example that
  works; anything else trains users to distrust the docs.
- **Done:** compose stack healthy from a clean checkout, systemd units verify,
  `SYS-DEPLOY-001` green + closed in `STATE.md`.

### 10.3 Permission-tier grant scripts

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — mechanical work
- **Files:** `deploy/sql/monitoring_user.sql` (new),
  `deploy/sql/README.md` (new)
- **Change:** one script that creates the monitoring role at a chosen tier, with
  the tiers separated by clearly marked blocks so an operator can stop reading at
  the tier they want.

  ```sql
  -- Tier 0 — read-only, no query text, no extensions. The default.
  CREATE ROLE pglens LOGIN PASSWORD :'pglens_password';
  GRANT pg_monitor TO pglens;
  GRANT CONNECT ON DATABASE :"dbname" TO pglens;

  -- Tier 1 — adds query text and EXPLAIN without ANALYZE.
  -- pg_read_all_stats is already implied by pg_monitor; listed for clarity.
  GRANT pg_read_all_stats TO pglens;

  -- Tier 2 — adds cancel and terminate. Grant ONLY if you intend to allow
  -- pglens to interrupt sessions, and only together with allow_signal.
  GRANT pg_signal_backend TO pglens;

  -- Tier 3 — extensions. pglens NEVER creates an extension itself; this block
  -- is for the DBA to run deliberately, on the databases they choose.
  -- CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
  -- CREATE EXTENSION IF NOT EXISTS pgstattuple;
  ```

  The tier 3 statements ship **commented out**, and the comment says why: pglens
  creating an extension would silently break the plan 001 constraint that no
  monitored instance needs one.

  `deploy/sql/README.md` states, in a table, exactly which checks and which
  advisor rules become available at each tier, and what pglens reports when the
  tier is insufficient — a `degraded` entry naming the missing grant, per
  invariant I-3. An operator deciding whether to grant `pg_signal_backend` needs
  to see what they gain and what they lose by refusing.

  Run the script at each tier against a container and confirm the agent starts
  and reports the expected degradation set. A grant script that has never been
  applied is a guess.

- **Unit tests:** none (SQL artefact).
- **e2e tests:** `SYS-PERM-002` — apply the tier 0 block only, start the agent,
  and assert the API reports the expected `degraded` set: no query text, no
  `EXPLAIN`, no signals, and each one named. Then apply tier 1 and assert the
  query-text rules go from degraded to active without an agent restart —
  permissions granted at runtime must take effect at the next scrape, not at the
  next deployment.
- **Done:** script applied at every tier against a real container, results match
  the README table, `SYS-PERM-002` green + closed in `STATE.md`.

### 10.4 Declared limits

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `docs/LIMITS.md` (new)
- **Change:** the honest list of what pglens does **not** do, following
  `IDEA.md` §11 and everything the previous phases established. This document
  exists because every one of these limits, undocumented, becomes a bug report.

  Cover at minimum:

  1. **Backups.** pglens observes the *archiver* — `archived_count`,
     `failed_count`, the age of the last success. It cannot tell you a
     `pg_basebackup` succeeded, that a restore works, that the archive
     destination is readable, or that a backup is restorable. Recovery
     verification is out of scope and no metric here should be read as proof of
     it.
  2. **Bloat.** The scheduled figure is a statistics-based **estimate** and can
     be wrong in both directions, especially on tables with wide or heavily
     `TOAST`ed rows or with stale statistics. The exact figure needs
     `pgstattuple`, is on-demand only, and takes a full relation scan.
  3. **Query plans.** Captured on request only, from the normalised statement
     text in `pg_stat_statements`. A statement whose placeholders make it
     unexplainable is reported as such; pglens does not invent parameter values,
     so the plan you get is never a plan for a query nobody ran.
  4. **`EXPLAIN ANALYZE` executes the statement.** It is rolled back, always,
     but the work happens: locks are taken, triggers fire, WAL is written, and
     side effects outside the transaction — `NOTIFY`, `COPY TO PROGRAM`,
     `dblink` — are not undone by a rollback.
  5. **Relation cardinality.** Above the configured budget, relation-level
     series are truncated by rank; the API reports `truncated: true` and the
     count not reported. The truncated relations are not sampled elsewhere and
     not backfilled later.
  6. **Host metrics** are available only for instances the agent runs beside, as
     determined by `host_local`. For a remote instance the host endpoint returns
     `available: false` with the reason. pglens does not infer host memory from
     inside PostgreSQL.
  7. **Advisor thresholds are heuristics** tuned for general-purpose OLTP. A
     data warehouse will trip several of them legitimately. Findings are advice
     with reasons attached, not verdicts, and the reason is printed so it can be
     disagreed with.
  8. **No sampling below the scrape interval.** A lock held for 200 ms between
     two 15 s scrapes is invisible, and no amount of post-processing recovers
     it. ASH sampling narrows this window; it does not close it.
  9. **PostgreSQL 15 to 18 only.** Older versions lack views this plan depends
     on; newer ones are untested until their column sets are verified the way
     phase 5 verified PG 18.
  10. **Single tenant in practice.** The schema carries `tenant_id` throughout,
      but no authentication separates tenants yet. Do not treat it as an
      isolation boundary.

  Link `docs/LIMITS.md` from `README.md` prominently, not in a footer. A limits
  document nobody finds does not do its job.

- **Unit tests:** none (documentation).
- **e2e tests:** none. **However**, every claim in the document must correspond
  to something the test suite shows or to a stated absence — walk the list and
  name, for each item, the test or the deliberate gap. Write that mapping into
  `STATE.md` §7, not into `LIMITS.md`, which stays readable for users.
- **Done:** all ten items written and cross-checked against the suite; gates
  green + closed in `STATE.md`.

### 10.5 Final documentation read and plan closure

Mandatory closing sub-phase of the phase — and, because this is the last phase,
the closing read of the whole plan.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — final review gate
- **Files:** `README.md` (repo root), `docs/LIMITS.md` (modified as needed),
  `CONTRIBUTING.md` (modified as needed), `STATE.md`
- **Change:** read `README.md` end to end **as a new user who has never seen this
  repository**, with the product running in front of you, and fix what is wrong.
  This is a read-and-repair sub-phase, not a writing sub-phase; the earlier
  README sub-phases already wrote the content, and what is being tested here is
  whether they cohere.

  Check, concretely:

  1. **Every command in the README was executed and produced what the README
     says.** Every one — not a sample.
  2. **Every configuration key mentioned exists**, spelled exactly as the code
     reads it, with the default the code actually uses. Grep the config structs
     and compare; a wrong default in a README is a support ticket with a delay
     fuse.
  3. **Every endpoint documented exists** and returns the documented shape. Walk
     the router and compare both ways: documented-but-missing **and**
     existing-but-undocumented.
  4. **The security section is complete and accurate** — the three
     `EXPLAIN ANALYZE` gates, the T2 requirement for signals, the audit trail,
     and `commands.enabled: false` as the strictly-read-only switch.
  5. **The structure serves a reader in this order:** what pglens is → what it
     needs → install → first run → what you get → configuration → security →
     limits → testing → contributing. A new user must reach a working install
     without scrolling past reference material.
  6. **`docs/LIMITS.md` is linked from the top half of `README.md`.**
  7. **No section describes something a later phase changed** — in particular
     the plan 001 README text about which checks exist and what the agent
     collects, which phases 3 to 6 made incomplete.

  Then close the plan in `STATE.md`: every phase `DONE`, every `T-*`, `INT-*`
  and `SYS-*` id resolved to `DONE` or `SKIPPED` with a reason, §9 blockers
  empty or each one explicitly carried forward with an owner, and Q-A and Q-B
  from `overview.md` answered or explicitly deferred to plan 003 with the reason
  recorded. An open question that is silently dropped becomes a surprise for
  whoever writes plan 003.

- **Unit tests:** none (documentation).
- **e2e tests:** none — the verification *is* executing every documented command
  against a running stack.
- **Done:** a new user can install, run, configure and secure pglens from
  `README.md` alone, and knows its limits from `docs/LIMITS.md`; every discrepancy
  found in the seven checks above is fixed, not noted; `STATE.md` shows the plan
  closed; gates green + closed in `STATE.md` with the §11 docs row for phase 10
  set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `make test-integration`, `make test-e2e-full` —
  the entire suite, one final time, on a clean checkout
- **Coverage:** `make coverage-gate`
- **CI gate:** all three CI jobs green on the branch, and observed failing once
  on a deliberately broken test
- **Deploy gate:** `SYS-DEPLOY-001` green from the unedited compose file
- **Permission gate:** `SYS-PERM-001` and `SYS-PERM-002` green
- **README:** `README.md` documents every shipped behaviour of phases 0–10, with
  `docs/LIMITS.md` linked from its top half
- **Docs gate:** every command in `README.md`, `deploy/sql/README.md` and
  `CONTRIBUTING.md` executed as written

## Phase done criterion

A clean clone, with Docker available, reaches a running pglens stack collecting
from a PostgreSQL instance by following `README.md` alone, with no source
reading and no undocumented step. CI runs unit, integration across PG 15–18, and
the full E2E suite, and has been seen to fail on a broken test. The grant script
has been applied at every tier and produces the documented degradation set.
`docs/LIMITS.md` states what pglens cannot see, including that it does not verify
backups. `STATE.md` shows all eleven phases `DONE`, every test id resolved, and
the plan closed.
