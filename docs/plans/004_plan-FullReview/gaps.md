# pglens monitoring gap backlog (PostgreSQL 15 to 19rc1, Tier 0)

Date: 2026-10-23. Branch: review/full-audit-2026-10 (working tree).
Inputs: `inventory.md`, `catalog-coverage.md`, `competitors.md` (this folder), the uncommitted diff, and one local container pass (section 7).
Read-only for the product: no monitored database was contacted, no repository file other than this one was written.

## 0. Rules applied

- **Invariant.** pglens reads monitored databases and never alters their data. Every monitoring role is read-only. Every item uses Tier 0 (pg_monitor and pg_control_system()) unless it says otherwise.
- **Excluded by rule** (section 6): anything that needs superuser, server log access, a stats reset function, extension creation by the agent, an untrusted-language helper, or row data.
- **Priority.** P0 = MUST (named by the user), plus the write-path gap and the PG 19 blockers. P1 = parity with pganalyze, AWS Performance Insights and Database Insights. P2 = nice to have.
- **Effort (proposal).** S = one check or rule plus tests, about a day. M = check, store, API and UI, a few days. L = store schema change or a new UI section.
- **Availability codes.** C = in the `catalog-coverage.md` matrix (readable at T0, 15 to 19rc1). X = verified in this review's container pass (section 7). U = not verified; treat as unknown, not OK.
- **Degradation names.** Same format as `check.Requirements.Supports` (`internal/check/check.go:34`): `missing extension <name>`, `requires PG >= N, got X`, `requires T1, got T0`, `role <r> not allowed`. Advisor findings use `degraded_reason` text, as `index.unused` already does. New names below follow that style.
- **Cardinality.** Per-object series keep top-N plus an `other` bucket, as `table_stats` and `index_stats` do. Partitions roll up to their parent and never take a top-N slot as leaves (G03).
- **Object resolution.** Resolve objects by oid joins on `pg_class` and `pg_namespace`. Do not cast text to regclass. At T0, `'schema.name'::regclass` fails with `permission denied for schema <schema>` when the schema does not grant USAGE to the role (X, 18.6 and 19rc1), and many user schemas do not.

## 1. Summary

| ID | Item | Pri | Effort | T0 status |
|---|---|---|---|---|
| G01 | Close write paths: lo_create, lo_from_bytea, TEMP on database | P0 | S | Write path confirmed (C, 19rc1); fix tested (C) |
| G02 | PostgreSQL 19 support (declaration, tests, ASH wait type, receiver status) | P0 | S-M | Collection already runs on 19rc1 (inventory §2); gaps listed in G02 |
| G03 | Partition monitoring | P0 | M | Readable (C, X) |
| G04 | Sequence monitoring, incl. int4 column on bigint sequence | P0 | M | Partial: last_value NULL without per-sequence SELECT (X) |
| G05 | Trigger monitoring | P0 | S | Readable (X) |
| G06 | Function monitoring | P0 | S | View readable; empty unless track_functions is pl or all; default none (X) |
| G07 | Missing FK index detection | P0 | S | Readable (X) |
| G08 | Index bloat producer; bloat estimate blind spot | P0 | M | Index estimate blind for unreadable columns (C §3.2) |
| G09 | Index usage windows; unused-rule standby handling | P0 | S | last_idx_scan readable (X, 18 and 19rc1) |
| G10 | Extension inventory; PostGIS presence, version, upgrade | P0 | S | Readable (X, C) |
| G11 | PostGIS column inventory (SRID, type) | P0 | M | Readable via pg_attribute and typmod helpers (X, 19rc1); geometry_columns blind (C) |
| G12 | Spatial columns without a spatial index | P0 | S | Readable (X, 19rc1) |
| G13 | Multixact age | P1 | S | mxid_age(datminmxid) readable (X); relminmxid column U |
| G14 | xmin horizon holders | P1 | S | Readable (C); standby feedback U |
| G15 | DB load by user, application, client | P1 | M | Readable under pg_read_all_stats (C) |
| G16 | Lock analysis: blocking time, session identity, tree-only query text | P1 | S | pg_locks readable (C); waitstart name U |
| G17 | Logical replication: subscriber side and slot stats | P1 | M | Readable, 0 rows here (X, C); subconninfo must never be read |
| G18 | Recovery conflicts per reason; standby prefetch; pg_stat_recovery (19) | P1 | S | Readable (X, C) |
| G19 | Progress: create_index, cluster, copy (repack on 19) | P1 | S | Readable, 0 rows (C); live only for VACUUM |
| G20 | Temp file count | P1 | S | Readable (X) |
| G21 | Database size and growth | P1 | S-M | pg_database_size readable (X); cost U |
| G22 | Table vacuum time (18+) and autovacuum scores (19) | P1 | S | Readable (C) |
| G23 | PG 19 views and columns not yet read (umbrella for G09, G13, G17, G18) | P1 | S | Readable (C) |
| G24 | WAL directory size and file count via pg_ls_waldir() | P2 | S | Readable (C) |
| G25 | SLRU counters | P2 | S | Readable (X, C) |
| G26 | JIT, pg_stat_statements_info.dealloc, new pg_stat_statements columns | P2 | S | Columns partly U |

## 2. P0 items (MUST)

### G01 Close the write paths for the monitoring role (HARD INVARIANT)
- **Priority / effort:** P0, S.
- **User value:** makes the read-only invariant hold in the database, not only by convention. Today the role can write persistent large objects once the session turns off `default_transaction_read_only`.
- **Sources:** functions `lo_create(oid)` and `lo_from_bytea(oid, bytea)`; the TEMPORARY privilege on each monitored database (PUBLIC).
- **T0 availability:** write path confirmed (C §3.3, 19rc1): with the default off, `SELECT lo_create(0)` and `lo_from_bytea` succeed and create rows in `pg_largeobject_metadata` owned by pglens. `CREATE TEMP TABLE` succeeds too. Fix tested (C): `REVOKE EXECUTE ON FUNCTION lo_create(oid), lo_from_bytea(oid, bytea) FROM PUBLIC` and `REVOKE TEMPORARY ON DATABASE <db> FROM PUBLIC` give permission denied.
- **PG versions:** tested on 19rc1 only. Repeat on 15 to 18 before rollout (U).
- **Scrape cost / cardinality:** none. The REVOKEs are a one-time operator step.
- **Pieces:** `deploy/sql/monitoring_user.sql` (add the REVOKEs per monitored database); an e2e test that connects as the role and asserts permission denied for `lo_create` and `CREATE TEMP TABLE`. No check changes: C found no pglens check that uses temp tables or large objects.
- **Degrades as:** not applicable (privilege step). The e2e test fails when the grant is missing.
- **Notes:** the agent must not run these REVOKEs (it is T0). The comment in `monitoring_user.sql` ("refused by the server even if a client ever tried one") is wrong for large objects. See H1. The role script must grant SELECT, never USAGE, on any sequence (G04), and must not grant any sequence privilege to PUBLIC.

### G02 PostgreSQL 19 support
- **Priority / effort:** P0, S-M.
- **User value:** 19 is at release candidate (docs banner: rc1, 2026-10-15). The user asked for support.
- **Current state:** collection already runs on 19rc1. No check sets MaxPG, and the monitoring path never refuses on version (inventory §2, "Version gating"). `pgtype.Supported()` (`internal/pgtype/version.go`) accepts 15 to 18 and is called only by `pglens-agent check`, which prints NOT SUPPORTED. The test matrix (`test/pgtest/versions.go`) lists 15 to 18 only. The uncommitted diff bumps minors only (15.19, 16.15, 17.11, 18.6; timescale 2.31.0-pg17). No PG 19 work is in progress.
- **Work items:**
  1. `pgtype.Supported()`: accept 19. This updates decision D11, which the product must record.
  2. `test/pgtest/versions.go`: add `19: postgres:19rc1-alpine`. Add a PostGIS 19rc1 run (`postgis/postgis:19rc1-3.6-alpine`) for G10 to G12. Run e2e on 15 to 19.
  3. `deploy/sql/monitoring_user.sql` header says 15 to 18. The file applies with rc=0 on 19rc1 (C). Fix the header.
  4. ASH wait type (X): on 19rc1 `pg_wait_events` has no `BufferPin` entry. It has type `Buffer` with names `BufferCleanup`, `BufferExclusive`, `BufferShareExclusive`, `BufferShared`. On 18.6 the type is `BufferPin` with name `BufferPin`. `web/src/components/charts/ash.options.ts` lists `BufferPin` in `ASH_WAIT_TYPES` (line 13) and `ASH_PALETTE` (line 24) and has no `Buffer`. Add `Buffer`; keep `BufferPin` for 15 to 18. How the UI folds unknown types into `other`: U.
     On 18.6, `pg_wait_events` also lists `LWLock | BufferContent`. On 19rc1 that entry is absent, and neither `BufferPin` nor `BufferContent` appears (X). The four `Buffer` events do not map one-to-one onto the old names. So wait-type shares (sibling and baseline comparisons) shift across an upgrade. Map the old and new names into one palette category and state the mapping. No code or UI hit for `BufferContent` (`git grep` over `internal`, `cmd`, `api`, `test`, `web/src`). `BufferPin` appears only in `web/src/components/charts/ash.options.ts` (lines 13 and 24).
  5. `replication_receiver` (inventory §2 row 9): PG 19 adds the value `connecting` to `pg_stat_wal_receiver.status` (competitors §4). The inventory says unmapped values map to -1 "other". Map it explicitly. Read the mapping code before changing it (U).
  6. Re-verify at GA. The 19rc1 results are RC results.
- **Scrape cost / cardinality:** none.
- **Degrades as:** no MaxPG is set, so the gate never fires on 19. Keep it that way.
- **Notes:** new views and columns are G23 (P1). They are not needed for support.

### G03 Partition monitoring
- **Priority / effort:** P0, M.
- **User value:** partitioned tables are invisible as a unit today. Operators need per-parent size and growth, rows in the default partition (they block new partitions and slow queries), missing future partitions, and invalid partitioned indexes.
- **Sources:** `pg_class` (relkind `p` parent, `r` leaf, `I` partitioned index; X: relkind p, r, i all visible); `pg_partitioned_table` (C: 1 row, 8 cols); `pg_inherits` (C); `pg_partition_root(regclass)` (X: returns the parent); `pg_partition_tree()` (C); `pg_get_expr(relpartbound, oid)` (X: returns `FOR VALUES FROM ... TO ...` and `DEFAULT`); `pg_stat_user_tables` for leaves (C).
- **T0 availability:** readable (X, C). Default-partition row count is an estimate from leaf `n_live_tup` (C). An exact count needs SELECT, which T0 lacks, so do not use it.
- **PG versions:** 15 to 19rc1. `pg_partition_root` needs PG 12 or later.
- **Scrape cost / cardinality:** leaves already come from `table_stats` (relkind `r`). Roll up per parent in the check: partition count, total bytes, dead tuples, oldest analyze, default-partition rows. Emit per-parent series under top-N parents. Today leaves compete for `table_stats` top-N slots (inventory §2 row 14: union of top-50 by dead tuples and top-50 by bytes, capped at 200). A 365-partition table can fill them. Change the selector to rank parents first and keep leaves only under the top parents.
- **Pieces:** check (new `partition_stats`, database scope, or an extension of `table_stats`); metrics `pg_partition_count`, `pg_partition_bytes_total`, `pg_partition_default_rows` per parent; facts `partition_tree` keyed by parent; store facts and metrics; API: partition rows in `/api/v1/instances/{id}/tables`, or a new `/partitions`; UI: group `RelationsSection` rows by parent; advisor rules: `partition.default_has_rows` (warning, from stats), `partition.index_invalid` (critical, see G09), and a proposal for "no partition covers the next N days" (P1 within G03, parses the range bound: U).
- **Degrades as:** `no stats for leaf <schema.table>` (the leaf is missing from `pg_stat_user_tables`). Otherwise none.

### G04 Sequence monitoring (exhaustion, including int4 columns on bigint sequences)
- **Priority / effort:** P0, M.
- **User value:** sequence exhaustion stops inserts. Two failure cases: a sequence that reaches its `max_value`, and an int4 column fed by a bigint sequence, where the column overflows at 2^31-1 long before the sequence is close to its limit.
- **Sources:** `pg_sequences` (X: rows visible at T0; `data_type`, `max_value` visible; `last_value` NULL without privilege); `pg_sequence` (X: `seqtypid`, `seqmax`, `seqincrement`, `seqcycle` readable at T0 on 18 and 19rc1); `pg_depend` (X: readable; deptype `a` for OWNED BY, `i` for identity); `pg_attrdef` (X: readable; `nextval()` defaults); `pg_attribute` (X: joins work at T0); `pg_statio_user_sequences` (C: block counters only).
- **T0 availability:** partial (X). Visible at T0: name, type, max, increment, cycle. `last_value` is NULL unless the role has SELECT on that sequence. X, with oid joins and no schema grant (18.6 and 19rc1): `pg_sequence_last_value(oid)` and `pg_get_sequence_data(oid)` (18+) return NULL. After `GRANT SELECT ON SEQUENCE` on 19rc1, `pg_sequence_last_value()` returned the value (3). The used fraction needs either a per-sequence SELECT grant (an operator step, read-only) or T1 (`pg_read_all_data`). Never grant USAGE on a sequence: USAGE allows `nextval()`, which advances the sequence persistently, and that breaks the HARD INVARIANT the same way `lo_create` does. The agent never grants anything.
- **PG versions:** 15 to 19rc1 for `pg_sequences`; `pg_get_sequence_data` is 18+ only.
- **Scrape cost / cardinality:** `pg_sequences` is about 4 ms on a small cluster (C). The `pg_depend` and `pg_attrdef` join was not timed (U). Per-sequence series: top-N by used fraction plus `other`. Keep per-sequence detail in facts.
- **Logic:**
  - Effective limit = min(sequence `max_value`, column type maximum: int2 32767, int4 2147483647, int8 9223372036854775807).
  - Used fraction (increment > 0) = (last_value - start) / (effective limit - start).
  - Cycle sequences do not get an exhaustion ratio; report them separately.
  - Link a sequence to a column via `pg_depend` (`a` or `i`) or an `attrdef` `nextval()` default. A sequence with no link is "unlinked" and uses its own max only.
  - The int4-on-bigint mismatch is a T0 finding from types and `max_value` alone, with no `last_value` needed.
- **Pieces:** check `sequence_stats` (database scope); metrics `pg_sequence_used_ratio` (top-N, label schema and sequence), `pg_sequence_remaining_values`; facts `sequence_link` (column, column type, effective limit, linked or unlinked); store facts and metrics; API `GET /api/v1/instances/{id}/sequences` (new, add to openapi); UI: Sequences section in the relations area; advisor rules: `sequence.exhaustion_risk` (proposal: warning at 0.75, critical at 0.90), `sequence.int4_on_bigint` (warning; static, no `last_value` needed), `sequence.last_value_hidden` (info, degraded per sequence).
- **Degrades as:** `sequence last_value not readable: needs SELECT on <schema.sequence>` (advisor degraded finding for that sequence only). Other sequences are still evaluated.

### G05 Trigger monitoring
- **Priority / effort:** P0, S.
- **User value:** a disabled user trigger silently changes write behaviour (audit, denormalisation). A disabled internal RI trigger means the foreign key is not enforced, so data can violate it.
- **Sources:** `pg_trigger` (X: `tgname`, `tgenabled`, `tgisinternal`, `tgrelid` readable at T0 on 18 and 19rc1; C: 19 columns); `pg_proc` through `tgfoid` for the function name (U: join not run at T0).
- **T0 availability:** readable (X, C). RI triggers are visible as `tgisinternal = t` with names `RI_ConstraintTrigger_*`. `tgenabled` values: `O` origin, `D` disabled, `R` replica, `A` always.
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** a few ms (C: 1 to 6 ms). Counts by state and kind per database; detail only for disabled triggers and for trigger functions in the top-N of G06.
- **Pieces:** check `trigger_inventory` (database scope, 5 min); metrics `pg_triggers_total{state,kind}`, `pg_triggers_disabled`; facts `trigger` keyed by `schema.table.trigger` (state, function, internal flag); store facts and metrics; API `GET /api/v1/instances/{id}/triggers` (new); UI: Triggers section in relation detail; advisor rules: `trigger.fk_disabled` (critical: an RI trigger with `tgenabled = 'D'`), `trigger.user_disabled` (warning).
- **Degrades as:** none expected at T0. Zero rows is a valid answer.
- **Notes:** a trigger's runtime cost is visible only through G06 (function stats), not here.

### G06 Function monitoring
- **Priority / effort:** P0, S.
- **User value:** which functions and trigger functions run most and take the most time. Plus two hygiene findings: SECURITY DEFINER functions without a pinned `search_path`, and functions with PUBLIC EXECUTE.
- **Sources:** `pg_stat_user_functions` (C: 6 columns on 15 to 18, 7 on 19 with `stats_reset`; X: query runs at T0 on 18 and 19rc1); `SHOW track_functions` / `pg_settings` (X: `none` by default on 18.6 and 19rc1); `pg_proc` (`prosecdef`, `proconfig`, `proacl`: U, not run at T0).
- **T0 availability:** the view is readable. It returns rows only when `track_functions` is `pl` or `all`. The default is `none` (X), so 0 rows is not success. The check must read the setting and report it.
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** about 3 ms (C). Rows = tracked functions, possibly thousands. Top-N by total time and by calls, plus `other`.
- **Pieces:** check `function_stats` (database scope); metrics `pg_function_calls_total`, `pg_function_total_time_ms_total`, `pg_function_self_time_ms_total` (labels schema and function, top-N); facts for the `pg_proc` hygiene findings (U until verified); store; API `GET /api/v1/instances/{id}/functions` (new); UI: Functions section; advisor rules: `function.untracked` (info, degraded: `track_functions = none`), `function.secdef_no_search_path` (warning, after pg_proc is verified), `function.slow_mean` (info; the same shape as `query.slow_mean`, threshold a proposal).
- **Degrades as:** `track_functions = none` (advisor degraded finding, also shown on the Functions section). Raising `track_functions` is a server setting the operator changes. The agent does not.
- **Notes:** `track_functions = all` adds SQL and C functions and more overhead. Document the cost before recommending it.

### G07 Missing foreign-key index detection
- **Priority / effort:** P0, S.
- **User value:** an FK with no index on the referencing columns makes parent DELETE and UPDATE scan the child table, which causes lock waits and seq scans. This is a common production lock problem.
- **Sources:** `pg_constraint` (X: readable at T0; `contype = 'f'`, `conrelid`, `confrelid`, `conkey`); `pg_index` (X: readable at T0, `indkey`, `indpred`, `indisvalid`, `indexrelid`); `pg_attribute` for column names (X).
- **T0 availability:** readable (X: the FK `child_parent_id_fkey` is visible at T0 on 18 and 19rc1).
- **PG versions:** 15 to 19rc1 (stable catalog).
- **Scrape cost / cardinality:** catalog only; 200 `pg_constraint` rows in the test database. Run every 5 min or 1 h in database scope. One row per unindexed FK, capped at 200 with `other`.
- **Logic:** an FK on column set S is covered when a valid (`indisvalid`), non-partial, non-expression btree index on the child table has S as its leading columns. Invalid indexes do not cover.
- **Pieces:** check `fk_index` (database scope); metrics `pg_fk_unindexed_total` per database; facts `fk_unindexed` keyed by constraint (child, parent, columns, child size if known from `table_stats`); store facts; API: findings or a new `/fk-indexes`; UI: badge in the IndexesSection; advisor rule `index.fk_unindexed` (warning when the child table is over 10 MiB, info otherwise; the threshold is a proposal).
- **Degrades as:** none at T0 (catalog only). `index input unavailable` only if `pg_index` is not readable.

### G08 Index bloat producer and the bloat estimate blind spot
- **Priority / effort:** P0, M.
- **User value:** bloated indexes waste space and slow scans. The `index.bloat_high` rule exists but cannot fire, because `bloat_estimate` emits only `object_kind = 'table'` (inventory §5.3). Also, the table estimate is silently empty for tables the role cannot read.
- **Sources:** the existing estimator (`bloat_estimate.sql`) reads `pg_stats`, `pg_class`, `pg_attribute`, `pg_relation_size`, reloptions. Options for indexes: (a) a catalog estimator on `pg_index` and `pg_stats`, the same blind spot; (b) `pgstatindex` or `pgstattuple_approx`, which need the extension installed by the operator. C §3.1 shows `pgstattuple(regclass)` works at T0 once installed. Index scans are I/O heavy, so keep them on demand, like the existing pgstattuple command.
- **T0 availability:** the table estimate is blind (C §3.2, §5 item 7). `pg_stats` returns fewer rows at T0 (387 vs 406 on PG 15). A 50%-dead table gets no estimate for Tier 0 and no error (about 14.1 MB of bloat for the superuser). The index estimate has the same column-statistics dependency (U, not run).
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** the table estimate runs every 6 h with a 120 s timeout (inventory row 16). The index estimate's cost is not measured (U). Use the same top-N and cap as `table_stats` and `index_stats`.
- **Pieces:** check `bloat_estimate`: add `object_kind = 'index'` rows (the `indexrelname` label already exists); add gauge `pg_bloat_skipped_tables` (count of relations with no readable stats) so the gap is visible; store metrics; API `/api/v1/instances/{id}/bloat` (existing, add index rows); UI bloat section; advisor: a producer for `index.bloat_high` (or remove the rule; decision needed), and `bloat.estimate_blind` (info, degraded, naming the skipped tables).
- **Degrades as:** `bloat estimate blind: no readable column statistics for <schema.table>`. Count the skipped relations, do not drop them silently.

### G09 Index usage windows and unused-rule standby handling
- **Priority / effort:** P0, S.
- **User value:** `index.unused` needs 7 days of the instance's own history. It ignores standby scans in its condition. An index used only by a standby's read queries gets flagged on the primary.
- **Sources:** `pg_stat_user_indexes.last_idx_scan` (X: the column exists on 18 and 19rc1 and is NULL when there was no scan; C §4.1: added in 16); `stats_reset` on per-object views (C §4.1: PG 19); `pg_stat_database.stats_reset` (already used as the reset hint).
- **Logic:**
  - On 16 and later, an index with `last_idx_scan` NULL since the stats reset is a candidate at once. Keep the 7-day rule for 15.
  - Before flagging, check sibling scans using the Siblings snapshot, as `index.divergence` already does (`internal/advisor/rules_relation.go:214` onward).
  - In `rules_relation.go:125` to 136 the condition reads only this instance's history. Standby scans appear in the remediation text only.
- **Do not change:** the duplicate hash. `index_stats.go:120` to 131 hashes everything between the first `(` and the last `)` of `pg_get_indexdef`. That span includes the predicate and INCLUDE columns, so partial and covering indexes do not collide. The method is compared separately. The inventory's wording "column list" understates it.
- **T0 availability:** readable (X, C). No cost change: same view.
- **Pieces:** advisor rule `index.unused` (inputs: Indexes plus Siblings); store `last_idx_scan` as a fact or gauge (`pg_index_last_scan_age_seconds`); API indexes endpoint adds last scan; UI IndexesSection column "last scan".
- **Degrades as:** keep `index usage history is shorter than 7 days` for 15. Add `standby scans unknown: no sibling snapshot` when no sibling data exists.

### G10 Extension inventory; PostGIS presence, version, upgrade
- **Priority / effort:** P0, S.
- **User value:** which extensions are installed in each database, and whether an upgrade is pending. PostGIS is the first use case. Today the inventory has only a pg_stat_statements presence flag (inventory §10).
- **Sources:** `pg_extension` (X: postgis 3.6.4, plpgsql, fuzzystrmatch, postgis_topology, postgis_tiger_geocoder at T0 on 19rc1); `pg_available_extensions` (X: `name`, `default_version`, `installed_version`; postgis default 3.6.4, installed 3.6.4); `postgis_lib_version()` (X: 3.6.4).
- **Upgrade available** = `pg_available_extensions.default_version` newer than `pg_extension.extversion`. Use a dotted-number comparison in Go (U for odd versions).
- **T0 availability:** readable (X).
- **PG versions:** 15 to 19rc1. PostGIS 3.6.4 on the 17 and 19rc1 images (C).
- **Scrape cost / cardinality:** `pg_available_extensions` is the expensive part: 26 to 241 ms in C §1 item 12. Run it at instance scope every 1 h. Use `postgis_lib_version()`, not `postgis_full_version()` (576 to 655 ms first call, C). Extension rows per database: a handful.
- **Pieces:** check `extensions` (instance: `pg_available_extensions`; database: `pg_extension`); metrics `pg_extension_installed{datname,extname,version}`, `pg_extension_upgrade_available{datname,extname}`; facts `extensions` (bounded list per database); store; API `GET /api/v1/instances/{id}/extensions` (new); UI: add a table to InventoryTables; advisor rules `ext.upgrade_available` (info) and `ext.postgis_upgrade_available` (info, PostGIS wording). Fold in the existing pg_stat_statements presence flag.
- **Degrades as:** `missing extension postgis` for PostGIS-specific checks in databases without it. This is a skip reason, not an error.
- **Notes:** never call `postgis_extensions_upgrade()` or any `ALTER EXTENSION`. They write catalog objects (section 6).

### G11 PostGIS column inventory with SRID and type
- **Priority / effort:** P0, M.
- **User value:** the list of spatial columns per database: schema, table, column, geometry or geography, subtype (Point, Polygon), SRID, dimensions. It finds SRID 0 (unknown) and mixed SRIDs.
- **Sources:** `pg_attribute` joined to `pg_type` (`typname` `geometry` or `geography`) (X: join works at T0); `postgis_typmod_srid(atttypmod)` (X: 4326 and 0 returned); `postgis_typmod_type(atttypmod)` (X: `Point`, `Polygon`); `spatial_ref_sys` (X: 8500 rows at T0; use it to check that an SRID exists); `postgis_typmod_dims` (U, not run).
- **T0 availability:** the column catalog is readable (X). `geometry_columns` and `geography_columns` return 0 rows at T0 and 13 and 1 rows for the superuser (C §3.2, X). Do not use the views for T0.
- **PG versions:** 19rc1 verified (X). 17: C ran the same PostGIS image, but the typmod helpers were not run on 17 in this pass (U).
- **Scrape cost / cardinality:** metadata only, no table data. A catalog join over columns: cost not measured (U). Run hourly, only where G10 finds PostGIS. Facts per column, capped at 500 with `Truncated`.
- **Pieces:** check `postgis_columns` (database scope, gated on extension postgis); metrics `pg_postgis_spatial_columns`, `pg_postgis_srid_unknown` per database; facts `spatial_column` keyed by `schema.table.column`; store facts; API `GET /api/v1/instances/{id}/postgis` (new); UI: PostGIS section (new); advisor rules `postgis.srid_unknown` (warning: SRID 0 or not in `spatial_ref_sys`), `postgis.srid_mixed` (info).
- **Degrades as:** `missing extension postgis`.
- **Notes:** table data is never read. The views would show 0 rows at T0, so do not switch to them without a T1 decision (C §5 item 5). ST_IsValid scans are excluded (section 6).

### G12 Spatial columns without a spatial index
- **Priority / effort:** P0, S (after G11).
- **User value:** a geometry or geography column queried with `&&` or `ST_DWithin` and no spatial index is a sequential scan.
- **Sources:** `pg_index`, `pg_am` (X: `gist` visible at T0), `pg_class`, `pg_attribute`.
- **T0 availability:** readable (X on 19rc1). In the test, `geo_pt` has a GiST index and `geo_noidx` and `geog_noidx` do not. Only catalogs were read.
- **Logic:** a column is covered when a valid index (`indisvalid`) on its table with access method `gist`, `spgist` or `brin` has that column as its first key (`indkey[0]`). Treat a partial index as not covering. BRIN is acceptable on correlated data, so report it at info, not warning.
- **Scrape cost / cardinality:** catalog only; same cadence as G11.
- **Pieces:** part of the `postgis_columns` check (same facts); advisor rule `postgis.spatial_unindexed` (warning when `reltuples` > 10,000, info otherwise; threshold a proposal); UI: PostGIS section.
- **Degrades as:** `missing extension postgis` only.
- **Notes:** SP-GiST and BRIN detection were not run (U). Do not add checks that read row data (no `ST_IsValid`, no `ST_Count`).

## 3. P1 parity items

### G13 Multixact age (top of P1)
- **Priority / effort:** P1, S.
- **User value:** multixact wraparound is a separate failure mode from xid wraparound. Inventory §2 lists multixact as absent: no `relminmxid` or `mxid` read anywhere.
- **Sources:** `pg_database.datminmxid` with `mxid_age()` (X: returns 0 at T0 on 18 and 19rc1); `pg_class.relminmxid` (pg_class readable at T0 (X); the column itself U); `pg_get_multixact_stats()` (PG 19; C: T0 EXECUTE true, 1 row); `pg_stat_slru` multixact entries (C: 8 SLRU rows readable; names U).
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** the database value is as cheap as the xid one in `activity` (10 s). Per table: `mxid_age(relminmxid)` in `table_stats` (5 min), top-N as today.
- **Pieces:** activity check gets `pg_max_datminmxid_age`; table_stats gets `pg_table_relminmxid_age`; advisor rules `txn.multixact_wraparound_risk` (critical above 1e9, mirroring `txn.wraparound_risk`) and `table.multixact_wraparound_risk`; a seeded alert rule (proposal) mirroring `txn.wraparound_risk`.
- **Degrades as:** none expected at T0.

### G14 xmin horizon holders
- **Priority / effort:** P1, S.
- **User value:** explains why VACUUM cannot remove dead rows and why `txn.long_running` fires: which session, standby, slot or prepared transaction holds the oldest xmin. AWS and pganalyze both show this (competitors §2.5).
- **Sources:** `pg_stat_activity.backend_xmin` and `backend_xid` (C: 22 columns; visible for other sessions under pg_read_all_stats, the same membership that shows other sessions' query text, X); `pg_replication_slots.xmin` and `catalog_xmin` (C: readable, 2 rows); `pg_stat_replication.backend_xmin` (C: 20 columns, 0 rows here; standby feedback not tested live: U); `pg_prepared_xacts` (already read); `age()` (C).
- **Output:** one fact `xmin_horizon` (holder kind, pid or slot or application, age), plus gauge `pg_xmin_horizon_age_seconds{holder_kind}`. Do not ship query text for this.
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** no extra query; more columns from the activity and replication reads. One fact plus counts per kind.
- **Pieces:** activity and replication checks add columns; facts in store; UI ActivitySection shows the oldest holder; advisor evidence for `txn.long_running` and `vacuum.starvation` names the holder.
- **Degrades as:** `xmin holder unknown: no activity snapshot`.

### G15 DB load by user, application and client
- **Priority / effort:** P1, M.
- **User value:** AWS DB load is sliced by user, host, application and database (competitors §2.1). Application slicing is on Aurora PG only there. pglens can do it on any PostgreSQL. Today the ASH key is `datname`, `wait_event_type`, `wait_event`, `state`, `queryid` (inventory §3).
- **Sources:** `pg_stat_activity` `usename`, `application_name`, `client_addr`, `client_port` (C: columns exist; visible under pg_read_all_stats; per-column visibility U).
- **T0 availability:** readable under pg_read_all_stats (X for other sessions' query text on the same rows).
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** no extra query. Do not multiply the 100-key window by the new dimensions. Add separate top-K rollups per dimension (top 10 plus `other` per window), not a cross product.
- **Privacy:** `usename` and `client_addr` are personal data. Ship them only as labels that the operator opts into. Ship no query text beyond the existing queryid.
- **Pieces:** `internal/ash` sampler and aggregator; `ash_samples` labels; API `/api/v1/ash` (new dimension parameter); UI AshPage dimension selector (user, application, client).
- **Degrades as:** empty `application_name` is shown as `(none)`. Not a degradation.

### G16 Lock analysis: blocking time, session identity, tree-only query text
- **Priority / effort:** P1, S.
- **User value:** AWS lock analysis gives blocking time from `waitstart`, a session id that survives pid reuse, and the root blocker (competitors §2.4). pglens already has a lock tree. Two defects: it ships query text for every client session (2 KB each; inventory §2 row 12, §11), and blocking time is not computed.
- **Sources:** `pg_locks` (C: 16 columns, 10 rows, 1 ms); `pg_stat_activity.backend_start` (C); `pg_blocking_pids()` (already used). `pg_locks.waitstart` (PG 14 and later): the name is U in this review.
- **T0 availability:** readable (C).
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** negligible; `pg_blocking_pids` already runs per blocked session.
- **Pieces:** locks check: fact `session_id` = hex(`backend_start`) plus pid; `blocking_time` from `waitstart` of the first waiter; ship query text only for sessions in `nodes` (blocked sessions and their blockers), not `sessions`; wire `lock_tree` JSON; API `/api/v1/locks` (payload shrinks: a breaking change to document); UI BlockingTree shows blocking time and root blocker; advisor rule `lock.long_wait` (proposal). This also fills the alerting gap noted in inventory §6.
- **Degrades as:** `query text withheld for non-blocking session` (a design note, not a degradation).

### G17 Logical replication: subscriber side and slot statistics
- **Priority / effort:** P1, M.
- **User value:** subscriber lag and errors, and per-slot spill and stream volume (decoding cost). Inventory: no `pg_stat_subscription`, `pg_subscription`, publications, or `pg_stat_replication_slots`.
- **Sources:** `pg_stat_subscription` (X: readable at T0, 0 rows; C: 9 to 11 columns); `pg_stat_subscription_stats` (C: 5 columns on 15 to 17, 12 on 18, 14 on 19; 0 rows); `pg_subscription` named non-secret columns (X: `oid`, `subname`, `subenabled`, `subowner` readable at T0); `pg_stat_replication_slots` (C: 10 columns on 15 to 18, 13 on 19, 1 row; not re-run in this pass); spill and stream column names (U).
- **Never read:** `pg_subscription.subconninfo`. X: a select that includes it returns permission denied at T0. It holds connection credentials. Do not read it, store it, or show it.
- **T0 availability:** readable (X, C). Subscriber rows are 0 here because no subscription could be created without a remote database. Subscriber behaviour is not tested live (U).
- **PG versions:** 15 to 19rc1. 19 renames `sync_error_count` to `sync_seq_error_count` and `sync_table_error_count`: probe the columns the way `io.go` does. pglens does not read the view today (C §4.3).
- **Scrape cost / cardinality:** low; one row per subscription or slot. Subscription count is small.
- **Pieces:** new check `subscriptions` (instance; 0 rows is normal, no gate); metrics `pg_subscription_enabled`, `pg_subscription_errors_total`, `pg_replication_slot_spill_bytes_total`, `pg_replication_slot_stream_bytes_total` (names U); API `/api/v1/clusters/{id}/slots` adds spill and stream; UI SlotsSection; advisor rules `subscription.errors` (warning when errors increase), `slot.spill_high` (info).
- **Degrades as:** 0 rows = no subscription (not degraded). The 19 column split is handled by the column probe.

### G18 Recovery conflicts per reason; standby prefetch; pg_stat_recovery (19)
- **Priority / effort:** P1, S.
- **User value:** standby queries cancelled by recovery conflicts, by reason. Today the only signal is the total counter `pg_conflicts_total` (inventory §2 row 4), and no advisor or alert uses it.
- **Sources:** `pg_stat_database_conflicts` (X: readable at T0, 3 rows; C: 7 to 9 columns); `pg_stat_recovery_prefetch` (X: readable, 1 row; C: 10 columns); `pg_stat_recovery` (PG 19; C: 9 columns, 0 rows on a primary; readable).
- **PG versions:** per-reason columns 15 to 19rc1; `confl_active_logicalslot` is PG 16+ (C §4.1). Per-reason names on 19 were not checked here (U).
- **Scrape cost / cardinality:** negligible. Per-database rows times reasons, bounded by `databases.max` (default 10).
- **Pieces:** `database_stats` adds `pg_conflicts_<reason>_total{database}`; standby-only prefetch counters from `pg_stat_recovery_prefetch`; advisor rule `standby.recovery_conflicts` (proposal: warning when the rate is above 0 for 15 min); alert rule proposal `db.recovery_conflicts`.
- **Degrades as:** on a primary, the standby rules are not evaluated: `role primary not allowed` (the SkipReason format for role gates).

### G19 Progress views: create_index, cluster, copy (repack on 19)
- **Priority / effort:** P1, S.
- **User value:** progress for long CREATE INDEX, CLUSTER, COPY and REPACK. pglens already reads vacuum, analyze and basebackup progress (inventory §10).
- **Sources:** `pg_stat_progress_create_index` (C: 16 columns, 0 rows); `pg_stat_progress_cluster` (C: 12 columns); `pg_stat_progress_copy` (C: 10 to 11 columns); `pg_stat_progress_repack` (PG 19; C: 14 columns).
- **T0 availability:** readable (C, 0 rows in every run). Live testing was done for VACUUM only (C §6). Live tests for create_index and copy are needed before release (U).
- **PG versions:** create_index, cluster and copy 15 to 19rc1; repack 19 only.
- **Scrape cost / cardinality:** 1 to 8 ms each (C). The first 20 jobs per kind, as `vacuum_progress` does, with `Truncated` set above that.
- **Pieces:** extend `vacuum_progress` with a metric family per kind; UI: running-maintenance table in ActivitySection; advisor none at first.
- **Degrades as:** 0 rows is a valid answer.

### G20 Temp file count
- **Priority / effort:** P1, S.
- **User value:** the number of spills, which complements temp bytes (already read). It shows how often work_mem is too small.
- **Sources:** `pg_stat_database.temp_files` (X: readable at T0 on 18 and 19rc1).
- **PG versions:** 15 to 19rc1 (since 9.2).
- **Scrape cost / cardinality:** none; one counter per database.
- **Pieces:** `database_stats` adds counter `pg_temp_files_total{database}`; `config.work_mem_low` and `query.temp_bytes_high` evidence include the count.
- **Degrades as:** none.

### G21 Database size and growth
- **Priority / effort:** P1, S-M.
- **User value:** per-database size and growth. Inventory: no database-size metric. The host disk rule covers the disk, not the database.
- **Sources:** `pg_database_size(oid)` (X: returns about 8 MB at T0 on 18 and 19rc1); `pg_database` rows (C).
- **T0 availability:** readable (X). Whether it needs CONNECT on the database: U. The role connects to each monitored database already.
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** scans every file of the database. Cost not measured (U). Run every 5 min or 1 h. One series per database, bounded by `databases.max`.
- **Pieces:** check `database_size` (instance scope, using the discovery the agent already runs); metric `pg_database_size_bytes{datname}`; API `instances/{id}/databases` adds size; UI databases table; advisor `db.growth_fast` (info, from 7-day history; proposal).
- **Degrades as:** none expected at T0.

### G22 Table vacuum time (18+) and autovacuum scores (19)
- **Priority / effort:** P1, S.
- **User value:** time spent in vacuum and analyze per table (explains starvation). On 19, `pg_stat_autovacuum_scores` shows the score that picks the next table to vacuum.
- **Sources:** `total_vacuum_time`, `total_autovacuum_time`, `total_analyze_time`, `total_autoanalyze_time` in `pg_stat_user_tables` and `pg_stat_all_tables` (C §4.1: 18 and later); `pg_stat_autovacuum_scores` (C: 12 columns, 1,112 rows, 6 ms; PG 19 only).
- **T0 availability:** readable (C).
- **PG versions:** time columns 18 and later; scores 19 and later.
- **Scrape cost / cardinality:** the scores view has one row per relation, the same as `table_stats`. Select top-N by score with `other`. Apply the parent rollup of G03.
- **Pieces:** `table_stats` gets a version-gated column probe; metrics `pg_autovacuum_score{relname}` (top 50, `other`); advisor evidence for `vacuum.starvation`.
- **Degrades as:** `requires PG >= 18` or `requires PG >= 19` (SkipReason format).

### G23 PG 19 views and columns not yet read (umbrella)
- **Priority / effort:** P1, S per row. This table consolidates the PG 19 additions from C §4 that G02 does not cover.

| Object | Status | Used by |
|---|---|---|
| `pg_stat_lock` (5 cols, 12 rows; C) | not read | Lock wait counters by type; feeds G16 and `lock.*` rules |
| `pg_stat_recovery` (9 cols, 0 rows on primary; C) | not read | G18 |
| `pg_get_multixact_stats()` (T0 EXECUTE true; C) | not read | G13 |
| `stats_reset` on per-object views (C §4.1) | not read | G09 windows; `table_stats` reset hint |
| `pg_stat_wal.wal_fpi_bytes` (C §4.1) | already emitted | The wal check emits every column (inventory §2 row 7); verify the name appears |
| `pg_replication_slots.slotsync_skip_reason`, `pg_stat_replication_slots.mem_exceeded_count` and slot-sync columns (C §4.1) | not read | G17 |
| `pg_stat_progress_*` new columns: vacuum `mode`, `started_by`; analyze `started_by`; basebackup `backup_type` (C §4.1) | not read | The vacuum check reads named columns, so it is unaffected; add only if needed |
| `pg_stat_statements` 1.13 `generic_plan_calls`, `custom_plan_calls` (C §4.1) | not read | G26 |
| `pg_dsm_registry_allocations`, `pg_publication_sequences`, `pg_ls_summariesdir` (C) | not needed | None |

- **Degrades as:** `requires PG >= 19, got 18.x` (SkipReason format) on each gated series.

## 4. P2 items (nice to have)

### G24 WAL directory size and file count
- **Priority / effort:** P2, S.
- **User value:** a WAL accumulation signal, which shows slot retention, archive failure, or checkpoint tuning. Today there is only `replication_slot_retained_bytes` and the archiver's ready-file count.
- **Sources:** `pg_ls_waldir()` (C: 4 to 6 rows on 15 to 19, 0.6 to 6 ms). `pg_stat_file` is not an option: its EXECUTE is false at T0 (C §3.3). `pg_ls_logdir` needs log access and is excluded.
- **PG versions:** 15 to 19rc1.
- **Scrape cost / cardinality:** lists the WAL directory (cost grows with `max_wal_size`). Two gauges.
- **Pieces:** `wal` check adds `pg_wal_dir_bytes` and `pg_wal_dir_files`; advisor `wal.dir_growing` (proposal).
- **Degrades as:** none expected.

### G25 SLRU counters
- **Priority / effort:** P2, S.
- **User value:** SLRU pressure (multixact, subtrans, commit_ts, serial) shows as misses and evictions. It supports G13.
- **Sources:** `pg_stat_slru` (X: 8 rows at T0 on 18 and 19rc1; C: 9 columns on 15 to 19). `pg_stat_reset_slru` is excluded.
- **Scrape cost / cardinality:** negligible; one row per SLRU name.
- **Pieces:** new small check `slru` (instance, 60 s); metrics per counter with label `name`. Exact column names: probe (U).
- **Degrades as:** none.

### G26 JIT, pg_stat_statements_info.dealloc, and new pg_stat_statements columns
- **Priority / effort:** P2, S.
- **User value:** JIT time share per query (explains latency spikes); `dealloc` counts evictions, which tells how truncated the top-N is; parallel-worker counters (18); generic and custom plan counts (19).
- **Sources:** `pg_stat_statements` `jit_*` columns (C §4.1 shows `jit_deform_count` and `jit_deform_time` added in 17; the other `jit_*` names: U); `pg_stat_statements_info` (C: 2 columns on 15 to 19; `stats_reset` is read; `dealloc` name: U); `parallel_workers_to_launch` and `parallel_workers_launched` (18; C §4.1); `generic_plan_calls`, `custom_plan_calls` (19; C §4.1).
- **T0 availability:** the view is readable once the extension is installed (C). Installing it is an operator step (section 6).
- **Scrape cost / cardinality:** no extra query; the `stat_statements` check already reads the view.
- **Pieces:** version-gated column probe in `stat_statements`; counters; advisor `query.jit_share` (info, proposal); statements API and UI.
- **Degrades as:** `missing extension pg_stat_statements` (the existing gate); `requires PG >= 18` and `requires PG >= 19`.

## 5. Hygiene and correctness findings (not features)

- **H1** `deploy/sql/monitoring_user.sql`: the header says PG 15 to 18 (C §5 item 1). The "refused by the server" comment is false for large objects (G01). The script must never grant USAGE on a sequence (G04); grant SELECT only where a per-sequence read is wanted. The GRANT lines for `pg_control_system()` and `pg_control_checkpoint()` are redundant on 15 to 19, since PUBLIC already has EXECUTE (C §3.4). Fix with G01.
- **H2** `index.bloat_high` has no producer (inventory §5.3). See G08.
- **H3** `bloat_estimate` is silently empty for tables the role cannot read (C §3.2, §5 item 7). See G08.
- **H4** `replication_receiver`: PG 19 `connecting` status. See G02.
- **H5** ASH wait palette has no `Buffer` type; `BufferPin` is gone on 19rc1. See G02.
- **H6** `pgstattuple` runs at T0 once installed (C §3.1, §5 item 3). The code requires T1 (inventory §9). Decide: keep the code gate at T1 and document the wider DB-level grant, or change the tier model.
- **H7** `locks` ships query text for every client session (inventory §2 row 12). See G16.
- **H8** Leaf partitions use `table_stats`, `index_stats` and `bloat_estimate` top-N slots. See G03.
- **H9** Index duplicate hash: no defect (section G09). Do not change it.
- **H10** `stat_statements` reads `stats_reset` only. `dealloc` is unread (G26).

## 6. Excluded (by rule)

- Stats reset: `pg_stat_reset()`, `pg_stat_reset_shared()`, `pg_stat_reset_slru()`, `pg_stat_statements_reset()` (superuser only, C §3.3). Also the pg_profile `take_sample()` pattern (competitors §3).
- Log access: `pg_ls_logdir` (C: no log directory), log files, log-based slow SQL, auto_explain (a server configuration change; competitors §2.3). `log_*` GUC dependent items.
- File reads: `pg_read_file`, `pg_read_binary_file`, `pg_ls_dir`, `pg_stat_file` (EXECUTE false, C §3.3).
- Extension creation or upgrade by the agent: `CREATE EXTENSION` (pgstattuple, pg_stat_statements, postgis, dblink), `postgis_extensions_upgrade()`, any `ALTER EXTENSION`. These are operator steps.
- Row data: `ST_IsValid` and other PostGIS scans, any SELECT on user tables, and any check that reads user rows (C §5 item 5).
- Untrusted-language helpers: PL/Python `cpu_load`, `psutil_*` (competitors §2.11, §3).
- Credentials: `pg_subscription.subconninfo` (X: denied at T0; never selected).
- Server-state functions, not monitoring: `pg_log_backend_memory_contexts`, `pg_terminate_backend`, `pg_cancel_backend`. EXECUTE is PUBLIC for cancel, but other roles' backends are refused at runtime (C §3.3). Signals belong to the D1 opt-in commands, not this backlog. `pg_shmem_allocations_numa` errors on these images (C §1 item 10); optional only.
- Host metrics from SQL (pgwatch-style): the host sampler covers them.

## 7. Container pass for this backlog (2026-10-23)

- **Containers (local, throwaway):** `pglens-review-gaps-pg18` (postgres:18.6-alpine), `pglens-review-gaps-pg19` (postgres:19rc1-alpine), `pglens-review-gaps-gis19` (postgis/postgis:19rc1-3.6-alpine). The role script `deploy/sql/monitoring_user.sql` returned rc=0 on all three. Probes ran as `pglens` (superuser false, `default_transaction_read_only` on, `track_functions` none). All three containers were removed (`docker ps -a` shows none).
- **Test-only grants (first pass):** USAGE on schema `gaps` for pglens, and `GRANT SELECT ON SEQUENCE gaps.seq_big` on 19rc1. Nothing in a real monitored database was changed. The first pass also showed that text-to-regclass casts fail without schema USAGE.
- **Second pass (oid joins, no schema grant):** containers `pglens-review-gaps-b18` (postgres:18.6-alpine) and `pglens-review-gaps-b19` (postgres:19rc1-alpine). Role script rc=0, seed as postgres, probes as pglens. Schema `gaps` has a NULL ACL (no PUBLIC USAGE). Results: pg_class joins work; `pg_trigger` lists RI and user triggers (`tgenabled` O); `pg_partition_root(oid)` returns `gaps.parent`; `pg_sequence_last_value(oid)` and `pg_get_sequence_data(oid)` return NULL without a grant (18 returns `(,)`, 19 returns `(,,)`, the extra field being `page_lsn`, C §4.3); the `pg_sequence` catalog is readable; `pg_depend` shows deptype `a` (seq_big to t_int) and `i` (identity); `pg_attrdef` shows `nextval('gaps.seq_big'::regclass)`; FK `child_parent_id_fkey` is visible; `pg_stat_user_functions` has 0 rows at track_functions none. The X tags for G03, G04 and G05 come from this pass. Both containers were removed.
- **Probe output:** `/tmp/claude-1000/-mnt-fabio-dati-Git-SperimentazioniAI-postgres-analyze/613631cf-4ae9-42d7-9ca9-e33f7ce4a509/scratchpad/gaps/probe-pg.txt` (first pass only; the later passes printed to the terminal and are summarised here).
- **Results used above (X):** pg_sequences rows and NULL last_value (18, 19); pg_sequence_last_value and pg_get_sequence_data NULL without a sequence grant, value 3 with one (19); pg_sequence catalog readable (18, 19); pg_depend, pg_attrdef and pg_constraint readable (18, 19); pg_trigger readable with RI triggers (18, 19); pg_stat_user_functions empty at track_functions none (18, 19); pg_partition_root and pg_get_expr(relpartbound) readable (18, 19); FK visible (18, 19); last_idx_scan column present (18, 19); pg_database_size and mxid_age readable (18, 19); pg_subscription named columns readable and subconninfo denied (18, 19); pg_stat_database.temp_files, pg_stat_slru, pg_stat_database_conflicts, pg_stat_recovery_prefetch readable (18, 19); wait events (18 BufferPin; 19rc1 Buffer, no BufferPin); PostGIS on 19rc1: pg_extension, pg_available_extensions default and installed versions, postgis_lib_version, geometry_columns 0 rows, pg_attribute and typmod helpers, pg_index and pg_am gist, spatial_ref_sys 8500 rows.
- **Not run:** the new probes on 15, 16, 17 (`pg_get_sequence_data` is 18+). Live subscriptions and standbys. Live progress for create_index, cluster and copy. Any timing of the new queries.

## 8. Open verifications (U)

1. `pg_locks.waitstart` column name (G16).
2. Parsing the range upper bound from `pg_get_expr(relpartbound)` for the "next N days" partition rule (G03).
3. Visibility of `pg_depend` link rows (deptype `a` and `i`) for sequences at T0 (G04). Only counts were run.
4. PostGIS typmod helpers and SP-GiST and BRIN detection on 17 (G11, G12).
5. Cost of `pg_database_size` and whether it needs CONNECT (G21).
6. `pg_stat_replication_slots` spill and stream column names; subscription apply-lag column names (G17).
7. `pg_stat_statements_info.dealloc` and `jit_*` names on 15 and 16 (G26).
8. Per-reason conflict column names on 19 (G18).
9. How the UI folds unknown wait types into `other` (G02). The BufferContent question is answered in G02 item 4.
10. The replication_receiver mapping code for `connecting` (G02).
11. Live progress for create_index, cluster and copy (G19).
12. Standby and subscriber behaviour: standby feedback xmin, recovery conflicts on a real standby, subscriber errors (G14, G17, G18).
13. `pg_roles` and `pg_proc` readability of `prosecdef`, `proconfig`, `proacl` at T0 (G06).
14. Repeat G01 on 15 to 18 (G01).

## 9. Suggested order

1. G01 and H1 (invariant), then G02 (PG 19 support: small, unblocks CI).
2. G03 and G04 (both change the top-N selectors; design them together).
3. G07, G09, G08 (index completeness; G08 also fixes a dead rule).
4. G10, G11, G12 (PostGIS; G10 first, it gates the rest).
5. G05, G06 (triggers and functions).
6. P1 by user value: G13, G14, G16, G15, G17, G18, G19, G20, G21, G22, G23.
7. P2: G24, G25, G26.
