# ConsoleInsights — Implementation state
Format: pev-state 2

Read first at every session. Updated 2026-11-07 13:5x UTC by agent-1:claude-opus-5-5 (V001-C1 compaction).
Plan ID: 005_plan-ConsoleInsights. Revision: 1. Repo root: /mnt/fabio/dati/Git/SperimentazioniAI/postgres-analyze.
Plan baseline: `5929be2d5ffe96867a63791b8a0b18687e41f8b5` (branch `feat/index-analysis`); pre-existing uncommitted changes: see §5 (preserved; committed once as the baseline commit at 0.2 S1b, D32).
Roster (user instruction 2026-11-04, `agent:sonnet` + "autorizzo sonnet per tutto il piano, con opus come advisor"; supersedes the 2026-10-29 "Sonnet is not used" instruction): agent-1:claude-opus-5-5 (supervisor/reviewer: the `advisor` tool, re-enabled by the user 2026-11-04, set to Opus 5.5; for a full-diff review the advisor tool is used, and a fresh read-only review agent with `model: opus` is an allowed supplement); agent-2:claude-sonnet-5-5 (implementer, the executing session); agent-3:claude-haiku-5-5 (exploration and mechanical work only, Agent tool `model: haiku`). Phase-file lines naming `agent-2:claude-opus-5-5` now read agent-2:claude-sonnet-5-5. Active execution style: handoff (the executing session implements; every point where a phase file says `advisor` is served by the advisor tool).
State writer: the executing session agent-1:claude-opus-5-5; ownership: ACTIVE since 2026-11-06 11:22 CET (previous writer agent-2:claude-sonnet-5-5 released at the 11:2x deadline handoff; takeover on the user's explicit invocation).


## 0. Resume and completion protocol

See `PROTOCOL.md` (static cold-start rules).

## 1. Current unit and scope

- Invocation (current run): `/plan-execute-verify verify 005` (2026-11-07 13:20 UTC; settings from §3: full-autonomous true, WIP on); audit V001 closed PASS WITH FINDINGS, correction pass C1, C2 under this invocation. Saved execution scope below unchanged.
- Previous invocation: `/pev-v3 full-autonomous:true --wip-commit execute 005` (2026-11-06 11:20 CET).
- Active scope: plan (whole `005_plan-ConsoleInsights`, no selector); full-autonomous true, WIP on (explicit). Unchanged by the audit.
- Scope result: COMPLETE (2026-11-07; deviations awaiting the user's confirmation: FINAL_REPORT.md).
- Type: audit correction. ID / attempt: V001-C1 / 1. Status: DONE (2026-11-07).
- Last closure: V001-C1 (unit:V001-C1:1); plan closure PLAN unit:PLAN:1 (`6c3acef`); audit V001 `30d329e`.
- Next action: V001-C2 (legacy commit map); then none — the user confirms A1–A3 and REQ-20 `allow_signal`, deletes the test webhook, reviews and pushes.
- Stop latch: none.
- Branch `feat/index-analysis` (never switch or create branches); uncommitted only `internal/webui/dist/index.html` (never staged), `.claude/`, `web/src/node_modules/` (not owned).
- Network: user authorised internet use (2026-11-05); still no push.
- Stack (2026-11-07 13:40 UTC): HA, 8 services healthy, hactl leader odoo-db (TL 12), pglens on 127.0.0.1:9090 with the final code; newest backup `04-final-20261107T020829Z` VERIFIED, valid until 2026-11-08 02:08 UTC.

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
- Notifications: `tgsend` is installed (`/home/agent/.bin/tgsend`); FINAL_REPORT corrects the earlier "not installed" disclosure.

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

none

## 5. Files and ownership

none

## 6. In-flight checkpoint

none

## 7. Verification and reviews

none

## 8. Technical revisions and deviations

none

## 9. Blockers

none

## 10. Do-not-repeat

none

## 11. Progress board

### Phases (collapsed at PLAN COMPLETE; detail in history.md; legacy commit map: history.md "V001-C2")
| ID | File | Closure unit | Status | Attempt | Sub-phases | Closure commit |
|----|------|--------------|--------|---------|------------|----------------|
| 0 | phase_01.md | P0 | DONE | 1 | 0.1–0.4 | `ce49894` |
| 1 | phase_02.md | P1 | DONE | 1 | 1.1–1.5 | `baffa78` |
| 2 | phase_03.md | P2 | DONE | 1 | 2.1–2.3 | `3ee8016` |
| 3 | phase_04.md | P3 | DONE | 1 | 3.1–3.3 | `59260b9` |
| 4 | phase_05.md | P4 | DONE | 1 | 4.1–4.5 | `e86d1bf` |
| 5 | phase_06.md | P5 | DONE | 1 | 5.1–5.1 | `3504309` |
| 6 | phase_07.md | P6 | DONE | 1 | 6.1–6.4 | `14f671a` |
| 7 | phase_08.md | P7 | DONE | 1 | 7.0–7.5 | `dda99e9` |
| 8 | phase_09.md | P8 | DONE | 1 | 8.1–8.2 | `bb5a587` |
| 9 | phase_10.md | P9 | DONE | 1 | 9.1–9.4 | `55d16f1` |
| 10 | phase_11.md | P10 | DONE | 1 | 10.1–10.2 | `a1d02b3` |
| 11 | phase_12.md | P11 | DONE | 1 | 11.0–11.5 | `6ce7c81` |
| 12 | phase_13.md | P12 | DONE | 1 | 12.1–12.3 | `27beed1` |
| 13 | phase_14.md | P13 | DONE | 1 | 13.1–13.3 | `a017cbc` |
| 14 | phase_15.md | P14 | DONE | 1 | 14.1–14.3 | `afd26ec` |
| 15 | phase_16.md | P15 | DONE | 1 | 15.1–15.4 | `e9459be` |
| 16 | phase_17.md | P16 | DONE | 2 | 16.1–16.4 | `00248fe` |
| 17 | phase_18.md | P17 | DONE | 1 | 17.1–17.3 | `89d711e` |
| PLAN | — | PLAN | DONE | 1 | — | `6c3acef` |

### Audits
| Report | Verdict | Current unresolved findings | Evidence |
|--------|---------|-----------------------------|----------|
| [V001](verify/verify_001_2026-11-07.md) | PASS WITH FINDINGS | V001-F1, V001-F2, V001-F3 (MINOR) | gates rerun 2026-11-07 |

### Corrections
| ID | Closes | Status | Commit |
|----|--------|--------|--------|
| V001-C1 | V001-F1, V001-F2 | DONE | unit:V001-C1:1 |
| V001-C2 | V001-F3 | TODO | — |

## 12. Suspended implementation (only while auditing unfinished work)

none
