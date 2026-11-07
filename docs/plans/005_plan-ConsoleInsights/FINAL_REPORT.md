# Plan 005 ConsoleInsights — final report

Branch `feat/index-analysis`, plan baseline `5929be2`. Executed by Claude Code (agent-1 Opus 5.5, acting for agent-2 where recorded); reviews by fresh read-only Opus agents. Live status: [STATE.md](STATE.md); settled history: [history.md](history.md).

Scope result: **COMPLETE** — every required gate passed on the final tree (2026-11-07); the deviations below need the user's confirmation.

## Completed

Phases 0–7 and 8 (units 8.1, 8.2) are DONE (STATE §11). Every requirement row REQ-01…REQ-44 is DONE or DONE with a recorded deviation, each with evidence (STATE §11 requirement trace; QA-TRACE-001 resolves each row in test sources).

Highlights:
- Index analysis: four tabs (Indexes, Cleanup, Dead rows, Suggestions), suggestions from the busiest statements with read-benefit upper bound, size estimate and write overhead.
- Instance overview: grouped time-axis charts, host charts, monitoring readiness view, plain-language captions with ⓘ tooltips on every card, unambiguous summary cards.
- Tables: search, filters, "Showing X of Y", 20/50/100 pages, sort buttons, URL state, on every data table.
- Settings as a tabbed control surface (Inventory, Preferences, Data retention, Advisor rules, Notifications, Names, Audit & server).
- Full HD fit at 1920×1080 and 1920×950 for every table page.
- Odoo PostgreSQL 18 migrated in place to an HA lab (3 nodes + `hactl` controller with automatic failover and a read-write endpoint); drills D1–D5 passed; gaps GAP-1…GAP-9 fixed and re-drilled (`ha-findings.md`).
- Monitoring role raised to Tier 2 on all three nodes (8.1).

## Final gates (8.2, on the final tree)

| Gate | Result | Evidence |
|------|--------|----------|
| G-GO-FULL | pass, 33 packages | STATE §7 8.2 S1 |
| G-GO-INT-ALL | pass, including the three PostGIS tests (`postgis/postgis:18-3.6-alpine`) | STATE §7 8.2 S1 |
| G-GO-INT-LONG | TestINTFIRSTRUN001 pg15 + pg18 pass (603 s) | STATE §7 8.2 S1 |
| G-RO | TestINTRO001 pg15 + pg18 pass at T2 | STATE §7 8.1 |
| G-WEB-FULL | typecheck, lint, vitest 200 files / 1874 tests, coverage gate, budget pass (rerun 06:50–07:09 UTC after the activity breakdown fix, plain `make web-coverage-gate web-budget`; budget again after the docs pass) | STATE §7 8.2 |
| G-DEPLOY | pass: rebuilt from the final tree after the activity breakdown fix and the docs pass (`deploy.sh`, last 2026-11-07 07:39 UTC) + COV-E2E-001 pass right after it | STATE §7 8.2, §8 "8.2 G-QA activity breakdown" |
| G-UI-LEGACY | SYS-UI 12/12 pass on the final tree (07:27–07:39 UTC, 689.8 s, `go test -list UI` 12 tests, result `ok`): UnauthenticatedNavigation, StandaloneLag, Smoke, PlanOnly, FindingMute, Failover, DisabledASH, Charts, Cancel, BlockingTree, AgentOutage, Accessibility (first 8.2 run 706 s before the fix) | STATE §7 8.2 S4 |
| G-VIS | 49 specs: 44 pass first run, 5 fail → correction 7.5.8 attempt 2 (`ce50126`), all 5 pass on rerun; NOTI-E2E-001/002 and VIS-040 pass. Later changes touched the Locks activity breakdown and the `signedInPage` fixture (re-sign-in on 401 only): VIS-LOCKS-001 and COV-E2E-001 rerun and pass, the rest of G-VIS was not repeated | STATE §7 8.2 S2, §8 |
| G-VIS VIS-031 tps 24h | pass 2026-11-07 06:42 UTC, threshold 0.95 kept | `scratchpad/82_vis031.log` |
| G-QA | run 1 16/28 (6 HA spec defects, 6 not run) → fixes; run 2 25/28, all lifecycle drills pass; targeted reruns IND-ASH, RDY-E2E-005, IND-CONN + IND-CONN-APP pass; QA-TRACE 6/6, QA-CLEAN-001 pass; one product defect found and fixed (activity breakdown kept a closed application up to 30 s) | STATE §7 8.2, §8 rows "8.2 G-QA …" |
| G-HA | HA-STATIC 5/5 pass (1.2 min), leader odoo-db | STATE §7 8.2 |
| G-STACK | 8 services running/healthy, `/healthz` 200, hactl leader odoo-db with pg-ha-2/3 standby (07:50 UTC); backup `04-final-20261107T020829Z` VERIFIED | STATE §7 8.2 S4 |
| G-SECRETS | pass: (a) 0 on every staged diff; in the tree only synthetic canaries (`CANARYSPACE0004`, `STOREDSPACE0101`, `UICANARYSPACE`, `spaces/X…key=K` in tests and the `phase_01.md` pattern note), no real key; (b) the real webhook value: 0 in repo files, git history, e2e artefacts, scratchpad logs and memory | STATE §7 8.2 S4 |

## Reference scenario (overview items 1–17)

| # | Item | Evidence |
|---|------|----------|
| 1 | Seeded Odoo 18 / PG 18, restorable | 6.3 counts (res_partner 30006, product_template 3010, sale_order 4000, mail_message 90061, 169 installed modules); backup `04-final-20261107T020829Z` restored into a throwaway `postgres:18` by `verify_backup.sh` (ENV-001..003) |
| 2 | Readable time axes, continuous TPS, grouped charts | VIS-010, VIS-031 tps 6h (6.2, G-VIS); tps 24h pass (8.2) |
| 3 | Host charts, filesystem from `data_directory` | IND-HOST (G-QA), G-VIS |
| 4 | Index analysis tabs, ground-truth suggestion | VIS-040 after `suggest_ground_truth.py` (column `email`, 1089 executions) in G-VIS |
| 5 | In-place HA migration, switchover, hard failover, gaps fixed | P7: HA-DRILL D1–D5, LIVE-HA, HA-STATIC (11 pass); GAP-1…9 FIXED; 8.2 HA-STATIC 5/5 |
| 6 | Tier 2 at the end | 8.1: T2 on three nodes, T2-001, G-RO at T2; `allow_signal` deviation below |
| 7 | Table controls | TBL-E2E-*, TBL-UI-* (vitest), G-VIS |
| 8 | Rename cluster/instance | CLNAME-E2E (G-VIS), HA-STATIC rename through the console |
| 9 | Tabbed Settings | SET-E2E-001, PREF-E2E, RET-E2E, ADV-E2E, NOTI-E2E-001/002 (G-VIS); `settings-inventory.md` |
| 10 | Monitoring readiness | VIS-RDY (G-VIS), RDY-E2E-* (G-QA) |
| 11 | Full HD fit | FHD-FIT-001, FHD-MEASURE-001, FHD-INST-001, HDR-E2E-001, QA-TOUR-001 (G-VIS reruns) |
| 12 | ASH one chart per wait group | IND-ASH (G-QA), G-VIS |
| 13 | Session origin split | VIS-LOCKS-001 (G-VIS), SORIGIN-E2E-001 (G-QA) |
| 14 | Full session info, per-application counts on | LIFE-ODOO-RESTART, IND-CONN-APP, sessions specs (G-QA), D28 unit tests; breakdown newest-scrape fix (`03c9e6f`) |
| 15 | Live Playwright three-way checks | G-QA (`@indicators`, `@lifecycle`), `test-strategy.md` |
| 16 | Captions and ⓘ tooltips | VIS-CAP-001..005 (G-VIS), CAPTION-* unit tests |
| 17 | Unambiguous summary cards | VIS-CAP-002 (G-VIS), `TILE_UNITS` unit tests |

## Skipped / blocked / unverified

- LIFE-DOWN-UP-FULL is not repeated on the HA topology (ran once at 6.4 on the single node; plan decision).
- Not drilled live (O-8): no-candidate failover, `/fault/isolate` partition, hactl crash (covered by hactl unit and integration tests).
- Agent "Up" 60 s vs `agent_down` 90 s mismatch (17.3 finding) is NOT live-checked.
- No other gate is skipped or unverified.

## Decisions and deviations needing the user's confirmation

- REQ-20: `allow_signal` stays `true` on the three lab targets (set at 7.5.7 for GAP-8/V-11), so the console offers Cancel/Terminate at T2. Revert: restore `ha/agent.ha.yaml.bak-757` and restart `pglens-agent`; for T1, `REVOKE pg_signal_backend FROM pglens_monitor;`.
- A1: REQ-33 "full metrics within 2 hours" interpreted as D22 (early first runs, statistics-age windows, completeness view); historical backfill is not possible.
- A2: Full HD = browser content area 1920×1080 (F11) or maximised window ≈ 1920×950.
- A3: raw query text of active sessions is opt-in (`checks.locks.session_query_text`, default false); the lab enables it.
- Q2 (resolved by the user 2026-10-30): webhook URLs stored write-only in pglens, not encrypted at rest in v1; environment variables remain a fallback.
- Historical (8.2 S1 only): that coverage run used `--testTimeout=15000` (two route-mount tests hit 5 s under external sandbox load, ~7 on 8 cores); the final coverage run used the plain timeout and passed.

## Disclosures

- `tgsend` (Telegram notifications) is installed (`/home/agent/.bin/tgsend`); earlier runs wrongly disclosed it as missing (corrected at the 8.2 S6 review), so no interim notifications were sent; one notification is sent at the plan closure.
- The Go module `github.com/pashagolub/pgxmock/v5 v5.2.0` was downloaded through the proxy during integration runs.
- Image `postgis/postgis:18-3.6-alpine` was pulled under the user's network authorisation for G-GO-INT-ALL; digest-pinned base images of `Dockerfile.agent`/`Dockerfile.server` may have been pulled by the stack builds (authorised).
- A Patroni/HAProxy production topology would need network authorisation; the lab uses local images and the in-repo `hactl` controller (D9 revised).
- Excluded settings and reasons: `settings-inventory.md`.
- The Google Chat test webhook lives only in `/home/agent/pglens-odoo-stack/.env` (chmod 600; backup `.env.bak-webhook`); the user said it is a test channel and will delete it. It appears in no repo file, log or commit.
- Baseline (D32): the pre-existing uncommitted work was committed once as `ce13a72` (after the plan folder `d7647d0`).
- RDY-E2E-005 allows HTTP 401 on `/api/v1/` in its own browser guard: its server restart ends the in-memory UI session by design; every other spec keeps the strict guard.
- `internal/webui/dist/index.html` is a regenerated build artefact: modified, never staged, never discarded.
- Local commits only; **nothing was pushed**.

## Product gaps found, out of scope (for a later plan)

- The console does not read `allow_signal`: a closed gate shows only as the agent's refusal.
- 17.2: `store.Rules()` leaves `Rule.Tier` unset; Findings scope filter offers only cluster and instance; the events endpoint returns only `failover_detected`; the ASH Disabled callout has no readiness item to link to; `originSentence` is used only by its test; rule id `conn.near_max` in the plan is really `conn.saturation`; the Alerts Pending filter is always empty (the engine never stores pending alerts).
- 17.3: Agent "Up" threshold 60 s vs 90 s for `agent_down`/`store.InstanceStaleAfter`; ANALYZE offered at T2 while Go requires T1 plus `allow_explain_analyze`; Server info "Replication" row outdated; `top_n` budget is per database, not per instance; Slowest queries empty while Query inspector lists statements (cause not confirmed); Findings scope "relation" also covers sequences, partitions, triggers and functions.
- Alerts and Events cluster filters are still free-text "Cluster ID" inputs (R-P10).

## Next actions for the user

1. Confirm or reject A1, A2, A3 and the REQ-20 `allow_signal` deviation.
2. Delete the Google Chat test webhook (channel and `.env` entry) when done.
3. Review the local commits and push when ready.
4. Schedule the product gaps above.

## Commits

Local commits `5929be2..HEAD` (138 up to the 8.2 G-QA commit `03c9e6f`, plus the 8.2, P8 and PLAN closures): `git log --oneline 5929be2..HEAD`. Plan `d7647d0`, baseline `ce13a72` (D32). Phase closures include `00248fe` P16, `e86d1bf` P4, `3504309` P5, `e9459be` P15, `89d711e` P17, `14f671a` P6, `dda99e9` P7; correction `ce50126` (7.5.8 attempt 2) and `03c9e6f` (8.2 G-QA fixes and the activity breakdown fix).
