# PostgreSQL monitoring competitors: feature catalog

Snapshot date: 2026-10-23. Branch: review/full-audit-2026-10.
Scope: pganalyze, AWS RDS Performance Insights and CloudWatch Database Insights, pgwatch, Datadog Database Monitoring (Postgres), Percona PMM, pg_profile. A PostgreSQL 19 delta section (section 4) is included because PG 19 support was requested.

Method: vendor and official pages were fetched directly where the URL returned content. Search snippets are tagged low confidence. Pages that returned 404 were not used. Where a vendor page is silent, the table says "not stated".

## Legend

- [V] verified in a page fetched on 2026-10-23. Source IDs (S1 to S38) are listed in section 6.
- [S] search snippet or third-party page only. Low confidence.
- [I] inferred from PostgreSQL behaviour, not a vendor claim. Marked "PG docs / inferred" where it matters.
- "not stated" means the vendor docs do not say.
- pglens fit: OK = read-only catalog or statistics views. HARDEN = needs guardrails (runs user-supplied SQL, reads log text, or touches server state). NO = conflicts with the read-only invariant.

## 1. Access model per product

| Product | Role used by the agent | Objects created in the monitored DB | Superuser | Extensions | Log access |
|---|---|---|---|---|---|
| pganalyze (self-managed) | `pganalyze` role, created by a superuser, `GRANT pg_monitor`, `CONNECTION LIMIT 5`, not SUPERUSER [V S5] | Schema `pganalyze`. SECURITY DEFINER functions `pganalyze.get_column_stats()` (reads pg_stats) and `pganalyze.get_relation_stats_ext()` (reads pg_stats_ext, omits most_common_vals). Optional `pganalyze.reset_stat_statements()` (SECURITY DEFINER, calls `public.pg_stat_statements_reset()`), only when reset mode is on [V S5] | Setup only. Runtime role is not superuser. auto_explain setup: not stated [V S4] | pg_stat_statements (its setup step is not fetched; the reset wrapper depends on it) [V S5] | Collector on the same host reads the log file via `db_log_location`. It runs as the `pganalyze` user [V S9]. The adm group detail comes from an old release note [S]. Required server logging GUCs: not stated in pganalyze pages [V S9] |
| Datadog Postgres | `datadog` role: `CREATE USER`, `GRANT pg_monitor`, `ALTER ROLE datadog INHERIT` on PG 15+ [V S28] | In every database: schema `datadog`, and `datadog.explain_statement(text)`. The function is PL/pgSQL, runs `EXPLAIN (FORMAT JSON)` in a read-only transaction through a cursor, returns NULL for NULL input, and is SECURITY DEFINER [V S28]. Whether search_path is pinned: not stated | Setup as superuser on the primary. Runtime: not stated, pg_monitor is the documented grant [V S28] | pg_stat_statements, created in each database. `track_utility=off` is recommended, which hides EXPLAIN from pg_stat_statements [V S28] | None stated [V S28] |
| Percona PMM 3 | `pmm` user with SUPERUSER (standard PG). On RDS, `GRANT rds_superuser TO pmm`. Superuser is also required on the client host [V S33] | No wrapper functions in the current page. pg_stat_monitor is installed separately. `CREATE EXTENSION pg_stat_statements SCHEMA public` in the postgres DB [V S33] | Yes, recommended and documented [V S33] | pg_stat_monitor (PG 13 to 18) and/or pg_stat_statements [V S33] | Not stated |
| pgwatch (GitHub master; README install example uses v5.0.0) | `pgwatch` role: LOGIN, member of pg_monitor, CONNECT, `EXECUTE` on `pg_stat_file(text)` and `pg_stat_file(text, boolean)` for wal_size. A connection limit of 5 is suggested for critical DBs [V S30] | Helpers for cpu_load, psutil_mem, psutil_disk, wal_size and others. Created with `pgwatch metric print-init <metric> \| psql` or the `--create-helpers` flag, which is not the default. Helpers GRANT EXECUTE to pgwatch. SECURITY DEFINER status: not stated [V S30] | Needed for `CREATE EXTENSION pg_stat_statements` and helper init via psql [V S30] | pg_stat_statements (powers "Stat statements Top"). pgstattuple not mentioned [V S30] | None stated |
| pg_profile | Runs in its own repository DB, which needs `plpgsql` and `dblink`. Optional pg_stat_statements in each monitored DB, and optional pg_stat_kcache [V S35] | Sample and history tables in the pg_profile DB. `take_sample()` calls `pg_stat_statements_reset()` on every sample [V S35] | Not stated [V S35] | dblink (required), pg_stat_statements (optional), pg_stat_kcache (optional) [V S35] | None |
| AWS RDS PI / CloudWatch Database Insights | Managed by RDS. Standard mode is the default [V S14]. Access is controlled by IAM policies (AmazonRDSPerformanceInsightsReadOnly, FullAccess, custom) [V S22] | Not applicable to the customer. DB-side role for RDS PG: not stated | Not stated | Not stated for RDS PG. Aurora plan capture needs `aurora_compute_plan_id` (and `aurora_stat_plans.with_analyze` for actual plans) [V S25] | Slow SQL needs log exports to CloudWatch Logs. Parameters: log_min_duration_statement=1000, log_statement=none, log_destination=stderr [V S20] |

Note: the pganalyze, Datadog and PMM superuser or setup details do not show whether runtime access is read-only at the server level. The PMM superuser requirement is the one that conflicts with the pglens invariant.

## 2. Feature catalog by theme

Columns: feature, who offers it, how it is computed, superuser / log / extension needs, pglens fit.

### 2.1 DB load and wait sampling

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| DB load (average active sessions, AAS) sampled every second, sliced by wait, SQL, host, user, database, application | AWS RDS PI and CloudWatch DBI [V S16, S15] | Each second, the sessions running a query are sampled. Each active session records SQL, state (CPU or waiting), host and user. AAS = total sessions / samples. Source views not named; pg_stat_activity is the likely base [I] | Not stated. No log access. No extension stated | OK. Same shape as ASH sampling (internal/ash) |
| Wait-event breakdown, "two or three events account for 90 percent of load" | AWS [V S16] | pg_stat_activity.wait_event_type / wait_event [I] | None | OK |
| Application slicing (Aurora PG only) | AWS [V S23] | application_name [I] | Not stated | OK |

### 2.2 Query statistics

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Normalised per-query stats: calls, total and mean time, rows, buffers, I/O vs CPU time, cache hit ratio | pganalyze (per-query I/O vs CPU, cache hit ratio) [S10]; Datadog query metrics `postgresql.queries.*`, top 200 normalised queries per host per 10 s by total time, 15-month retention [V S27, S28]; AWS Top SQL and statement stats [V S23]; PMM QAN [V S33]; pgwatch stat_statements dashboards [V S30]; pg_profile statement sections [V S35] | pg_stat_statements | Any role can read the view once it is installed in the DB. Only superusers and pg_read_all_stats members see SQL text and queryid of other users' queries [V S37]. pg_monitor includes pg_read_all_stats [I, PG docs]. Extension: pg_stat_statements, which needs shared_preload_libraries and a restart | OK for reads. Never call pg_stat_statements_reset() (pg_profile does it on each sample; pganalyze's reset mode is optional) |
| Time-bucketed statistics | PMM via pg_stat_monitor [S34] | pg_stat_monitor buckets, configurable count | Extension, PG 13 to 18 [V S33] | OK, but adds a non-core extension |
| Fingerprinting (grouping by normalised query) | pganalyze, fingerprint via pg_query [S11]; Datadog obfuscates bind parameters, but table, column, index and DB names are not obfuscated, and comments may pass through [V S27] | Parsing or normalisation | None | OK. Mirror Datadog's obfuscation note: literals and comments are a data-exposure risk |
| Query text samples | Datadog query samples: who ran what, when, and the user, app or client host [V S26]; PMM query examples via pg_stat_monitor, on by default, can be disabled. pg_stat_statements never collects examples [V S33]; AWS slow SQL from logs [V S20] | Source not stated for Datadog. Likely pg_stat_activity [I]. PMM: pg_stat_monitor | pg_stat_monitor extension for PMM. Text visibility depends on role [I] | OK for pg_stat_activity. Redaction is needed because query text carries literals |

### 2.3 Execution plans

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Plans from slow-query logs via auto_explain (preferred), or log-based fallback | pganalyze [V S4]. auto_explain gives higher-quality plans. The log-based fallback runs plain `EXPLAIN` after the query completes, for queries logged under log_min_duration_statement. It cannot show I/O or buffers. Supabase needs per-role auto_explain settings [V S4] | auto_explain (contrib), loaded through shared_preload_libraries | Superuser: not stated by pganalyze. Loading shared_preload_libraries needs a restart and superuser to set [I]. Logs: yes. Extension: auto_explain | HARDEN. Enabling log_analyze changes server config and makes ordinary queries execute ANALYZE. Keep it off by default |
| On-demand plan via wrapper: `EXPLAIN (FORMAT JSON)` in a read-only transaction | Datadog `explain_statement` [V S28] | SECURITY DEFINER function in each DB. Runs EXPLAIN without ANALYZE in a read-only transaction through a cursor | Setup as superuser. Runtime not stated. No extension | HARDEN. This is the strongest precedent for the D1 path. Caveats: plain EXPLAIN does not execute the statement, but planning can call immutable or stable functions with constant arguments [I, PG docs]. SECURITY DEFINER with user-supplied text needs a pinned search_path (not stated by Datadog) |
| Scheduled plan capture for top-cost queries, retained with DB Insights data | AWS: every five minutes, the most resource-intensive queries get their plans captured. Retention follows the DB Insights setting (7 days default) [V S16]. Aurora PG 14.10 / 15.5 or later with `aurora_compute_plan_id=on` [V S25]. Actual plans need `aurora_stat_plans.with_analyze` [V S25] | Aurora-specific extension and parameters. RDS PG mechanism: not stated | Not stated | Not portable to vanilla PG |
| Plan change over time, plans per digest | Datadog [V S26]; AWS Plans dimension and Plans count per digest [V S25, S23] | Stored plan history | Not stated | OK, if plans are stored |
| EXPLAIN ANALYZE benchmarking with varied parameter values | pganalyze Query Tuning [S11, brochure only] | EXPLAIN ANALYZE executes the statement. A DML statement would run | Not stated | NO by default. Only with a read-only transaction guard and SELECT only. Not verified against docs |
| Plan comparison (structure and per-node metrics) | pganalyze [S11] | Plan diff | None | OK |

### 2.4 Locks and blocking

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Lock tree (root blocker, blocked count, blocking time, high-locking banner) | AWS CloudWatch DBI lock analysis. Covers Aurora PG (all versions) and RDS PG. Requires Advanced mode [V S24] | Snapshots every 15 s from pg_locks + pg_stat_activity. `session_id` = HEX(pg_stat_activity.backend_start) . HEX(pg_locks.pid). `blocked_sessions_count` from the blocked session IDs. `blocking_time` from pg_locks.waitstart of the first waiter. The high-locking banner fires after 15 consecutive minutes of snapshots [V S24] | Not stated. pg_locks is a global view of all cluster locks, with no row or role filter on the page [V S38]. Other sessions' query and state columns in pg_stat_activity are hidden from non-privileged roles [I, PG docs]. No extension | OK. The best reusable recipe in the set |
| Blocking query detection and alerting | pganalyze "Blocking Queries" check [V S7]. Connection tracing finds the root blocking query and can alert when N queries are blocked [S11] | pg_locks + pg_stat_activity [I] | Not stated | OK |
| Common blocking queries per host | Datadog [V S26] | Not stated | Not stated | OK |
| blocking_locks metric | pgwatch [V S31] | pg_locks + pg_stat_activity | None | OK |
| Blocking dimensions in load charts (blocking object, session, SQL) | AWS [V S23] | Derived from the lock snapshots | None | OK |
| Lock waits in logs (Locks category) | pganalyze Log Insights [V S3]. PG 19 turns log_lock_waits on by default [V S36] | Log events | Logs. Server GUC log_lock_waits | OK via log read. PG 19 default raises log volume [I] |

### 2.5 Vacuum, bloat and wraparound

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Autovacuum advisor: insufficient VACUUM frequency, VACUUM blocked by xmin horizon, inefficient index phase, wraparound | pganalyze [V S1]. Enabled by default. Alerts need a rule and destination | Not stated. The page mentions a 35-day statistics retention for the xmin horizon graph [V S1] | Not stated | OK if built from pg_stat_all_tables and pg_class ages [I] |
| Table bloat estimate (heuristic) and pgstattuple to confirm | pganalyze says the estimate "can be incorrect" and recommends pgstattuple. Index bloat is not tracked [V S1] | Heuristic (method not stated). pgstattuple reads the relation [I, PG docs] | pgstattuple extension. Its functions are restricted by default, per PG docs [I] | HARDEN. pgstattuple scans the whole relation. pglens already has an on-demand pgstattuple command (internal/agent, exec_*) |
| VACUUM simulator | pganalyze [V S8] | Not stated | Not stated | OK |
| Transaction ID and multixact ID counters (oldest xid ages, max_used_xact_ids, hot_standby_feedback age) | AWS [V S17]. pganalyze check [V S7]. PG 19 wraparound warnings start at 100M remaining, previously 40M [V S36] | pg_class.relfrozenxid, pg_database.datfrozenxid, backend_xmin, replication slots, prepared transactions [I, PG docs] | None | OK |
| Autovacuum scores | PG 19 view pg_stat_autovacuum_scores [V S36]. pgwatch autovacuum_scores metric [V S31] | Autovacuum scoring | PG 19 only | OK, version-gated |
| Vacuum progress with started_by and mode | PG 19 pg_stat_progress_vacuum new columns [V S36] | Progress views | PG 19 only | OK |

### 2.6 Index advice

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Missing-index recommendations with a ready CREATE INDEX | pganalyze Index Advisor [V S2, S6]. The Indexing Engine runs in pganalyze's SaaS, not on the monitored DB. Inputs: pg_stat_statements workload, table and index definitions and sizes, and schema statistics. Uses a "What If?" analysis that extends the planner, and needs no EXPLAIN samples. Tunes for read-heavy and write-heavy tables. Missing-index detection covered B-Tree and GiST in a 2025 deck [S11, low confidence] | pg_stat_statements, pg_stats via the wrapper, pg_class and pg_index | Not superuser. Column and extended stats wrappers are needed, and skipping them reduces accuracy [V S5]. HypoPG is not named [V S6] | OK for collection. The what-if analysis runs off-host, so it adds no load on the monitored DB |
| Proactive threshold recommendations | AWS [V S21]. Thresholds come from baseline behaviour. Needs Advanced mode and a paid retention period | Not stated | Not stated | OK (analysis layer) |
| Unused and invalid index checks | pganalyze checks: Invalid Indexes, Unused Indexes [V S7]. pgwatch invalid_indexes (pg_index, pg_class, pg_namespace) and index_stats (pg_stat_user_indexes, pg_statio_user_indexes, pg_index) [V S31] | Catalog and statistics views | None | OK |
| Index write overhead, last scan, buffer hits per index | pganalyze schema statistics [V S8] | Not stated | Not stated | OK |
| Index consolidation | pganalyze, early access [V S2] | Not stated | Not stated | OK |

### 2.7 Schema and column statistics

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Table and index size, growth, TOAST, partitions | pganalyze schema statistics [V S8]. pgwatch db_size (pg_database_size, pg_class, pg_stat_user_indexes) and db_size_approx (page counts from pg_class) [V S31] | pg_database_size(), pg_class | Not superuser. The approximate variant avoids a full size scan [I] | OK |
| Column statistics: null_frac, n_distinct, avg_width, correlation | pganalyze `get_column_stats()` reads pg_stats and excludes pg_catalog, information_schema and pg_subscription. SECURITY DEFINER. No table data read [V S5] | pg_stats | Setup as superuser. Runtime through the wrapper | OK. pg_stats only shows tables the caller can SELECT [I, PG docs]. That is why a wrapper is needed |
| Extended statistics | pganalyze `get_relation_stats_ext()` reads pg_stats_ext and omits most_common_vals [V S5] | pg_stats_ext | As above | OK |
| Config, index-definition and change-event hashes | pgwatch configuration_hashes (pg_settings), index_hashes (pg_index, pg_get_indexdef()), change_events [V S31] | pg_settings, pg_get_indexdef() | None | OK |

### 2.8 Logs

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Log ingestion and event classification: 100+ events, categories Server, Connections, WAL and Checkpoints, Maintenance, Locks, Statements, Standby, Constraint Violations, Application Errors. SQLSTATE mapped where one exists | pganalyze Log Insights [V S3] for the categories and SQLSTATE mapping. The 100+ event count comes from the homepage [S10]. Redaction [S10]. Ingest is rate limited [V S3] | Server log (file, syslog or cloud log streaming). Server settings log_line_prefix, log_checkpoints, log_autovacuum_min_duration and others: not stated in pganalyze pages, PG docs [I] | Log read access. No superuser. No extension | HARDEN. Log text contains literals. Redaction must be applied |
| Log file discovery | pganalyze `--discover-log-location` and `db_log_location` [V S9] | Log location | Read access to the log file | OK |
| Slow SQL for RDS PG | AWS: log_min_duration_statement=1000, log_statement=none, log_destination=stderr, exported to CloudWatch Logs. AWS advises masking sensitive data [V S20] | Log events | Parameter group. Superuser not stated | OK |
| Logs by process type | PG 19 log_min_messages accepts type:level [V S36] | Server GUC | PG 19 only | OK |

### 2.9 Sessions and connections

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Active queries, idle-in-transaction, long-running sessions | pganalyze checks: Active Queries, Idle Transactions [V S7]. AWS counters idle_in_transaction_count, idle_in_transaction_max_time, idle_in_transaction_aborted_count, blocked_transactions [V S17]. pgwatch backends (pg_stat_activity) [V S31] | pg_stat_activity | None | OK |
| Connection count, max_connections, auth attempts | AWS numbackends, max_connections, total_auth_attempts [V S17] | pg_stat_database.numbackends, SHOW max_connections | None | OK |
| Breakdown by user, host, application | AWS dimensions User and Host, plus Application on Aurora PG [V S23]. Datadog attributes query samples to user, application or client host [V S26]. PMM groups by client IP through pg_stat_monitor [S34] | pg_stat_activity: usename, application_name, client_addr | None | OK |
| Calling services from application telemetry | AWS, via CloudWatch Application Signals [V S23] | External APM | None | OK, outside the DB |
| Connection tracing over time | pganalyze Connections page [S11] | Not stated | Not stated | Not verified |

### 2.10 Checkpoints, WAL, temp files and I/O

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Checkpoint counts and latency | AWS checkpoints_req, checkpoints_timed, checkpoint_write_time, checkpoint_sync_time. Derived latency = time / (timed + req) [V S17]. pgwatch checkpointer from pg_stat_checkpointer, PG 17+ [V S31] | pg_stat_bgwriter (up to PG 16), pg_stat_checkpointer (PG 17+) | None | OK, version-gated |
| Block read time and read latency | AWS blk_read_time, blks_read, read_latency = blk_read_time / blks_read [V S17]. Datadog lists track_io_timing as an optional setting [V S28]. The pg_profile README recommends it [S] | pg_stat_database. Needs the track_io_timing GUC | Server setting | OK |
| Temporary file volume | AWS temp_bytes, temp_files [V S17] | pg_stat_database | None | OK |
| Archiver counters | AWS archived_count, archive_failed_count [V S17]. pgwatch archiver (pg_stat_archiver) [V S31] | pg_stat_archiver | None | OK |
| WAL directory size | pgwatch wal_size via pg_stat_file on pg_wal, EXECUTE granted to pgwatch [V S30] | pg_stat_file | EXECUTE on pg_stat_file, not granted by default | HARDEN. Scope the file access tightly |
| Per-backend I/O (pg_stat_io, PG 16+) | Not covered by the fetched competitor pages. pglens already has an io check (internal/check/io.go) | pg_stat_io | None | OK |
| PG 19 WAL bytes for full-page images | PG 19 pg_stat_wal and pg_stat_get_backend_wal() [V S36] | WAL stats | PG 19 only | OK, version-gated |

### 2.11 OS and host metrics

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| CPU, memory, disk, network counters (os.* family, per minute) | AWS, from Enhanced Monitoring. Enhanced Monitoring is needed for detail [V S17, S18] | RDS host agent, not from SQL | Not DB-side | Not available from SQL |
| OS process list (CPU %, memory %, VM) | AWS, needs Enhanced Monitoring and the RDSOSMetrics log group [V S23] | RDS host agent | Not DB-side | Not available from SQL |
| Load average, memory and disk through PL/Python helpers | pgwatch cpu_load (os.getloadavg()), psutil_mem, psutil_disk [V S31, S30] | PL/Python functions installed in the monitored DB | Superuser to install. Untrusted language extension [I] | AVOID. This puts untrusted-language code in the monitored DB |
| Out-of-disk-space check | pganalyze Checks, System category [V S7] | Method not stated | Not stated | Not verified |

### 2.12 Settings and configuration review

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Settings checks: disabled fsync, small shared_buffers, stats collection disabled, small work_mem, disabled features | pganalyze Checks, Settings category [V S7] | pg_settings | None | OK |
| Settings and config hash history | pgwatch configuration_hashes [V S31] | pg_settings | None | OK |

### 2.13 Alerts, anomalies and recommendations

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Checks with info, warning and critical severities. Alert rules route to Slack, PagerDuty, email or webhook. Slack and webhooks get every severity, PagerDuty and email get critical only | pganalyze [V S7] | Check engine | Not stated | OK (product pattern) |
| Anomaly and threshold recommendations | AWS proactive recommendations, thresholds from baseline, Advanced mode and paid retention [V S21]. Datadog anomaly alerts on query metrics and a Recommendations view [V S26] | Metric history | Not stated | OK |
| Replication checks: high lag, missing HA follower | pganalyze [V S7] | pg_stat_replication [I] | Not stated | OK |
| Dashboards with alerting | pgwatch Grafana dashboards [V S32]; pgwatch alerting not verified. PMM Grafana not verified | Grafana | Not stated | Not verified |

### 2.14 Historical reports and retention

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Report between two samples: load profile, statement sections, a warning at 90 percent of pg_stat_statements.max | pg_profile [V S35]. Samples by take_sample() on cron. get_report() builds an HTML report between two samples | Delta between samples. take_sample() also calls pg_stat_statements_reset() | dblink extension. Superuser not stated | NO as implemented, because the reset changes shared stats. The report design is reusable with a non-resetting sampler that takes deltas of cumulative counters |
| On-demand performance analysis over a time window | AWS "Analyze Performance" [V S23] | Not stated | Not stated | OK |
| Retention periods | AWS: 7 days default, 1 to 24 months. Advanced mode needs a 465-day PI retention [V S18, S19]. Datadog: query metrics 15 months, query samples 15 days [V S27]. pganalyze: xmin-horizon graph limited by 35-day statistics retention [V S1] | Storage | Not applicable | Note for the retention design |

### 2.15 Custom metrics

| Feature | Who | How computed | Superuser / log / extension | pglens fit |
|---|---|---|---|---|
| Custom SQL metrics | Datadog custom_queries [V S26]. pgwatch custom SQL metrics [S] | User-defined SQL | Depends on the SQL | HARDEN. Arbitrary SQL must run in a read-only transaction under a read-only role |

## 3. Patterns to copy, guard or avoid (against the pglens invariant)

- COPY. Read-only EXPLAIN wrapper (Datadog `explain_statement` [V S28]). It uses EXPLAIN without ANALYZE, a read-only transaction and a cursor, and returns NULL for NULL input. For D1: keep the same default. Add ANALYZE only as an explicit opt-in, and run it inside `SET TRANSACTION READ ONLY` so that DML fails rather than runs [I, PG docs]. Caveat [I]: a read-only transaction does not stop side effects that leave the transaction, such as dblink, postgres_fdw, or functions with external effects. It limits writes to the local database only, so it cannot be the sole control.
- COPY. Role model. pg_monitor covers the query statistics and the lock-tree recipe with no wrapper. pg_locks is a global view, and pg_monitor carries pg_read_all_stats, which unhides other users' query text [V S37, S38; I, PG docs]. Wrappers are needed only for pg_stats and pg_stats_ext (column and extended statistics). pganalyze, Datadog and pgwatch all start from pg_monitor [V S5, S28, S30].
- COPY. Blocking recipe (AWS lock analysis [V S24]). Use pg_locks joined to pg_stat_activity, with a session_id built from backend_start and pid so it survives pid reuse. Snapshot every 15 s. Measure blocking time from pg_locks.waitstart.
- COPY. Off-host index analysis (pganalyze Indexing Engine [V S6]). Index recommendations need no EXPLAIN samples, so no planning work runs on the monitored DB.
- COPY, with guards. Column stats through a narrow wrapper (pganalyze `get_column_stats` and `get_relation_stats_ext` [V S5]). They read pg_stats and pg_stats_ext and no table rows. For pglens: prefer a read-only role with pg_monitor and pg_stat_statements. If a wrapper is needed, make it STABLE, read-only, owned by a non-superuser, with a pinned search_path. None of the vendor pages state the search_path or owner [I, PG docs].
- CAUTION. pganalyze Query Tuning runs EXPLAIN ANALYZE with parameter values [S11]. ANALYZE executes the statement, so DML would run. Not acceptable under the invariant without the read-only guard and a SELECT-only filter. Not verified against docs.
- CAUTION. auto_explain with log_analyze or log_buffers turned on is a server configuration change. Its output goes to logs. Keep it off by default [I].
- CAUTION. pgstattuple scans the whole relation. pganalyze itself says to use it only to confirm bloat [V S1]. Keep it on demand.
- CAUTION. Cancel and terminate. None of the six products in the fetched docs offers on-demand cancel or terminate. There is no vendor precedent. PG docs: pg_cancel_backend and pg_terminate_backend need the pg_signal_backend role or the same role [I, PG docs]. They change server state, not data, so they stay opt-in under D1.
- AVOID. PMM's monitoring user is SUPERUSER, or rds_superuser on RDS [V S33]. A superuser can write data, which breaks "every monitoring role must be read-only". Do not copy.
- AVOID. pg_profile's take_sample() resets pg_stat_statements on every sample [V S35]. This changes shared statistics that other tools read, including pganalyze's optional reset mode. Use deltas instead.
- AVOID. DDL in the monitored DB. pgwatch's `--create-helpers` and init SQL create functions [V S30]. Datadog creates a schema and an extension in every DB during setup [V S28]. For pglens, treat any object creation as an operator step done outside the agent, not something the agent does.
- AVOID. PL/Python helpers (pgwatch cpu_load, psutil_*) put untrusted-language code in the monitored DB [V S31, I].
- NOTE. Datadog recommends track_utility=off, which hides EXPLAIN from pg_stat_statements [V S28]. If pglens adds on-demand EXPLAIN, it will not appear in those statistics.

## 4. PostgreSQL 19 delta affecting this catalog

Status: the PG docs page for 19 says "PostgreSQL 19 RC 1 Released!" dated October 15, 2026, version string 19rc1. The release date is still a placeholder [V S36]. Items may change before the final release. The task's local image postgres:19rc1-alpine is available for verification. It was not run in this step.

| Change in PG 19 (per release notes) | Effect on this catalog and on pglens | Source |
|---|---|---|
| Wait event BUFFERPIN renamed to BUFFER. Queries filtering on the old name must change | A case-insensitive repo search finds "BufferPin" in web/src/components/charts/ash.options.ts (chart category and colour) and in docs/plans/003_plan-Frontend/phase_12.md. No Go code match. Confirm whether the rename changes wait_event_type, wait_event, or both on 19rc1 before changing code | [V S36], repo grep |
| pg_stat_subscription_stats: sync_error_count renamed to sync_table_error_count. New update_deleted and sync_seq_error_count | No repo reference to sync_error_count found | [V S36], repo grep |
| New views: pg_stat_lock with pg_stat_get_lock(), pg_stat_recovery, pg_stat_autovacuum_scores. New function pg_get_multixact_stats() | Candidates for lock and vacuum checks, gated to PG 19 | [V S36] |
| stats_reset column added to pg_stat_all_tables, pg_stat_all_indexes, pg_statio_all_sequences, pg_stat_user_functions, pg_stat_database_conflicts | Per-object reset tracking. Useful for delta sampling | [V S36] |
| pg_stat_progress_vacuum: started_by and mode. pg_stat_progress_analyze: started_by. pg_stat_progress_basebackup: backup_type | Vacuum progress readers see extra columns. Named-column reads are unaffected | [V S36] |
| pg_stat_replication_slots: mem_exceeded_count and slot-sync columns. pg_replication_slots gains slot-sync columns | Replication slot checks can use the new counters | [V S36] |
| pg_stat_wal_receiver.status gains the value connecting | Status mapping breaks only if it is an exhaustive enum | [V S36] |
| pg_stat_wal and pg_stat_get_backend_wal() report WAL bytes for full-page images | WAL checks, gated to PG 19 | [V S36] |
| pg_stats, pg_stats_ext, pg_stats_ext_exprs gain OID columns | Positional SELECTs would break. Named columns are fine | [V S36] |
| pg_stat_statements: FETCH queries grouped by size. New generic and custom plan counts | Changes query normalisation. Exact column names to verify on 19rc1 | [V S36] |
| EXPLAIN: ANALYZE option IO, EXPLAIN (ANALYZE, WAL), Memoize estimates, faster timing via timing_clock_source | Plan parsers see new keys. auto_explain gains log_io and log_extension_options | [V S36] |
| log_autoanalyze_min_duration added. log_autovacuum_min_duration now covers vacuum only | Log parsing rules for autovacuum and autoanalyze lines must be split | [V S36] |
| log_lock_waits enabled by default | More lock-wait log events and more log volume | [V S36] |
| Wraparound warnings start at 100 million remaining (was 40 million) | Log-based alert thresholds change | [V S36] |
| max_locks_per_transaction default 64 becomes 128 | Advisor sizing guidance should be updated | [V S36] |
| Autovacuum: five new score weights. autovacuum_max_parallel_workers and per-table autovacuum_parallel_workers | VACUUM advisor should account for parallel workers | [V S36] |
| MD5 password warnings. password_expiration_warning_threshold | Monitoring role authentication messages | [V S36] |
| effective_wal_level reported by pg_controldata and pg_control_checkpoint() | Optional diagnostic | [V S36] |

Competitor PG 19 status, as fetched:
- pgwatch metrics.yaml has PG 19-keyed SQL, and its autovacuum_scores metric reads pg_stat_autovacuum_scores [V S31].
- pg_profile README lists PostgreSQL 12 to 19 [V S35].
- Datadog self-hosted lists PostgreSQL 9.6 to 18 [V S28].
- PMM pg_stat_monitor lists PG 13 to 18 [V S33].
- pganalyze, AWS and Percona: PG 19 support is not stated in the fetched pages.

## 5. Gaps and next steps

- Not fetched or not found: pganalyze Query Performance page, pganalyze Connection tracing page, pganalyze Log Insights redaction, pganalyze check threshold values, pgwatch full preset list (a conference slide mentions 15 presets [S]), AWS RDS PG execution plan mechanism, Datadog query-sample source views.
- Not verified: pganalyze's auto_explain settings and superuser needs; pganalyze's bloat method; whether any vendor pins search_path in SECURITY DEFINER functions.
- PMM 3 monitoring-user details come from one PMM 3 page. Versions quoted in third-party posts are low confidence.
- PG 19 items: confirm wait-event naming, pg_stat_lock columns and the pg_stat_statements plan-count column names against a 19rc1 container. Not done here.
- Repo check: the tokensave search tool refused, because the served branch is main and the working tree is review/full-audit-2026-10. The grep for PG 19 names used fixed strings across the repo, excluding .git.

## 6. Sources

Fetched in this step (content returned):
- S1 https://pganalyze.com/docs/vacuum-advisor
- S2 https://pganalyze.com/docs/index-advisor
- S3 https://pganalyze.com/docs/log-insights
- S4 https://pganalyze.com/docs/explain
- S5 https://pganalyze.com/docs/install/self_managed/01_create_monitoring_user
- S6 https://pganalyze.com/docs/indexing-engine
- S7 https://pganalyze.com/docs/checks
- S8 https://pganalyze.com/docs/schema-statistics
- S4b https://pganalyze.com/docs/explain/setup (log-based plain EXPLAIN, auto_explain preferred, Supabase per-role note)
- S9 https://pganalyze.com/docs/log-insights/setup/self-managed/02_configure_collector
- S13 https://pganalyze.com/docs/query-advisor (landing page only)
- S14 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_DatabaseInsights.html
- S15 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_PerfInsights.html
- S16 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_PerfInsights.Overview.ActiveSessions.html
- S17 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_PerfInsights_Counters.html
- S18 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_PerfInsights.Enabling.html
- S19 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_DatabaseInsights.Considerations.html
- S20 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_DatabaseInsights.SlowSQL.html
- S21 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_PerfInsights.InsightsRecommendationViewDetails.html
- S22 https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_PerfInsights.access-control.html
- S23 https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Database-Insights-Database-Instance-Dashboard.html
- S24 https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Database-Insights-Lock-Analysis.html
- S25 https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Database-Insights-Execution-Plans.html
- S26 https://docs.datadoghq.com/database_monitoring/
- S27 https://docs.datadoghq.com/database_monitoring/data_collected/
- S28 https://docs.datadoghq.com/database_monitoring/setup_postgres/selfhosted/
- S30 https://github.com/cybertec-postgresql/pgwatch/blob/master/docs/tutorial/preparing_databases.md
- S31 https://github.com/cybertec-postgresql/pgwatch/tree/master/internal/metrics/metrics.yaml (first part of the file only)
- S32 https://github.com/cybertec-postgresql/pgwatch (README, partial)
- S33 https://docs.percona.com/percona-monitoring-and-management/3/install-pmm/install-pmm-client/connect-database/postgresql.html
- S35 https://github.com/zubkov-andrei/pg_profile
- S36 https://www.postgresql.org/docs/19/release-19.html
- S37 https://www.postgresql.org/docs/current/pgstatstatements.html
- S38 https://www.postgresql.org/docs/current/view-pg-locks.html

Search snippets only (low confidence, not fetched):
- S10 https://pganalyze.com/ (homepage claims: query trends, I/O vs CPU, cache hit ratio, redaction)
- S11 https://resources.pganalyze.com/pganalyze_in_Action_2025.pdf (2025 feature brochure: fingerprinting, Query Tuning, plan diff, lock monitoring, B-Tree and GiST)
- S34 https://docs.percona.com/percona-monitoring-and-management/2/release-notes/2.36.0.html (PMM 2.36 added pg_stat_monitor 2.0 support)

Not used as sources (HTTP 404): pganalyze install/01_setup_the_monitoring_user, pganalyze docs/collector/setup, pgwatch.readthedocs.io preparing_databases page.

PostgreSQL-behaviour claims marked [I] are not vendor statements. They are based on the PostgreSQL documentation and general knowledge, and were not fetched in this step.
