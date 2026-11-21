# ObservabilityParity — State

Format: pev-state 2
Plan 007 rev 1 · repo /mnt/fabio/dati/Git/SperimentazioniAI/postgres-analyze · baseline b728cb73793b479d4511e57fab1c82f7a71a34af · updated 2026-11-21 09:43 UTC by agent:opus (claude-opus-5-5)
§0 Protocol: without this skill read PROTOCOL.md once. History: history.md (append-only, never read to resume).

## 1. Now
- Run: execute 007 · result RUNNING · style handoff (single session; agent:opus implements and self-reviews) · acting as agent:opus (claude-opus-5-5)
- Settings: autonomy true (explicit (prompt) `full-autonomous:true`) · WIP on (explicit (prompt) `--wip-commit`) · deadline none
- Stop latch: none
- Scope: plan 007 · RUNNING · overrides autonomy true, WIP on, roster agent:opus (explicit)
- Unit: none
- Base: HEAD 6f596c64e4180eddbb7ebc7cc1b1f0504246b3ba + tree 7a600d9705836bf5753bd4337ac702af8bb2eb63 · branch main · last WIP tree none
- Last closure: unit:4.3:1
- Next: open P4 · next eligible: P4
- Owner: agent:opus (claude-opus-5-5) active since 2026-11-21 07:10 UTC · takeover none (previous owner released)

## 2. Readiness
READY rev 1 — agent-1 checked: 16 phase files, 60 sub-phases with the seven fields each; every invariant of the overview has a named guarding test in a phase file; research R1–R12 confirmed or accepted with a guard (R9: `pg_hint_plan` build, guard in units 0.2 and 13.2). Open questions / required unverified claims: none.

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
- 4.3 owned also: web/src/hooks/useComparePeriods.ts web/src/components/layout/ComparePeriodPicker.tsx web/src/lib/captionsets/queries.ts web/src/lib/captionsets/activity.ts web/src/i18n/domain/it/captions/queries.ts web/src/i18n/domain/it/captions/activity.ts

## 8. Revisions and decisions still needed
- A1 (autonomous, 2026-11-21): the index holds staged deletions not owned by this plan (`web/src/features/indexes/SuggestionsPanel{,.test}.tsx`). Options: stop as blocker; unstage them; commit through a temporary index. Chosen: every plan-007 commit is built in a temporary index seeded from HEAD with only the unit's owned patch (base tree..closure tree), then the real index is reset for those paths only; the foreign staged entries stay untouched. Shared dirty files: each owned file is rebuilt as HEAD plus the unit's base→closure hunks projected line-wise (helper `pevc.py`); a hunk touching foreign lines stops the commit as a conflict. Risk: none to foreign work; reverse by `git reset --soft` of the plan commits. No hooks exist, so hooks are not bypassed (`GIT_INDEX_FILE=… git commit` runs them anyway).
- A2 (autonomous): plan files (untracked, written by the planning session for this request) enter the 0.1 completion commit.
- Baseline for regression comparison (0.1): G-GO-FULL 35 packages ok, 0 FAIL, 1 SKIP; vitest 227 files / 2027 tests; G-I18N and G-RO pass. B1: `make web-lint` fails at `web-gen-api` (`git diff --exit-code -- web/src/api/generated.ts`) only because that file holds uncommitted plan-006 hunks; gen output is idempotent.
- A4 (autonomous, 2026-11-21 07:30 UTC): plan-007 units edit files that the uncommitted plan 006 created (untracked locale catalogs, help topics, `findingCheck.ts`, …), so no unit commit could hold them without capturing plan-006 content (§6 safety stop). Options: stop and ask; leave those edits uncommitted; commit the plan-006 tree separately first. Plan 006 is complete and live (dev-20261121b), no other session writes, and the plan recommended this commit (D20). Chosen: separate commit bf38f97 `chore(006): record plan 006 I18n work …` = HEAD + baseline snapshot 1d7158a without plan 007, without `web/src/**/node_modules/`, `internal/webui/dist/index.html`, `BUGFIX_ENHANCEMENTS_20_11_2026.md` (528 paths, includes the previously staged SuggestionsPanel deletions). Local only, no push; reverse with an interactive history edit dropping bf38f97 (content stays identical in the working tree). A1 is moot from here: the index is clean.
- A3 (autonomous): G-WEB-FULL substitution for B1. Retired at P1 (2026-11-21): with plan 006 committed (A4) `make web-lint` is green again, so G-WEB-FULL runs as registered.
- A5 (autonomous, 2026-11-21, unit 2.5): the console had no group-by control, only the wait drill path. Options: add the dimensions as drill levels; one Group by control with the wait drill path as one choice plus the three dimensions. Chosen: the control; selecting a dimension value sets its filter (chip, URL `usename=`/`client_addr=`/`application_name=`) and returns to wait event types, so the drill continues on those sessions. Dimension groups always stack (wait-group cards do not apply).
- A6 (autonomous, 2026-11-21, unit 3.1): R2 asks for "the same localized texts as Google Chat", which has no localized fact titles. Chosen: with a detail the Teams title and facts are the detail's (already in the channel's language), its sections and footer the monospace block; without one the title is the upper-case severity and the facts are the English Cluster/Instance/Database/Severity/Time, as the chat channels' fallback. Empty facts and an empty FactSet are left out.
- A7 (autonomous, 2026-11-21, unit 3.1): the database refused the new kinds (check constraint of 0028/0031) and the plan had no migration in phase 3. Chosen: migration 0034_notification_kinds widens both checks to the six kinds at once (test NCH-MIG-001). Existing tests that list every kind gain the new rows; tests that used `teams` or `email` as an unknown kind now use `sms`; no other assertion changed.
- A8 (autonomous, 2026-11-21, unit 3.3): the email kind needs the SMTP settings, but the kind hooks took only the address. Chosen: `AlertConfig.SMTP` (`SMTPConfig` = `notify.SMTPSettings`) filled by `LoadAlertConfig` through `LoadSMTPConfig(getenv, readFile)`, which also reads `PGLENS_SMTP_PASSWORD_FILE`; `build` and `available` take the config, and `NewNotificationChannel(cfg, kind, address)` gains it. main needs no change. Body = the plain text of the Slack message (summary + context line, or detail title, fields, sections, footer). The email channel removes its recipients and password from its own errors. Listings gain an email row; tests that used `email` as an unknown kind now use `sms`.
- A9 (3.4, autonomous): webhook kinds keep the field label "Console URL" (existing contract, e2e selector); PagerDuty shows "Routing key", email "Recipients". The panel checks the address while typing and disables Save while invalid; the buttons keep "Set URL"/"Replace URL" for every kind. The 409 smtp_not_configured carries detail "set PGLENS_SMTP_HOST and PGLENS_SMTP_FROM on the server" (OpenAPI Error requires detail), via kind field unavailableDetail. deploy/server.example.env gains the Teams, PagerDuty and SMTP examples.
- A10 (autonomous, 2026-11-21, unit 4.1): `handleStatements` groups by (cluster_id, queryid, datname); the compare CTEs group by (queryid, datname) with MIN(cluster_id) for the text lookup, so a FULL OUTER JOIN on (datname, queryid) gives one row per statement even if the instance changed cluster. Sums and the per-row seconds expression are copied unchanged. The 404 for an unknown instance is a separate `SELECT 1 FROM instances` (handleStatements has no 404); the compare itself is one statement with the 4 period bounds plus the 2 period lengths and limit as bind parameters. Ratio of a `gone` row is 0 (B counted as 0), null only when A is 0 or missing. All eight metrics appear in a, b, delta and ratio (superset of the example).
- A11 (autonomous, 2026-11-21, unit 4.2): the inline group_by whitelist of `handleASH` became the package var `ashGroupColumns`, shared by `/api/v1/ash/compare` (no copy; behaviour of `/api/v1/ash` unchanged). `group_by` on compare takes exactly one column (a comma list is a 400), the row `key` is the value as text, '' when unknown. Each period's samples and ticks come from `ashWhere` built four times, placeholders renumbered into one statement; filters narrow samples, not ticks, as `/ash/top`. The response carries `group_by` instead of `order_by` and no `normalized` field (AAS is already a rate).
- A12 (4.3): compare mode is URL `mode=compare` on Query Inspector and Wait events (AshPage already uses `view`), switched by a shared `ModeToggle` radiogroup in the PageHeader actions; A preset in `compare` (`previous` default, unwritten), custom A in `a_from`/`a_to` (datetime-local in the console zone). B = page range floored to the minute (stable key); hooks `useStatementsCompare`/`useAshCompare` use `REFRESH.static` (no polling). `formatRatio(r, status?)` takes the row status to tell new/gone from a missing ratio; the views translate `new`/`gone`. Warning style = `Badge variant="warning"` on the ratio cell (DataTable has no row-class hook; not widened). Queries measure follows the list's `sort`; Activity group follows the page `group` plus its filters. Added captions `queries.compare-table`/`ash.compare-table` (en+it), `queryPath` moved to `lib/statements.ts`, hook `hooks/useComparePeriods.ts` and `ComparePeriodPicker` added to the unit's files.

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
| 3 Notification channels | DONE | 1 | 3.1–3.4 | unit:P3:1 |
| 4 Period A versus B | IN_PROGRESS | 1 | 4.1–4.3 | — |
| · 4.1 statements/compare | DONE | 1 | — | unit:4.1:1 |
| · 4.2 ash/compare | DONE | 1 | — | unit:4.2:1 |
| · 4.3 console compare views | DONE | 1 | — | unit:4.3:1 |
| · P4 closure | TODO | 1 | — | — |
| 5 Rollups | TODO | 1 | 5.1–5.4 | — |
| 6 Log ingest | TODO | 1 | 6.1–6.6 | — |
| 7 Automatic plans | TODO | 1 | 7.1–7.4 | — |
| 8 Autovacuum history | TODO | 1 | 8.1–8.3 | — |
| 9 Plan insights | TODO | 1 | 9.1–9.3 | — |
| 10 Validated index suggestions | TODO | 1 | 10.1–10.4 | — |
| 11 Query to service link | TODO | 1 | 11.1–11.4 | — |
| 12 Per-process OS metrics | TODO | 1 | 12.1–12.4 | — |
| 13 Tuning workbook | TODO | 1 | 13.1–13.4 | — |
| 14 MCP server | TODO | 1 | 14.1–14.4 | — |
| 15 Integration and documents | TODO | 1 | 15.1–15.3 | — |
| PLAN | TODO | 1 | — | — |

Statuses: TODO, IN_PROGRESS, IN_REVIEW, DONE, SKIPPED (reason kept), BLOCKED.

## 12. Suspended implementation
none
