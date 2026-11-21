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
