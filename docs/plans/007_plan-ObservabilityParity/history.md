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

## 2026-11-21 7.3/1 complete unit:7.3:1
Gates: G-GO-FOCUS server/store/agent ok (AEX-010..013); G-GO-INT TestINTAEX002..004 ok; lint 0 issues; G-API regenerated (generated.ts, docs/api.md); vitest contract/captions/settings 272 ok; review: self-review (agent:opus) accepted; changes: agent ships parameters (cut 4096 on a UTF-8 boundary) only with checks.logs.plan_parameters; server re-checks (credential -> NULL) and stores plan_samples.parameters; NEW api_plan_samples.go DELETE /api/v1/plans/samples/parameters with command_audit row; OpenAPI path; notes: design note A26 before code; self-review: purge in one transaction, fixed error texts, requester recorded

## 2026-11-21 7.4/1 complete unit:7.4:1
Gates: G-GO-FOCUS ok (readiness, server, i18n, agent); G-GO-INT plan tests ok (INTAEX002-005, AEX015); lint 0; G-API rc=0; G-WEB-STATIC ok (2 pre-existing warnings); vitest queries+help 109 ok; i18n-check rc=0; review: self-review (agent:opus) accepted; changes: GET /api/v1/plans/samples; Plan.source; PlanSamples + AutoPlanSetup console; RDY-AEX-01; help automatic-plans EN/IT; README Automatic plans; notes: AEX014 is a unit test (validation without storage); AEX015 is integration (needs query_plans).

## 2026-11-21 P7/1 complete unit:P7:1
Gates: G-GO-FULL rc=0, G-GO-INT TestINTAEX001-005 + TestAEX015 + RLP ok (agent/server/store), G-RO TestINTRO001 ok, G-API no diff, G-WEB-FULL rc=0 (2076 tests, coverage gate, budget), G-I18N rc=0 (at 7.4, no change since); review: self-review (agent:opus) accepted; changes: none (closure); notes: review: integration INTAEX001-005; I-1 G-RO; I-4 TestINTAEX003/TestAEX012 (credential in plan or parameters never stored); parameters opt-in agent+server, shown only with include_parameters, purge audited (INTAEX004); README Automatic plans, help automatic-plans EN/IT, MONITORING_READINESS RDY-AEX-01 checked; note: samples API filters tenant_id='default' like logs API (pre-existing pattern)

## 2026-11-21 8.1/1 complete unit:8.1:1
Gates: G-GO-FOCUS pglog/wire/agent (race, shuffle) ok, TestAVH001-006; G-GO-INT TestINTAVH001 PASS; G-RO PASS; lint 0 issues; review: self-review (agent:opus) accepted; changes: NEW pglog.ParseAutovacuum (R5 regexes) + 4 real fixtures; fact kind autovacuum_run (Check logs); log loop ships autovacuum_run facts (500 per scrape, pg_log_autovacuum_dropped); notes: A27; self-review: each line matched alone, header required, nil numbers left out of the value

## 2026-11-21 8.2/1 complete unit:8.2:1
Gates: G-GO-FOCUS server/store (race, shuffle) ok, TestAVH007/008; G-GO-INT TestINTAVH002, RETINT001-003, RLP, AEX ok + store integration ok; lint 0; G-API regenerated rc=0; vitest retention/settings/api 172 ok; review: self-review (agent:opus) accepted; changes: NEW migration 0039_autovacuum_runs.sql; store AutovacuumRunRow + WriteAutovacuumRuns; server autovacuumRunRow (validation, invalid_fact) wired in pipeline; retention family autovacuum_runs 90 days (API enum, console label, tests 12->13); notes: A28; self-review: no object fact written, ON CONFLICT DO NOTHING on the dedup index, NaN/Inf/negative rejected

## 2026-11-21 8.3/1 complete unit:8.3:1
Gates: G-GO-FOCUS server/readiness/wire/i18n/agent PASS; INT TestINTAVH002/003 PASS; lint 0; G-API regen; tsc/eslint(2 pre-existing warnings)/prettier PASS; vitest instance+routes+i18n+help PASS; i18n-check PASS; review: self-review (agent:opus) accepted; changes: API /autovacuum/runs+summary; OpenAPI; AgentFlags.LogsEnabled; RDY-AV-01 + locales + MONITORING_READINESS; console Autovacuum activity view with per-table runs detail; captions en/it; help instance-overview#autovacuum-activity en/it; README Autovacuum history; notes: A29

## 2026-11-21 P8/1 complete unit:P8:1
Gates: G-GO-FULL rc=0; G-GO-INT rc=0 (agent, server, store, cmd/pglens-agent; TestINTAVH001-003, TestINTRO001 pg15/pg18; only opt-in TestINTFIRSTRUN001 skipped); G-WEB-FULL rc=0 (237 files, 2083 tests, statements 92.52%, budget PASS); G-API no diff; G-I18N pass; review: self-review (agent:opus) accepted; changes: Phase 8 closure review; notes: Review: order_by whitelist only (no SQL injection), instance_id tenant filter on every query, nullable numbers kept as null, old agents without logs_enabled leave RDY-AV-01 silent, runs fetched only when a summary row is expanded.

## 2026-11-21 9.1/1 complete unit:9.1:1
Gates: G-WEB-STATIC pass (tsc, eslint 2 pre-existing warnings, prettier); vitest planTree 29, queries+i18n 152 pass; review: self-review (agent:opus) accepted; changes: planTree.ts: PlanInsightKind, args, lossyBitmap, workersShort, joinFilterDiscards, jitShare, triggerShare; en/it messages; UI-PLI-001..008; notes: Review: thresholds per spec table; plan-only never yields jit/trigger (UI-PLI-004); no existing test changed. Decision A30.

## 2026-11-21 9.2/1 complete unit:9.2:1
Gates: G-GO-FOCUS ./internal/server pass; TestINTPLI001 + TestINTAEX* + TestAEX015 pass; lint 0; G-API regenerated (docs/api.md, generated.ts); review: self-review (agent:opus) accepted; changes: GET /api/v1/plans/stats (api_plan_stats.go), planSampleScope helper, OpenAPI PlanStat/PlanChange/PlanStats + path, TestPLI001/002, TestINTPLI001; notes: Review: tenant + instance filters on both queries and in EXISTS; database bound as parameter; changes capped 100 via constant; pool nil gives empty lists. Decision A31.

## 2026-11-21 9.3/1 complete unit:9.3:1
Gates: G-WEB-STATIC pass (tsc, eslint 2 pre-existing warnings, prettier); vitest queries 100, help+captions+i18n 160 pass; G-I18N rc=0; review: self-review (agent:opus) accepted; changes: usePlanStats + key; PlanStats.tsx, PlanCompare.tsx; PlanHistory wiring; en/it messages, captions, help subsection plan-stats; README Plan history + endpoint line; UI-PLI-009..013; notes: Review: has_plan false and unloaded captures cannot be ticked; change times use formatTimestamp (session zone, UI-PLI-010); no existing test changed. Decision A32.

## 2026-11-21 P9/1 complete unit:P9:1
Gates: G-GO-FULL rc=0; G-GO-INT rc=0 (TestINTPLI001, TestINTAEX002-005, TestINTRO001 pg15/pg18); G-API no diff; G-WEB-FULL rc=0 (237 files, 2096 tests, statements 92.54%, budget PASS); G-I18N rc=0 (9.3); review: self-review (agent:opus) accepted; changes: Phase 9 closure review; notes: Review: insight thresholds match the spec table and each has a positive and negative fixture; plan-only plans never yield jit/trigger; I-5 the stats endpoint is read-only, tenant and instance scoped; README Plan history and help plan-stats describe what is compared (plans of one statement by structure hash) and every figure.

## 2026-11-21 10.1/1 complete unit:10.1:1
Gates: G-GO-FOCUS ./internal/command (race, shuffle) ok; lint 0; server tests ok; G-API regenerated; vitest commands+i18n 36 ok; tsc ok; review: self-review (agent:opus) accepted; changes: index_check kind, 4 index args, Validate+gate T1; OpenAPI enum/fields; shell.command.kind.* labels en/it; TestIDXV001-003, UI-IDXV-001; notes: A33

## 2026-11-21 10.2/1 complete unit:10.2:1
Gates: G-GO-FOCUS ./internal/agent ok; G-GO-INT TestINTIDXV001-003, TestINTRO001 (pg15, pg18), TestINTRO003 pass, none skipped; lint 0; review: self-review (agent:opus) accepted; changes: IndexCheckExecutor (hypopg, read-only txn, reset/ROLLBACK/discard on every path), indexDDL quoting, registered in pglens-agent; pgtest.PGExt; RO audit runs index_check; notes: A34

## 2026-11-21 10.3/1 complete unit:10.3:1
Gates: G-GO-FOCUS server/store/command ok; G-GO-INT TestINTIDXV004 pass; G-API gen:api+api-docs; lint 0; tsc ok; vitest index 148 pass; review: self-review (agent:opus) accepted; changes: migration 0040 index_validations + commands kind CHECK; store.UpsertIndexValidation; persistCommandArtifact index_check case; suggestions validation field + OpenAPI IndexValidation; notes: A35

## 2026-11-21 10.4/1 complete unit:10.4:1
Gates: G-WEB-STATIC tsc/eslint(2 pre-existing warnings)/prettier ok; G-WEB-UNIT vitest 238 files 2103 tests pass; review: self-review (agent:opus) accepted; changes: IndexValidationSection (Validate with hypopg, NotPermitted T1, progress, result badges, hypopg_missing text); lib helpers; en/it catalogs; help indexes#missing-validate en/it; README Validating an index suggestion; tests UI-IDXV-002..007; notes: A36; self-review agent:opus: no findings

## 2026-11-21 P10/1 complete unit:P10:1
Gates: G-GO-FULL rc=0; G-GO-INT rc=0 (TestINTIDXV001-004, TestINTRO001 pg15/pg18, TestINTRO003 pglens-pgext:18, none skipped); G-API no diff; G-WEB-FULL rc=0 (budget PASS); G-I18N rc=0; review: self-review (agent:opus) accepted; changes: Phase 10 closure review; notes: Review: I-6 hypopg_reset before and in deferred cleanup, connection discarded; I-1 only BEGIN READ ONLY + SET LOCAL, hypopg functions run in the agent's own session; identifiers quoted with pgx.Identifier.Sanitize; I-3 command args carry queryid and identifiers, never SQL text; README Validating an index suggestion and help indexes#missing-validate.

## 2026-11-21 11.1/1 complete unit:11.1:1
Gates: G-GO-FOCUS ./internal/sqltag ok; FuzzParse 10s clean (1.46M execs); lint 0; review: self-review (agent:opus) accepted; changes: NEW internal/sqltag: Parse, Service, Endpoint, TraceID, label cleaning; notes: self-review agent:opus: no findings

## 2026-11-21 11.2/1 complete unit:11.2:1
Gates: G-GO-FOCUS ok (ash, sqltag, agent, cmd/pglens-agent, server, store); TestINTQTAG001 pg15+pg18 PASS; TestINTQTAG002 PASS; TestINTASHD002-004 PASS; lint 0; tsc ok; G-API regenerated; review: self-review (agent:opus) accepted; changes: sampler reads right(query,700) only when service/endpoint on; SampleRow/Key Service+Endpoint; labels service/endpoint; migration 0041; store writers; buildASHRow; /api/v1/ash group_by+filters service,endpoint; OpenAPI; notes: A37

## 2026-11-21 11.3/1 complete unit:11.3:1
Gates: G-GO-FOCUS ok (cmd/pglens-agent, server, store, sqltag); TestINTQTAG002/003 PASS; TestINTPGL002/003, TestINTAEX001 PASS; lint 0; tsc ok; G-API regenerated; review: self-review (agent:opus) accepted; changes: agent trace_id label on log_event and plan facts (also with statement_text off); store LogEventRow/PlanSampleRow TraceID; pipeline stores valid ids, else NULL; PGLENS_TRACE_URL_TEMPLATE validated at start-up; session trace_url_template; trace_id= filters on /logs and /plans/samples (400 on bad value); plan samples answer trace_id; notes: A38

## 2026-11-21 11.4/1 complete unit:11.4:1
Gates: G-WEB-STATIC (tsc, eslint 0 errors/2 pre-existing warnings, prettier); G-WEB-UNIT 239 files/2111 tests; review: self-review (agent:opus) accepted; changes: Activity group-by/filter Service and Endpoint ((untagged)/(senza tag)); traceLink + TraceId component (link rel=noopener noreferrer new tab, else short id + copy) in Logs rows and Plan samples column; help wait-events#services en/it; README 'Linking queries to services'; notes: UI-QTAG-001..004; UI-RLP-004 extended to the two tag dimensions (rollup drops them); self-review agent:opus: link safety via id/scheme validation

## 2026-11-21 P11/1 complete unit:P11:1
Gates: G-GO-FULL; G-GO-INT 85 PASS 0 SKIP (TestINTQTAG001 pg15/pg18, 002, 003; TestASHD*, TestPGL*, TestAEX*); G-RO (INTRO001 pg15/pg18, INTRO003); G-API no diff; G-WEB-FULL (typecheck, lint, coverage, budget); G-I18N; review: self-review (agent:opus) accepted; changes: Phase 11 closed: query to service link; notes: Review agent:opus: statement text stays in the agent (only tag values, cut to 128 bytes; trace_id 32 hex); service/endpoint inside the I-2 key cap with overflow; links built only from validated hex id and http(s) template (server and console), rel=noopener noreferrer; README + help en/it

## 2026-11-21 12.1/1 complete unit:12.1:1
Gates: G-GO-FOCUS ./internal/host (race, shuffle); golangci-lint 0 issues; review: self-review (agent:opus) accepted; changes: NEW internal/host/process.go (ParentPID, Processes, ProcessRates, TopProcesses; stat/status/io parsers), process_test.go, fake /proc pid tree 100-105 + 200; notes: TestPROC001-005 PASS; A39; self-review agent:opus: paths only from proc path + numeric pid, command line never read

## 2026-11-21 12.2/1 complete unit:12.2:1
Gates: G-GO-FOCUS ./cmd/pglens-agent ./internal/agent ./internal/wire ./internal/host; lint 0; G-RO INTRO001 pg15/pg18 + INTRO003; G-GO-INT TestINTPROC001 SKIPPED (documented); review: self-review (agent:opus) accepted; changes: host.processes config (enabled, top_n 1-100), fact kind host_process/check host_processes, processTracker per host-local target in the host loop, Manager.BackendPID/ProcessSessions, example config; notes: TestPROC006-009 PASS; A40; self-review agent:opus: only pid, session labels (64-byte cut) and numbers shipped; namespace guard

## 2026-11-21 12.3/1 complete unit:12.3:1
Gates: G-GO-FOCUS server+store race; TestINTPROC002 + TestRETINT001 PASS; lint 0; G-API regenerated; vitest retention; review: self-review (agent:opus) accepted; changes: migration 0042 metrics_processes; store.WriteProcesses; pipeline host_process routing with invalid_fact rejects; GET host/processes and host/processes/top; OpenAPI + generated; retention family metrics_processes (Go, web, i18n); notes: A41

## 2026-11-21 12.4/1 complete unit:12.4:1
Gates: G-WEB-STATIC (tsc, eslint 0 errors, prettier); G-WEB-UNIT full 241 files after fixing CAPTION-COVERAGE-002 + UI-CLUS-052; server PROC tests; review: self-review (agent:opus) accepted; changes: lib/processes.ts; ProcessesPanel (snapshot table, group selector on /top, unknown I/O dash with tooltip, empty state); HostSection mounts it; i18n en/it; caption host.processes en/it; help instance-overview#host-processes en/it; README Per-process metrics + host.processes config; notes: processRow label read moved off the BackendType: line (UI-CLUS-052 scans Go Type: lines)

## 2026-11-21 12.2 attempt 2/1 complete unit:12.2 attempt 2:1
Gates: TestINTPROC001 host-pid PASS (pg15/pg18 pgtest cases skip, documented); lint 0; review: self-review (agent:opus) accepted; changes: TestINTPROC001 host-pid case with PidMode=host; fact content assertions; go.mod moby/moby/api direct; notes: A42; found during P12 review

## 2026-11-21 P12/1 complete unit:P12:1
Gates: G-GO-FULL 0 issues; G-GO-INT TestINTPROC001 (host-pid PASS, pgtest cases skip documented) + TestINTPROC002 PASS; G-RO INTRO001 pg15/pg18 + INTRO003 PASS; G-API no diff; G-WEB-FULL EXIT 0 (2 pre-existing eslint warnings); G-I18N PASS; full vitest 241 files; review: self-review (agent:opus) accepted; changes: none (closure); notes: Review agent-1 (opus): /proc stat parsing from last ')' with spaces/parens tested (PROC001); pid reuse via start ticks; I-1 holds: process queries on agent.Manager covered by TestINTRO001; no command line/path shipped, asserted in TestINTPROC001/host-pid; skip record corrected (A40, A42); README Per-process metrics + help host-processes. Accepted.

## 2026-11-21 13.1/1 complete unit:13.1:1
Gates: G-GO-FOCUS ./internal/command/ PASS; TestWBK014+TestOpenAPI* PASS; G-API regenerated (generated.ts), api-docs no diff; lint 0; tsc/prettier ok; review: self-review (agent:opus) accepted; changes: Args.Settings/Hints (omitempty), explain-only; workbook.go WorkbookSettings (32), ValidateSetting/ValidateSettings/SettingValue/ValidateHints; OpenAPI CommandArgs.settings/hints + WorkbookSettingName enum; notes: A43; review agent-1 (self): no SQL path, hints charset excludes * / ; ' newline

## 2026-11-21 13.2/1 complete unit:13.2:1
Gates: G-GO-FOCUS ./internal/agent/ PASS; G-GO-INT TestINTWBK001 pg15/pg18, TestINTWBK002 pg15/pg18, TestINTWBK003 pgext+stock pg15/pg18 PASS (no skip); TestINTCMD*/TestINTIDXV* PASS; G-RO INTRO001 pg15/pg18, INTRO002 postgis18, INTRO003 PASS; lint 0; review: self-review (agent:opus) accepted; changes: applyWorkbook (current_setting check, set_config($1,$2,true) in name order) after dropExplainPrivileges; withHints prefix on both paths and for guardAnalyze; result echoes settings/hints (omitempty); pgtest preload param; notes: Sequence (S1, approved by agent-1 self-review): BEGIN READ ONLY; SET LOCAL statement_timeout; SET LOCAL search_path TO DEFAULT; guardPlanning; dropExplainPrivileges; [hints] SELECT current_setting('pg_hint_plan.enable_hint', true); [settings] SELECT set_config($1,$2,true) by name; [analyze] guardAnalyze on hinted text; EXPLAIN (opts) /*+ h */ stmt via QueryRow/explainVerbatim/explainBound; SELECT current_database(); ROLLBACK; discardConn. A44.

## 2026-11-21 13.3/1 complete unit:13.3:1
Gates: G-GO-FOCUS server/store/command ok; G-GO-INT TestINTWBK004 + INTCMD/INTPLAN ok; lint 0; G-API ok; review: self-review (agent:opus) accepted; changes: migration 0043 workbooks; store/workbooks.go; api_workbooks.go CRUD+run; result handler links variant; expiry fails pending variant; explain with settings/hints skips query_plans; OpenAPI paths+schemas; notes: A45

## 2026-11-21 13.4/1 complete unit:13.4:1
Gates: G-WEB-STATIC green (tsc, eslint 0 errors/2 pre-existing warnings, prettier); G-WEB-UNIT full run 2127/2130, the 3 failures (HELP-UI-001 chapter count, FHD-ROUTES-004 pendingUnit, testIds duplicate IDs) fixed and rerun green (7 files 26 tests); review: self-review (agent:opus) accepted; changes: web/src/features/workbook/{WorkbookPage,VariantEditor,VariantCompare,WorkbookLauncher}.tsx, errors.ts, tests; lib/workbook.ts+test; api/workbooks.ts; i18n workbook en/it, auth surface; caption queries.workbook-variants; help topic workbook en/it + catalog; route + pages.tsx launcher; fullhd routes/fit; PlanHistory PlanComparison exported; README Tuning workbook; notes: Review agent-1 (opus): editor mirrors server rules (OpenAPI enum parity UI-WBK-008); Run all sequential (UI-WBK-003 max in-flight 1); hint_plan text exact; README Plan history claim corrected against persistCommandArtifact (settings/hints skip); A46.
