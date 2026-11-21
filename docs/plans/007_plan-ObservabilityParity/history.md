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
