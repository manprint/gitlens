# Plan 007 — history

## 2026-11-21 execute start
Invocation `/plan-execute-verify execute 007 full-autonomous:true --wip-commit agent:opus`: scope plan 007, autonomy true and WIP on (explicit), roster override agent:opus (claude-opus-5-5) for all positions, reviews labelled self-review. Autonomous decisions A1 (temporary-index commits around foreign staged deletions) and A2 (plan files enter the 0.1 commit) recorded in STATE.md §8.

## 2026-11-21 0.1/1 complete unit:0.1:1
Gates: G-GO-FULL pass (35 pkgs, 2773 PASS, 0 FAIL, 1 SKIP); G-WEB-FULL pass with B1 (gen-api git-diff check blocked by uncommitted plan-006 generated.ts; gen idempotent; vitest 2027/227; budget PASS); G-I18N pass (48 vitest); G-RO pass; review: agent:opus self-review accepted; changes: STATE.md, history.md (plan files added); notes: A1–A3.

## 2026-11-21 0.2/1 complete unit:0.2:1
Gates: S4 check pass (hypopg.so, pg_hint_plan.so; hypopg 1.4.5 created, pg_hint_plan loaded); review: self-review (agent:opus) accepted; changes: test/pgext/{Dockerfile,build.sh,README.md}, Makefile (pgext-image), .gitignore (test/pgext/src/); notes: build cache pruned after build.

## 2026-11-21 P0/1 complete unit:P0:1
Gates: baseline recorded (0.1), image check (0.2) pass; review: self-review (agent:opus) — baseline counts and B1 assessed as pre-existing environmental, image verified live; changes: STATE.md, history.md; notes: phase 0 collapsed.

## 2026-11-21 1.1/1 complete unit:1.1:1
Gates: G-GO-FOCUS pass (MXW-001, MXW-002); G-GO-INT pass (INT-MXW-001 pg15+pg18); G-RO pass; review: self-review (agent:opus) accepted; changes: internal/check/activity.go, activity_test.go, activity_integration_test.go, xmin_horizon_test.go (mock matches datminmxid); notes: second query (not a combined column), same error path as datfrozenxid

## 2026-11-21 decision A4
Plan-006 working tree committed separately as bf38f97 (528 paths) because plan-007 units edit files plan 006 created and never committed; see STATE §8 A4.

## 2026-11-21 1.2/1 complete unit:1.2:1
Gates: G-GO-FOCUS pass (MXW-003, MXW-004, MXW-005, TestCatalogParity, seeded-rule test); package tests advisor/server/i18n/store/alert ok; G-GO-INT pass (INT-MXW-002); review: self-review (agent:opus) accepted; changes: internal/advisor/rules_query.go(+test), internal/i18n/locales/{advisor_query,en,it}*.json, internal/server/api_locks.go, internal/store/migrations/0032_multixact_rule.sql, internal/store/migrate{,_integration}_test.go; notes: migration 0032 (D19); notification texts rule.txn.multixact_wraparound_risk.* added (describer title/problem/actions); describe_evidence.go unchanged: its frozen-table evidence orders by relfrozenxid, metrics_tables has no relminmxid; api_locks.go lists the new metric for the activity view

## 2026-11-21 1.3/1 complete unit:1.3:1
Gates: G-WEB-STATIC pass; G-WEB-UNIT 249 pass (UI-MXW-001/002/003 discovered); G-GO-INT TestINTMXW003 pg15+pg18 pass; review: self-review (agent:opus) accepted; changes: ruleText/it rules, findingCheck multixact case, ruleThreshold, help en/it alert-rules+findings, README rules row, TestINTMXW003; notes: self-review agent:opus: SQL read-only, catalog skeleton en/it equal; B1 no longer reproduces (make web-lint green)

## 2026-11-21 P1/1 complete unit:P1:1
Gates: G-GO-FULL 35 pkgs ok; G-GO-INT TestINTMXW001/002/003 pass (pg15+pg18); G-RO pass; G-WEB-FULL pass (registered command, coverage floors + budget, entry 246.31 KiB); G-I18N pass (48); review: self-review (agent:opus) accepted; changes: none (closure); notes: self-review agent:opus: I-1 (multixact query read-only, pg_database only), I-5 (en/it keys+placeholders, TestMXW004, I18N parity); rule catalogue assertion = advisor registry + TestMXW003/004 (no separate /api/v1/findings catalogue endpoint exists); README row added in 1.3; no regression vs baseline

## 2026-11-21 2.1/1 complete unit:2.1:1
Gates: G-GO-FOCUS ./internal/ash/ TestASHD001/002/003 pass; ./internal/ash ./cmd/pglens-agent -race pass; G-GO-INT TestINTASHD001 + TestINTASH001-008 pg15+pg18 pass; G-RO pass; go build ./... ok; review: self-review (agent:opus) accepted; changes: SampleRow/Key + Usename, ClientAddr, ApplicationName; query adds usename, host(client_addr), application_name; cut64; Add(r SampleRow); keyLess tie-break; caller passes agg.Add; watch_test mock tolerates extra columns; notes: self-review agent:opus: only callers in cmd/pglens-agent and internal/ash; overflow otherKey keeps '' dimensions (I-2 TestASHD003)

## 2026-11-21 2.2/1 complete unit:2.2:1
Gates: G-GO-FOCUS ./cmd/pglens-agent ./internal/agent TestASHD004-007 pass; both packages -race pass; make lint 0 issues; review: self-review (agent:opus) accepted; changes: CheckConfig.Dimensions + ASHDimensions(); validation (unknown name, non-ash key); ashRowFilter blanks off dimensions before Add; ashWindowToMetrics adds non-empty labels; README + deploy/agent.example.yaml; notes: self-review agent:opus: unknown-dimension error text exact (no validation: prefix, per contract)

## 2026-11-21 2.3/1 complete unit:2.3:1
Gates: G-GO-FOCUS ./internal/server TestASHD008 pass; server+store -race pass; G-GO-INT TestINTASHD002/003 pass (TimescaleDB 2.31); review: self-review (agent:opus) accepted; changes: migration 0033_ash_dimensions.sql (as designed); ASHRow + both writers (batch, temp-table copy) carry usename, client_addr, application_name; buildASHRow reads labels, missing = ''; notes: self-review agent:opus: migration reviewed before run; R7 exercised live (0033 applied over a compressed chunk, TestINTASHD003); ON CONFLICT DO NOTHING kept

## 2026-11-21 2.4/1 complete unit:2.4:1
Gates: G-GO-FOCUS (ASH-D-009..011, server/ash/store/cmd), G-GO-INT TestINTASHD004 + TestIntASH0*, G-API (gen:api, api-docs), web typecheck; review: self-review (agent:opus) accepted; changes: api_ash.go: group_by usename/client_addr/application_name, filters as bind params (- = empty), echo on grouped rows, fold keeps dims; openapi enum + 3 params + AshBucket fields; generated.ts; notes: doc comments reworded from '' to empty (gofmt rewrites '' into a curly quote); raw ungrouped rows keep wait-event dimensions only, documented

## 2026-11-21 2.5/1 complete unit:2.5:1
Gates: G-WEB-STATIC (make web-typecheck web-lint), G-WEB-UNIT (features/ash, lib, api, i18n: 728 tests), UI-ASHD-001..004; review: self-review (agent:opus) accepted; changes: AshPage Group by control + filter chips; AshBreakdown dimension groups ((unknown) <-> -); lib/ash dimension helpers; query keys carry filters; captions ash.breakdown-dimension/ash.stack-dimension en+it; help wait-events#session-dimensions en+it; README; notes: A5 recorded

## 2026-11-21 2.5/2 complete unit:2.5:2
Gates: G-WEB-UNIT testIds + ash 8 files 64 tests; prettier; review: self-review (agent:opus) accepted; changes: two test titles lose their UI-ASHD ID so each ID owns one title; notes: attempt 2 after P2 G-WEB-FULL failure

## 2026-11-21 P2/2 complete unit:P2:2
Gates: G-GO-FULL 35 ok; G-GO-INT TestINTASHD001-004 (001 on PG15 and PG18) and old ASH integration tests; G-RO; G-API no diff; G-I18N 48; G-WEB-FULL typecheck/lint/budget (attempt 1) and coverage gate 227 files/2038 tests (attempt 2); review: self-review (agent:opus) accepted; changes: none (closure); notes: attempt 1 failed on duplicate UI-ASHD test IDs, fixed by 2.5 attempt 2; review: I-1 the sampler only adds SELECT columns and G-RO is green; I-2 TestASHD003 keeps the sample total under the key cap; I-5 locale parity green; old ASH tests only had mechanical row() call rewrites, assertions unchanged; README covers 2.2 and 2.5

## 2026-11-21 3.1/1 complete unit:3.1:1
Gates: G-GO-FOCUS notify/server/cmd (race, shuffle); G-GO-INT TestINTNCH001 + NOTIINT; store migration tests incl. NCH-MIG-001; lint 0 issues; review: self-review (agent:opus) accepted; changes: notify.NewTeams (R2 Adaptive Card), Teams hosts redacted; notificationKind validate/host/available hooks with problem/shownHost/logHost/unavailable; teams kind, PGLENS_TEAMS_WEBHOOK_URL(_FILE), buildChannels; migration 0034; notes: A6, A7

## 2026-11-21 3.2/1 complete unit:3.2:1
Gates: G-GO-FOCUS notify/server/cmd (race, shuffle); lint 0 issues; review: self-review (agent:opus) accepted; changes: notify.NewPagerDuty (R1 Events API v2, endpoint package variable, key redacted from errors), ValidatePagerDutyKey; kind pagerduty with validate and host; PGLENS_PAGERDUTY_ROUTING_KEY(_FILE), invalid key fails start; listings gain the pagerduty row; notes: resolve is Phase "resolve" (notifier); a test message triggers without dedup_key

## 2026-11-21 3.3/1 complete unit:3.3:1
Gates: G-GO-FOCUS (notify, alert, server, cmd/pglens-server; race, shuffle) green; lint 0; G-GO-INT TestINTNCH00[12] + NOTIINT green; review: self-review (agent:opus) accepted; changes: notify email.go (SMTPSettings, NewEmail, buildEmail, net/smtp transport with injectable dial); server SMTPConfig + LoadSMTPConfig; kind email (validate 1-20 recipients, host N recipients, available smtp_not_configured); NewNotificationChannel takes cfg; notes: A8; tests NCH020-024, INTNCH002

## 2026-11-21 3.4/1 complete unit:3.4:1
Gates: G-GO-FOCUS server/cmd/alert race+shuffle pass; golangci-lint 0; G-API regen; tsc/eslint/prettier clean; vitest 228 files/2043 tests rc=0; G-I18N 48 pass; review: self-review (agent:opus) accepted; changes: OpenAPI six kinds + 409 smtp_not_configured; console registry/labels/live validation for Teams, PagerDuty, Email; help en/it; README channels table + env rows; example env; notes: A9 recorded

## 2026-11-21 P3/1 complete unit:P3:1
Gates: G-GO-FULL 35 pkgs ok, lint 0; G-GO-INT TestINTNCH001/002 + NOTIINT001/002 pass, integration lint 0; G-API no diff; G-I18N 48; G-WEB-FULL typecheck/lint/coverage 228 files/2043 tests/budget rc=0; go.mod unchanged; review: self-review (agent:opus) accepted; changes: none (closure); notes: self-review agent:opus accepted: I-5 en/it catalogs + help parity (TestCatalogParity, I18N tests); I-7 secrets never in responses/logs (NCH-005/014/024 canaries, email redact of recipients+password, host-only listing); header injection: headerValue strips CR/LF, NCH-021, recipients parsed by net/mail and CRLF refused (NCH-024); docs: README six-channel table + env rows, example env, help en/it

## 2026-11-21 4.1/1 complete unit:4.1:1
Gates: G-GO-FOCUS TestCMP001-003 race+shuffle pass; server+cmd packages pass; G-GO-INT TestINTCMP001 pass; golangci-lint 0 (plain + integration); G-API regenerated; review: self-review (agent:opus) accepted; changes: GET /api/v1/statements/compare: parseComparePeriods (D7 checks, field-named 400), per-period CTE from handleStatements SQL, FULL OUTER JOIN, per-second normalisation, delta/ratio/status, order by abs delta then queryid; OpenAPI StatementCompare; notes: self-review agent:opus: SQL sums identical to handleStatements; A10

## 2026-11-21 4.2/1 complete unit:4.2:1
Gates: G-GO-FOCUS TestCMP004-005 + server package race+shuffle pass; G-GO-INT TestINTCMP001/002 + ASH integration pass; golangci-lint 0 (plain + integration); G-API regenerated; review: self-review (agent:opus) accepted; changes: GET /api/v1/ash/compare: one group_by column from shared ashGroupColumns, per-period samples/ticks via ashWhere renumbered, AAS = samples/ticks, delta/ratio/status, order by abs delta then key; OpenAPI AshCompare; notes: self-review agent:opus: group_by interpolated only after whitelist; A11

## 2026-11-21 4.3/1 complete unit:4.3:1
Gates: G-WEB-STATIC (tsc, eslint 0 errors, prettier); G-WEB-UNIT 231 files 2052 tests; help+i18n 48; review: self-review (agent:opus) accepted; changes: compare.ts (presets, formatRatio, values), useStatementsCompare/useAshCompare, ModeToggle, ComparePeriodPicker, QueryCompare, AshCompare, mode=compare on both pages, en/it texts, captions, help sections compare-periods, README Comparing two periods; notes: A12; self-review: hooks unconditional, no polling, B floored to the minute

## 2026-11-21 P4/1 complete unit:P4:1
Gates: G-GO-FULL 35 pkgs ok, lint 0; G-GO-INT TestINTCMP001/002 pass, integration lint 0; G-API no diff; G-I18N 48; G-WEB-FULL typecheck/lint/coverage 231 files/2052 tests/budget rc=0; go.mod unchanged; review: self-review (agent:opus) accepted; changes: none (closure); notes: compare sums identical to handleStatements (grouping only, A10); windows [from,to); per-second normalisation; I-5 GET, 10 s timeout, limit <= 200, <= 31 days; docs: README "Comparing two periods", OpenAPI, help en/it compare-periods

## 2026-11-21 5.1/1 complete unit:5.1:1
Gates: G-GO-INT ./internal/store/ race (TestINTRLP001-003 + all store integration) pass; server retention integration pass; golangci-lint integration 0; review: self-review (agent:opus) accepted (SQL reviewed before run: IF NOT EXISTS, WITH NO DATA, policies if_not_exists, no refresh in the migration); changes: migration 0035_rollups.sql (metrics_statements_1h, metrics_ash_1h, metrics_1h, refresh 3 d/1 h/30 min, retention 730 d); rollups_integration_test.go; notes: A13

## 2026-11-21 5.2/1 complete unit:5.2:1
Gates: G-GO-FOCUS ./internal/server race+shuffle pass (TestRET*, TestRLP001-003); G-GO-INT TestRETINT* + TestINTRLP004 pass; golangci-lint 0 (plain + integration); G-API regenerated (docs/api.md unchanged); tsc clean; vitest settings + retention 14 files/112 tests; review: self-review (agent:opus) accepted; changes: retentionFamily MinDays/MaxDays/Rollup, three rollup families (730 d, 31..1825), preview resolves the materialization hypertable, row field rollup; OpenAPI enum/rollup/max_days; console labels; notes: A14

## 2026-11-21 5.3/1 complete unit:5.3:1
Gates: G-GO-FOCUS server ok (race, shuffle); TestINTRLP005 + TestWSERINT/TestINTRET/ASH/TestINTADV integration ok; lint (both tags) 0; G-API regenerated (docs/api.md unchanged); web tsc + vitest src/lib src/api 641 ok; review: self-review (agent:opus) accepted; changes: rollup_route.go (rollupFor, rawRetentionDays, routeToRollup, rollupSources), statements/ASH/workload read the rollup on old ranges, `resolution` field; notes: A15, A16.

## 2026-11-21 5.4/1 complete unit:5.4:1
Gates: G-WEB-STATIC tsc 0/eslint 0 errors/prettier ok; G-WEB-UNIT 232 files 2059 tests pass after test-ID fix; G-API no diff; review: self-review (agent:opus) accepted; changes: retention panel two groups (raw, hourly rollups) with per-row bounds; RollupNote (shell.rollup.note) on Queries list, Slowest, Wait events page+top, Overview workload card subtitle; ASH session groupings disabled with tooltip on 1h responses; help en/it retention + limits; README Retention and rollups + Known limits; notes: A17; UI-RLP-001..006

## 2026-11-21 P5/1 complete unit:P5:1
Gates: G-GO-FULL 35 pkgs ok 0 FAIL; G-GO-INT server+store ok (TestINTRLP00*, TestRET*, TestINTRET*, TestWSER*); G-API no diff (web-lint gen); G-WEB-FULL 232 files/2059 tests, coverage gate and budget pass; G-I18N pass; integration lint 1 pre-existing errcheck (A18); review: self-review (agent:opus) accepted; changes: none (closure); notes: self-review: migration 0035 rollups, read rule (raw within retention, 1h beyond), bounds 31-1825, README section; accepted

## 2026-11-21 6.1/1 complete unit:6.1:1
Gates: G-GO-FOCUS ./internal/pglog/ (race, shuffle) ok; lint 0 issues; review: self-review (agent:opus) accepted; changes: NEW internal/pglog entry/jsonlog/csvlog/reader + parse_test (PGL-001..005, 015); fixtures from postgres:18.6/15.19-alpine containers (27 records each); notes: self-review: Split keeps half records unread, CSV restarts after a broken record at the next line, cut at 262144; fixtures hold no host or user data

## 2026-11-21 6.2/1 complete unit:6.2:1
Gates: G-GO-FOCUS ./internal/pglog/ ok; lint 0 issues; review: self-review (agent:opus) accepted; changes: NEW classify.go (Classify, Classes, Shipped, class constants), classify_test.go (PGL-006..008); notes: self-review: SQLSTATE rules first (57014 split by message), then English message rules in spec order, then severity; Classes derived from the rule tables so the test catches a missing case

## 2026-11-21 6.3/1 complete unit:6.3:1
Gates: G-GO-FOCUS ./internal/pglog/ ok (PGL-009..014); G-GO-INT TestINTPGL001 pass on PG 18.6; lint (integration tag) 0 issues; review: self-review (agent:opus) accepted; changes: NEW internal/pglog source.go (Source, Position, Chunk, Settle, Advance), source_file.go, source_sql.go, position.go, source_test.go, source_sql_integration_test.go; NEW test/pgtest/logging.go (StartLogging); notes: design note A19 approved before code; self-review: names with / or .. refused in both sources, SQL permission error 42501 -> ErrLogReadDenied, positions via temp file + rename

## 2026-11-21 6.4/1 complete unit:6.4:1
Gates: G-GO-FOCUS ok (agent, cmd agent, wire, pglog); G-GO-INT cmd/pglens-agent ok; INT-PGL-002/003 ok; G-RO INTRO001 pg15+pg18 ok; lint 0 issues; review: self-review (agent:opus) accepted; changes: checks.logs config + validation; agent LogSettings/ReadLogSettings/LogQuerier; cmd agent log loop (logs.go, logs_build.go) wired in run.go; log_event fact kind; example config block; RO audit covers log reader; notes: A21: PG15 grant of pg_current_logfile; shared position store

## 2026-11-21 6.5/1 complete unit:6.5:1
Gates: G-GO-FOCUS server/store ok; INT-PGL-004 ok; RETINT001-003, RLP ok; store integration ok; vitest retention/settings 114 ok; typecheck ok; lint 0; review: self-review (agent:opus) accepted; changes: migration 0036 log_events; store LogEventRow/WriteLogEvents; pipeline routes log_event facts (server withhold, class check); retention family log_events 14d (API enum, generated.ts, console label); notes: A22

## 2026-11-21 6.6/1 complete unit:6.6:1
Gates: G-GO-FOCUS (readiness, server, i18n, store, pglog, alert, agent, deploy), golangci-lint 0 issues, G-GO-INT TestINTPGL001-005 PASS, G-RO PASS, G-API no diff, G-WEB-STATIC (tsc, eslint 0 errors, prettier), vitest full 2072 (2 route inventories fixed, rerun green); review: self-review (agent:opus) accepted; changes: migration 0037 log.* alert rules; describer quotes log lines; /api/v1/logs + classes; console Logs page; RDY-LOG-01 readiness rule; help chapter logs en/it; README PostgreSQL logs; notes: A23

## 2026-11-21 P6/1 complete unit:P6:1
Gates: G-GO-FULL rc=0, G-GO-INT TestINTPGL001-005 PASS, G-RO TestINTRO001 PASS, G-API no diff, G-WEB-FULL rc=0 (2072 tests, coverage gate, budget), G-I18N rc=0; review: self-review (agent:opus) accepted; changes: none (closure); notes: review: integration (INTPGL001-005), I-1 G-RO, I-4 TestPGL020, I-9 TestPGL014/015, position rules A19 (pglog source tests), default config off TestPGL016 + startLogs guard; README PostgreSQL logs and MONITORING_READINESS RDY-LOG-01 checked

## 2026-11-21 7.1/1 complete unit:7.1:1
Gates: G-GO-FOCUS pglog/agent/cmd (race, shuffle), TestAEX001-006, TestINTAEX001 + TestINTPGL002/003 PASS, G-RO PASS, golangci-lint 0 issues; review: self-review (agent:opus) accepted; changes: pglog.ParseAutoExplain + fixtures from postgres:18.6 auto_explain; agent.PlanHash; log loop ships plan facts (body memo 1 h/5000, 50 per scrape, 256 KiB body cap, drop reasons, note auto_explain_format_not_json); TestPGL017 expects the note; notes: A24

## 2026-11-21 7.2/1 complete unit:7.2:1
Gates: G-GO-FOCUS ./internal/server/ ./internal/store/ ok (AEX-007..009, RLP); G-GO-INT ./internal/server/ ./internal/store/ ok (INTAEX002/003); lint 0 issues; G-API regenerated; vitest retention/settings 114 ok; review: self-review (agent:opus) accepted; changes: NEW migration 0038_plan_samples.sql (query_plans.source, plan_samples hypertable, 30-day retention); store QueryPlanRow.Source, PlanSampleRow, WritePlanSamples; NEW server plan_samples.go (autoPlanFromFact) wired in pipeline; commands pass Source command; retention family plan_samples (API enum, console label); notes: self-review: command plans keep source command and /api/v1/plans unchanged (existing plan tests green); first stored row of a shape keeps its source (ON CONFLICT DO NOTHING); A25
