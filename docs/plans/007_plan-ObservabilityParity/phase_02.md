# Phase 1 — Multixact wraparound rule

Intent: pglens reports and alerts on multixact ID wraparound risk, exactly as it does for transaction ID wraparound.
Prerequisites: P0.
Phase closure: P1; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D6: metric `pg_max_datminmxid_age` (gauge, no labels, check `activity`); rule id `txn.multixact_wraparound_risk`; severity critical; threshold `1_000_000_000`.
- D18, I-5: every new text exists in `en` and `it` with the same placeholders (`TestCatalogParity`).
- R6: `SELECT COALESCE(max(mxid_age(datminmxid)), 0)::float8 FROM pg_database` works on PostgreSQL 15–18 for a `pg_monitor` role.
- Pattern to copy: every place that names `txn.wraparound_risk` or `pg_max_datfrozenxid_age`. Find them with
  `grep -rn 'wraparound_risk\|pg_max_datfrozenxid_age' internal web/src api docs --include='*.go' --include='*.json' --include='*.sql' --include='*.ts' --include='*.tsx' --include='*.yaml' --include='*.md'`.
  Known places: `internal/check/activity.go` (query near line 123, metric near line 271), `internal/advisor/rules_query.go` (`wraparoundAge`, `metricRule(...)` near line 219), `internal/i18n/locales/advisor_query.{en,it}.json`, `internal/store/migrations/0006_alerts.sql` (seed near line 85), `internal/server/api_locks.go`, `internal/server/describe_evidence.go`, `web/src/lib/ruleText.ts`, `web/src/lib/findingCheck.ts`, `web/src/lib/help/topics/{en,it}/{alert-rules,findings}.ts`, `web/src/features/alerts/ruleThreshold.ts`, `web/src/i18n/domain/it/rules.ts`.
- Texts (English; the Italian catalog gets the translation, technical words stay English):
  - title: `Multixact ID wraparound risk`
  - detail: `The oldest multixact ID in a database is {age} multixacts old. PostgreSQL stops creating multixacts near 2 billion.`
  - remediation: `Run VACUUM (FREEZE) on the tables with the oldest relminmxid, and check what blocks autovacuum (long transactions, prepared transactions, replication slots).`
  Use the placeholder names of the existing `advisor.txn.wraparound_risk.*` keys, not new ones.

## Sub-phases

### 1.1 Agent metric `pg_max_datminmxid_age`
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `internal/check/activity.go` (the `pg_max_datfrozenxid_age` query and its metric append), `internal/check/mockconn_test.go`; WRITE `internal/check/activity.go`, the activity unit test file that covers `pg_max_datfrozenxid_age`.
- **Change:**
  Preconditions: P0 DONE.
  Contract: each `activity` result has one more gauge `pg_max_datminmxid_age` with the value of the R6 query. A query error is handled exactly like an error of the `datfrozenxid` query.
  Scope: no other metric changes.
  Steps:
  1. S1 — add the R6 query as a constant next to the `datfrozenxid` one, prefixed with the same `/* pglens: … */` comment style; run it in the same place and append the metric with the same kind.
  2. S2 — extend the mock connection expectations of the activity tests with the new query.
  Recovery boundary: none.
  Failure handling: the two queries are combined in one statement in the source → add a second column instead of a second query and say so in the unit notes.
- **Unit tests:** `TestMXW001` (activity result contains `pg_max_datminmxid_age` with the mocked value 123), `TestMXW002` (query error: same behavior as the `datfrozenxid` error case) in the activity test file. Gate G-GO-FOCUS on `./internal/check/` with `-run 'TestMXW00[12]'`; 2 tests discovered.
- **e2e tests:** `TestINTMXW001` in the existing activity integration test file (`-tags=integration`): on PG 15.19 and 18.6 the metric exists and is `>= 0`. Gate G-GO-INT.
- **Done:** metric emitted; MXW-001, MXW-002, INT-MXW-001 pass; G-RO passes; reviewed; unit closed and committed.

### 1.2 Advisor rule, texts, alert seed
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `internal/advisor/rules_query.go` (`metricRule`, `wraparoundAge`), `internal/store/migrations/0006_alerts.sql` (seed row of `txn.wraparound_risk`), `internal/store/migrations/0029_redundancy_rule.sql` (latest seed style); WRITE `internal/advisor/rules_query.go`, `internal/i18n/locales/advisor_query.en.json`, `internal/i18n/locales/advisor_query.it.json`, `internal/server/describe_evidence.go` and `internal/server/api_locks.go` only where they list `txn.wraparound_risk`; NEW `internal/store/migrations/0032_multixact_rule.sql` (D19).
- **Change:**
  Preconditions: 1.1 DONE.
  Contract: the rule list contains `metricRule("txn.multixact_wraparound_risk", SeverityCritical, "pg_max_datminmxid_age", wraparoundAge)` right after the XID rule. Catalog keys `advisor.txn.multixact_wraparound_risk.{title,detail,remediation}` exist in both locales. The migration inserts one alert rule row copied from the `txn.wraparound_risk` seed with the new rule id and metric, with `ON CONFLICT DO NOTHING`.
  Steps:
  1. S1 — add the rule; add the catalog keys in both files.
  2. S2 — write the migration by copying the column list and values of the XID seed; change only id, name and metric.
  3. S3 — in each server file that switches on `txn.wraparound_risk`, read the code: add the new rule id only where the logic is about "age of the oldest ID in a database"; leave lock-specific code alone. List what changed in the unit notes.
  Failure handling: `metricRule` reads the texts by rule id from a different catalog file → add the keys where the XID keys are.
- **Unit tests:** `TestMXW003` (rule fires at `1_000_000_001`, not at `999_999_999`), `TestMXW004` (finding text in `en` and `it` contains the age; run with `strictMessages`) in `internal/advisor/`; `TestCatalogParity` still passes; `TestMXW005` in `internal/store/` (the migration list contains `0032` and the SQL has `ON CONFLICT DO NOTHING`). Gate G-GO-FOCUS on `./internal/advisor/ ./internal/store/ ./internal/i18n/`.
- **e2e tests:** `TestINTMXW002` (`./internal/store/`, integration): after migrations the alert rule row exists once; running migrations twice changes nothing. Gate G-GO-INT.
- **Done:** rule, texts and seed exist; tests pass; reviewed; unit closed and committed.

### 1.3 Console texts, help, verification SQL, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `web/src/lib/ruleText.ts`, `web/src/lib/findingCheck.ts`, `web/src/features/alerts/ruleThreshold.ts`, `web/src/i18n/domain/it/rules.ts`, `web/src/lib/help/topics/en/alert-rules.ts`, `web/src/lib/help/topics/it/alert-rules.ts`, `web/src/lib/help/topics/en/findings.ts`, `web/src/lib/help/topics/it/findings.ts`, their existing test files, `README.md` (rule list, if it lists rules).
- **Change:**
  Preconditions: 1.2 DONE.
  Contract: wherever the console knows `txn.wraparound_risk`, it knows `txn.multixact_wraparound_risk` with its own text. The "How to verify" SQL is:
  ```sql
  BEGIN TRANSACTION READ ONLY;
  SELECT datname, mxid_age(datminmxid) AS multixact_age FROM pg_database ORDER BY 2 DESC;
  SELECT c.oid::regclass AS relation, mxid_age(c.relminmxid) AS multixact_age
    FROM pg_class c WHERE c.relkind IN ('r','m','t') AND c.relminmxid <> '0'
   ORDER BY 2 DESC LIMIT 20;
  ROLLBACK;
  ```
  Steps:
  1. S1 — add the rule to each file by copying the XID entry and changing the texts.
  2. S2 — add the help paragraphs in `en` and `it`.
  3. S3 — README: add the rule where rules are listed; if rules are not listed, write "README lists no rules; unchanged" in the unit notes.
- **Unit tests:** `UI-MXW-001` (rule text exists in `en` and `it`), `UI-MXW-002` (verification SQL starts with `BEGIN TRANSACTION READ ONLY` and ends with `ROLLBACK`), `UI-MXW-003` (threshold editor shows the rule with the same unit as the XID rule), added to the existing test files of those modules. Gates G-WEB-STATIC, G-WEB-UNIT on the touched paths.
- **e2e tests:** `TestINTMXW003`: the verification SQL runs without error on PG 15.19 and 18.6 (add it to the existing integration test that runs the `findingCheck` statements; if none exists, add it to the activity integration test file with the SQL copied as a Go constant and a comment naming `findingCheck.ts`). Gate G-GO-INT.
- **Done:** console shows the rule; tests pass; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTMXW00[123]`), G-RO, G-WEB-FULL, G-I18N; assertions: rule visible in `/api/v1/findings` rule catalogue test, no new failure against the baseline.
- README obligation: unit 1.3.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews integration, invariants I-1 and I-5, tests, and documentation.
- Open closure unit P1, record review and result, mark the phase DONE, close, commit `phase(007/P1): PHASE 1 COMPLETE — Multixact wraparound rule; gates and review passed`.
- P1 edits no production code or tests. A blocked gate keeps P1 OPEN and BLOCKED. A failing gate or rejected review closes the attempt as failed; reopen the responsible sub-phase with a new attempt, then rerun P1.
