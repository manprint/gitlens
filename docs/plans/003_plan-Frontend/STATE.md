# STATE — 003 Frontend

_Last updated: 2026-08-31 — sub-phase 2.2 closed; sub-phase 2.3 opened._

Single source of execution truth for this plan. No other file in this folder
claims a status. When this file and the repository disagree, **the repository
wins** and this file gets corrected.

---

## §0 — Session protocol (read this first, every session)

**Unit of work.** One sub-phase, one `task`, one `bug`, one `verify` audit, or
one correction from a verify report. Never anything larger, never anything
smaller.

**Session start — any agent, any context state:**

1. Read this file **before any other plan file**.
2. Read §1 `Status`:
   - `OPEN` — a unit was claimed and may be half-written. Read §6, then **finish
     or revert it** before starting anything new. When §3 shows WIP commits on
     and `HEAD` is a `wip:` commit, that commit *is* the in-flight work: its diff
     is the authoritative record of what was written, and §6 says why it stopped.
   - `none` — nothing in flight. Open the unit named in §1 `Next action:`.
3. **Verify reality before editing.** Run the gate commands in §3 and compare the
   result with what §1, §7 and §11 claim. This file describes intent; the repo is
   the truth.
4. Read only the file §1 points at — the phase file at the named sub-phase, or
   the verify report for a correction. Read `overview.md` only when §1 flags
   missing design context.

**Open a unit — before touching any code, mandatory:**
set §1 `Type`, `ID`, `Status: OPEN`, `Intent`, `Next action:`, `Assigned`; set §6
to `claimed — nothing written yet`; bump the header timestamp. Only then edit.

**Close a unit — after its gates are green, mandatory:**
append the §4 ledger row; reset §6 to `none — tree consistent`; update §5 (files
touched), §7 (verification results), §8 (deviations), §9 (blockers), §10 (dead
ends), and the §11 board; set §1 to the next unit with `Status: none`; bump the
timestamp. When §3 shows WIP commits on, commit the closed unit now — code,
tests, state, docs, ledger in one commit — and record its sha in the §4 row.

**Interrupted mid-unit:** leave §1 `OPEN` and flush §6 with exactly what is
half-done — files written, edits still pending, temporary code to remove. `OPEN`
with an empty §6 is an execution bug.

A unit is **not** `DONE` until its gates are green **and** it is closed here.

---

## §1 — Current position

| Field | Value |
|-------|-------|
| **Type** | sub-phase |
| **ID** | 2.3 |
| **Status** | `OPEN` |
| **Intent** | Protect API routes with the exact session, bearer, and exemption credential rule |
| **Next action:** | Complete sub-phase **2.3** in [phase_03.md](phase_03.md): add session middleware and its tests |
| **Assigned** | `agent-2:sonnet` |
| **Repo state** | Phase 0 and phase 1 sub-phases 1.1–1.6 and phase 2 sub-phases 2.1–2.2 are complete and committed. E2E evidence is durable, all three plan 002 audit findings are `FIXED`, Q-B is closed by D11, the OpenAPI contract has bidirectional route coverage, the static API reference is generated offline, README links the authoritative contract, UI configuration defaults/validation are covered, and the in-memory session store has bounded expiry cleanup. This unit adds the security boundary. |
| **Phase file** | [phase_03.md](phase_03.md) |

Phase 0 sub-phases 0.1–0.6 and phase 1 sub-phases 1.1–1.6 plus phase 2 sub-phases 2.1–2.2 are closed; sub-phase 2.3 is the next unit.
Phase 2 and the remaining frontend work are still pending.

---

## §2 — Recap (self-contained; assume no prior context)

**What pglens is.** A self-hostable Go monitoring system for fleets of
PostgreSQL servers: an agent per host pushes metrics to a server backed by
TimescaleDB, and the server exposes a read API plus alerting, an advisor and
on-demand operations. Plans 001 (collection) and 002 (analysis backend) are
complete: 41 HTTP routes, 39 advisor rules, alerting with silences, an ASH
sampler, and an L1–L5 test pyramid with a Docker-compose E2E harness.

**What plan 003 adds.** The web interface — the product's tenth missing piece.
Until now the only client is `curl`. This plan builds a browser client and, to
make it possible, closes four backend gaps found in the code, not assumed:

1. no OpenAPI contract, so no typed client and no way to detect drift;
2. no authentication on the read routes, so a browser client would expose the
   whole fleet to anyone who can reach the port;
3. no CORS — deliberately kept that way by serving the interface from the server
   itself, which removes the entire class of cross-origin cookie problems;
4. no streaming, so the interface polls on a documented per-resource schedule.

**Shape of the work.** 18 phases. Phase 0 closes the three open findings from
plan 002's audit. Phases 1–3 are Go work: the contract, session authentication,
and the embedded SPA. Phases 4–7 build the frontend workspace, its test harness,
the typed API layer and the app shell. Phases 8–16 build the nine pages. Phase 17
packages it, proves it in a real browser, and finishes the documentation.

**The single idea the plan is built around.** The product's stated principle is
that the interface must never present an absence as a value: an unknown lag is
not zero, a disabled sampler is not an empty chart, a truncated list is not a
complete one, and a rule that could not be evaluated is not a rule that passed.
Everything structural in this plan — the eight state primitives held at 100%
coverage, the mandatory degraded-path matrix (rule T-4), the ajv-validated
fixtures, the MSW server that errors on any unhandled request — exists to make
that principle mechanically enforced rather than merely intended.

**Where to read more.** [overview.md](overview.md) holds the decisions D1–D18,
the invariants I-1…I-6, the reuse map with `path:line` anchors, and the
reference table R1–R16. Each phase file is self-contained and executable without
re-reading the codebase.

---

## §3 — Environment and gate commands

| Field | Value |
|-------|-------|
| **Repo root** | `/mnt/fabio/dati/Git/SperimentazioniAI/postgres-analyze` |
| **Branch at plan time** | `main` |
| **Go** | 1.25 (`go.mod`) |
| **Node** | 24 LTS, **pnpm** 10 (decision D8) |
| **WIP commits** | **on** — enabled by the `--wip-commit` invocation. Close each unit with one local commit; record its sha in §4. |

### Gate commands

Run from the repo root.

| Gate | Command | Applies from |
|------|---------|--------------|
| Go format | `make fmt-check` | phase 0 |
| Go lint | `make lint` | phase 0 |
| Go unit + integration-lite | `make test` | phase 0 |
| Go coverage gate | `make coverage-gate` | phase 0 |
| Integration (L2) | `make test-integration` | phase 2 |
| Build images | `make build-images` | phase 3 |
| E2E (L3), container agent | `make test-e2e` | phase 2 |
| E2E (L3), binary agent | `AGENT_MODE=binary make test-e2e` | phase 2 |
| E2E full with evidence | `make test-e2e-full-evidence` | phase 0 (created there) |
| Web install | `make web-install` | phase 4 |
| Web typecheck | `make web-typecheck` | phase 4 |
| Web lint | `make web-lint` | phase 4 |
| Web unit tests | `make web-test` | phase 5 |
| Web coverage gate | `make web-coverage-gate` | phase 5 |
| Web build | `make web-build` | phase 4 |
| Bundle budget | `make web-budget` | phase 17 |
| UI acceptance (L3+browser) | `make test-ui-e2e` | phase 17 |

**Minimum gate for any sub-phase touching Go:** `make fmt-check lint test`.
**Minimum gate for any sub-phase touching `web/`:** `make web-lint web-typecheck
web-test`. Coverage gates run at every phase boundary, not only at the end.

---

## §4 — Ledger (one row per closed unit)

| # | Type | ID | Closed | Intent | Commit |
|---|------|-----|--------|--------|--------|
| 0.1 | sub-phase | 0.1 | 2026-08-31 | Capture durable E2E evidence and close V001-F1 | `8824301` |
| 0.2 | sub-phase | 0.2 | 2026-08-31 | Reconcile phase-10.5 scope and close V001-F2 | `4341cb4` |
| 0.3 | sub-phase | 0.3 | 2026-08-31 | Make plan 002 §11 traceability complete and close V001-F3 | `31b8436` |
| 0.4 | sub-phase | 0.4 | 2026-08-31 | Update plan 002 audit register and close V001-F1/F2/F3 | `7415f38` |
| 0.5 | sub-phase | 0.5 | 2026-08-31 | Close plan 002 Q-B with D11 | `75c79ff` |
| 0.6 | sub-phase | 0.6 | 2026-08-31 | Document durable E2E evidence command in README | `0bf364b` |
| 1.1 | sub-phase | 1.1 | 2026-08-31 | Author OpenAPI skeleton and shared components | `c1a5122` |
| 1.2 | sub-phase | 1.2 | 2026-08-31 | Document the read endpoints | `21a0286` |
| 1.3 | sub-phase | 1.3 | 2026-08-31 | Document the write, agent and infrastructure endpoints | `b197785` |
| 1.4 | sub-phase | 1.4 | 2026-08-31 | Enforce OpenAPI route coverage | `f8dbc67` |
| 1.5 | sub-phase | 1.5 | 2026-08-31 | Generate the checked-in static API reference | `3eb931c`, `0002314` |
| 1.6 | sub-phase | 1.6 | 2026-08-31 | Link the API contract and generated reference from README | `5f1561d` |
| 2.1 | sub-phase | 2.1 | 2026-08-31 | Add UI configuration loader and validation | `67965ac` |
| 2.2 | sub-phase | 2.2 | 2026-08-31 | Add in-memory session store with expiry and reap | `103757f` |

---

## §5 — Files touched

`Makefile`; `README.md`; `scripts/e2e_evidence.sh`; `scripts/id_audit.sh`; `internal/scripts/doc.go`; `internal/scripts/scripts_test.go`; `test/harness/harness.go`; `test/scenario/net.go`; `test/scenario/topo_cascading.go`; `docs/plans/002_plan-AnalysisBackend/STATE.md`; `docs/plans/002_plan-AnalysisBackend/phase_11.md`; `docs/plans/002_plan-AnalysisBackend/verify/index.md`; `docs/plans/002_plan-AnalysisBackend/verify/verify_001_2026-08-30.md`; `docs/plans/003_plan-Frontend/STATE.md`; `api/openapi.yaml`; `internal/server/openapi_test.go`; `internal/tools/apidocs/main.go`; `internal/tools/apidocs/main_test.go`; `docs/api.md`.

---

## §6 — In-flight work

`claimed — sub-phase 2.3; nothing written yet`

---

## §7 — Verification results

| Date | Unit | Gate | Result | Notes |
|------|------|------|--------|-------|
| 2026-08-31 | 0.1 | `make fmt-check` | PASS | Go formatting gate green. |
| 2026-08-31 | 0.1 | `make lint` | PASS | Go lint gate green. |
| 2026-08-31 | 0.1 | `make test` | PASS | Unit and integration-lite tests green. |
| 2026-08-31 | 0.1 | `make coverage-gate` | PASS | Coverage gate green. |
| 2026-08-31 | 0.1 | `go test -tags=e2e -timeout=20m -count=1 ./test/e2e -run '^TestFull_VacuumMaintenance$'` | PASS | Focused maintenance regression passed in 65s after the harness timeout correction. |
| 2026-08-31 | 0.1 | `go test -tags=e2e -timeout=20m -count=1 ./test/e2e -run '^TestFull_PgPausedTreatedAsUnreachable$'` | PASS | Focused paused/unpause regression passed in 200s. |
| 2026-08-31 | 0.1 | focused SYS-NET-001 / network pair / SYS-REPL-006 E2E regressions | PASS | Outage 135s, combined network pair 332s, cascading stale-edge regression 108s after timing-boundary corrections. |
| 2026-08-31 | 0.1 | `make test-e2e-full-evidence` | PASS | Artifact `test/e2e/_artifacts/e2e-full-20260831T040622Z.log`; `RESIDUAL_CONTAINERS=0`; `RESIDUAL_NETWORKS=0`; `EXIT_STATUS=0 FINISHED_AT=2026-08-31T05:12:01Z COMMAND=make test-e2e-full`; elapsed 3939s (65m39s). |
| 2026-08-31 | 0.2 | `make fmt-check lint test` | PASS | `0 issues`; race/shuffle unit suite green; exit 0 at 05:15:54Z after 18s. The phase-10.5 Files list now covers all seven paths in `06f50ea`, and D-059 is preserved exactly. |
| 2026-08-31 | 0.3 | `bash -n scripts/id_audit.sh`; `go test ./internal/scripts -count=1` | PASS | Script syntax and both shell-script tests are green. After adding all 51 source-only IDs to plan 002 §11, rerun reports 0 `ONLY_IN_SOURCE`, 68 retained state-only historical/planned IDs, and `DIFF_COUNT=68`. |
| 2026-08-31 | 0.4 | audit-register consistency review | PASS | Both `verify/index.md` and `verify_001_2026-08-30.md` mark V001-F1/F2/F3 `FIXED`; the index reports `0 OPEN MINOR (3 FIXED)`; original finding text and `Open blockers: None` are preserved. |
| 2026-08-31 | 0.5 | plan 002 §9 Q-B documentation review | PASS | Question text and assumed default are unchanged; status is `CLOSED 2026-08-31 — resolved in plan 003 as D11: existing contract retained`; `Resolve at` is `plan 003 — D11`. |
| 2026-08-31 | 0.6 | README command-list review; `make fmt-check lint test` | PASS | README contains the new `make test-e2e-full-evidence` command with the required durable-log/exit-status description; Go gates exit 0 at 05:28:18Z after 18s. |
| 2026-08-31 | 1.1 | `go test ./internal/server -run '^TestOpenAPIDocumentParses$' -count=1` | PASS | OpenAPI document parses and exposes the required 3.1.0 version and `Error` schema. |
| 2026-08-31 | 1.1 | `make fmt-check lint test coverage-gate` | PASS | Go gates exit 0 at 05:31:17Z after 44s; global coverage is 75.0%. |
| 2026-08-31 | 1.2 | `go test ./internal/server -run 'TestOpenAPI(ReadOperationsHaveExamples|DocumentParses)$' -count=1` | PASS | OpenAPI parses and every documented read operation exposes a JSON schema and example. |
| 2026-08-31 | 1.2 | `make fmt-check lint test coverage-gate` | PASS | Go gates exit 0 at 05:43:48Z after 45s; race/shuffle passed and global coverage is 75.0%. |
| 2026-08-31 | 1.3 | `go test ./internal/server -run 'TestOpenAPI(ReadOperationsHaveExamples|DocumentParses|OperationsHaveTagsAndUniqueIDs|AgentRoutesAreBearerOnly)$' -count=1` | PASS | OpenAPI parses; operation IDs are unique, every operation is tagged, agent routes require only `agentBearer`, and infrastructure routes are unauthenticated. |
| 2026-08-31 | 1.3 | `make fmt-check lint test coverage-gate` | PASS | Go gates exit 0 at 05:52:21Z after 45s; race/shuffle passed and global coverage is 75.0%. |
| 2026-08-31 | 1.4 | focused OpenAPI route/ID/normalization tests | PASS | Route set equality passed after canonicalizing the findings catch-all to its public parameterized paths; a temporary `GET /api/v1/__probe` made the gate fail with the expected missing-route diagnostic, then was reverted. |
| 2026-08-31 | 1.4 | `make fmt-check lint test coverage-gate` | PASS | Go gates exit 0 at 05:56:21Z after 43s; race/shuffle passed and global coverage is 75.1%. |
| 2026-08-31 | 1.5 | `go test ./internal/tools/apidocs -run '^TestAPIDocsGeneratorIsDeterministic$' -count=1` | PASS | Generator output is deterministic and matches the checked-in `docs/api.md`. |
| 2026-08-31 | 1.5 | `make fmt-check lint test coverage-gate` | FAIL then PASS | First run completed in 42s at 06:00:36Z with global coverage 75.0% after adding the generator; validation tests in `0002314` raised generator coverage to 80.6%, and the rerun completed in 46s at 06:02:25Z with global coverage 75.1%. |
| 2026-08-31 | 1.6 | `make api-docs`; `git diff --exit-code -- docs/api.md api/openapi.yaml`; `git diff --check` | PASS | The generated reference is reproducible and clean; README links the machine-readable and offline references and documents `make api-docs`. |
| 2026-08-31 | 1.6 | `make fmt-check lint test coverage-gate` | PASS | Go gates exit 0 at 06:04:43Z after 44s; race/shuffle passed and global coverage is 75.1%. |
| 2026-08-31 | 2.1 | `go test ./internal/server -run '^TestLoadUIConfig' -count=1` | PASS | UI configuration defaults, password-file precedence/trimming, TTL bounds, secure-cookie parsing, and secret-safe errors pass. |
| 2026-08-31 | 2.1 | `make fmt-check lint test coverage-gate` | PASS | Go gates exit 0 at 06:07:58Z after 46s; race/shuffle passed and global coverage is 75.2%. |
| 2026-08-31 | 2.2 | `go test -race ./internal/server -run '^TestSessionStore' -count=1` | PASS | Creation, URL-safe token shape, validation, expiry, deletion, bounded reap, and randomness error propagation pass under the race detector. |
| 2026-08-31 | 2.2 | `make fmt-check lint test coverage-gate` | PASS | Go gates exit 0 at 06:11:06Z after 47s; race/shuffle passed and global coverage is 75.3%. |

Sub-phase 17.4 must record the server image size before and after the frontend
is embedded. Sub-phase 17.5 must record the measured initial and lazy chunk
sizes. Sub-phase 17.6 must record the captured `EXIT_STATUS` line from
`make test-e2e-full-evidence`. These are the three numbers a later audit cannot
reconstruct.

---

## §8 — Deviations from the plan

| # | Sub-phase | Deviation | Why | Plan updated |
|---|-----------|-----------|-----|--------------|
| 1 | 0.1 – 0.5 | This plan writes into **plan 002's** folder (`docs/plans/002_plan-AnalysisBackend/verify/index.md`, its report file, and its `STATE.md` §11) | Decision **D12**: the three open findings V001-F1/F2/F3 from plan 002's audit are fixed here rather than left open, and a finding's status must be updated in the register that owns it. Recorded in advance so a later audit reads a cross-plan write as intentional. | yes — D12 in [overview.md](overview.md) |
| 2 | 0.1 | The E2E evidence implementation also adjusts the harness scenario timeout from 10m to 20m, captures network transition timestamps before Docker state changes, widens the fresh-sample window to 180s, and widens the cascading stale-edge window to 120s. | The full-suite evidence runs exposed legitimate propagation/load delays and timing-boundary races; focused regressions passed after each correction, and the successful full run completed with no residual resources. | yes — §6, §7 and this row |
| 3 | 0.3 | The identifier audit's symmetric `DIFF_COUNT` remains 68 after the preferred source-side cataloguing resolution. | The 68 `ONLY_IN_STATE` entries are historical/planned identifiers retained by the closed plan; the claim being repaired is that every identifier present in Go source is enumerated in §11, which now has 0 `ONLY_IN_SOURCE`. | yes — plan 002 §11 scope sentence and §7 audit row |

---

## §9 — Blockers and open questions

| ID | Question | Status | Owner | Resolve by |
|----|----------|--------|-------|------------|
| Q-C | Sessions live in server memory and are lost on restart, so every operator is signed out by a deployment. Acceptable for v0.1, or does the session store need to be persisted in TimescaleDB? | **OPEN — non-blocking.** Proceed with the in-memory store; document the behaviour in `docs/LIMITS.md` (sub-phase 17.4) and in the README's known limits (17.8). Revisit only if a user reports it. | user | before v1.0 |
| Q-D | One shared password with no user accounts, roles or per-user audit. The command audit therefore records *what* was done, not *who* did it. Acceptable for v0.1? | **OPEN — non-blocking.** Proceed. The Settings page states the absence explicitly (sub-phase 16.2) and `docs/LIMITS.md` records it (17.4). Real accounts are a v1.0 decision, not a v0.1 one. | user | before v1.0 |
| Q-B | (Inherited from plan 002 §9, `DEFERRED TO PLAN 003`.) Does the activity contract need a per-user connection breakdown for the UI? | **CLOSED** by decision **D11**: no — the existing contract (per state, per database, opt-in per application) is what the Locks and Activity page renders. Sub-phase 0.5 writes the closure into plan 002's `STATE.md`; sub-phase 13.3 carries an explicit absence test (`UI-LOCK-022`) so a future contributor adding a per-user control breaks a test and reads D11. | `agent-3:haiku` | closed in 0.5 |

Neither Q-C nor Q-D blocks any sub-phase. Do not stop to ask.

---

## §10 — Dead ends (do not retry)

| # | What was tried | Why it failed | Instead |
|---|----------------|---------------|---------|
| 1 | TypeScript **7.0.2**, the latest release | `typescript-eslint@8.68.0` declares a peer range of `>=4.8.4 <6.1.0`. TS 7 would leave the project with **no typed linting**, silently disabling the rules that enforce `no-floating-promises` and exhaustive switches. | TypeScript **6.0.3**, pinned exactly, recorded as decision **D13** with the peer range and the revisit condition. |
| 2 | Rendering ECharts in jsdom to assert chart contents | jsdom has no canvas and no layout; the library renders nothing assertable, so the tests would prove only that a component mounted. | Decision **D14**: chart **option objects** are built by pure functions and asserted exactly; wrappers are smoke-tested with the library mocked; pixels are proven once, in a real browser, by `SYS-UI-007`. |
| 3 | Letting Playwright's `webServer` block start the stack | The Go E2E harness already owns compose lifecycle, port allocation and teardown. Two orchestrators fighting over the same containers produces flakes that look like product bugs. | Decision **D17**: no `webServer` block. `test/e2e/ui_test.go` starts the stack via `harness.Start` and shells out to Playwright with the resolved base URL. |
| 4 | Adding CORS middleware so a separately-served frontend could call the API | Introduces cross-origin cookie handling, `SameSite` decisions, preflight caching and a second deployable — all to solve a problem created by the split itself. | Decision **D2**: `go:embed` the built SPA and serve it from the same origin through chi's `NotFound` handler. Same-origin means no CORS at all. |
| 5 | Keeping the original 10m harness timeout and narrow 90s/60s E2E evidence windows | Full-suite runs timed out during slow maintenance/propagation and cut across the actual network transition/staleness boundaries. | Use the 20m scenario timeout, pre-transition timestamps, a 180s fresh-sample window, and a 120s stale-edge window; verify each with focused tests before rerunning the full suite. |

---

## §11 — Progress board

### Phases

| Phase | File | Title | Assigned | Review gate | Status |
|-------|------|-------|----------|-------------|--------|
| 0 | [phase_01.md](phase_01.md) | Plan 002 closure and contract prerequisites | `agent-2:sonnet` / `agent-3:haiku` | 0.4 | DONE — 6/6 sub-phases closed |
| 1 | [phase_02.md](phase_02.md) | OpenAPI contract and route-coverage gate | `agent-2:sonnet` | 1.1, 1.4 | DONE — 6/6 sub-phases closed |
| 2 | [phase_03.md](phase_03.md) | UI session authentication | `agent-2:sonnet` | 2.1, 2.2, 2.3 | IN_PROGRESS — 2.3 next |
| 3 | [phase_04.md](phase_04.md) | Embedded SPA serving and dev proxy | `agent-2:sonnet` | 3.2 | TODO |
| 4 | [phase_05.md](phase_05.md) | Frontend workspace scaffold | `agent-2:sonnet` | 4.1 | TODO |
| 5 | [phase_06.md](phase_06.md) | Frontend test harness and quality gates | `agent-2:sonnet` | 5.2, 5.4, 5.6, 5.9 | TODO |
| 6 | [phase_07.md](phase_07.md) | Typed API client, query layer, state primitives | `agent-2:sonnet` | 6.4, 6.6 | TODO |
| 7 | [phase_08.md](phase_08.md) | App shell, navigation, time range | `agent-2:sonnet` | 7.3 | TODO |
| 8 | [phase_09.md](phase_09.md) | Fleet Overview | `agent-2:sonnet` | 8.4 | TODO |
| 9 | [phase_10.md](phase_10.md) | Cluster Detail | `agent-2:sonnet` | 9.2 | TODO |
| 10 | [phase_11.md](phase_11.md) | Instance Detail | `agent-2:sonnet` | — | TODO |
| 11 | [phase_12.md](phase_12.md) | ASH and wait analysis | `agent-2:sonnet` | 11.2 | TODO |
| 12 | [phase_13.md](phase_13.md) | Query Inspector and plan history | `agent-2:sonnet` | 12.3 | TODO |
| 13 | [phase_14.md](phase_14.md) | Locks and Activity | `agent-2:sonnet` | 13.4 | TODO |
| 14 | [phase_15.md](phase_15.md) | Advisor findings | `agent-2:sonnet` | — | TODO |
| 15 | [phase_16.md](phase_16.md) | Alerts, silences, rules, events | `agent-2:sonnet` | 15.3 | TODO |
| 16 | [phase_17.md](phase_17.md) | Settings and fleet inventory | `agent-2:sonnet` | — | TODO |
| 17 | [phase_18.md](phase_18.md) | Packaging, UI acceptance suite, documentation | `agent-2:sonnet` / `agent-3:haiku` | 17.2, 17.7, 17.8 | TODO |

`agent-1:opus` owns every review gate listed above and approves each phase before
the next one opens.

### Sub-phases

| ID | Title | Assigned | Status |
|----|-------|----------|--------|
| 0.1 | Capture durable evidence for `make test-e2e-full` (V001-F1) | `agent-2:sonnet` | DONE |
| 0.2 | Reconcile the phase-10.5 declared scope (V001-F2) | `agent-2:sonnet` | DONE |
| 0.3 | Make the §11 traceability claim precise and complete (V001-F3) | `agent-2:sonnet` | DONE |
| 0.4 | Update plan 002's audit register | `agent-3:haiku` | DONE |
| 0.5 | Close plan 002's deferred question Q-B | `agent-3:haiku` | DONE |
| 0.6 | Update README.md | `agent-3:haiku` | DONE |
| 1.1 | Author `api/openapi.yaml` — skeleton and shared components | `agent-2:sonnet` | DONE |
| 1.2 | Document the read endpoints | `agent-2:sonnet` | DONE |
| 1.3 | Document the write, agent and infrastructure endpoints | `agent-2:sonnet` | DONE |
| 1.4 | Route-coverage gate: `TestOpenAPICoversEveryRoute` | `agent-2:sonnet` | DONE |
| 1.5 | Generate a static API reference page | `agent-3:haiku` | DONE |
| 1.6 | Update README.md | `agent-3:haiku` | DONE |
| 2.1 | UI configuration loader | `agent-2:sonnet` | DONE |
| 2.2 | Session store | `agent-2:sonnet` | DONE |
| 2.3 | Session middleware and the `either credential` rule | `agent-2:sonnet` | OPEN |
| 2.4 | The three session endpoints | `agent-2:sonnet` | TODO |
| 2.5 | Update the E2E harness client to authenticate | `agent-2:sonnet` | TODO |
| 2.6 | Full regression sweep | `agent-2:sonnet` | TODO |
| 2.7 | Update README.md and LIMITS.md | `agent-3:haiku` | TODO |
| 3.1 | The embed package and its placeholder | `agent-2:sonnet` | TODO |
| 3.2 | The static handler and the SPA fallback | `agent-2:sonnet` | TODO |
| 3.3 | Serve the SPA only when the UI is enabled | `agent-2:sonnet` | TODO |
| 3.4 | Container and compose wiring | `agent-2:sonnet` | TODO |
| 3.5 | Prove both build modes | `agent-2:sonnet` | TODO |
| 3.6 | Update README.md | `agent-3:haiku` | TODO |
| 4.1 | Package manifest with exact pins | `agent-2:sonnet` | TODO |
| 4.2 | TypeScript configuration | `agent-2:sonnet` | TODO |
| 4.3 | Vite configuration and the dev proxy | `agent-2:sonnet` | TODO |
| 4.4 | Tailwind CSS 4 and the design tokens | `agent-2:sonnet` | TODO |
| 4.5 | shadcn/ui installation and the first primitives | `agent-2:sonnet` | TODO |
| 4.6 | ESLint, Prettier, and the project's own rules | `agent-2:sonnet` | TODO |
| 4.7 | Makefile and CI integration | `agent-2:sonnet` | TODO |
| 4.8 | Update README.md and add `web/README.md` | `agent-3:haiku` | TODO |
| 5.1 | Test dependencies and the Vitest configuration | `agent-2:sonnet` | TODO |
| 5.2 | The test architecture rules (T-1…T-10) | `agent-2:sonnet` | TODO |
| 5.3 | Render helpers and the provider wrapper | `agent-2:sonnet` | TODO |
| 5.4 | Fixtures and contract validation | `agent-2:sonnet` | TODO |
| 5.5 | The MSW server and handler factory | `agent-2:sonnet` | TODO |
| 5.6 | Coverage configuration and the UI coverage gate | `agent-2:sonnet` | TODO |
| 5.7 | The accessibility assertion | `agent-2:sonnet` | TODO |
| 5.8 | Determinism: clock, timezone, locale, randomness | `agent-2:sonnet` | TODO |
| 5.9 | Playwright bootstrap and the flake policy | `agent-2:sonnet` | TODO |
| 5.10 | Meta-tests: prove the harness catches what it claims | `agent-2:sonnet` | TODO |
| 5.11 | Update TESTING.md and README.md | `agent-3:haiku` | TODO |
| 6.1 | Type generation from the contract | `agent-2:sonnet` | TODO |
| 6.2 | The typed client | `agent-2:sonnet` | TODO |
| 6.3 | Query layer: keys, policies, and the poll clock | `agent-2:sonnet` | TODO |
| 6.4 | Authentication state and the single-flight 401 | `agent-2:sonnet` | TODO |
| 6.5 | Formatting library | `agent-2:sonnet` | TODO |
| 6.6 | The state primitives | `agent-2:sonnet` | TODO |
| 6.7 | Freshness plumbing | `agent-2:sonnet` | TODO |
| 6.8 | Update README.md | `agent-3:haiku` | TODO |
| 7.1 | Route tree and code splitting | `agent-2:sonnet` | TODO |
| 7.2 | The shell: header, sidebar, content region | `agent-2:sonnet` | TODO |
| 7.3 | Time range as URL state | `agent-2:sonnet` | TODO |
| 7.4 | Page scaffolding primitives | `agent-2:sonnet` | TODO |
| 7.5 | Theme, density and preferences | `agent-2:sonnet` | TODO |
| 7.6 | Global error and offline handling | `agent-2:sonnet` | TODO |
| 7.7 | Update README.md | `agent-3:haiku` | TODO |
| 8.1 | Fleet derivation library | `agent-2:sonnet` | TODO |
| 8.2 | Cluster cards and the fleet grid | `agent-2:sonnet` | TODO |
| 8.3 | Agent health on the fleet page | `agent-2:sonnet` | TODO |
| 8.4 | Health semantics, exactly as the server defines them | `agent-2:sonnet` | TODO |
| 8.5 | Degraded and error paths (rule T-4) | `agent-2:sonnet` | TODO |
| 8.6 | Update README.md | `agent-3:haiku` | TODO |
| 9.1 | Replication derivation library | `agent-2:sonnet` | TODO |
| 9.2 | The topology graph | `agent-2:sonnet` | TODO |
| 9.3 | Replication lag charts | `agent-2:sonnet` | TODO |
| 9.4 | Slots, drift and cluster settings | `agent-2:sonnet` | TODO |
| 9.5 | The event timeline | `agent-2:sonnet` | TODO |
| 9.6 | Degraded and error paths (rule T-4) | `agent-2:sonnet` | TODO |
| 9.7 | Update README.md | `agent-3:haiku` | TODO |
| 10.1 | Instance header and role banner | `agent-2:sonnet` | TODO |
| 10.2 | Database selector and the unmonitored count | `agent-2:sonnet` | TODO |
| 10.3 | Metric tiles and time series | `agent-2:sonnet` | TODO |
| 10.4 | Host metrics with honest unavailability | `agent-2:sonnet` | TODO |
| 10.5 | Settings, change history and durability | `agent-2:sonnet` | TODO |
| 10.6 | Relations, bloat and truncation | `agent-2:sonnet` | TODO |
| 10.7 | Update README.md | `agent-3:haiku` | TODO |
| 11.1 | ASH derivation library | `agent-2:sonnet` | TODO |
| 11.2 | The stacked wait chart | `agent-2:sonnet` | TODO |
| 11.3 | Drill-down: type to event to query | `agent-2:sonnet` | TODO |
| 11.4 | Honesty: disabled, under-sampled, unattributable | `agent-2:sonnet` | TODO |
| 11.5 | Degraded and error paths (rule T-4) | `agent-2:sonnet` | TODO |
| 11.6 | Update README.md | `agent-3:haiku` | TODO |
| 12.1 | Statement derivation library | `agent-2:sonnet` | TODO |
| 12.2 | The statement list | `agent-2:sonnet` | TODO |
| 12.3 | The command lifecycle client | `agent-2:sonnet` | TODO |
| 12.4 | The EXPLAIN flow and its gates | `agent-2:sonnet` | TODO |
| 12.5 | Plan history | `agent-2:sonnet` | TODO |
| 12.6 | Degraded and error paths (rule T-4) | `agent-2:sonnet` | TODO |
| 12.7 | Update README.md | `agent-3:haiku` | TODO |
| 13.1 | Lock-tree derivation library | `agent-2:sonnet` | TODO |
| 13.2 | The blocking tree view | `agent-2:sonnet` | TODO |
| 13.3 | Activity view | `agent-2:sonnet` | TODO |
| 13.4 | Cancel and terminate | `agent-2:sonnet` | TODO |
| 13.5 | Degraded and error paths (rule T-4) | `agent-2:sonnet` | TODO |
| 13.6 | Update README.md | `agent-3:haiku` | TODO |
| 14.1 | Findings derivation library | `agent-2:sonnet` | TODO |
| 14.2 | The findings list | `agent-2:sonnet` | TODO |
| 14.3 | Muting | `agent-2:sonnet` | TODO |
| 14.4 | The rule catalogue view | `agent-2:sonnet` | TODO |
| 14.5 | Degraded and error paths (rule T-4) | `agent-2:sonnet` | TODO |
| 14.6 | Update README.md | `agent-3:haiku` | TODO |
| 15.1 | Alerts derivation library | `agent-2:sonnet` | TODO |
| 15.2 | The alert list | `agent-2:sonnet` | TODO |
| 15.3 | Alert rules | `agent-2:sonnet` | TODO |
| 15.4 | Silences | `agent-2:sonnet` | TODO |
| 15.5 | Notification channels, stated honestly | `agent-2:sonnet` | TODO |
| 15.6 | Fleet-wide event timeline and T-4 paths | `agent-2:sonnet` | TODO |
| 15.7 | Update README.md | `agent-3:haiku` | TODO |
| 16.1 | Inventory tables | `agent-2:sonnet` | TODO |
| 16.2 | Server information and product limits | `agent-2:sonnet` | TODO |
| 16.3 | Command audit | `agent-2:sonnet` | TODO |
| 16.4 | Degraded and error paths (rule T-4) | `agent-2:sonnet` | TODO |
| 16.5 | Update README.md | `agent-3:haiku` | TODO |
| 17.1 | The Go-driven UI acceptance runner | `agent-2:sonnet` | TODO |
| 17.2 | The acceptance scenarios | `agent-2:sonnet` | TODO |
| 17.3 | CI integration | `agent-2:sonnet` | TODO |
| 17.4 | Release packaging | `agent-2:sonnet` | TODO |
| 17.5 | Performance and bundle budget | `agent-2:sonnet` | TODO |
| 17.6 | Full regression sweep | `agent-2:sonnet` | TODO |
| 17.7 | Rewrite the README's API examples for authentication | `agent-3:haiku` | TODO |
| 17.8 | Final documentation | `agent-3:haiku` | TODO |

### Tests

Go tests carry their Go test name. Frontend unit and route tests are identified
by the `UI-<AREA>-<NNN>` ids named in each sub-phase; the row below tracks the
**suite**, and the sub-phase file is the authoritative list of its ids. Harness
meta-tests (`T-META-*`) and browser acceptance scenarios (`SYS-UI-*`) each get
their own row, because each one is a single named artefact whose absence would
not otherwise be visible.

| ID / suite | Kind | Owning sub-phase | Status |
|------------|------|------------------|--------|
| `TestOpenAPICoversEveryRoute` | Go, contract gate | 1.4 | DONE |
| `TestAPIDocsGeneratorIsDeterministic` | Go, unit | 1.5 | DONE |
| `TestLoadUIConfig` | Go, unit | 2.1 | DONE |
| `TestSessionStore` | Go, unit | 2.2 | DONE |
| `TestRequireCredential` | Go, unit | 2.3 | TODO |
| `TestSessionEndpoints` | Go, integration | 2.4 | TODO |
| `TestSPAHandler` | Go, unit | 3.2 | TODO |
| `TestSPADisabled` | Go, unit | 3.3 | TODO |
| `T-META-1` … `T-META-6` | Vitest, harness meta-tests | 5.10 | TODO |
| `UI-API-001` … `UI-API-010` | Vitest, unit | 6.2 – 6.4 | TODO |
| `UI-FMT-*` | Vitest, unit | 6.5 | TODO |
| `UI-STATE-*` (100% coverage) | Vitest, unit | 6.6 | TODO |
| `UI-SHELL-*` | Vitest, component | 7.1 – 7.6 | TODO |
| `UI-RANGE-*` | Vitest, unit | 7.3 | TODO |
| `UI-FLEET-*` | Vitest, unit + route | 8.1 – 8.5 | TODO |
| `UI-CLUS-*` | Vitest, unit + route | 9.1 – 9.6 | TODO |
| `UI-INST-*` | Vitest, unit + route | 10.1 – 10.6 | TODO |
| `UI-ASH-*` | Vitest, unit + route | 11.1 – 11.5 | TODO |
| `UI-QRY-*` | Vitest, unit + route | 12.1 – 12.6 | TODO |
| `UI-LOCK-*` | Vitest, unit + route | 13.1 – 13.5 | TODO |
| `UI-FIND-*` | Vitest, unit + route | 14.1 – 14.5 | TODO |
| `UI-ALRT-*` | Vitest, unit + route | 15.1 – 15.6 | TODO |
| `UI-SET-*` | Vitest, unit + route | 16.1 – 16.4 | TODO |
| `SYS-UI-000` — the stack serves the interface | Playwright via Go harness | 17.1 | TODO |
| `SYS-UI-001` — failover visible, `cluster_id` byte-identical | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-002` — the interface requires a session | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-003` — a down agent is visible on the landing page | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-004` — an unknown value is never rendered as zero | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-005` — ASH disabled reads as disabled | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-006` — the EXPLAIN flow works end to end | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-007` — charts actually paint | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-008` — contention appears in the blocking tree | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-009` — cancel is gated and works | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-010` — a finding can be muted and unmuted | Playwright via Go harness | 17.2 | TODO |
| `SYS-UI-011` — zero serious or critical axe violations | Playwright via Go harness | 17.2 | TODO |

### Documentation deliverables

Every phase closes with a documentation sub-phase. None may be skipped: an
undocumented feature is an unshipped one.

| Doc unit | Sub-phase | Deliverable | Status |
|----------|-----------|-------------|--------|
| README — plan 002 closure | 0.6 | Evidence artefact and the corrected traceability claim | TODO |
| README — API contract | 1.6 | `api/openapi.yaml` and the generated `docs/api.md` | DONE |
| README + LIMITS — authentication | 2.7 | `PGLENS_UI_PASSWORD`, session TTL, the credential rule | TODO |
| README — embedded interface | 3.6 | How the interface is served and how to disable it | TODO |
| README + `web/README.md` | 4.8 | Building and developing the frontend | TODO |
| TESTING.md + README | 5.11 | The frontend test architecture and its gates | TODO |
| README — client behaviour | 6.8 | Poll intervals and what a stale reading means | TODO |
| README — navigation | 7.7 | Pages, time range, keyboard shortcuts | TODO |
| README — Fleet Overview | 8.6 | The landing page and health semantics | TODO |
| README — Cluster Detail | 9.7 | Topology, lag, slots, events | TODO |
| README — Instance Detail | 10.7 | Metrics, host availability, settings, relations | TODO |
| README — wait analysis | 11.6 | ASH sampling, disabled state, under-sampling | TODO |
| README — Query Inspector | 12.7 | Statements, EXPLAIN gates, plan history | TODO |
| README — Locks and Activity | 13.6 | Sampling limit, cancel and terminate gates | TODO |
| README — Advisor findings | 14.6 | Four states, `degraded`, mute semantics, catalogue | TODO |
| README — Alerts | 15.7 | Alerts, rules, silences, channels | TODO |
| README — Settings | 16.5 | Inventory, tiers, audit, what does not exist | TODO |
| README — API examples | 17.7 | Every `curl` example carries a credential and was run | TODO |
| Final documentation | 17.8 | README, LIMITS, TESTING, CONTRIBUTING coherent as one document | TODO |

### Audits

| ID | Date | Scope | Result | Report |
|----|------|-------|--------|--------|
| _(none yet)_ | | | | |

Plan 002's audit V001 left three MINOR findings open; they are closed by
sub-phases 0.1 – 0.4 of **this** plan and re-statused in plan 002's own register
(deviation §8 #1).
