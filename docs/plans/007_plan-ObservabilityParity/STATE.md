# ObservabilityParity — State

Format: pev-state 2
Plan 007 rev 1 · repo /mnt/fabio/dati/Git/SperimentazioniAI/postgres-analyze · baseline b728cb73793b479d4511e57fab1c82f7a71a34af · updated 2026-11-21 21:51 UTC by agent:opus (claude-opus-5-5)
§0 Protocol: without this skill read PROTOCOL.md once. History: history.md (append-only, never read to resume).

## 1. Now
- Run: verify 007 · audit V001 closed FAIL (1 MAJOR, 1 MINOR) · correction pass done: C1, C2 DONE, findings FIXED · saved execution scope unchanged
- Settings: autonomy true (explicit (prompt) `full-autonomous:true`) · WIP on (explicit (prompt) `--wip-commit`) · deadline none
- Stop latch: none
- Scope: plan 007 · COMPLETE · overrides autonomy true, WIP on, roster agent:opus (explicit)
- Unit: none
- Base: HEAD c91aecfaa35f927fe05e8821b280806196c9f20e + tree 62c11f4af78fe2d3a07f4b35b6def5b554d6f4cc · branch main · last WIP tree none
- Last closure: 25293e6 (PLAN:1; legacy SHA reference, V001-F1)
- Next: none · PLAN 007 COMPLETE
- Owner: agent:opus (claude-opus-5-5) active since 2026-11-21 07:10 UTC · takeover none (previous owner released)

## 2. Readiness
none

## 3. Settings, roster, gates
- Roster: agent:opus (claude-opus-5-5) for every position (explicit Execute override 2026-11-21; planned roster agent-1:claude-fable-5-2, agent-2:claude-haiku-5-5). Effect: agent:opus implements every unit and performs every review, each labelled self-review.
- Plan defaults: autonomy true · WIP on · executor power low — set by Plan 2026-11-21; all three given by the Plan invocation
- Setup: Go 1.27.3; Node 24.22.1, pnpm 12.21.0 (cwd `web/` for web commands); Docker for integration tests (images `postgres:15.19-alpine`, `postgres:18.6-alpine`, `timescale/timescaledb:2.31.0-pg17`, local `pglens-pgext:18` from unit 0.2). Sandbox: outbound proxy `http://gateway.docker.internal:3128`; image builds need `--network host` and the proxy build arguments; `github.com` is TLS-intercepted, so download sources on the host with `curl` and never disable certificate checks. Disk and memory are limited: after each unit that used Docker, remove its containers, networks, volumes and build cache. Lab stack `/home/agent/pglens-odoo-stack` only in unit 15.2 (its `.env` is never printed). No push, no access to the real deploy machines, no network other than localhost and the outbound internet.
- Baseline note (D20): the working tree had 403 uncommitted paths of plan 006 when this plan was written. Units stage only their own hunks. Recommended before Execute: commit the plan 006 work.
- Supervisor access: agent-1 runs the session and dispatches each unit to agent-2 as a sub-agent with the unit's phase file section; agent-2 returns its diff and evidence to agent-1, the only state writer. Without sub-agents: session handoff per PROTOCOL.md. Current run: single agent:opus session, no dispatch.
- Test ID mapping: `ABC-001` ↔ Go `TestABC001`; integration `INT-ABC-001` ↔ `TestINTABC001`; web `UI-ABC-001` is the test title prefix.

| Gate | Active from | Command (cwd) | Must assert / discover |
|------|-------------|---------------|------------------------|
| G-BASE | unit 0.1 | G-GO-FULL, G-WEB-FULL, G-I18N, G-RO on the baseline tree (repo) | result and test count per gate recorded in §7; a failure is recorded as pre-existing and reported to agent-1 |
| G-GO-FOCUS | unit 1.1 | `go test -race -shuffle=on -count=1 <pkgs> -run '<IDs>'` (repo) | every listed test ID discovered and passed; zero discovery = fail |
| G-GO-INT | unit 1.1 | `go test -tags=integration -race -count=1 <pkg> -run '<IDs>'` (repo, Docker) | every listed `TestINT…` discovered and passed; a skip counts only when the unit names it |
| G-GO-FULL | baseline | `make fmt-check lint vet-tags test` (repo) | all green |
| G-GO-INT-ALL | P15 | `go test -tags=integration -race -count=1 ./... -skip 'TestINTRO002\|TestINTGIS001\|TestINTEXT002'` (repo, Docker) | all green; skips only those recorded in phases 10, 12, 13 |
| G-RO | baseline | `go test -tags integration ./internal/agent/ -run TestINTRO001 -count=1` (repo, Docker) | agent sends no statement outside the allowed read-only list (I-1) |
| G-WEB-STATIC | baseline | `make web-typecheck web-lint` (repo) | no error |
| G-WEB-UNIT | unit 1.1 | `TZ=UTC pnpm exec vitest run <paths>` (`web/`) | every listed `UI-…` ID discovered and passed |
| G-WEB-FULL | baseline | `make web-typecheck web-lint web-coverage-gate web-budget` (repo) | coverage gate and bundle budget green |
| G-API | first unit that edits `api/openapi.yaml` (phase 2) | `pnpm run gen:api` (`web/`), then `make api-docs` (repo) | `web/src/api/generated.ts` and `docs/api.md` regenerated and staged; second run leaves no diff |
| G-I18N | baseline | `make i18n-check` (repo) | locale parity, same placeholders (I-5) |
| G-FINAL | PLAN | G-GO-FULL, G-GO-INT-ALL, G-RO, G-API, G-WEB-FULL, G-I18N on the final tree, plus the recorded reference scenario of unit 15.2 | all green; nine scenario steps passed or deviation accepted by agent-1 |

## 4. Unsettled ledger
none

## 5. Owned files
none

## 6. Checkpoint
none

## 7. Open-phase evidence
none

## 8. Revisions and decisions still needed
none

## 9. Blockers
none

## 10. Do-not-repeat
none

## 11. Board
| Phase | Status | Attempt | Sub-phases | Closure ref |
|-------|--------|---------|------------|-------------|
| 0 Baseline and test images | DONE | 1 | 0.1–0.2 | unit:P0:1 |
| 1 Multixact wraparound rule | DONE | 1 | 1.1–1.3 | unit:P1:1 |
| 2 ASH dimensions | DONE | 2 | 2.1–2.5 | unit:P2:2 |
| 3 Notification channels | DONE | 1 | 3.1–3.4 | 04e7077 |
| 4 Period A versus B | DONE | 1 | 4.1–4.3 | a58d37a |
| 5 Rollups | DONE | 1 | 5.1–5.4 | d3241a3 |
| 6 Log ingest | DONE | 1 | 6.1–6.6 | 3035c8f |
| 7 Automatic plans | DONE | 1 | 7.1–7.4 | 3a6d840 |
| 8 Autovacuum history | DONE | 1 | 8.1–8.3 | a3783cc |
| 9 Plan insights | DONE | 1 | 9.1–9.3 | d8fc7aa |
| 10 Validated index suggestions | DONE | 1 | 10.1–10.4 | cd94f39 |
| 11 Query to service link | DONE | 1 | 11.1–11.4 | e841290 |
| 12 Per-process OS metrics | DONE | 1 | 12.1–12.4 | 1ef70b2 |
| 13 Tuning workbook | DONE | 1 | 13.1–13.4 | e97e8e7 |
| 14 MCP server | DONE | 1 | 14.1–14.4 | 4242004 |
| 15 Integration and documents | DONE | 1 | 15.1–15.3, 15.F1 | c91aecf |
| PLAN | DONE | 1 | — | 25293e6 |

Corrections (V001):
| Correction | Status | Attempt | Closes | Closure ref |
|------------|--------|---------|--------|-------------|
| C1 | DONE | 1 | V001-F1 | unit:C1:1 |
| C2 | DONE | 1 | V001-F2 | unit:C2:1 |

Statuses: TODO, IN_PROGRESS, IN_REVIEW, DONE, SKIPPED (reason kept), BLOCKED.

## 12. Suspended implementation
none
