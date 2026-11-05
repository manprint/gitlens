# ConsoleInsights — Implementation state
Format: pev-state 2

Read first at every session. Updated 2026-11-05 by agent-2:claude-sonnet-5-5 (execution session).
Plan ID: 005_plan-ConsoleInsights. Revision: 1. Repo root: /mnt/fabio/dati/Git/SperimentazioniAI/postgres-analyze.
Plan baseline: `5929be2d5ffe96867a63791b8a0b18687e41f8b5` (branch `feat/index-analysis`); pre-existing uncommitted changes: see §5 (preserved; committed once as the baseline commit at 0.2 S1b, D32).
Roster (user instruction 2026-11-04, `agent:sonnet` + "autorizzo sonnet per tutto il piano, con opus come advisor"; supersedes the 2026-10-29 "Sonnet is not used" instruction): agent-1:claude-opus-5-5 (supervisor/reviewer: the `advisor` tool, re-enabled by the user 2026-11-04, set to Opus 5.5; for a full-diff review the advisor tool is used, and a fresh read-only review agent with `model: opus` is an allowed supplement); agent-2:claude-sonnet-5-5 (implementer, the executing session); agent-3:claude-haiku-5-5 (exploration and mechanical work only, Agent tool `model: haiku`). Phase-file lines naming `agent-2:claude-opus-5-5` now read agent-2:claude-sonnet-5-5. Active execution style: handoff (the executing session implements; every point where a phase file says `advisor` is served by the advisor tool).
State writer: the executing session (agent-2:claude-sonnet-5-5, took over 2026-11-04 after the 2026-10-31 user-stop handoff released the previous session); ownership: ACTIVE since 2026-11-05 08:20Z, agent-2:claude-sonnet-5-5 (took over from the released 2026-11-04 23:17 handoff; previous writer stopped).


## 0. Resume and completion protocol

See `PROTOCOL.md` (static cold-start rules).

## 1. Current unit and scope

- Invocation: `/pev-v3 full-autonomous:true --wip-commit execute 005` (2026-11-05, explicit); style handoff; acting as agent-2:claude-sonnet-5-5; supervisor advisor Opus 5.5.
- Active scope: plan (whole `005_plan-ConsoleInsights`, no selector); full-autonomous true, WIP on (explicit)
- Scope result: RUNNING
- Type: sub-phase
- ID / attempt: 4.2 / 1
- Status: TODO (4.1 DONE; 4.2 not opened)
- Intent: execute the plan in the order of §11 "Execution order" with exhaustive tests and no regressions.
- Assigned: agent-2:claude-sonnet-5-5; supervisor: advisor (Opus 5.5)
- File / unit heading: phase_05.md → 4.2
- Current step: —
- Next action: read phase_05.md 4.2 and open it (execution order §11).
- Stop latch: none (cleared by explicit invocation 2026-11-05)
- Last closure: unit:4.1:1 (sub-phase 4.1 DONE)
- Unit base: HEAD (after the P16 commit); branch `feat/index-analysis` (never switch or create branches)
- Repo state: uncommitted only `internal/webui/dist/index.html` (never staged) and `.claude/`.
- Network: user authorised internet use (2026-11-05 message "autorizzo ad usare la rete internet"); still no push.
- Stack: restarted 2026-11-05 08:15Z (`up -d --no-recreate`, all healthy); backup `02-loaded-20261104T170945Z` VERIFIED (created 2026-11-04T17:09:45Z, < 24 h until 2026-11-05 17:09Z).

## 2. Feature context and readiness

Readiness: READY — finalised on the user's instruction ("applica i fix e non fare più verifiche") after review rounds R-PLAN-1..11: every finding of R-PLAN-1..11 is applied, but the R-PLAN-11 fixes were not re-reviewed (no R-PLAN-12). Residual risk (an unlisted existing-test break, an unverified runtime assumption) is handled in development by the focused gate of every unit, the phase-closure gates and the `advisor` reviews; an existing test is never edited without recording it in §8. Implementation starts only after the user's explicit OK.
Goal, reference scenario items 1–17, exclusions, requirement register REQ-01…REQ-44, decisions D1–D33, assumptions A1–A3, open question Q2, invariants I-1…I-17, interfaces, reuse map and risks: [overview.md](overview.md). Test strategy: [test-strategy.md](test-strategy.md).
Current decisions needed by remaining work: all rows of overview "Decisions" (D9 revised: HA lab with local images and the `hactl` controller; D11 commits local with trailers; D32 baseline commit).
Unresolved design/acceptance questions: none. Q2 resolved 2026-10-30 (yes, write-only storage; §8). A1, A2, A3 are non-blocking assumptions to confirm in the final report. Q1 resolved (local images only).
Research evidence: overview R1, R2 CONFIRMED; R4 CONFIRMED at 0.3 S6 (both posts delivered, §7); R3 UNVERIFIED until 7.0 S1 (blocks 7.1+ only).
Required unverified external claims: R3 (7.1+). No other phase depends on one.
Required supervisor reviews cannot be silently replaced by weaker self-review.

### Authorization scope (standing user instructions, `/home/agent/.claude/CLAUDE.md` and the user's messages)
- Network: only the local network, **plus** (explicitly authorised by the user) image pulls/builds for the existing Odoo stack and an HTTPS POST to the user-provided Google Chat dev webhook host `chat.googleapis.com` (a sandbox policy allow may be needed: ask the user to run `sbx policy allow network chat.googleapis.com`). Not authorised: Patroni/etcd/HAProxy pulls, any other host. Go module downloads happen only if a test needs a module not in the module cache — record each in §8.
- Git: **never push**; never create or switch branches; local commits per §3 only (trailers below); stage only owned paths; never stage `internal/webui/dist/index.html` (commit with explicit pathspecs, never `git add -A`; `git diff --cached --name-only` must not list it) and never discard it (it was already modified at plan start, I-4); never commit `.claude/`.
- Dev Odoo stack (user, 2026-10-30): the Odoo app and its monitored PostgreSQL (`pglens-odoo-odoo-db-1`, db `odoo`) are a dev environment; anything may be written, loaded or provoked there (deadlocks included) to find pglens bugs. Throwaway objects live in schema `qa_tmp`.
- Destructive operations: only after a verified backup (I-4); never `docker compose down -v`; lifecycle tests are guarded (test-strategy.md).
- Secrets: the Google Chat webhook URL lives **only** in `/home/agent/pglens-odoo-stack/.env` (`PGLENS_GOOGLE_CHAT_WEBHOOK_URL`, chmod 600). It never appears in plan files, repo files, logs, fixtures, memory, commit messages or reports; never echo it; gate G-SECRETS enforces it; the user will delete the webhook later.
- Tools: prefer tokensave/serena (serena failed to connect) for code search; no Explore agents for code research (tokensave rule); use `TOKENSAVE_DISABLE_GREP_HOOK=1` only for non-symbol greps; caveman mode only in chat replies, never in code, docs or commits.
- AGENTS.md wants `tgsend` Telegram notifications: `tgsend` is not installed here — disclose in the final report.

## 3. Environment, settings, and gate registry

- Full autonomous: **true** (source: user message 2026-10-29, "settalo con wip commit = on ed autonomous=true").
- WIP commits: **on** (same source).
- Completion policy: full autonomy commits operational units and phase closures locally; WIP commits (`PEV-Result: wip`, OPEN state) are made on interruption of an owned unit; plan authoring commits nothing by itself (the plan folder is committed at 0.2 S1b). Never push.
- Commit message format: Conventional Commits subject; body one short paragraph; trailers `PEV-Plan: 005_plan-ConsoleInsights`, `PEV-Unit: <id>`, `PEV-Attempt: <n>`, `PEV-Result: complete|wip`, then the `Co-Authored-By:` line given by the harness attribution reminder of the executing session (from 2026-11-04 it names Sonnet 5.5, e.g. `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`; if it names a model that is not on the current roster: stop and tell the user). Ledger reference `unit:<id>:<attempt>`.
- Scope/roster survive handoff. Explicit invocation overrides stored settings.
- Setup: Go toolchain from the repo (`go 1.27.x`, module `github.com/manprint/pglens`), Node 24 + pnpm via corepack (`web/`), Docker daemon (testcontainers through `test/pgtest` with `PGLENS_PG_VERSIONS` unset = majors 15 and 18, the local images `postgres:15.19-alpine`/`postgres:18.6-alpine`; majors 16/17/19 would pull and are forbidden; also local: `postgres:18`, `postgres:18.6-alpine`, `postgres:15.19-alpine`, `odoo:18`, `timescale/timescaledb:2.31.0-pg17`, `testcontainers/ryuk`, deployed `ghcr.io/manprint/pglens-{server,agent}:dev`); Playwright 1.64.1 browsers installed; stack dir `/home/agent/pglens-odoo-stack/` (compose project `pglens-odoo`, containers `pglens-odoo-{odoo-db,odoo,timescaledb,pglens-server,pglens-agent}-1`, UI http://127.0.0.1:9090 password in `.env`, Odoo http://127.0.0.1:9091); scratch files in the scratchpad directory only; shell cwd resets between calls (use absolute paths).
- Roster and delegation (user instruction 2026-11-04, replaces 2026-10-29): implementation (production code, tests, scripts, plan/STATE edits, commits) is Sonnet 5.5 (the executing session); every supervisor review is Opus 5.5 through the `advisor` tool (called before committing to an approach on a unit that needs design judgement, and before closing every phase `P<N>`/risky unit; not for mechanical steps). The executing session must run on Sonnet 5.5 or Opus 5.5 (confirm by the harness attribution line). Every Agent call sets `model` explicitly (`opus` for a read-only review agent, `haiku` for mechanical work; implementer agents use `sonnet`); an omitted `model` inherits the parent.
- Haiku 5.5 (agent-3) is used only for (a) exploration/recon: locate files and symbols, list call sites and existing tests, read-only, answers as `path:line` (the user asked for explorations, so the tokensave "no Explore agents" rule is lifted for these; the prompt includes the tokensave instructions); (b) mechanical steps whose exact command or list the implementer gives: run a gate command and report the verbatim exit status and counts, regenerate generated files (`web/src/api/generated.ts`, the web build) only where a unit says so, formatter/lint `--fix`, bulk renames from an exact list, listing evidence files. Haiku never stages or commits, writes STATE.md, touches `.env`/secrets, runs destructive or backup/restore steps, designs or writes production logic or test oracles, edits an existing test, or diagnoses a failure. Haiku output is a claim: the implementer re-reads the changed files or the gate output before relying on it, and a gate counts only after that check. The implementer stays the sole state writer.
- Supervisor access: the `advisor` tool is ENABLED again (user `/advisor` Opus 5.5, 2026-11-04). Wherever a phase file names `advisor`, call it (it sees the whole transcript); before calling, make the deliverable durable (files written, state updated, WIP commit). A fresh read-only review agent with `model: opus` (self-contained prompt: unit contract + base and owned diff) may supplement it for a large diff. Record the review in §7.

| Gate | Stage / active from | Exact command and cwd | Required assertions/discovery | Setup |
|------|---------------------|-----------------------|-------------------------------|-------|
| G-BASE | 0.2 baseline; rerun after 0.3 | repo root: `go build ./... && go vet ./... && go test ./internal/check ./internal/wire ./internal/server ./internal/agent`; `cd web && pnpm run typecheck && pnpm run lint && TZ=UTC pnpm exec vitest run` | pass/fail per command recorded in §7; pre-existing failures into §10, never fixed silently | none |
| G-GO-FOCUS | every Go unit | repo root: `go test -race -shuffle=on -count=1 <packages of the unit> [-run '<IDs>']` | named tests discovered (`go test -list '<IDs>' <pkgs>` > 0) and passed; hyphenated test IDs map to Go test names and `-run` patterns by dropping **all** hyphens (`WSER-001` → `TestWSER001`, `WSER-INT-001` → `TestWSERINT001`, pattern `WSERINT`; `INT-FIRSTRUN-001` → `TestINTFIRSTRUN001`, pattern `INTFIRSTRUN`), each unit's command states the exact pattern and the discovery check (`go test -list`) must list > 0 tests | none |
| G-GO-INT | Go units with an integration test | repo root: `go test -tags=integration -race -count=1 <pkg> -run '<IDs>'` | named tests discovered and passed (zero discovery = fail) | Docker, local images only |
| G-GO-FULL | 6.1, final | repo root: `make fmt-check lint vet-tags test` | all green | none |
| G-GO-INT-ALL | 6.1, final | repo root: `go test -tags=integration -race -shuffle=on -timeout=15m -skip 'TestINTRO002|TestINTGIS001|TestINTEXT002' ./...` (= `make test-integration` minus three PostGIS tests: `test/pgtest/postgis.go` maps major 18 to `postgis/postgis:18-3.6-alpine`, which is not a local image and the plan authorises no pull; each of the three is recorded "not run: image not local" in STATE §8 and in the final report; long: background + notification, `timeout` 600000 ms per call) | all green except baseline-recorded failures | Docker, local images only |
| G-GO-INT-LONG | 13.1 closure, P13, final | repo root: `PGLENS_FIRSTRUN_LONG=1 go test -tags=integration -race -count=1 -timeout=20m ./cmd/pglens-agent -run INTFIRSTRUN` (≈ 11 min of real clock: background + notification; the test calls `t.Skip` when the variable is unset, so `make test-integration` stays inside its 15-minute per-package budget) | the test is discovered (`go test -tags=integration -list INTFIRSTRUN ./cmd/pglens-agent` > 0) and passes; a skip is not a pass | Docker, local images only |
| G-UI-LEGACY | P1, P3, P9, P11, P13, P14, P15, P16, P17 closures, 8.2 | repo root: `make web-build` (regenerates the tracked `internal/webui/dist/index.html`, which is never staged or discarded), then `go test -tags=e2e -timeout=40m -count=1 ./test/e2e/... -run 'UI'` — the body of `make test-ui-e2e` without its `build-images` prerequisite, which runs `docker build --pull` (long: background + notification); the harness starts the pglens server/agent as `test/harness` decides (record in §6 whether from the local `ghcr.io/manprint/pglens-*:dev` images that G-DEPLOY builds or from binaries) and uses the Playwright Chromium already installed | SYS-UI-000..011 discovered and passing; when the run cannot start offline (missing local image or browser), record the exact error in §9, mark the gate UNVERIFIED and list it in the final report (REQ-24) — never skipped silently | Docker, local images only, G-DEPLOY |
| G-WEB-STATIC | every web unit | repo root: `make web-typecheck web-lint` (web-lint regenerates the API types and runs lint + format check, ending with `git diff --exit-code` on the generated types) | zero errors; a unit that changes `api/openapi.yaml` also runs G-API and stages the regenerated `web/src/api/generated.ts` first (the diff check compares against the index); `react-hooks/rules-of-hooks` clean | pnpm install done |
| G-WEB-UNIT | every web unit | `cd web && TZ=UTC pnpm exec vitest run <paths of the unit>` | named test files discovered, all pass | none |
| G-WEB-FULL | P-closures touching web; 6.1; final | repo root: `make web-typecheck web-lint`; `cd web && TZ=UTC pnpm exec vitest run`; `make web-coverage-gate web-budget` | all green; coverage gates lib 95 %, features 80 %, charts 90 %, state 100 %; bundle budget (split routes with `React.lazy` rather than raise it, `advisor` approval) | none |
| G-API | units changing `api/openapi.yaml` or response shapes | repo root, in this order: `cd web && pnpm run gen:api`; `make api-docs`; `git add web/src/api/generated.ts docs/api.md` (the Makefile gate compares against the index); then `make web-gen-api api-docs` and `git diff --exit-code -- web/src/api/generated.ts docs/api.md` | no diff after regeneration; fixtures per operationId exist (contract test) | the regenerated files are owned by the unit |
| G-RO | units touching the agent; P-closures of phases 13, 14, 16; 8.1; final | repo root: `go test -tags integration ./internal/agent/ -run TestINTRO001 -count=1` | passes; read-only audit green at the current tier | Docker |
| G-STACK | before/after live units | `docker compose -p pglens-odoo -f /home/agent/pglens-odoo-stack/compose.yml [-f /home/agent/pglens-odoo-stack/ha/compose.ha.yml once it exists] ps` all `healthy`/`running` (after 7.2 also `pg-ha-2`, `pg-ha-3`, `hactl`); `curl -s http://127.0.0.1:9090/healthz`; latest backup in `/home/agent/odoo-backups/` verified (`verify_backup.sh`, 0.1) | all services healthy; backup age < 24 h before any destructive/lifecycle step | stack running |
| G-DEPLOY | every unit with a live e2e | `/home/agent/pglens-odoo-stack/deploy.sh` (created at 0.1 S4): plain `docker build -f Dockerfile.agent -t ghcr.io/manprint/pglens-agent:dev --build-arg VERSION=dev .` and the same for `Dockerfile.server` (never `make build-images`: it adds `--pull` and reaches the registry; a missing base image falls back to the stack-authorised pull), then `docker compose up -d --no-deps --force-recreate pglens-server pglens-agent` and wait healthy (the builds fetch Go modules and pnpm packages through the proxy: inside the user's authorisation for builds of this stack, disclosed in the final report) | images built from the current tree; both containers healthy; `/api/v1/instances` answers within 2 collection cycles; image IDs recorded in §7 | stack running |
| G-VIS | units with a live visual check | `cd web && PGLENS_UI_BASE_URL=http://127.0.0.1:9090 PGLENS_UI_PASSWORD=<from stack .env> PGLENS_BOOTSTRAP_TOKEN=<from stack .env> pnpm exec playwright test --project=desktop-fullhd -g '<ID>'` (selection by test title across the project's specs; a unit may add a spec path only when it says so) | `playwright test --project=desktop-fullhd --list -g '<ID>'` count > 0 recorded; all pass; screenshots under `test/e2e/_artifacts/ui/` | G-DEPLOY, seeded Odoo |
| G-QA-HARNESS | 0.4 | `cd web && PGLENS_QA_ALLOW_STIMULUS=1 PGLENS_UI_BASE_URL=http://127.0.0.1:9090 PGLENS_UI_PASSWORD=<from .env> PGLENS_BOOTSTRAP_TOKEN=<from .env> pnpm exec playwright test --project=harness` (node-only: 14 tests = QA-HARNESS-001..009, 012..015 and QA-CLEAN-001 last), then the same environment with `--project=desktop-fullhd e2e/qa/harness-browser.spec.ts` (QA-HARNESS-010/011; 2 tests) — `QA-TRACE-001` joins the `harness` project at 6.4 | discovery counts above recorded in §7 and every listed test passing | none beyond the stack |
| G-QA | live stimulus/lifecycle units, 6.4, final | `cd web && PGLENS_QA_ALLOW_STIMULUS=1 [PGLENS_QA_ALLOW_LIFECYCLE=1 — mandatory whenever the selection contains a test that stops or restarts a service: all of `@lifecycle`, and in `@indicators` RDY-E2E, SORIGIN-E2E-002/003 and IND-NOTIF] PGLENS_UI_BASE_URL=http://127.0.0.1:9090 PGLENS_UI_PASSWORD=<from .env> PGLENS_BOOTSTRAP_TOKEN=<from .env> pnpm exec playwright test --project=desktop-fullhd -g '<ID or @tag>'` (`QA-CLEAN-*` and `QA-TRACE-*` live in `qa/harness.spec.ts` / `qa/traceability.spec.ts`, which every browser project ignores: run those two with `--project=harness -g '<ID>'` in the same environment; no spec path: the 6.4 specs `indicators`, `lifecycle`, `slow`, `qa_tour` and the unit specs are found by title/tag) | discovery > 0; every test has a control run and the recorded sabotage run; QA-CLEAN-001 passes after the suite | backup < 24 h for lifecycle |
| G-HA | 7.x | `cd web && PGLENS_QA_ALLOW_STIMULUS=1 PGLENS_QA_ALLOW_LIFECYCLE=1 PGLENS_UI_BASE_URL=http://127.0.0.1:9090 PGLENS_UI_PASSWORD=<from .env> PGLENS_BOOTSTRAP_TOKEN=<from .env> pnpm exec playwright test --project=desktop-fullhd e2e/ha.spec.ts -g 'LIVE-HA|HA-STATIC|HA-DRILL'` + repo root `go test -race -shuffle=on -count=1 ./test/ha/hactl` (+ `go test -tags=integration -race -count=1 -timeout=10m ./test/ha/hactl -run HACTLINT`) | V-1…V-12 outcomes recorded; discovery > 0 for each pattern | HA stack up, verified backup |
| G-SECRETS | before every commit; final | repo root; only counts or exit codes are printed. (a) pattern: `git diff HEAD` and `git diff --cached` piped to `grep -cE`, and `grep -rIcE` over `docs/plans web/src web/e2e test`, for the regex `chat\.googleapis\.com/v1/spaces/[A-Za-z0-9_-]{8,}` and for `[?&]key=[A-Za-z0-9_-]{8,}` and `[?&]token=[A-Za-z0-9_-]{8,}` (one-character placeholders such as `spaces/X` or `key=K` do not match, and this gate text does not match itself). (b) value: in a subshell that sources the stack `.env` (`set -a; . /home/agent/pglens-odoo-stack/.env; set +a`), stop with an error when `PGLENS_GOOGLE_CHAT_WEBHOOK_URL` is empty, then `git grep -qF --untracked -- "$PGLENS_GOOGLE_CHAT_WEBHOOK_URL"`, `git diff HEAD` and `git diff --cached` piped to `grep -qF --`, and `grep -rIqF -- "$PGLENS_GOOGLE_CHAT_WEBHOOK_URL" docs/plans web/src web/e2e test` must each find nothing (exit status only; the value is never printed or logged) | all pattern counts 0 and every value search finds nothing | stack `.env` readable |
| G-FINAL | 8.2 | all of the above on the final tree + reference scenario items 1–17 each ticked with evidence | every item proven | all |


## 4. Work ledger

Settled rows: `history.md` (2026-11-05 migration entry). Only unsettled rows (uncommitted/handoff/failed) belong here.

| Type | ID / attempt | Plan revision | Agent | Changes | Evidence/review | Commit |
|------|--------------|---------------|-------|---------|-----------------|--------|

## 5. Files and ownership

Pre-existing changes to preserve (from the session start; not owned by any unit; committed once at 0.2 S1b as the baseline commit, never reverted):

| Path | State | Note |
|------|-------|------|
| README.md, api/openapi.yaml, deploy/agent.example.yaml, deploy/sql/README.md | M | index-analysis phase-1 follow-ups (dead rows, table columns) |
| internal/agent/readonly_audit_integration_test.go, internal/server/{api_relations.go,api_relations_integration_test.go,api_relations_test.go,staleness.go}, internal/wire/factkinds.go | M | `/tables` extension, `table_columns` fact |
| internal/check/table_columns.go, internal/check/table_columns_test.go | ?? | `table_columns` check |
| web/src/api/{generated.ts,keys.ts,queries.ts}, web/src/features/indexes/IndexAnalysisPage.tsx | M | |
| web/src/components/charts/Sparkline.{tsx,test.tsx}, web/src/features/indexes/{CleanupPanel,DeadRowsPanel}.{tsx,test.tsx}, web/src/features/indexes/SortHeader.tsx, web/src/lib/{deadRows,indexCleanup,sparkline}.{ts,test.ts}, web/src/test/fixtures/getInstanceTableHistory.ts | ?? | cleanup / dead-rows UI |
| internal/webui/dist/index.html | M | build artefact: **never staged, never discarded** (explicit-pathspec commits keep it out) |
| .claude/ | ?? | local tool settings: **never committed** |
| docs/plans/005_plan-ConsoleInsights/ | ?? | this plan (committed at 0.2 S1b in its own commit) |

Unit-owned paths are listed in each phase file's sub-phase `Files` field and recorded here when a unit opens.

## 6. In-flight checkpoint

Phase 16 DONE (P16 attempt 2 passed the advisor review; attempt 1 failed). STATE compacted at P16 DONE (16.x rows in `history.md`). Uncommitted: only `internal/webui/dist/index.html` (rebuilt by G-UI-LEGACY, never staged) and `.claude/`. Backup `02-loaded-20261104T170945Z` stops being VERIFIED at 2026-11-05 17:09Z: take a fresh verified backup before any lifecycle/G-QA test after that time.
Running resources: stack `pglens-odoo` up and healthy (images from 8256b9f; production code unchanged since). Backup `02-loaded-20261104T170945Z` VERIFIED (expires 2026-11-05 17:09Z); stack `agent.yaml` = `agent.yaml.bak-16.4`. Research checkpoint: R3 pending (7.0).

## 7. Verification and reviews

Latest result per gate; earlier detail in `history.md`.

- P16 gates (2026-11-05; attempt-1 results, valid for 16.4 attempt 2 because production code is unchanged): G-GO-FOCUS 5 packages ok (`-race -shuffle=on`); G-GO-INT `TestACTCFGINT001` pg15+pg18, `TestSESSAPIINT001` ok; G-RO `TestINTRO001` pg15+pg18 ok; G-API no diff (`web-gen-api api-docs`); G-WEB-UNIT `src/features/locks src/lib` 444 passed; G-DEPLOY: no production change since 8256b9f, images `pglens-server:dev` c812ba5a3f3b, `pglens-agent:dev` 043139c08de5; G-VIS `VIS-LOCKS-001` (list = 1) passed; G-QA SORIGIN-E2E-001..003 passed (001 re-run after the control-run change: sample = pg_stat_activity after 13 s, baseline control after 10 s, sabotage detected), QA-CLEAN-001 passed, `agent.yaml` cmp-equal.
- Attempt 2 reruns (2026-11-05): G-WEB-STATIC (lint 0 errors / 2 pre-existing warnings, prettier, `tsc -b`) after the spec edits; G-UI-LEGACY `go test -tags=e2e -timeout=40m -count=1 ./test/e2e/... -run UI` after `make web-build` (12 tests: SYS-UI-000 = `TestUI_Smoke`, 001..011): run 2 failed once in `TestUI_FindingMute` at the post-unmute API poll (state not back within 60 s; cause not determined, test passed alone 3 times); the poll now reports the last API state on timeout; run 3: 12 of 12 pass (incl. SYS-UI-008 BlockingTree, 009 Cancel, 011 Accessibility; ok 697 s). If `TestUI_FindingMute` fails again, its diagnostic names the last state.
- 4.1 (2026-11-05): G-GO-FOCUS `go test -race -shuffle=on -count=1 -cover ./internal/indexadvice` ok, coverage 93.2 % (gate ≥ 90 %); `go test -list 'IDXA' ./internal/indexadvice` lists 36 tests; corpus 120 statements (≥ 60), all invariants of `TestIDXACORPUS002_Invariants` hold; gofmt, `go vet`, golangci-lint 0 issues. Review (advisor, Opus 5.5): fuzz target renamed to `FuzzIDXAFUZZ001` so the gate pattern discovers it, invariants asserted, `ILIKE` demoted to `expression` skip. G-SECRETS: only the host name in text.
- G-SECRETS: pattern counts over the diff 0; the only match under `web/src` is the committed canary fixture `NotificationsPanel.test.tsx` (`spaces/UICANARYSPAC…`, fake); (b) value check not runnable (webhook deferred).
- Review P16 (advisor, Opus 5.5): attempt 1 rejected (README metric names, baseline control run, G-UI-LEGACY red); attempt 2 approved after record fixes (tests table, §9 FindingMute open item, README names verified against `internal/check/activity.go`, `lockSessionsLimit = 100`).

## 8. Technical revisions and deviations

| Revision | Previous decision/step | Approved replacement and reason | Supervisor | Dependents/revalidation |
|----------|------------------------|---------------------------------|------------|------------------------|
| 1 | (initial) | Plan authored in one session; requirements added during planning by user messages: phases 9–17, sub-phases 0.4, 1.5, 6.4, 13.x, D22–D32 | `advisor` review R-PLAN-1 pending | — |
| 1 | D9 (Patroni/etcd/HAProxy, Q1) | HA lab with local images only and the `hactl` Go controller; no external pull is authorised; Q1 resolved | supervisor decision (network constraint) | phase_08 written accordingly |
| 1 | pre-start amendments from R-PLAN-1 | T2 = `pg_signal_backend` only (no pgstattuple), stack role `pglens_monitor` grant command; advisor windows limited to `unusedWindow`/`backupHistory`; cold-start envelope fields `agent_started_at`/`check_intervals` (13.2 S0); one session classifier `internal/sessionorigin` (D27); `deploy.sh` replaces `make build-images`; Playwright projects `chromium`/`harness`/browser projects; hactl timeline-aware election and docker-level fencing; retention per-family `min_days`; test-ID to Go `-run` mapping; batch boundaries for large sweeps | supervisor review R-PLAN-1/2 | all phase files |
| 1 | roster: agent-2 = Sonnet 5.5 implementer | user 2026-10-29: implementation and reviews Opus 5.5, only exploration and mechanical work Haiku 5.5, Sonnet not used (every `Model:` field, the overview summary, §3 roster/delegation rules and the commit trailer changed; no unit contract changed) | user | roster-only edit; per the user's stop on review rounds it was not re-reviewed |
| 1 | supervisor = `advisor` tool | user disabled the `advisor` tool at execution start (2026-10-29, `/advisor` → disabled; model Opus 5.5, effort medium); supervisor reviews are made by fresh read-only Opus 5.5 review agents (`model: opus`) with self-contained prompts | user | every review point in the phase files; §3 Supervisor access |
| 1 | D11 (no commits) | commits local with trailers (user set full-autonomous + WIP on) | user | all closure lines say "Commit per STATE §3" |
| 1 | G-GO-INT-ALL (R-PLAN-6) | `make test-integration` | skips `TestINTRO002_AgentOnlyReadsPostGIS`, `TestINTGIS001_SpatialColumns`, `TestINTEXT002_PostGISVersion` (PostGIS image not local, no pull authorised); to be listed as "not run: image not local" in the final report | supervisor (network constraint) | 6.1, 8.2 |
| 1 | Network constraint (user, 2026-10-29) | only the Odoo stack images/builds and `chat.googleapis.com` authorised | user: "autorizzo i download e l'uso della rete internet" — downloads and internet use authorised (toxiproxy download retroactively covered); still no push. Unblocks golangci-lint install, PostGIS image, other pinned images | user | all |
| 1 | 3.3 VIS-031 tps 6h/24h and VIS-010 6h outcome | accepted outcomes: pass, or UNVERIFIED skip only when the history precondition fails | the precondition held but the tests failed (6h 0.858–0.864, 24h 0.946) only because of the 2026-10-29 21:09–21:57 UTC stack outage (environment restart outside this session; evidence §7). The 0.95 threshold is not loosened and the tests are not turned into skips: recorded FAILED (environment outage) → UNVERIFIED; explicit re-proof added to 6.2 (`phase_07.md`); the 6 h window is clear after 2026-10-30 03:57 UTC, the 24 h one after 21:57 UTC; P3 closes before 03:57 UTC, so 6.2 owns the re-proof of all three | supervisor (R-3.3-C3, approved with conditions) | 6.2 |
| 1 | D26 budget (15.1 S1, measured > 8 px off) | app header 48, page header 56, tab bar 40, toolbar 44, table header 32, rows 28, pager 40, padding 32 = 852 | measured: shell header 57 (56 + border), padding 48 → 32 from 1600 px, page header 89/112/134, tab bar 36, toolbar 32/52, table header 41 → 33, one-line row 29, pager 32; budget formula and examples in `fullhd-inventory.md` and the D26 row; 15.1 also fixed a 1 px page overflow on every page (`main` min-h ignored the header border; all widths, a bug fix); density classes in `fullhdTable.ts` apply from 1600 px only (FHD-005); DataTable column hints via `columnDef.meta` (`DataTableColumnMeta`: width, truncate, title), SimpleTable via `width`/`truncate`; the 15.1 route inventory marks only `/instances/:instanceId/locks` (16.3) and `/settings` (11.1) with `pendingUnit` (15.3 owns the cluster page fit, 15.2 rewrites the instance entry); `min-[1600px]:table-fixed` applies only to tables whose columns carry `width`/`truncate` hints (`FULLHD_FIXED`; an unhinted fixed table split columns equally and made `/indexes?tab=dead-rows` and `/alerts` taller); `assertFits` has the signature `assertFits(page, api, route)` (the API context resolves `:instanceId`, `:clusterId`, `:queryid`; later units 10.2, 11.1, 12.2, 14.2, 16.3, 4.4, 5.1 call it so) and asserts "no table card scrolls" for every kind; DataTable `meta` is typed through TanStack's `columnMeta` slot; table cards carry `data-table-card` for the measurement selectors | executor (measured); R-15.1 | 15.2, 15.3, 15.4, 10.2, 11.1, 12.2, 14.2, 16.3, 4.4, 5.1 |
| 1 | 15.2 existing-test and spec changes | phase_16 15.2 allows only InstancePage.test changes for one-section-at-a-time, ChartNav.test, and the named live specs | made: `ChartNav.test.tsx` OVC-005 ×3 render inside a Router (`renderWithProviders`, route `/instances/i-1?db=app`), the Host link href becomes `/instances/i-1?db=app&section=host`, the observer list excludes the Host tab link and the `host-metrics` report lines are dropped (no target on the Overview); `overviewCharts.test.ts` OVC-001 expects the `host` link with `section: 'host'`; `InstancePage.test.tsx` only gains an optional `route` param in `renderPage` and the INSTSEC tests (no existing expectation changed); live specs: VIS-020 opens `?section=host`, VIS-030 treats Host as a tab switch then clicks the Overview tab, TBL-E2E-005 visits `?section=catalog` (opening the Sequences sub-tab, Triggers is empty on Odoo) and `?section=relations`; §8 follow-up done: `SettingsSection` drops its `role="region"` wrapper (the table carries the name) and its empty state reads "No observed setting changes fall in the selected time range." (UI-INST-046 follows, same ID/intent); legacy specs (smoke, ui_acceptance) need no change: they only assert the instance header, which stays above the tabs. Tab labels: Overview, Host, Settings, Tables and indexes, Schema objects, Logical replication, Internals; tab switches use `replace` navigation like the table state | executor; R-15.2 | 15.2, 15.3, P15 |
| 1 | 15.3 existing-test and live-spec changes (beyond the cluster-page ones the contract lists) | contract: only `ClusterPage.test.tsx` (UI-CLUS-042/046/047/048, `renderPage(section?)`) and SYS-UI-001/007 | the failure handling restructured more pages, so these follow (IDs and intent kept): `AlertsPage.test.tsx` UI-ALERT-040/041/042 open `?section=delivery`; `EventsPage.test.tsx` UI-ALERT-052/058 open `?section=alerts` and read the Suppression column of the "Alert suppression status" table; `AlertRow.test.tsx` UI-ALERT-060 renders the alert row of the `alertColumns` table (Cluster/Instance cells read Unknown); `FindingsPage.test.tsx` `renderPage` expands every finding row (`expandFindings()`; the card is the row detail), UI-FIND-045 expands again after retry, UI-FIND-024 scopes `open` to its card; `IndexAnalysisPage.test.tsx` IDX-UI-001 opens the Summary view, IDX-UI-002 the Advisor findings/Foreign keys/Summary views; `RelationsSection.test.tsx` UI-INST-050/053/054/055/056/058/059/067/068/069 open their `?view=`; `InternalsSection.test.tsx` UI-INT-001/002, TBL-UI-021/023/024 and `LogicalReplicationSection.test.tsx` UI-LREP-001, TBL-UI-026 open their view; `QueryListPage.test.tsx` none (UI-QRY-013 note now in the toolbar line, still visible without rows); live specs: SYS-UI-010 opens the finding row (paging to it) before the card, `getByLabel('State', { exact: true })`; SYS-UI-001 opens `/events?cluster_id=…&type=failover_detected` and `/alerts?cluster_id=…` (paged lists); TBL-E2E-005 adds `/findings?view=rules`; FHD-FIT-001 new; `assertFits` also checks the pager per viewport (R-15.3) | executor; R-15.3 | 15.3, P15 (G-UI-LEGACY) |
| 1 | 15.3 layout changes below 1600 px (I-7 "responsive behaviour unchanged") | `min-[1600px]:` classes only | allowed by the failure handling (tabs, controls into the toolbar line, tiles into headers) and so also below 1600 px: DatabaseSelector moved into the instance Overview tab (it scopes only the Overview; its loading/error state shows there); Slowest queries server/rank/database controls in the table toolbar (a bar while there are no rows; `useKeepFocus` keeps focus when they move), tiles and top-N note in the list header; Query inspector sort select in the Section header, budget/eviction notes in the toolbar line; `TableToolbar` empty option `<Label>: all` (was `All`); Findings `?view=rules` tab, summary chips in the Findings header, filters one row, findings a paged `SimpleTable` with the card as row detail, the review-not-automatic note at the end of the page subtitle (truncated from 1600 px, full text in its `title`); alerting `?section=` tabs (`/alerts` alerts/delivery, `/events` events/alert status), alerts/events/alert-status tables, rule editor above the list (focus moves to its heading); Index analysis controls and tabs in the page header, Indexes tab `?view=` inventory/summary/findings/foreign-keys, notes/figures in each panel header; instance header two lines with section tabs, flat `InstancePanel`, `?view=` sub-tabs (relations, catalog, internals, logical), hints in toolbar lines; follow-up R-P9 Index analysis `break-all` resolved (names truncate with `title`) | executor; R-15.3 | 15.3, 15.4 |
| 1 | R-15.3 findings not applied | m8 keyboard order of the instance header from 1600 px (metrics before Instance views) | kept: the DOM order is the below-1600 visual order (metrics, views, section tabs); the metrics hold one focusable element (the owning-cluster link); reordering the DOM would break the below-1600 order instead. Nits: `PanelNote`/`ToolbarNote`/`FilterLineNote` and `InstancePanel` vs `Section` stay feature-local (candidate shared "truncated note" and "flat Section" for 15.4); event payload helpers moved to `lib/events.ts` (applied) | supervisor | 15.4 |
| 1 | follow-up (R-15.3) | routes that fit only because the live stack had no rows (cluster topology/slots/drift/events tabs, `/alerts`, `/alerts/silences`, catalog partitions/functions/security-definer/postgis, internals temp) | marked "unproven at 20 rows (no data)" in `fullhd-inventory.md`; the cluster `events` tab still renders `EventTimeline` as an unpaged card list: 15.4 / 7.x re-measure on the HA stack and page/table it if it overflows | supervisor | 15.4, 7.x |
| 1 | 17.1 API details (R-17.1) | contract 17.1 and the phase-17 worked examples | (a) caption ids are dot/kebab case (`index.redundant-prefix`, `activity.connections-used`, `overview.cache-hit-ratio`) because CAPTION-005 requires it; (b) the worked example `overview.tps` "summed over all databases (rollbacks are not included)" is untrue to the Overview chart (per selected database, rollbacks drawn as their own line): the entry reads "…in the selected database" and its how-to-read no longer claims a cause for gaps; `overview.cache-hit-ratio` likewise; `locks.origin.pglens` is not in the catalogue yet (the session origin arrives with 16.1–16.3, which adds it; rule 2); (c) `formatUnit` returns the spelled unit (D30); exports are `glossary`/`columnHelp` as in the contract (+ `xmin`, `queryid`, `wait event`); (d) NEW `web/src/components/layout/tooltipPlacement.ts` (the measuring helper); the tooltip renders in a portal, always present and hidden when closed (stable `aria-describedby`/`aria-controls`), stays open while hovered (100 ms close grace, WCAG 1.4.13), closes on Escape anywhere and on a tap outside, follows scroll/resize; `Term` is focusable; (e) CARD-003 vs CAPCMP-005: a tile with `captionId` carries `data-caption-id` and the ⓘ, no visible caption line; ChartCard/Section/DataTable carry `data-caption-id` on the root only (the inner `Caption` passes `marked={false}`); (f) DataTable caption sits in the toolbar row (`flex-initial` toolbar, caption takes the rest, truncated from 1600 px) and requires `ariaLabel` (type union); Section puts the ⓘ beside its h2; (g) `captionCoverage.test.ts` parses TSX with TypeScript: only `captionId="…"` counts, computed ids are listed in `DYNAMIC_ALLOWLIST` and need a run-time spec test (17.2), spreads count as missing, wrapper components must take a required `captionId`; CARD rule flags template literals, numbers, strings outside `TILE_UNITS` and `format*()` except `formatUnit`; allowlists are exact counts (91 uncaptioned, 0 dynamic, 6 bad units) | executor; R-17.1 | 17.2, 17.3 |
| 1 | follow-up (R-17.1, for 17.2) | column hints | a `ColumnHint` adds about 24 px inside fixed-width, `whitespace-nowrap` header cells and joins the column header's accessible name: 17.2 widens `meta.width` where it adds help and keeps header queries by regex; chart/table specs take a required `captionId` with a run-time test per spec list; every `InfoHint` leaves a hidden tooltip in `document.body`, so tests query a caption sentence inside its card or through `getByRole('tooltip')`, never `screen.getByText(sentence)`; the DataTable caption (below 1600 px a full line above the controls, from 1600 px beside them, `min-w-48 max-w-md`, truncated) has no live proof yet: the first 17.2 table that gets a `captionId` must pass `assertFits` at 1920×950 with its filters (toolbar ≤ 52 px, caption width > 0) before more tables follow, and 17.3 re-checks; `MetricTile` `unit` variables are closed by 17.2 S9 (`TileUnit`) | supervisor | 17.2, 17.3 |
| 1 | 10.2 existing-test changes and live checks (R-10.2) | contract 10.2 | UI-FLEET-021 (`FleetPage.test.tsx`) expects `addr:port (cluster)`: the agent-health strip now uses `instanceLabel` (falls back to `addr:port`); UI-FIND-012 now selects listed ids (the Findings Cluster/Instance filters are selects from the clusters list; an unknown URL id stays as an extra option, still asserted with `42`/`…0007`); UI-FLEET-046 and the unnamed "uses instance and firing-alert summary counts as filters" test give their alerts a `cluster_id` (Fleet now assigns an alert to a cluster by `cluster_id`, else by its instance's cluster — the server never sets a `cluster` label, so matching a label against the name was never right); UI-REPL-011 follows the rename `replicationInstanceLabel`; CLNAME-E2E-001 runs `assertFits` on `/`, the cluster and the instance pages, while `/settings` stays `pendingUnit: '11.1'` (whole-page fit UNVERIFIED) but the Names table — the first `DataTable` with a `captionId` (R-17.1 follow-up) — is measured live at 1920×950 (toolbar row ≤ 52 px, caption width > 0); `PageHeader` gains `truncateTitle` (one cut line from 1600 px, full text in `title`) for named clusters/instances | executor; R-10.2 | 10.2, 11.1, 17.2 |
| 1 | follow-up (R-P10) | Alerts and Events cluster filters | still free-text "Cluster ID" inputs (`AlertsPage.tsx`, `EventsPage.tsx`) while the tables show names; the phase 10 contract names only Index analysis and Findings selects: candidate for 17.2 (helper text) or a later UI unit (select built from the clusters list like Findings) | supervisor | 17.2 |
| 1 | follow-up (R-11.1 minor 2) | `/settings?view=databases` registered `fit` | the view stacks one database table per instance, so its height grows with the fleet; it fits on the one-instance dev stack only: marked "unproven with several instances" in `fullhd-inventory.md`; 7.x (HA lab, 3 instances) / 15.4 re-measure, paginate instances if it fails | supervisor (R-11.1) | 7.x, 15.4 |
| 1 | 11.5a deviations (R-11.5a) | contract 11.5 Files / N1–N7 | (a) exported `notify.RedactError`; (b) channels built once (`wireNotificationChannels`) and shared by the API and `alert.NewChannelNotifier` (N3); (c) redaction of every configured URL, its `Redacted()` form, user/password, path, query, query values and path segments ≥ 8 chars; (d) status `timeout` also when the 10 s test deadline expired (the channel turns a last-attempt timeout into text); (e) generic 409 detail (variable names live in the client, N6); (f) 429 `test already running` (one test in flight per kind); (g) test messages render without the cluster/instance context line (Slack, Google Chat); (h) extra files: `cmd/pglens-server/*`, `internal/alert/notify/*`, `web/src/api/{keys,queries}.ts`, `SettingsPage*`, `DeliverySection.test.tsx`, `captions.ts`; (i) 11.5b not implemented: BLOCKED-Q2 (UI shows the environment instructions only); (j) badge caption (I-16) → 17.2 | supervisor (R-11.5a) | 11.5b, 17.2 |
| 1 | 13.2 deviations (R-13.2) | contract 13.2 Files | (a) new table `check_results(instance_id, check_name, datname, first_result_at, last_result_at, last_failed)` written by the ingest pipeline in the envelope transaction (`WHERE EXISTS` on the instance; flag only, never error text, I-10) — nothing durable recorded per-check result times before; (b) `FirstRunWarmupCap`/`FirstRunBound` moved to `internal/wire` (server must not import `internal/agent`); (c) `index.unused` on PG16+ skips an index whose `last_idx_scan` is inside the window (D22b, WINDOW-007) and the statistics observation is anchored on `last_idx_scan` when set; (d) `flushEnvelope` gained the coverage parameter (6 call sites); (e) `queries.test.tsx` probes `useCoverage`; (f) test file `advisor_window_source_test.go` (a `_windows` suffix is a GOOS build tag); (g) README/LIMITS written in 13.2 (PG ≤ 15 limitation, coverage endpoint, windows now effective). Migration 0027 was edited after its first dev deploy (unshipped) and reset on the dev DB (backup in `/home/agent/odoo-backups/pglens-0027-reset-*`). Follow-up (not in contract): an index last scanned before the window with short pglens history stays degraded (D22b "statistics age from last_idx_scan" shortcut not implemented) | supervisor (R-13.2) | 13.3 / later |
| 1 | User rulings 2026-11-04 (live gates) | stack absent after sandbox reset | (a) rebuild the Odoo stack (dir `/home/agent/pglens-odoo-stack`, backups `/home/agent/odoo-backups`, images `odoo:18`/`postgres:18` were gone; network pulls authorised); files recovered from earlier transcripts impossible (not present), so `compose.yml`, `deploy.sh`, `backup.sh`, `verify_backup.sh`, `seed/*`, `load/rpc_load.py`, README are REBUILT to the 0.1/0.2 contracts (record per file in §7); this is attempt 2 of 0.1/0.2 evidence. (b) Google Chat webhook deferred by the user: G-SECRETS value half and the notification-channel live e2e are NOT RUN by user decision (tested later by the user), not BLOCKED. (c) HA topology tested as 1 primary + 2 standbys (7.x). (d) all supervisor-recommended decisions approved; finish the plan. (e) backups are still required: QA guard and HA scripts accept only a fresh `VERIFIED` backup (< 24 h); `backup.sh`/`verify_backup.sh` rebuilt and run. | user | all live units, 7.x |
| 1 | 4.1 deviations (R-4.1) | contract 4.1 | (a) test IDs beyond the planned ranges: IDXA-LEX-007, IDXA-PARSE-017..020, IDXA-CORPUS-002 (invariants); (b) `Analysis` carries `Statement` (first verb) and tables/predicates carry `Scope` (query level, 1-based) so 4.2 can tell outer from sub-select references; (c) `ILIKE`/`~~*` are skipped as `expression` (not servable by a plain btree); `LIKE` prefix is `like_prefix`, operator class/collation choice is a 4.2 rule; (d) `MaxInput` = 64 KiB and nesting depth 48 bound the work; (e) corpus golden values were generated by `-update` and reviewed (invariants + sampled entries), not hand-written |
| 1 | commit identity | sandbox lost git config ("Author identity unknown") | commits use `git -c user.name=fabio -c user.email=fabio@git.it` (same author as earlier commits); no config written | executor | all commits |
| 1 | G-SECRETS canary | pattern count over the whole tree must be 0 | the one match under `web/src` is the fake fixture `web/src/features/settings/NotificationsPanel.test.tsx` (`UICANARYSPAC…`, `spaces/X`), committed since 6d9032b and accepted at P14; the real webhook appears nowhere | executor | final G-SECRETS |

## 9. Blockers

- Q2: RESOLVED 2026-10-30 by the user (yes, store write-only; see §8). No open questions.
- RESOLVED (user authorised downloads 2026-10-29): `golangci-lint` to be installed before the next G-GO-FULL (P1 closure).
- RESOLVED 2026-11-04: 14.1 RDY-INT-001 was blocked by the absent TimescaleDB image; the user authorised network, image pulled, test passes. Live-stack gates (Odoo stack, backups, `/home/agent/pglens-odoo-stack/.env`) are still absent: they are not needed before 14.3.
- UNBLOCKED 2026-11-04 by user ruling (rebuild stack, §8): was BLOCKED (14.2): live gates G-DEPLOY, VIS-RDY-001, G-QA need the Odoo stack, backups and `/home/agent/pglens-odoo-stack/.env` (webhook secret), all absent. Rebuilding the stack (phase 0.2) is the user's decision: needs secret and backups only the user holds. 14.3 also needs the stack; strict order stops the run here.
- OPEN (non-blocking, owner P15 G-UI-LEGACY run, 2026-11-05): one unexplained `TestUI_FindingMute` failure in the second full G-UI-LEGACY run at the post-unmute API poll (state not back to its real value within 60 s); the poll now reports the last API state on timeout; it passed alone 3 times and in the third full run. Earlier root cause, RESOLVED in 16.4 attempt 2: `TestUI_FindingMute` (SYS-UI-010) failed in G-UI-LEGACY at `ui_acceptance.spec.ts:816/835` because `openFindingCard` clicked `Show details of …` on a row that was already expanded (the card stays open across the state change); the spec now opens the card only when `aria-expanded` is not `true`. `TestUI_FindingMute` passes.
- No other blocker.

## 10. Do-not-repeat

- The grep hook denies symbol-like patterns in code files: use `tokensave_search` or prefix `TOKENSAVE_DISABLE_GREP_HOOK=1` for genuinely non-symbol greps.
- `tokensave_read` with `mode: "lines"` needs `lines: "a-b"`.
- The tokensave index serves `main` (branch `feat/index-analysis` is not tracked): read files changed on this branch directly; do not trust tokensave for them.
- Never put the webhook URL in any file or output.
- Git identity is not configured globally: commit with `git -c user.name=Fabio -c user.email=fabio@git.it commit …` (the repository's existing author).
- Vitest keeps a module cache in `web/node_modules/.vitest-cache`: a new fixture file under `web/src/test/fixtures/` may not be seen by `contract.test.ts` until run with `--experimental.fsModuleCache=false` (10.1). Confirmed 14.2: stale cache ran 92 of 127 contract tests (16 fixtures skipped silently, incl. getReadiness*); moving `.vitest-cache` aside gave 127 and full suite 1546. Compare test counts after adding fixtures.
- `make build-images` (`--pull`) reached the registry once in 14.2 with user network authorisation (2026-11-04); images `ghcr.io/manprint/pglens-{agent,server}:dev` are now local (legacy UI e2e needs them).
- The background `deploy.sh` log goes to the scratchpad (`deploy<n>.log`).
- Never use `docker build --pull` / `make build-images` (reaches a registry): use `deploy.sh`.
- Never run `test/harness/dump_test.go` (`-tags=e2e`, image `timescale/timescaledb:2.29.0-pg17` is not local): `make vet-tags` only compiles it.
- Never `git add -A` and never discard `internal/webui/dist/index.html`: commit with explicit pathspecs and check `git diff --cached --name-only`.
- Never run two Playwright jobs (or a `make web-build`/e2e gate) at the same time against `test/e2e/_artifacts/ui/results`: traces collide (`ENOENT … recording.trace` on `context.close()`).
- After a stack restart check `docker ps -a` for a `Created` agent (`up -d --no-recreate pglens-agent`); an outage stays in every chart window it covers (6 h/24 h live ratios fail until it ages out).
- Units that change `api/openapi.yaml`: stage regenerated `generated.ts`/`docs/api.md` before G-API/G-WEB-STATIC.
- Server integration tests (`getSharedPool`) need `timescale/timescaledb:2.31.0-pg17`: absent here; verify SQL on `postgres:18.6-alpine` with minimal DDL (temporary test, delete after) and seed Replace facts with `wire.AgentFlagsFact`/`wire.AgentFlagsKey`, never a literal key.

## 11. Progress board

### Execution order (strict; closures `P<N>` follow their last sub-phase)


0.1 → 0.2 → 0.3 → 0.4 → P0 → 13.1 → 1.1 → 1.2 → 1.3 → 1.4 → 1.5 → P1 → 2.1 → 2.2 → 2.3 → P2 → 3.1 → 3.2 → 3.3 → P3 → 9.1 → 9.2 → 9.3 → 9.4 → P9 → 15.1 → 15.2 → 15.3 → 17.1 → 10.1 → 10.2 → P10 → 11.0 → 11.1 → 11.2 → 11.3 → 11.4 → 11.5 → P11 → 13.2 → 13.3 → P13 → 12.1 → 12.2 → 12.3 → P12 → 14.1 → 14.2 → 14.3 → P14 → 16.1 → 16.2 → 16.3 → 16.4 → P16 → 4.1 → 4.2 → 4.3 → 4.4 → 4.5 → P4 → 5.1 → P5 → 15.4 → P15 → 17.2 → 17.3 → P17 → 6.1 → 6.2 → 6.3 → 6.4 → P6 → 7.0 → 7.1 → 7.2 → 7.3 → 7.4 → 7.5 → P7 → 8.1 → 8.2 → P8 (P0 closes after 0.4 and 13.1 is in Phase 13)


### Sub-phases
| ID | Phase file | Depends on | Status | Attempt | Evidence / reason |
|----|------------|------------|--------|---------|-------------------|
| 15.1 | phase_16.md | 9.3, 0.4 | DONE | 1 | FHD-001..006, FHD-ROUTES-001..003, FHD-MEASURE-001, `fullhd-inventory.md`, D26 revised (§7, §8); R-15.1 |
| 15.2 | phase_16.md | 15.1 | DONE | 1 | INSTSEC-001..009, FHD-ROUTES-001..003, FHD-INST-001 (§7 15.2); R-15.2 |
| 17.1 | phase_18.md | 1.2, 9.1 | DONE | 1 | CAPTION-001..005, GLOSSARY-001/002, CARD-001..005, CAPCMP-001..005, CAPTION-COVERAGE-001, CARD-COVERAGE-001 (§7 17.1); R-17.1 |
| 4.1 | phase_05.md | 0.2 | DONE | 1 | IDXA-LEX-001..007, IDXA-PARSE-001..020, IDXA-DEF-001..006, IDXA-CORPUS-001/002 (120 statements), IDXA-FUZZ-001 (§7 4.1, §8) |
| 4.2 | phase_05.md | 4.1, 3.1 | TODO | 1 | — |
| 4.3 | phase_05.md | 4.2 | TODO | 1 | — |
| 4.4 | phase_05.md | 4.3, 9.1, 15.1, 12.2 | TODO | 1 | — |
| 4.5 | phase_05.md | 4.4 | TODO | 1 | — |
| 5.1 | phase_06.md | 4.4, 1.2, 9.1, 15.1 | TODO | 1 | — |
| 15.3 | phase_16.md | 15.2, 9.1–9.3 | DONE | 1 | CLTAB-001..007, FHD-APPLY-001..051, FHD-FIT-001, FHD-ROUTES-001 (§7 15.3); R-15.3 |
| 15.4 | phase_16.md | 15.3, 11.1, 14.2, 16.3, 4.4, 5.1 | TODO | 1 | — |
| 17.2 | phase_18.md | 17.1, every UI unit | TODO | 1 | — |
| 17.3 | phase_18.md | 17.2 | TODO | 1 | — |
| 6.1 | phase_07.md | phases 0–5, 9–17 closed | TODO | 1 | — |
| 6.2 | phase_07.md | 6.1 | TODO | 1 | — |
| 6.3 | phase_07.md | 6.2 | TODO | 1 | — |
| 6.4 | phase_07.md | 6.2, 6.3, 0.4 | TODO | 1 | — |
| 7.0 | phase_08.md | 6.3, 10.2 | TODO | 1 | — |
| 7.1 | phase_08.md | 7.0 | TODO | 1 | — |
| 7.2 | phase_08.md | 7.1, 6.3 | TODO | 1 | — |
| 7.3 | phase_08.md | 7.2 | TODO | 1 | — |
| 7.4 | phase_08.md | 7.3 | TODO | 1 | — |
| 7.5 | phase_08.md | 7.4 (one unit 7.5.<n> per gap) | TODO | 1 | — |
| 8.1 | phase_09.md | 7 closed | TODO | 1 | — |
| 8.2 | phase_09.md | 8.1 | TODO | 1 | — |

README obligation per phase: see the Documentation table below; each phase file's "README obligation" line is the contract.

### Phases
| ID | File | Closure unit | Status | Review / commit reference |
|----|------|--------------|--------|---------------------------|
| 0 | phase_01.md | P0 | DONE | R-P0-1 / unit:P0:1 |
| 1 | phase_02.md | P1 | DONE | unit:P1:1 (baffa78) |
| 2 | phase_03.md | P2 | DONE | unit:P2:1 (3ee8016) |
| 3 | phase_04.md | P3 | DONE | R-P3 / unit:P3:1 |
| 4 | phase_05.md | P4 | TODO | — |
| 5 | phase_06.md | P5 | TODO | — |
| 6 | phase_07.md | P6 | TODO | — |
| 7 | phase_08.md | P7 | TODO | — |
| 8 | phase_09.md | P8 | TODO | — |
| 9 | phase_10.md | P9 | DONE | R-P9 / unit:P9:1 |
| 10 | phase_11.md | P10 | DONE | unit:P10:1 |
| 11 | phase_12.md | P11 | DONE (11.5b done after Q2, unit:11.5b:1) | R-P11, R-P11b / unit:P11:1 |
| 12 | phase_13.md | P12 | DONE | R-P12 / unit:P12:1 |
| 13 | phase_14.md | P13 | DONE | R-P13, R-P13b / unit:P13:1 |
| 14 | phase_15.md | P14 | DONE | R-P14 / unit:P14:1 |
| 15 | phase_16.md | P15 | IN_PROGRESS | — |
| 16 | phase_17.md | P16 | DONE | R-P16 (advisor, attempt 2; attempt 1 rejected) / unit:P16:2 |
| 17 | phase_18.md | P17 | TODO | — |
| PLAN | — | PLAN | TODO | — |

Statuses: TODO, IN_PROGRESS, IN_REVIEW, DONE, SKIPPED, BLOCKED. SKIPPED retains a reason and does not automatically satisfy dependent units. 11.5b may end BLOCKED (Q2) without blocking P11 closure when recorded.

### Requirements
| REQ | Requirement | Owning units | Status | Evidence |
|-----|-------------|--------------|--------|----------|
| REQ-01 | Odoo 18 + PG 18 stack, pglens built from latest source, agent on Odoo DB, app on 9090 | done earlier; re-verified 6.2 | TODO | — |
| REQ-02 | Load data and install modules in Odoo | 0.1 | TODO | — |
| REQ-03 | Odoo env persistent and replicable; backups in /home/agent; easy restore | 0.1, 6.3, 7.1 | TODO | — |
| REQ-04 | Suggest missing indexes from frequent/slow queries, with weight and read/write overhead | 4.1–4.5 | TODO | — |
| REQ-05 | Flag duplicate/unused indexes | built (uncommitted); verified 6.1/6.2 | TODO | — |
| REQ-06 | Dead rows visibility of the most affected tables | built (uncommitted); verified 6.1/6.2 | TODO | — |
| REQ-07 | 360° index section (columns inventory, per-index history, index tax) | 4.1, 5.1 (+ built `table_columns`) | TODO | — |
| REQ-08 | Overview x-axis readable, time ranges visible | 1.1, 1.2 | TODO | — |
| REQ-09 | No balloon/tooltip-like markers | 1.2 | TODO | — |
| REQ-10 | TPS chart correct (most important chart) | 1.1–1.3 | DONE | §7 CSTEP/CTIME, OV-STEP, INT-API-007, VIS-010 1h/6h; R-P1 |
| REQ-11 | Chart section separated from summary cards | 3.2 | DONE | §7 OVC (3.2), VIS-030 |
| REQ-12 | More charts about queries and indexes | 3.1, 3.2 | DONE | §7 WSER, OVC, VIS-030 |
| REQ-13 | Charts in semantic sub-sections | 3.2 | DONE | §7 OVC (3.2), VIS-030 |
| REQ-14 | Host metrics arrive from the agent container | stack fix done; documented 2.2 | DONE | §7 2.2, P2 live re-check |
| REQ-15 | Host metrics charts, done well | 2.3 | DONE | HOSTCH, VIS-020, R-2.3-C2 |
| REQ-16 | Filesystem total/free populated | 2.1, 2.2 | DONE | LIVE-HOST-001..003, P2 live re-check |
| REQ-17 | Settings/autovacuum data populated (was jitter, not a bug) | 13.1, 6.2 | TODO | — |
| REQ-18 | Tests on all new developments: unit, integration, visual | every unit; gates G-* | TODO | — |
| REQ-19 | Role at T1 for dev/testing | 0.2 | TODO | — |
| REQ-20 | Raise deploy to T2 at the end; allow_signal false; commands enabled | 8.1 | TODO | — |
| REQ-21 | Migrate the current Postgres to HA (primary + 2 standbys, failover, load balance); check pglens handles it; implement if not | 7.0–7.5 | TODO | — |
| REQ-22 | Look at pglens "a db carico" while browsing the app | 6.2 | TODO | — |
| REQ-23 | Standing rules: local network only, no push, backups before destructive steps, caveman/tokensave tooling | all | TODO | — |
| REQ-24 | Disclosures: `tgsend` missing; pgxmock module download | 8.2 | TODO | — |
| REQ-25 | Plan first, then run methodically; do not lose pieces | this plan; STATE board | TODO | — |
| REQ-34 | Every table paginated, 20 entries per page (slow queries, index analysis, … all tables) | 9.1, 9.2, 9.3, 9.4 | DONE | §7 9.1–9.4, TBL-E2E-005 (I-12) |
| REQ-32 | ASH / wait analysis: x-axis timestamps overlap when the page is narrowed; fix | 1.4 | DONE | §7 VIS-015 (4 widths), VIS-016 390 px; R-1.4-C1b, R-P1 |
| REQ-33 | At startup collect the full set of metrics within 2 hours; we cannot wait 7 days for the complete picture | 13.1, 13.2, 13.3 (+ 11.4 window settings) | DONE | §7 13.3 matrix: 31/31 in ≤ 15 min |
| REQ-31 | Full search over everything in Slowest queries and Index analysis (all queries, indexes, tables, columns — not only the rows on screen), and… | 12.1, 12.2, 12.3 | DONE | 12.1–12.3, P12 (§7 12.1, 12.2, 12.3 live, P12 gates) |
| REQ-30 | Configure notifications with the dev Google Chat webhook so event/alert notifications are tested (webhook is a dev secret the user will dele… | 0.3 (channel + live test), 7.4 (HA event notifications V-12), 11.5 (UI) | TODO | — |
| REQ-29 | Enrich "Settings": whatever can be configured from the UI should be | 11.0–11.5 | TODO | — |
| REQ-28 | Possibility to rename the cluster in the settings (instances too: same mechanism, useful for HA node names) | 10.1, 10.2 | TODO | — |
| REQ-26 | In many sections there are no filters: add them | 9.1, 9.2, 9.3 (+ new tabs in 4.4 and 5.1, 11.x, 14.2, 16.3 use the same primitives) | DONE (phase 9 tables; later units reuse the primitives) | §7 9.2, 9.3, `tables-inventory.md` |
| REQ-27 | On tables, quick buttons to sort ascending/descending | 9.1, 9.2, 9.3 | DONE | §7 SORTBTN, TBL-UI, TBL-E2E-002 |
| REQ-35 | For each server, warn when something not configured prevents FULL monitoring, with the instructions to enable it (from the console where ava… | 14.1, 14.2, 14.3 | TODO | — |
| REQ-36 | Software optimised for Full HD monitors; table sizes handled for that size so that no scrollbar appears when the browser is full screen | 15.1, 15.2, 15.3 (+ rule I-13 for every later page, re-run in 6.2) | TODO | — |
| REQ-37 | ASH / wait analysis: CPU / client / IO / lock as separate charts with a clear legend | 1.5 | TODO | — |
| REQ-38 | Locks and activity, Active sessions: clear distinction (possibly two tables) between connections opened by pglens and those of the applicati… | 16.1, 16.2, 16.3 | TODO | — |
| REQ-39 | Active sessions: more information about the connections and the active queries | 16.1, 16.2, 16.3, 16.4 | TODO | — |
| REQ-40 | `checks.activity.by_application` must be on by default (the "Per-application activity counts disabled" callout must not appear on a default … | 16.1 (D28a), 16.3 | TODO | — |
| REQ-41 | Front-end tests with Playwright on the active instance: tear it down, update it, bring it up; stimulate Odoo with data to see pglens indicat… | 0.4 (harness), 6.4 (indicator and lifecycle suites), every unit's live spec | TODO | — |
| REQ-42 | Interface captions clear and understandable, saying exactly what each element does and monitors, for everybody | 17.1, 17.2, 17.3 (+ rule I-16 for every unit that adds UI) | TODO | — |
| REQ-43 | Summary cards must always be clear: no second unlabelled number (example: Index analysis "Duplicate groups 0 / 9 redundant prefix" is confus… | 17.1 (API), 17.2 (sweep of all cards), 17.3 | TODO | — |
| REQ-44 | A small circled "i" on cards that are hard to understand; hovering it with the mouse opens a tooltip with additional information | 17.1 (`InfoHint`), 17.2 (placement everywhere), 17.3 | TODO | — |

### Tests (families; every ID is listed in its unit's `Unit tests` / `e2e tests` field; counts recorded in §7)
| Family | Owning unit | Gate | Status | Evidence |
|--------|-------------|------|--------|----------|
| FHD-* | 15.1 | G-WEB-UNIT | TODO | — |
| FHD-MEASURE-* | 15.1 | G-VIS | TODO | — |
| FHD-ROUTES-* | 15.1 | G-WEB-UNIT | TODO | — |
| FHD-INST-* | 15.2 | G-VIS | TODO | — |
| INSTSEC-* | 15.2 | G-WEB-UNIT | TODO | — |
| CLTAB-* | 15.3 | G-WEB-UNIT | DONE | §7 15.3 |
| FHD-APPLY-* | 15.3 | G-WEB-UNIT | DONE | §7 15.3 |
| FHD-FIT-* | 15.3 | G-VIS | DONE | §7 15.3 (deploy 15.3g) |
| FHD-FIT-* | 15.4 | G-VIS | TODO | — |
| FHD-ROUTES-* | 15.4 | G-WEB-UNIT | TODO | — |
| VIS-FHD-* | 15.4 | G-VIS | TODO | — |
| CAPCMP-* | 17.1 | G-WEB-UNIT | DONE | §7 17.1 |
| CAPTION-* | 17.1 | G-WEB-UNIT | DONE | §7 17.1 |
| CAPTION-COVERAGE-* | 17.1 | G-WEB-UNIT | DONE | §7 17.1 |
| CARD-* | 17.1 | G-WEB-UNIT | DONE | §7 17.1 |
| CARD-COVERAGE-* | 17.1 | G-WEB-UNIT | DONE | §7 17.1 |
| CAPTION-COVERAGE-* | 17.2 | G-WEB-UNIT | TODO | — |
| CAPTION-PAGE-* | 17.2 | G-WEB-UNIT | TODO | — |
| CARD-ALL-* | 17.2 | G-WEB-UNIT | TODO | — |
| CARD-INDEX-* | 17.2 | G-WEB-UNIT | TODO | — |
| VIS-CAP-* | 17.3 | G-VIS | TODO | — |
| IDXA-CORPUS-* | 4.1 | G-GO-FOCUS | DONE | IDXA-CORPUS-001/002 (120 statements: 112 live Odoo, 8 synthetic) passed (§7 4.1) |
| IDXA-DEF-* | 4.1 | G-GO-FOCUS | DONE | IDXA-DEF-001..006 passed (§7 4.1) |
| IDXA-FUZZ-* | 4.1 | G-GO-FOCUS | DONE | FuzzIDXAFUZZ001: 131 seed sub-tests passed; 25 s fuzz run 1.4 M execs without panic (§7 4.1) |
| IDXA-LEX-* | 4.1 | G-GO-FOCUS | DONE | IDXA-LEX-001..007 passed (§7 4.1) |
| IDXA-PARSE-* | 4.1 | G-GO-FOCUS | DONE | IDXA-PARSE-001..020 passed (§7 4.1) |
| IDXS-* | 4.2 | G-GO-FOCUS | TODO | — |
| IDXS-W-* | 4.2 | G-GO-FOCUS | TODO | — |
| IDXE-* | 4.3 | G-GO-FOCUS | TODO | — |
| IDXE-INT-* | 4.3 | G-GO-INT | TODO | — |
| SUGG-UI-* | 4.4 | G-WEB-UNIT | TODO | — |
| VIS-* | 4.5 | G-VIS | TODO | — |
| IDXH-* | 5.1 | G-WEB-UNIT | TODO | — |
| IDXH-INT-* | 5.1 | G-GO-INT | TODO | — |
| INDEXDETAIL-* | 5.1 | G-WEB-UNIT | TODO | — |
| INDEXTAX-* | 5.1 | G-WEB-UNIT | TODO | — |
| TAXPANEL-* | 5.1 | G-WEB-UNIT | TODO | — |
| VIS-* | 5.1 | G-VIS | TODO | — |
| QA-TOUR-* | 6.2 | G-VIS / G-QA | TODO | — |
| ENV-* | 6.3 | G-GO-FOCUS | TODO | — |
| QA-CLEAN-* | 6.4 | G-QA-HARNESS | TODO | — |
| QA-TRACE-* | 6.4 | G-QA-HARNESS | TODO | — |
| SORIGIN-E2E-* | 6.4 | G-VIS / G-QA | TODO | — |
| HACTL-* | 7.1 | G-GO-FOCUS | TODO | — |
| HACTL-INT-* | 7.1 | G-GO-INT | TODO | — |
| LIVE-HA-* | 7.2 | G-HA | TODO | — |
| HA-STATIC-* | 7.3 | G-HA | TODO | — |
| HA-DRILL-* | 7.4 | G-HA | TODO | — |
| T2-* | 8.1 | G-VIS | TODO | — |
| IND-<NAME> (13 indicator tests, no numeric suffix) | 6.4 | G-QA | TODO | — |
| LIFE-<NAME> (7 lifecycle drills, no numeric suffix) | 6.4 | G-QA | TODO | — |

### Documentation
| Document/sections | Owning unit | Status | Evidence |
|-------------------|-------------|--------|----------|
| `README.md` (Index analysis → Suggestions, API entry, estimate caveats), `docs/api.md`. | P4 (phase_05.md) | TODO | — |
| `README.md` Index analysis section (history, Index tax), `docs/api.md`. | P5 (phase_06.md) | TODO | — |
| `README.md` checked against the final feature set (Settings, readiness, search, tables, charts, activity), `docs/LIMITS.md` and `deploy/sql/README.md`; stack README (`/home/agent/pglens-odoo-stack/README.md`) updated for load script and backups. | P6 (phase_07.md) | TODO | — |
| `README.md` (replication/topology section: what pglens shows for HA, the lab topology and its limits), `docs/` HA notes, stack README (`ha/`). | P7 (phase_08.md) | TODO | — |
| final consistency pass over `README.md`, `docs/LIMITS.md`, `deploy/sql/README.md`, stack README. | P8 (phase_09.md) | TODO | — |
| `README.md` (supported viewport statement), `docs/LIMITS.md` (below 1600 px behaviour), `fullhd-inventory.md`. | P15 (phase_16.md) | TODO | — |
| `README.md` UI section (captions and the glossary), `web/README.md` (how to add a caption/`captionId`, the card contract and the closed unit list). | P17 (phase_18.md) | TODO | — |

### Audits
| Report | Verdict | Current unresolved findings | Evidence |
|--------|---------|-----------------------------|----------|
| — | — | none | — |

## 12. Suspended implementation (only while auditing unfinished work)

Snapshot state: none.
