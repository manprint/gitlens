# Phase 15 — Integration and documents

Intent: all new features work together on the lab stack in English and Italian, and the documents describe the product as it now is.
Prerequisites: P1–P14.
Phase closure: P15, then PLAN; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D18, I-5, and every invariant I-1 … I-9.
- Lab stack: `/home/agent/pglens-odoo-stack` (read its `README.md` first; `.env` holds secrets and is never printed, copied into a record, or committed). It runs HA: three PostgreSQL 18 nodes and `hactl`; compose needs `-f compose.yml -f ha/compose.ha.yml`; the agent configuration is `ha/agent.ha.yaml`; a stopped database node is never started by hand (`ha/rejoin.sh`). UI sessions live in server memory: after a server restart, sign in again.
- Rules of this phase:
  - Only the lab stack. The real deploy machines named in the project `CLAUDE.md` are out of scope: no connection, no image transfer, no change.
  - No `git push`.
  - A step that restarts a database node or changes a PostgreSQL setting that needs a restart is destructive: before it, a VERIFIED backup younger than 24 hours must exist (`backup.sh`, then `verify_backup.sh`, directory `/home/agent/odoo-backups`); record the backup name and the verification output line in STATE.md §6. Without it the step does not run.
  - Settings that need only a reload are changed with `ALTER SYSTEM` + `SELECT pg_reload_conf()` on the primary by the lab's superuser (this is the lab's administrator action, not the pglens agent: I-1 is about the agent), and the previous values are recorded first (`SELECT name, setting FROM pg_settings WHERE name = ANY(...)`) so they can be restored.
  - After the checks: remove the test endpoints, containers, networks, volumes and build cache this phase created; leave the lab stack running in its previous shape.
- Reload-only settings used: `log_destination` (add `jsonlog`), `log_autovacuum_min_duration = 0`, `auto_explain.log_min_duration`, `auto_explain.log_format = json`, `auto_explain.log_analyze` (off), `log_lock_waits = on`, `log_temp_files = 0`, `log_checkpoints = on`. Restart-needing settings: `logging_collector` (must be `on` for `jsonlog`), `shared_preload_libraries` (for `auto_explain`, `pg_hint_plan`; `auto_explain` can instead be loaded per session with `session_preload_libraries`, which needs only a reload — prefer that). Check the current values first; a restart is needed only if `logging_collector` is `off`.
- Reference scenario (overview "Goal and reference scenario"), the nine steps checked in 15.2:
  1. Activity grouped by user, client address, application and service.
  2. Firing and resolved alert delivered to local Teams and email test endpoints; PagerDuty is proved by its tests (`TestNCH010`–`TestNCH014`), because its endpoint is fixed and no message is ever sent to the real PagerDuty service from the lab.
  3. Finding `txn.multixact_wraparound_risk` when the metric crosses the threshold.
  4. Yesterday against today on top SQL and on waits.
  5. A range older than raw retention read from hourly rollups.
  6. Logs filtered by class; `auto_explain` plans and autovacuum runs that came from the log.
  7. A suggested index validated; cost before and after.
  8. A workbook variant with one planner setting; two plans compared.
  9. `tools/list` and `tools/call` on `/mcp` with an API token return the same data as the REST API.

## Sub-phases

### 15.1 Language parity, pseudo-language, Italian e2e
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `README.md` section "Adding a language", `web/e2e/i18n_it.spec.ts`, `web/e2e/fullhd_routes.ts`; WRITE `web/e2e/fullhd_routes.ts` (the new routes: Logs, Workbook, and any other route added by this plan), `web/e2e/i18n_it.spec.ts`, the glossary file named in that README section (new terms: log class names stay English; `Servizio`, `Endpoint`, `Processi`, `Esecuzioni autovacuum`, `Quaderno di tuning`), any catalog file a gate reports.
- **Change:**
  Preconditions: P1–P14 DONE.
  Contract: `make i18n-check` passes with no key missing in `it` (console catalogs, Go catalogs, help topics); the pseudo-language `en-XA` renders every new view without a raw key or an untranslated literal (the existing test that scans for literals must cover the new feature folders `features/logs` and `features/workbook`: add them to its folder list if it has one); the lazy chunk `i18n-it` stays under 64 KiB gzip (`make web-budget`); Italian fits Full HD on the new routes (I18N-E2E-001). Selectors stay language-neutral (`data-*` attributes).
  Steps: 1. S1 — run `make i18n-check` and `make web-budget`; fix what they report in the catalogs only. 2. S2 — add the new routes to the Full HD route list. 3. S3 — glossary.
  Failure handling: the `it` chunk exceeds the budget → stop and ask agent-1 (splitting the chunk is a design decision).
- **Unit tests:** existing parity tests (`TestCatalogParity`, the console catalog and help parity tests) pass with the new keys; `UI-I18N-007-01` (the list of feature folders scanned for untranslated literals contains `logs` and `workbook`). Gates G-I18N, G-WEB-FULL.
- **e2e tests:** `I18N-E2E-001` extended to the new routes — it runs against the lab stack, so it is executed in unit 15.2 step S6; here it must compile and list the new routes (`pnpm exec playwright test --list e2e/i18n_it.spec.ts`).
- **Done:** gates pass; reviewed; unit closed and committed.

### 15.2 Lab verification of the reference scenario
- **Agent:** agent-2
- **Assignment:** implement (verification scripts and evidence only); agent-1 reviews the evidence and decides on every deviation.
- **Files:** NEW `test/parity/README.md` (how to run the check), `test/parity/endpoints/main.go` (one small Go program serving two local test endpoints: a Teams-style webhook that records request bodies to a file, and an SMTP sink on a local port that records messages; it binds only the address the lab server container can reach on the Docker host and no public interface), `test/parity/scenario.sh` (the API-level checks below, using `curl` against the lab server with a token read from an environment variable, never echoed), `test/parity/scenario_test.go` only if a helper needs a unit test; WRITE `web/e2e/` — NEW `parity.spec.ts` (console steps); the lab stack files outside the repository are changed only as listed here: `ha/agent.ha.yaml` (enable `checks.logs`, `host.processes`, ASH dimensions), the server environment in the lab compose override (`PGLENS_MCP_ENABLED=true`, `PGLENS_SMTP_*` pointing to the sink, `PGLENS_TRACE_URL_TEMPLATE`), each with a `.bak-007` copy made first.
- **Change:**
  Preconditions: 15.1 DONE; the lab stack is up and healthy (`hactl` status on `127.0.0.1:8008/status` shows one primary and two replicas).
  Contract: every step of the reference scenario is checked against the lab stack with the server and agent built from this working tree, and the evidence (command, expected, observed, pass or fail) is recorded in `history.md` under this unit. A failed step is a defect of the phase that owns the feature: report it to agent-1, who reopens that unit (PROTOCOL.md); this unit does not patch product code.
  Steps:
  1. S1 — backup rule: check `logging_collector`; if a restart is needed, make and verify the backup first and record it. Record previous setting values and make the `.bak-007` copies.
  2. S2 — build the images from the working tree (sandbox rule: `--network host` and the proxy build arguments) and roll them to the lab with its `deploy.sh`. `COV-E2E-001`, if run, must start within 2 minutes after `deploy.sh`.
  3. S3 — start the test endpoints; configure the `teams` and `email` channels through the API with the endpoints' addresses. Do not configure `pagerduty` with any key on the lab: its endpoint is the real service; rerun `TestNCH010`–`TestNCH014` instead and record their result as the PagerDuty evidence of step 2.
  4. S4 — workload: run tagged statements (sqlcommenter comment with `application`, `route`, `traceparent`) from two database users and two `application_name` values for 5 minutes; create and delete rows in a test table of a scratch database to trigger autovacuum; run one statement slower than `auto_explain.log_min_duration`; provoke one deadlock in the scratch database (the lab's existing deadlock scenario, if the lab README names one).
  5. S5 — `scenario.sh` checks, one function per scenario step: (1) `/api/v1/ash/top` with `group_by` = `usename`, `client_addr`, `application_name`, `service` each returns at least two groups; (2) the Teams file and the SMTP sink each contain a firing and a resolved message for the deadlock alert; (3) `/api/v1/findings` has no `txn.multixact_wraparound_risk` at the default threshold, and has it after the rule's threshold override is set to 0 through the advisor override route, which is then removed; (4) `/statements/compare` and `/ash/compare` for yesterday against today answer 200 with both periods present; (5) a metrics query whose `from` is older than the raw retention of its family answers `resolution: "1h"` (the lab may hold no data that old: the check is on the `resolution` field and on status 200, and it records how many buckets came back; retention settings of the lab are not changed); (6) `/logs?class=lock.deadlock` returns the deadlock, `/plans/samples` returns the slow statement with `source` `auto_explain`, `/autovacuum/runs` returns a run for the test table; (7) an `index_check` command for a suggestion on the scratch table reaches `done` with `cost_after < cost_before` — only if `hypopg` is installed in the lab database; if not, the check expects `hypopg_missing` and records it (installing an extension on the lab is allowed in the scratch database only: `CREATE EXTENSION hypopg` if the package is present in the lab image; do not rebuild the lab database image); (8) a workbook with a variant `enable_indexscan=off` runs and its plan hash differs from Baseline; (9) `POST /mcp` `tools/list` returns the registry and `tools/call pglens_list_instances` returns the same instance ids as `GET /api/v1/instances`.
  6. S6 — console: `parity.spec.ts` opens Activity (group by service), Logs (class filter), Plan samples, Autovacuum activity, Processes panel, Missing indexes (Validate), Workbook, Settings (three new channels, retention groups); then `I18N-E2E-001` in Italian on the new routes. Session handling through the existing `signedInPage` fixture.
  7. S7 — restore: remove the two channels and the override, stop the endpoints, restore the `.bak-007` files and the recorded settings unless agent-1 decides the lab keeps the new configuration (record the decision), `pg_reload_conf()`, redeploy if the configuration changed, confirm `hactl` status healthy, clean Docker objects created by this unit.
  Recovery boundary: after S2, S5 and S6.
  Failure handling: lab stack unhealthy before S1 → BLOCKED, report to agent-1 (do not repair the lab inside this unit). Any doubt whether a step is destructive → treat it as destructive.
- **Unit tests:** `go vet ./test/parity/...` and `go build ./test/parity/...` pass; `bash -n test/parity/scenario.sh` passes; `shellcheck` if installed. The Go program is covered by its use in S3 (no production code).
- **e2e tests:** the nine `scenario.sh` checks and `parity.spec.ts` pass on the lab; `I18N-E2E-001` passes. This is the reference scenario of G-FINAL.
- **Done:** nine steps pass, or each deviation named in the Contract is recorded with its reason and accepted by agent-1; lab restored and healthy; no secret in any record; evidence in history.md; unit closed and committed.

### 15.3 Documents
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `README.md` (feature list or matrix at the top: one line per new capability with a link to its section; configuration reference: every new agent key and server environment variable in the existing tables), `CLAUDE.md` of the project (short lines, in its existing style, for: the `logs` check and `internal/pglog`; fact kinds and their tables; rollups and `rollupFor`; `index_check` and workbook through commands; `internal/sqltag`; `internal/mcp` and `PGLENS_MCP_ENABLED`; `test/parity`; keep the file lean — add at most 10 lines), `web/CLAUDE.md` (new feature folders), `docs/onboarding.md` (dated update of the source map: new packages, migrations, routes), `CONTRIBUTING.md` only if a new make target was added, `docs/api.md` through `make api-docs`.
- **Change:**
  Preconditions: 15.2 DONE.
  Contract: every interface row of the overview table "Interfaces and compatibility" is documented in the README with its default; every environment variable and agent key named in phases 1–14 appears in the README configuration reference (check with `grep` for each name); no document names a customer, a real host or a secret; documents state the limits recorded during execution (skips, deviations of 15.2, `track_activity_query_size`, hint limits, `/proc` I/O permission, at-most-once log delivery).
  Steps: 1. S1 — list of names to document (from the overview table and STATE/history records). 2. S2 — README. 3. S3 — the two `CLAUDE.md` files and onboarding. 4. S4 — `make api-docs`.
- **Unit tests:** `TestMCP014` still passes (README tool table); `make api-docs` leaves no diff after a second run; a `grep -c` for each name of S1 in `README.md` is ≥ 1 (record the list and the counts). Gates G-API, G-GO-FOCUS `./internal/server/ -run TestMCP014`.
- **e2e tests:** N/A — documents.
- **Done:** documents updated; counts recorded; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT-ALL, G-RO, G-API, G-WEB-FULL, G-I18N; assertions: every invariant test of the overview table "Invariants" is discovered and green (list them by name in the evidence); no integration test skipped except the skips recorded with a reason in phases 10, 12, 13.
- README obligation: unit 15.3.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews the scenario evidence, the restored lab, the documents against the interfaces table.
- Open closure unit P15, record review and result, mark the phase DONE, close, commit `phase(007/P15): PHASE 15 COMPLETE — Integration and documents; gates and review passed`.
- P15 edits no production code or tests. Blocked gate: P15 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P15.
- PLAN closure (right after P15 DONE): open unit PLAN, run G-FINAL (the full gates above on the final tree plus the recorded reference scenario of 15.2, rerun only if the tree changed after 15.2 in a way that touches product code), confirm the board has every phase DONE, compact STATE.md, close, commit `plan(007): PLAN 007 COMPLETE — ObservabilityParity; 16 phases, final gates passed`. No push, no deploy to the real machines.
