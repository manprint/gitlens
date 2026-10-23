# 004 — Full review, read-only hardening, monitoring completeness

_Opened 2026-10-23 on branch `review/full-audit-2026-10`._

## Goals (from the request)

1. Systematic review of the whole project for every class of defect: bugs,
   races, deadlocks, inaccuracies, performance problems, inconsistencies.
2. Detailed frontend verification: what the UI shows must be coherent with the
   API and with PostgreSQL; fix frontend bugs.
3. Invariant **"pglens only ever reads a monitored database"**, always. pglens
   must never alter data in a monitored database.
4. Every script that creates users, extensions or plugins creates read-only
   users only.
5. PostGIS analysis (the request said "postfix"; the user confirmed PostGIS).
6. Monitoring of partitions, indexes, sequences, triggers — PostgreSQL
   monitoring must be safe and complete.
7. Features of other tools (pganalyze, AWS Performance Insights / Database
   Insights) that pglens lacks: implement them where they fit the product.
8. Bump Go, Go modules and every frontend library to the latest release.
9. Support PostgreSQL 19 (release candidate at the time of writing).

Constraints: work locally only, Docker for every database; never touch a
remote database. Telegram notifications only for significant events.

## Decisions

| ID | Decision | Source |
|----|----------|--------|
| D1 | On-demand commands stay opt-in and default off, hardened: `EXPLAIN ANALYZE` is refused when the statement's plan calls a volatile non-built-in function or a built-in with effects outside the transaction (dblink, signals, advisory locks, notify, stats resets, ...); cancel/terminate stay T2 + `allow_signal`. Tests prove the invariant. | user, 2026-10-23 |
| D2 | "postfix" means PostGIS. | user, 2026-10-23 |
| D3 | TypeScript 7 runs side by side with the TypeScript 6 API (`@typescript/typescript6`) until typescript-eslint and openapi-typescript support TS 7. | toolchain constraint |
| D4 | Work is committed locally on the branch, one verified unit per commit; nothing is pushed. | session |

## Phases

| Phase | Content | Gate |
|-------|---------|------|
| P1 | Dependency and image bump (Go 1.27.3, modules, web majors, PG minors, TimescaleDB 2.31, toxiproxy 2.13) | unit + lint + vuln + web gates + integration PG15-18 both profiles + e2e smoke |
| P2 | Systematic review: finders per area and lens, adversarial verification with a reproduction, findings ledger in `findings.md` | ledger written |
| P3 | Read-only invariant hardening: role scripts, fixture roles, every connection path, EXPLAIN ANALYZE gate, privilege-audit integration test, e2e statement audit | integration + e2e |
| P4 | Fix verified review findings, batched by area | unit + integration |
| P5 | PostgreSQL 19: probe view columns, matrix, version-gated SQL | integration PG15-19 |
| P6 | Monitoring completeness: partitions, sequences, triggers/functions, index health, PostGIS, gap features vs pganalyze / Performance Insights | unit + integration + e2e |
| P7 | Frontend coherence on a live stack; UI for P6 | web gates + Playwright |
| P8 | Final review, full gates, docs | everything green |

Every new monitoring item states its source view, T0 availability under
`pg_monitor`, version range, scrape cost and cardinality impact; a missing
prerequisite degrades by name (a missing observation is never reported as
healthy).
