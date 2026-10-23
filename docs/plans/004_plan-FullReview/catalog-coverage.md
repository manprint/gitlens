# PostgreSQL catalog coverage for pglens monitoring (PostgreSQL 15 to 19rc1)

Scope: the Tier 0 monitoring role created by `deploy/sql/monitoring_user.sql`, checked on PostgreSQL 15, 16, 17, 18 and 19rc1, plus PostGIS 3.6 on 17 and 19rc1. Measured on local throwaway containers on 2026-10-23. Only this file was written to the repository. The containers were removed at the end.

## 1. Summary

1. **PostgreSQL 19rc1 is supported by the Tier 0 role as it stands.** `monitoring_user.sql` applies with exit code 0 on 19rc1 and on both PostGIS images. Every monitoring view that pglens reads exists on 19rc1, and no column pglens references is removed or renamed in 19. The 141 (view, column) pairs listed from the queries in `internal/check`, `internal/agent`, `internal/ash` and `cmd/pglens-agent` all exist on 15 to 19, given the version gates the code already has. The script comment that says "15-18" is out of date. The file was not edited because it is outside this task's scope.
2. **New monitoring views in 19rc1** are `pg_stat_autovacuum_scores`, `pg_stat_lock`, `pg_stat_progress_repack` and `pg_stat_recovery`. All four are readable at Tier 0. pglens does not read them yet. Section 4 has the full diff, including the 41 new and 31 removed function names in 19rc1 (section 4.3).
3. **Write guard (HARD INVARIANT): not complete.** With the default setting, every write attempt failed. The session can switch the setting off (`SET default_transaction_read_only = off` succeeds). With it off, the role still cannot UPDATE a table, CREATE TABLE in `public`, or run `EXPLAIN ANALYZE` on a write. It can, however, `CREATE TEMP TABLE` (session-local), and it can call `lo_create()` or `lo_from_bytea()`. Those two write **persistent large objects** into the monitored database, owned by `pglens` and visible in `pg_largeobject_metadata`. Verified on 19rc1 in a throwaway container. pglens's own code makes no such call (section 5, item 8).
4. **Finding: the write guard is a session setting plus a privilege model with a gap.** The comment in `monitoring_user.sql` says a write "is refused by the server even if a client ever tried one". That holds for the default setting and for tables. It does not hold for large objects, because PUBLIC can execute `lo_create` and `lo_from_bytea`, or for temp tables, which are session-local. Fix, tested on 19rc1: `REVOKE EXECUTE ON FUNCTION lo_create(oid), lo_from_bytea(oid, bytea) FROM PUBLIC` and `REVOKE TEMPORARY ON DATABASE <db> FROM PUBLIC` in each monitored database. After that, both large-object calls and the temp table are refused with `permission denied`, and read queries still work (section 5, item 8).
5. **Finding: the `pg_control_*` GRANT lines are redundant on 15 to 19.** The comment says these functions are "restricted to superusers by default". Their ACLs say otherwise: `pg_control_system()` and `pg_control_checkpoint()` have `=X` (PUBLIC) plus the explicit grant, and `pg_control_init()` and `pg_control_recovery()` have a NULL ACL, which means PUBLIC EXECUTE. All four return rows to the monitoring role. The grants are harmless but do not do what the comment says.
6. **Finding: `pgstattuple` already runs at Tier 0.** `pg_monitor` includes `pg_stat_scan_tables`, and that role has EXECUTE on `pgstattuple(text)` and `pgstattuple(regclass)`. Once the DBA installs the extension, the Tier 0 role can run it on tables it cannot SELECT from. It returns physical statistics only, not row values. The only gate is the extension itself.
7. **Finding: some functions have PUBLIC EXECUTE but are refused at runtime.** `pg_cancel_backend`, `pg_logical_slot_peek_changes`, `pg_drop_replication_slot`, `pg_logical_slot_get_changes` and `pg_replication_slot_advance` are executable by PUBLIC. The server refused cancel of a superuser backend, and peek and drop of a slot, with permission errors. The role has no REPLICATION attribute. `get_changes` and `advance` were not executed, because they would change slot state.
8. **Hidden data at Tier 0.** `pg_sequences.last_value` is NULL unless the role has SELECT or USAGE on the sequence (the superuser sees 2). `pg_stats` returns fewer rows (387 against 406 on PG15) because it filters by column privilege. `geometry_columns` and `geography_columns` return zero rows to the monitoring role, while the superuser sees 13 and 1 rows (both PostGIS images). The view filters by table SELECT privilege, and `pg_read_all_data` (Tier 1) shows them.
9. **Other sessions are visible.** Query text from other roles is visible in `pg_stat_activity` and `pg_stat_statements`. A live VACUUM started by another user appeared in `pg_stat_progress_vacuum`.
10. **Environment limit.** `pg_shmem_allocations_numa` (PG18 and later) errors for everyone on these images: "libnuma initialization failed or NUMA is not supported on this platform". The agent must treat it as optional.
11. **Column discovery is safe.** `information_schema.columns` returns the same columns as `pg_attribute` at Tier 0 for every monitored view. The `io.go`, `wal.go` and `vacuum_progress.go` discovery therefore works at this tier.
12. **Cost, single run on an idle cluster with about 1,000 user tables:** most views take 1 to 10 ms. Exceptions: `pg_available_extensions` 2 to 241 ms (it reads control files), `pg_stats` 15 to 195 ms (scales with column count), `pg_stat_user_tables` 7 to 110 ms (scales with relations), `pg_stat_activity` 2 to 97 ms, `pg_ls_waldir()` up to 6 ms. The timings are noisy and should be read as orders of magnitude.
13. **Not covered by a live test:** subscriptions (no remote database was allowed, so `pg_stat_subscription*` are empty), a real standby (`pg_stat_replication` and `pg_stat_wal_receiver` are empty), `pg_stat_progress_create_index`, `cluster`, `copy` and `basebackup` (no live run), and `pg_stat_recovery` (no recovery). For these the result is "readable, 0 rows", checked by count only.
14. **Finding: the bloat estimate is silently empty for tables Tier 0 cannot read.** `bloat_estimate.sql` reads `pg_stats`, which hides columns the role cannot SELECT. On 19rc1, a table with 50% dead rows gets an estimate for the superuser (about 14.1 MB of bloat) and none for Tier 0. The role does get an estimate for a table it was granted SELECT on. No error is raised.

## 2. Environment and method

| Container (`pglens-review-cov-`) | Image | Server version | pg_stat_statements | pgstattuple | PostGIS |
|---|---|---|---|---|---|
| `pg15` | `postgres:15.19-alpine` | PostgreSQL 15.19 | 1.10 | 1.5 | none |
| `pg16` | `postgres:16.15-alpine` | PostgreSQL 16.15 | 1.10 | 1.5 | none |
| `pg17` | `postgres:17.11-alpine` | PostgreSQL 17.11 | 1.11 | 1.5 | none |
| `pg18` | `postgres:18.6-alpine` | PostgreSQL 18.6 | 1.12 | 1.5 | none |
| `pg19rc1` | `postgres:19rc1-alpine` | PostgreSQL 19rc1 | 1.13 | 1.5 | none |
| `postgis17` | `postgis/postgis:17-3.6-alpine` | PostgreSQL 17.11 | 1.11 | 1.5 | 3.6.4 |
| `postgis19rc1` | `postgis/postgis:19rc1-3.6-alpine` | PostgreSQL 19rc1 | 1.13 | 1.5 | 3.6.4 |

- **Second set (verification, removed afterwards):** `pglens-review-cov2-pg19rc1`, `pglens-review-cov2-postgis17`, `pglens-review-cov2-postgis19rc1`, same images and the same role script. Used for the bloat fixture, the temporary `pg_read_all_data` test on PostGIS, and the function and view checks in sections 3.6 and 4.3.
- **Large-object and privilege tests (19rc1 only):** `pglens-review-lo-pg19rc1`, `pglens-review-lo2-pg19rc1`, `pglens-review-lo3-pg19rc1`, same role script. Each one was removed after its test.
- **Start options (test only):** `shared_preload_libraries=pg_stat_statements`, `max_prepared_transactions=5`, `wal_level=logical`, `max_replication_slots=10`, `track_io_timing=on`, `track_functions=all`. None of these changes what a monitoring role can read.
- **Role:** `psql -v pglens_password=x -v dbname=postgres -f deploy/sql/monitoring_user.sql` as `postgres`, on every container. Exit code 0 everywhere. Then all probes connected as `pglens` over TCP with a password.
- **Identity check before every probe run** (the row shows user, superuser flag, read-only default, statement_timeout, and role memberships). Values were the same on every container:
  - user `pglens`, superuser `f`
  - `default_transaction_read_only = on`, `statement_timeout = 15s`
  - member of `pg_monitor` and `pg_read_all_stats`; not a member of `pg_read_all_data` or `pg_signal_backend`
  - CONNECT on `pglens_scratch` is granted
- **Seed data** (as `postgres`, for hidden-value tests): a 1,000-row table `review_t` with SELECT granted only to `review_app`; sequence `review_seq` with two `nextval` calls; partitioned table `review_part` with a trigger; PL/pgSQL function `review_fn` called ten times; prepared transaction `review_gid`; 1,000 empty tables for the cost tests; `review_big` (300,000 rows, half deleted) for a live VACUUM; PostGIS tables `review_geo` and `review_geog` on the PostGIS images; a physical slot `review_psl` and a logical slot `review_lsl` (test_decoding); `pg_stat_statements` in `postgres` and `pglens_scratch`; `pgstattuple` in `postgres`.
- **Background activity:** a session `pg_sleep(120)` with `application_name = review-other`; a VACUUM with cost delay, observed in `pg_stat_progress_vacuum`; a query run under `SET ROLE review_app`.
- **Column data:** from `pg_attribute`, `pg_proc` (with `proacl`), `pg_auth_members` and `pg_class.relacl`, read as the superuser. Diffs are by column name and type, not by name alone.
- **Code references:** `tokensave` was refused for this session (the server serves `main`, the working tree is `review/full-audit-2026-10`), so the references were found with grep over `internal/` and `cmd/`.

## 3. Tier 0 role: what it can read

### 3.1 Coverage matrix

Cell format: `columns · rows seen by pglens · time of the pglens count query`. `(su N)` is added when the superuser sees a different row count. `—` means the object does not exist in that version. `n/a` means the extension is not installed. Column counts for `pg_wait_events` and `pg_aios` were not captured. Every value is a single run.

| Object | pg15 | pg16 | pg17 | pg18 | pg19rc1 | pg17 + PostGIS | pg19rc1 + PostGIS |
|---|---|---|---|---|---|---|---|
| **Sessions, locks, replication** | | | | | | | |
| `pg_stat_activity` | 22 col · 8 rows · 4.6 ms | 22 col · 8 rows · 3.9 ms | 22 col · 8 rows · 2.2 ms | 22 col · 11 rows · 2.5 ms | 22 col · 10 rows · 3.3 ms | 22 col · 8 rows · 2.2 ms | 22 col · 10 rows · 97.4 ms |
| `pg_locks` | 16 col · 10 rows · 1.4 ms | 16 col · 10 rows · 2.0 ms | 16 col · 10 rows · 1.0 ms | 16 col · 10 rows · 1.1 ms | 16 col · 10 rows · 1.0 ms | 16 col · 10 rows · 1.0 ms | 16 col · 10 rows · 1.3 ms |
| `pg_stat_ssl` | 8 col · 3 rows · 1.0 ms | 8 col · 3 rows · 0.8 ms | 8 col · 3 rows · 1.2 ms | 8 col · 3 rows · 1.7 ms | 8 col · 3 rows · 0.8 ms | 8 col · 3 rows · 1.4 ms | 8 col · 3 rows · 1.3 ms |
| `pg_stat_gssapi` | 4 col · 3 rows · 0.9 ms | 5 col · 3 rows · 1.5 ms | 5 col · 3 rows · 1.0 ms | 5 col · 3 rows · 1.1 ms | 5 col · 3 rows · 1.7 ms | 5 col · 3 rows · 1.0 ms | 5 col · 3 rows · 1.7 ms |
| `pg_stat_replication` | 20 col · 0 rows · 4.0 ms | 20 col · 0 rows · 4.8 ms | 20 col · 0 rows · 67.9 ms | 20 col · 0 rows · 5.2 ms | 20 col · 0 rows · 27.2 ms | 20 col · 0 rows · 29.0 ms | 20 col · 0 rows · 2.8 ms |
| `pg_stat_wal_receiver` | 15 col · 0 rows · 1.1 ms | 15 col · 0 rows · 1.4 ms | 15 col · 0 rows · 0.8 ms | 15 col · 0 rows · 1.2 ms | 15 col · 0 rows · 0.7 ms | 15 col · 0 rows · 0.7 ms | 15 col · 0 rows · 1.1 ms |
| `pg_stat_subscription` | 9 col · 0 rows · 31.7 ms | 10 col · 0 rows · 31.2 ms | 11 col · 0 rows · 25.9 ms | 11 col · 0 rows · 13.0 ms | 11 col · 0 rows · 48.6 ms | 11 col · 0 rows · 26.6 ms | 11 col · 0 rows · 8.6 ms |
| `pg_stat_subscription_stats` | 5 col · 0 rows · 1.0 ms | 5 col · 0 rows · 1.2 ms | 5 col · 0 rows · 1.0 ms | 12 col · 0 rows · 1.1 ms | 14 col · 0 rows · 21.6 ms | 5 col · 0 rows · 1.5 ms | 14 col · 0 rows · 7.1 ms |
| `pg_stat_replication_slots` | 10 col · 1 row · 2.3 ms | 10 col · 1 row · 3.0 ms | 10 col · 1 row · 2.1 ms | 10 col · 1 row · 3.9 ms | 13 col · 1 row · 3.3 ms | 10 col · 1 row · 3.5 ms | 13 col · 1 row · 2.5 ms |
| `pg_replication_slots` | 15 col · 2 rows · 2.3 ms | 16 col · 2 rows · 2.5 ms | 20 col · 2 rows · 2.6 ms | 21 col · 2 rows · 1.9 ms | 22 col · 2 rows · 2.5 ms | 20 col · 2 rows · 2.1 ms | 22 col · 2 rows · 2.1 ms |
| `pg_prepared_xacts` | 5 col · 1 row · 2.7 ms | 5 col · 1 row · 2.3 ms | 5 col · 1 row · 2.4 ms | 5 col · 1 row · 2.3 ms | 5 col · 1 row · 3.7 ms | 5 col · 1 row · 2.7 ms | 5 col · 1 row · 2.2 ms |
| **Progress (pg_read_all_stats view of other sessions)** | | | | | | | |
| `pg_stat_progress_vacuum` | 11 col · 1 row · 2.2 ms | 11 col · 1 row · 2.2 ms | 14 col · 1 row · 2.1 ms | 15 col · 1 row · 2.2 ms | 17 col · 1 row · 4.3 ms | 14 col · 1 row · 3.6 ms | 17 col · 1 row · 2.9 ms |
| `pg_stat_progress_analyze` | 12 col · 0 rows · 1.7 ms | 12 col · 0 rows · 1.9 ms | 12 col · 0 rows · 1.9 ms | 13 col · 0 rows · 3.5 ms | 14 col · 0 rows · 1.9 ms | 12 col · 0 rows · 2.7 ms | 14 col · 0 rows · 2.4 ms |
| `pg_stat_progress_cluster` | 12 col · 0 rows · 4.5 ms | 12 col · 0 rows · 23.9 ms | 12 col · 0 rows · 7.7 ms | 12 col · 0 rows · 8.2 ms | 12 col · 0 rows · 7.7 ms | 12 col · 0 rows · 4.0 ms | 12 col · 0 rows · 4.3 ms |
| `pg_stat_progress_create_index` | 16 col · 0 rows · 5.7 ms | 16 col · 0 rows · 3.5 ms | 16 col · 0 rows · 2.1 ms | 16 col · 0 rows · 13.7 ms | 16 col · 0 rows · 1.8 ms | 16 col · 0 rows · 2.4 ms | 16 col · 0 rows · 3.4 ms |
| `pg_stat_progress_copy` | 10 col · 0 rows · 2.8 ms | 10 col · 0 rows · 1.7 ms | 11 col · 0 rows · 17.6 ms | 11 col · 0 rows · 2.5 ms | 11 col · 0 rows · 8.2 ms | 11 col · 0 rows · 24.3 ms | 11 col · 0 rows · 2.9 ms |
| `pg_stat_progress_basebackup` | 6 col · 0 rows · 12.2 ms | 6 col · 0 rows · 9.5 ms | 6 col · 0 rows · 1.0 ms | 6 col · 0 rows · 1.0 ms | 7 col · 0 rows · 4.4 ms | 6 col · 0 rows · 1.2 ms | 7 col · 0 rows · 2.1 ms |
| `pg_stat_progress_repack` | — | — | — | — | 14 col · 0 rows · 1.8 ms | — | 14 col · 0 rows · 3.2 ms |
| **Tables, indexes, sequences, functions** | | | | | | | |
| `pg_stat_all_tables` | 23 col · 1114 rows · 15.6 ms | 26 col · 1114 rows · 10.7 ms | 26 col · 1114 rows · 31.9 ms | 30 col · 1113 rows · 22.3 ms | 31 col · 1113 rows · 13.5 ms | 26 col · 1178 rows · 27.9 ms | 31 col · 1177 rows · 8.4 ms |
| `pg_stat_user_tables` | 23 col · 1004 rows · 12.4 ms | 26 col · 1004 rows · 109.8 ms | 26 col · 1004 rows · 27.9 ms | 30 col · 1004 rows · 9.4 ms | 31 col · 1004 rows · 7.3 ms | 26 col · 1043 rows · 20.9 ms | 31 col · 1043 rows · 15.6 ms |
| `pg_stat_sys_tables` | 23 col · 110 rows · 10.2 ms | 26 col · 110 rows · 7.6 ms | 26 col · 110 rows · 10.9 ms | 30 col · 109 rows · 8.2 ms | 31 col · 109 rows · 7.4 ms | 26 col · 135 rows · 48.0 ms | 31 col · 134 rows · 7.2 ms |
| `pg_stat_xact_user_tables` | 11 col · 1004 rows · 8.3 ms | 12 col · 1004 rows · 23.2 ms | 12 col · 1004 rows · 9.2 ms | 12 col · 1004 rows · 10.2 ms | 12 col · 1004 rows · 9.5 ms | 12 col · 1043 rows · 7.3 ms | 12 col · 1043 rows · 11.2 ms |
| `pg_stat_all_indexes` | 8 col · 167 rows · 4.7 ms | 9 col · 169 rows · 4.6 ms | 9 col · 169 rows · 13.4 ms | 9 col · 168 rows · 9.3 ms | 10 col · 168 rows · 15.2 ms | 9 col · 268 rows · 10.2 ms | 10 col · 267 rows · 8.7 ms |
| `pg_stat_user_indexes` | 8 col · 3 rows · 14.2 ms | 9 col · 3 rows · 7.1 ms | 9 col · 3 rows · 7.0 ms | 9 col · 3 rows · 6.5 ms | 10 col · 3 rows · 9.1 ms | 9 col · 77 rows · 5.9 ms | 10 col · 77 rows · 9.5 ms |
| `pg_stat_sys_indexes` | 8 col · 164 rows · 6.1 ms | 9 col · 166 rows · 5.3 ms | 9 col · 166 rows · 5.0 ms | 9 col · 165 rows · 6.5 ms | 10 col · 165 rows · 5.5 ms | 9 col · 191 rows · 4.8 ms | 10 col · 190 rows · 6.9 ms |
| `pg_statio_all_tables` | 11 col · 1113 rows · 4.8 ms | 11 col · 1113 rows · 5.7 ms | 11 col · 1113 rows · 8.1 ms | 11 col · 1112 rows · 3.9 ms | 12 col · 1112 rows · 6.7 ms | 11 col · 1177 rows · 9.7 ms | 12 col · 1176 rows · 4.7 ms |
| `pg_statio_user_tables` | 11 col · 1003 rows · 6.1 ms | 11 col · 1003 rows · 4.6 ms | 11 col · 1003 rows · 4.9 ms | 11 col · 1003 rows · 4.5 ms | 12 col · 1003 rows · 6.6 ms | 11 col · 1042 rows · 7.7 ms | 12 col · 1042 rows · 4.8 ms |
| `pg_statio_user_indexes` | 7 col · 3 rows · 6.6 ms | 7 col · 3 rows · 8.2 ms | 7 col · 3 rows · 5.5 ms | 7 col · 3 rows · 5.4 ms | 8 col · 3 rows · 7.3 ms | 7 col · 77 rows · 6.9 ms | 8 col · 77 rows · 6.1 ms |
| `pg_statio_user_sequences` | 5 col · 1 row · 4.6 ms | 5 col · 1 row · 4.0 ms | 5 col · 1 row · 7.3 ms | 5 col · 1 row · 3.3 ms | 6 col · 1 row · 6.3 ms | 5 col · 18 rows · 3.9 ms | 6 col · 18 rows · 4.8 ms |
| `pg_sequences` | 11 col · 1 row · 3.5 ms | 11 col · 1 row · 3.8 ms | 11 col · 1 row · 3.6 ms | 11 col · 1 row · 3.6 ms | 11 col · 1 row · 3.5 ms | 11 col · 18 rows · 15.0 ms | 11 col · 18 rows · 5.9 ms |
| `pg_stat_user_functions` | 6 col · 5 rows · 3.4 ms | 6 col · 5 rows · 3.5 ms | 6 col · 5 rows · 4.3 ms | 6 col · 5 rows · 2.8 ms | 7 col · 5 rows · 3.5 ms | 6 col · 34 rows · 3.8 ms | 7 col · 34 rows · 5.3 ms |
| `pg_stat_xact_user_functions` | 6 col · 0 rows · 3.1 ms | 6 col · 0 rows · 3.3 ms | 6 col · 0 rows · 3.2 ms | 6 col · 0 rows · 4.1 ms | 6 col · 0 rows · 3.2 ms | 6 col · 0 rows · 3.2 ms | 6 col · 0 rows · 3.7 ms |
| **Database, I/O, WAL, checkpoints, archiver** | | | | | | | |
| `pg_stat_database` | 28 col · 5 rows · 2.6 ms | 28 col · 5 rows · 2.7 ms | 28 col · 5 rows · 2.1 ms | 30 col · 5 rows · 1.8 ms | 30 col · 5 rows · 2.3 ms | 28 col · 6 rows · 2.4 ms | 30 col · 6 rows · 1.9 ms |
| `pg_stat_database_conflicts` | 7 col · 4 rows · 1.5 ms | 8 col · 4 rows · 1.3 ms | 8 col · 4 rows · 1.2 ms | 8 col · 4 rows · 1.5 ms | 9 col · 4 rows · 1.7 ms | 8 col · 5 rows · 4.2 ms | 9 col · 5 rows · 1.6 ms |
| `pg_stat_bgwriter` | 11 col · 1 row · 1.0 ms | 11 col · 1 row · 1.0 ms | 4 col · 1 row · 0.8 ms | 4 col · 1 row · 0.7 ms | 4 col · 1 row · 0.8 ms | 4 col · 1 row · 1.0 ms | 4 col · 1 row · 1.3 ms |
| `pg_stat_checkpointer` | — | — | 9 col · 1 row · 0.9 ms | 11 col · 1 row · 0.8 ms | 11 col · 1 row · 0.9 ms | 9 col · 1 row · 0.9 ms | 11 col · 1 row · 1.1 ms |
| `pg_stat_wal` | 9 col · 1 row · 0.8 ms | 9 col · 1 row · 0.8 ms | 9 col · 1 row · 0.8 ms | 5 col · 1 row · 0.8 ms | 6 col · 1 row · 0.9 ms | 9 col · 1 row · 1.7 ms | 6 col · 1 row · 1.0 ms |
| `pg_stat_io` | — | 18 col · 30 rows · 0.8 ms | 18 col · 35 rows · 1.1 ms | 20 col · 79 rows · 1.5 ms | 20 col · 79 rows · 1.1 ms | 18 col · 35 rows · 0.9 ms | 20 col · 79 rows · 1.0 ms |
| `pg_stat_slru` | 9 col · 8 rows · 0.7 ms | 9 col · 8 rows · 1.2 ms | 9 col · 8 rows · 1.0 ms | 9 col · 8 rows · 0.9 ms | 9 col · 8 rows · 1.3 ms | 9 col · 8 rows · 1.7 ms | 9 col · 8 rows · 1.4 ms |
| `pg_stat_recovery_prefetch` | 10 col · 1 row · 1.2 ms | 10 col · 1 row · 0.7 ms | 10 col · 1 row · 1.6 ms | 10 col · 1 row · 1.5 ms | 10 col · 1 row · 0.8 ms | 10 col · 1 row · 1.2 ms | 10 col · 1 row · 3.7 ms |
| `pg_stat_recovery` | — | — | — | — | 9 col · 0 rows · 1.1 ms | — | 9 col · 0 rows · 0.9 ms |
| `pg_stat_archiver` | 7 col · 1 row · 1.4 ms | 7 col · 1 row · 0.7 ms | 7 col · 1 row · 0.9 ms | 7 col · 1 row · 0.8 ms | 7 col · 1 row · 1.1 ms | 7 col · 1 row · 1.1 ms | 7 col · 1 row · 1.3 ms |
| `pg_stat_lock` | — | — | — | — | 5 col · 12 rows · 1.8 ms | — | 5 col · 12 rows · 0.9 ms |
| `pg_stat_autovacuum_scores` | — | — | — | — | 12 col · 1112 rows · 6.0 ms | — | 12 col · 1176 rows · 4.5 ms |
| `pg_wait_events` | — | — | 265 rows · 0.9 ms | 274 rows · 1.4 ms | 287 rows · 0.8 ms | 265 rows · 1.6 ms | 287 rows · 1.7 ms |
| `pg_aios` | — | — | — | 0 rows · 1.2 ms | 0 rows · 1.4 ms | — | 0 rows · 2.0 ms |
| **Memory** | | | | | | | |
| `pg_shmem_allocations` | 4 col · 61 rows · 0.7 ms | 4 col · 61 rows · 0.9 ms | 4 col · 67 rows · 1.5 ms | 4 col · 77 rows · 1.9 ms | 4 col · 82 rows · 0.8 ms | 4 col · 67 rows · 1.5 ms | 4 col · 82 rows · 2.8 ms |
| `pg_shmem_allocations_numa` | — | — | — | err: libnuma initialization failed or NUMA is not supported on th | err: libnuma initialization failed or NUMA is not supported on th | — | err: libnuma initialization failed or NUMA is not supported on th |
| `pg_backend_memory_contexts` | 9 col · 116 rows · 1.0 ms | 9 col · 118 rows · 1.2 ms | 9 col · 119 rows · 3.4 ms | 10 col · 122 rows · 0.8 ms | 10 col · 121 rows · 1.1 ms | 9 col · 119 rows · 1.7 ms | 10 col · 121 rows · 1.9 ms |
| **pg_stat_statements (extension)** | | | | | | | |
| `pg_stat_statements` | 43 col · 168 rows · (su 169) · 2.0 ms | 43 col · 170 rows · (su 171) · 1.8 ms | 49 col · 174 rows · (su 175) · 1.6 ms | 52 col · 176 rows · (su 177) · 2.5 ms | 54 col · 184 rows · (su 185) · 1.7 ms | 49 col · 189 rows · (su 190) · 7.0 ms | 54 col · 199 rows · (su 200) · 1.7 ms |
| `pg_stat_statements_info` | 2 col · 1 row · 0.9 ms | 2 col · 1 row · 0.7 ms | 2 col · 1 row · 0.8 ms | 2 col · 1 row · 0.9 ms | 2 col · 1 row · 0.8 ms | 2 col · 1 row · 0.9 ms | 2 col · 1 row · 1.3 ms |
| **Catalog and settings** | | | | | | | |
| `pg_extension` | 8 col · 3 rows · 0.9 ms | 8 col · 3 rows · 1.1 ms | 8 col · 3 rows · 0.8 ms | 8 col · 3 rows · 1.1 ms | 8 col · 3 rows · 1.0 ms | 8 col · 7 rows · 1.4 ms | 8 col · 7 rows · 1.1 ms |
| `pg_available_extensions` | 4 col · 61 rows · 205.2 ms | 4 col · 61 rows · 226.2 ms | 4 col · 59 rows · 2.4 ms | 4 col · 60 rows · 120.9 ms | 5 col · 61 rows · 241.2 ms | 4 col · 66 rows · 193.1 ms | 5 col · 68 rows · 25.9 ms |
| `pg_available_extension_versions` | 113 rows · 8.8 ms | 118 rows · 7.7 ms | 117 rows · 11.2 ms | 131 rows · 13.7 ms | 130 rows · 8.4 ms | 136 rows · 20.3 ms | 149 rows · 26.5 ms |
| `pg_settings` | 17 col · 359 rows · 5.5 ms | 17 col · 368 rows · 5.9 ms | 17 col · 384 rows · 3.2 ms | 17 col · 404 rows · 2.2 ms | 17 col · 424 rows · 2.9 ms | 17 col · 384 rows · 1.9 ms | 17 col · 424 rows · 2.8 ms |
| `pg_database` | 16 col · 4 rows · 0.9 ms | 17 col · 4 rows · 1.0 ms | 18 col · 4 rows · 1.4 ms | 18 col · 4 rows · 1.1 ms | 18 col · 4 rows · 1.2 ms | 18 col · 5 rows · 2.0 ms | 18 col · 5 rows · 1.3 ms |
| `pg_roles` | 13 col · 15 rows · 1.8 ms | 13 col · 17 rows · 1.6 ms | 13 col · 18 rows · 2.3 ms | 13 col · 19 rows · 3.6 ms | 13 col · 19 rows · 3.7 ms | 13 col · 18 rows · 1.9 ms | 13 col · 19 rows · 2.2 ms |
| `pg_partitioned_table` | 8 col · 1 row · 1.2 ms | 8 col · 1 row · 0.7 ms | 8 col · 1 row · 1.1 ms | 8 col · 1 row · 0.8 ms | 8 col · 1 row · 1.2 ms | 8 col · 1 row · 1.3 ms | 8 col · 1 row · 0.9 ms |
| `pg_inherits` | 4 col · 2 rows · 0.9 ms | 4 col · 2 rows · 0.9 ms | 4 col · 2 rows · 1.5 ms | 4 col · 2 rows · 1.1 ms | 4 col · 2 rows · 1.1 ms | 4 col · 2 rows · 2.0 ms | 4 col · 2 rows · 0.9 ms |
| `pg_trigger` | 19 col · 2 rows · 1.1 ms | 19 col · 2 rows · 5.6 ms | 19 col · 2 rows · 3.5 ms | 19 col · 2 rows · 2.0 ms | 19 col · 2 rows · 2.4 ms | 19 col · 7 rows · 2.3 ms | 19 col · 7 rows · 1.2 ms |
| `pg_stats` | 14 col · 387 rows · (su 406) · 33.8 ms | 14 col · 392 rows · (su 409) · 194.7 ms | 17 col · 393 rows · (su 410) · 141.7 ms | 17 col · 395 rows · (su 412) · 152.6 ms | 19 col · 400 rows · (su 417) · 157.0 ms | 17 col · 413 rows · (su 437) · 23.5 ms | 19 col · 420 rows · (su 444) · 14.9 ms |
| `pg_stats_ext` | 15 col · 0 rows · 11.0 ms | 15 col · 0 rows · 17.9 ms | 15 col · 0 rows · 18.9 ms | 15 col · 0 rows · 9.0 ms | 17 col · 0 rows · 14.0 ms | 15 col · 0 rows · 18.2 ms | 17 col · 0 rows · 11.0 ms |
| `pg_publication_sequences` | — | — | — | — | 0 rows · 18.8 ms | — | 0 rows · 10.1 ms |
| `pg_dsm_registry_allocations` | — | — | — | — | 0 rows · 1.0 ms | — | 0 rows · 1.1 ms |
| **Functions** | | | | | | | |
| `pg_ls_waldir()` | 4 rows · 0.9 ms | 4 rows · 0.7 ms | 4 rows · 0.8 ms | 4 rows · 0.8 ms | 4 rows · 0.8 ms | 5 rows · 5.6 ms | 6 rows · 0.6 ms |
| `pg_ls_archive_statusdir()` | 0 rows · 0.7 ms | 0 rows · 0.6 ms | 0 rows · 0.8 ms | 0 rows · 0.8 ms | 0 rows · 1.2 ms | 0 rows · 0.6 ms | 0 rows · 0.7 ms |
| `pg_ls_tmpdir()` | 0 rows · 0.9 ms | 0 rows · 2.4 ms | 0 rows · 1.1 ms | 0 rows · 0.8 ms | 0 rows · 0.8 ms | 0 rows · 1.2 ms | 0 rows · 0.8 ms |
| `pg_ls_logdir()` | error: no log directory (logging_collector off)| error: no log directory (logging_collector off)| error: no log directory (logging_collector off)| error: no log directory (logging_collector off)| error: no log directory (logging_collector off)| error: no log directory (logging_collector off)| error: no log directory (logging_collector off)|
| `pg_ls_summariesdir()` | — | — | — | 0 rows · 0.5 ms | 0 rows · 1.1 ms | — | 0 rows · 1.0 ms |
| `pg_control_system()` | 1 row · 0.5 ms | 1 row · 0.7 ms | 1 row · 0.7 ms | 1 row · 0.5 ms | 1 row · 9.9 ms | 1 row · 0.8 ms | 1 row · 0.8 ms |
| `pg_control_checkpoint()` | 1 row · 1.1 ms | 1 row · 1.7 ms | 1 row · 1.1 ms | 1 row · 0.5 ms | 1 row · 0.8 ms | 1 row · 0.8 ms | 1 row · 0.8 ms |
| `pg_control_init()` | 1 row · 0.7 ms | 1 row · 0.6 ms | 1 row · 0.9 ms | 1 row · 0.5 ms | 1 row · 0.6 ms | 1 row · 0.7 ms | 1 row · 0.7 ms |
| `pg_control_recovery()` | 1 row · 0.5 ms | 1 row · 0.5 ms | 1 row · 1.0 ms | 1 row · 0.5 ms | 1 row · 0.6 ms | 1 row · 0.9 ms | 1 row · 0.5 ms |
| `pg_partition_tree('review_part')` | 2 rows · 0.9 ms | 2 rows · 1.1 ms | 2 rows · 1.1 ms | 2 rows · 0.7 ms | 2 rows · 0.9 ms | 2 rows · 1.6 ms | 2 rows · 1.3 ms |
| `pg_partition_ancestors('review_part_2026')` | 2 rows · 1.0 ms | 2 rows · 0.9 ms | 2 rows · 1.4 ms | 2 rows · 1.1 ms | 2 rows · 0.8 ms | 2 rows · 1.1 ms | 2 rows · 0.7 ms |
| **PostGIS (extension objects)** | | | | | | | |
| `geometry_columns` | n/a | n/a | n/a | n/a | n/a | 7 col · 0 rows (su 13) · 64.3 ms | 7 col · 0 rows (su 13) · 49.3 ms |
| `geography_columns` | n/a | n/a | n/a | n/a | n/a | 7 col · 0 rows (su 1) · 16.4 ms | 7 col · 0 rows (su 1) · 12.4 ms |
| `spatial_ref_sys` | n/a | n/a | n/a | n/a | n/a | 5 col · 8500 rows · 122.9 ms | 5 col · 8500 rows · 60.8 ms |
| `postgis_full_version()` | n/a | n/a | n/a | n/a | n/a | text, `POSTGIS="3.6.4 0" [EXTENSION] PGSQL="170` (first 40 characters) · 576 ms (first call) | text, `POSTGIS="3.6.4 0" [EXTENSION] PGSQL="190` (first 40 characters) · 655 ms (first call) |

Count differences between pglens and the superuser for `pg_stat_statements` (one row, which is 168 against 169 on PG15, and so on) were not investigated. Query text from other roles is visible to pglens, so the rows are not hidden.

### 3.2 Hidden, NULL and denied values

| Object | As pglens | As superuser | Mechanism |
|---|---|---|---|
| `pg_sequences.last_value` for `review_seq` | NULL | 2 | View returns the value only when the role has SELECT or USAGE on the sequence (verified in the view definition). |
| `SELECT last_value FROM review_seq` | permission denied for sequence | 2 | Normal privilege. |
| `pg_statio_user_sequences` for `review_seq` | visible (block counters) | | Stats views are readable. |
| `pg_stats` | 387 rows on PG15 (406 for the superuser) | | View filters by `has_column_privilege(..., 'select')` (verified). |
| Bloat estimate (`bloat_estimate.sql`) for `review_bloat` (50% dead rows) | no estimate | about 50% bloat, 14,123,008 bytes | The query reads `pg_stats`, which hides columns the role cannot SELECT, so the table drops out with no error. pglens gets an estimate for `review_open`, which it was granted SELECT on (verified on 19rc1). |
| `geometry_columns`, `geography_columns` | 0 rows | 13 and 1 rows (postgis17 and postgis19rc1) | View filters by `has_table_privilege(..., 'SELECT')`. With `pg_read_all_data` granted temporarily (Tier 1 test), pglens sees 13 and 1 rows on both images. |
| `SELECT count(*) FROM review_geo` | permission denied for table | | Normal privilege. |
| `pg_stat_activity` query text of another role | visible | | Membership of `pg_read_all_stats`. |
| `pg_stat_statements` query text of role `review_app` | visible (`SELECT count(*) FROM review_t WHERE id = $1`) | | Same. |
| `pg_stat_progress_vacuum`, another user's VACUUM | visible (`review_big`, scanning heap, 2,500 blocks) | | Same. |
| `pg_shmem_allocations_numa` | error (libnuma) | error (libnuma) | Environment, not privilege. |
| `pgstattuple('review_t')` (no SELECT on the table) | returns counts (1,000 tuples, table_len 73728) | | Membership of `pg_stat_scan_tables` (via `pg_monitor`). |
| `pg_control_system()`, `pg_control_checkpoint()`, `pg_control_init()`, `pg_control_recovery()` | rows returned | rows returned | See section 3.4. |

### 3.3 Write and execute guards (HARD INVARIANT)

Each row was run as `pglens`. "Refused" means the server returned an error and nothing changed.

| Attempt as `pglens` | Result | Reading |
|---|---|---|
| `UPDATE review_t ...` (default settings) | refused: cannot execute UPDATE in a read-only transaction | Read-only default works. |
| `SET default_transaction_read_only = off; UPDATE review_t ...` | refused: permission denied for table review_t (SHOW returns `off`) | The session can change the default. Privileges are the real guard. |
| `SET ...= off; CREATE TABLE review_pglens_new` | refused: permission denied for schema public | PG15+ revokes CREATE on `public` from PUBLIC. |
| `SET ...= off; CREATE TEMP TABLE` | allowed | Session-local, not monitored data. TEMP is granted to PUBLIC on the database, so this is still a write path. Fix tested: `REVOKE TEMPORARY ON DATABASE ... FROM PUBLIC` (refused afterwards). |
| `lo_create(0)` (default settings) | refused: cannot execute lo_create() in a read-only transaction | Default works. |
| `SET ...= off; SELECT lo_create(0)` | allowed, returns an oid | **Persistent write.** Creates a row in `pg_largeobject_metadata` in the monitored database, owned by `pglens`. Two objects were created in one test (oids 16386 and 16387). PUBLIC has EXECUTE on `lo_create`. Fix tested: after `REVOKE EXECUTE ... FROM PUBLIC` the call is refused with `permission denied for function lo_create`. |
| `SET ...= off; SELECT lo_from_bytea(0, decode('00ff','hex'))` | allowed, returns an oid | **Persistent write**, as with `lo_create`. Fix tested: refused after `REVOKE EXECUTE ON FUNCTION lo_from_bytea(oid, bytea) FROM PUBLIC`. |
| `CREATE TEMP TABLE` (default settings) | refused: cannot execute CREATE TABLE in a read-only transaction | Read-only default applies to temp tables too. |
| `EXPLAIN SELECT * FROM review_t` (no ANALYZE) | refused: permission denied for table review_t | EXPLAIN needs SELECT on the tables it names. This confirms the script's comment that T1 relies on `pg_read_all_data`. |
| `EXPLAIN ANALYZE SELECT ...` | refused: permission denied for table review_t | Same. |
| `EXPLAIN ANALYZE UPDATE review_t ...` | refused: cannot execute UPDATE in a read-only transaction | ANALYZE on a write is blocked by the default. |
| `pg_stat_statements_reset(...)`, `pg_stat_reset()`, `pg_stat_reset_shared(text)` | EXECUTE = false | Superuser-only. |
| `pg_terminate_backend`, `pg_reload_conf`, `pg_switch_wal`, `pg_promote`, `pg_create_restore_point` | EXECUTE = false | No `pg_signal_backend`. |
| `pg_read_file`, `pg_read_binary_file`, `pg_ls_dir`, `pg_stat_file` | EXECUTE = false | Not granted. |
| `pg_cancel_backend(pid)` on the `review-other` session (superuser) | refused (PG15: must be a superuser to cancel superuser query; PG16 and later: permission denied to cancel query) | EXECUTE is PUBLIC, but the server rejects cancelling a superuser's backend. Cancelling its own role's backends was not tested. |
| `pg_logical_slot_peek_changes('review_lsl', ...)` | refused: a replication-slot permission error | The role has no REPLICATION attribute (`rolreplication = f`). |
| `pg_drop_replication_slot('review_psl')` | refused: same kind of error | Same. |
| `pg_logical_slot_get_changes`, `pg_replication_slot_advance` | not executed | EXECUTE is PUBLIC (`has_function_privilege` = true). Not run because a successful call would advance a slot. |

Conclusion: under the default session setting the role cannot write anything (tested). With the setting switched off, the role still cannot change tables, schemas or slots, and cannot signal other roles' backends. It **can** create large objects in the monitored database, and temp tables, unless PUBLIC's EXECUTE on `lo_create` and `lo_from_bytea` and TEMP on the database are revoked. Section 5, item 8, has the fix. The guard is the default setting plus the privilege model, and the privilege model has this gap.

### 3.4 pg_control_* functions and the GRANT lines

| Function | ACL on 15 to 19 | Result for pglens |
|---|---|---|
| `pg_control_system()` | `=X` (PUBLIC) plus `pglens=X` (explicit grant) | rows returned on all versions |
| `pg_control_checkpoint()` | same | rows returned on all versions |
| `pg_control_init()` | NULL (default, PUBLIC EXECUTE) | rows returned on all versions, with no explicit grant |
| `pg_control_recovery()` | NULL (default, PUBLIC EXECUTE) | rows returned on all versions, with no explicit grant |

The explicit `GRANT EXECUTE ON FUNCTION pg_control_system()` and `pg_control_checkpoint()` lines in `monitoring_user.sql` are therefore no-ops on 15 to 19. This is not a privilege problem. It is a documentation error to fix in the script.

### 3.5 Checks that tie the matrix to the code

| Area | pglens file | Objects and columns | Result on 15 to 19 |
|---|---|---|---|
| Checkpoints | `internal/check/checkpointer.go` | `pg_stat_checkpointer` (`num_timed`, `num_requested`, `write_time`, `sync_time`) for 17 and later; `pg_stat_bgwriter` checkpoint columns for earlier versions; `buffers_clean`, `maxwritten_clean`, `buffers_alloc` for all | OK. The checkpoint columns are gone from `pg_stat_bgwriter` on 17 and later, which the gate at 17 handles. |
| Vacuum progress | `internal/check/vacuum_progress.go` | `pg_stat_progress_vacuum`: `max_dead_tuples`/`num_dead_tuples` before 17, `max_dead_tuple_bytes`/`dead_tuple_bytes` from 17; name chosen with an `information_schema` probe | OK on all five versions. |
| pg_stat_statements | `internal/check/stat_statements.go`, `internal/agent/exec_explain.go` | `calls`, `total_exec_time`, `rows`, `shared_blks_hit`, `shared_blks_read`, `wal_bytes`, `temp_blks_written`, `queryid`, `dbid`, `userid`, `toplevel`, `query`; `pg_stat_statements_info.stats_reset` | OK. `blk_read_time` was renamed on 17, but the code does not read it. |
| I/O and WAL | `internal/check/io.go`, `internal/check/wal.go` | `pg_stat_io` and `pg_stat_wal` columns discovered with `information_schema` and then filtered | OK. `op_bytes` (removed on 18) and `wal_write`/`wal_sync` (removed on 18) are handled. |
| Replication | `replication_streaming.go`, `replication_receiver.go`, `replication_slots.go` | `pg_stat_replication` (`application_name`, `client_addr`, `client_port`, `state`, `sync_state`, lag columns, LSNs), `pg_stat_wal_receiver` (`sender_host`, `sender_port`, `slot_name`), `pg_replication_slots` (`slot_type`, `active`, `active_pid`, `wal_status`, `safe_wal_size`, `restart_lsn`) | OK. |
| Activity and ASH | `activity.go`, `locks.go`, `internal/ash/sampler.go`, `internal/agent/exec_signal.go` | `pg_stat_activity` (`backend_type`, `wait_event_type`, `wait_event`, `xact_start`, `state_change`, `query`, `query_id`, `usename`, `application_name`, `pid`) | OK. `query_id` exists from 14. |
| Tables, indexes, database | `table_stats.go`, `index_stats.go`, `database_stats.go` | `pg_stat_user_tables` (`n_*`, `last_*`, `*_count`), `pg_stat_user_indexes` (`idx_*`), `pg_statio_user_indexes` (`idx_blks_*`), `pg_stat_database` (`xact_*`, `blks_*`, `deadlocks`, `temp_bytes`, `conflicts`, `numbackends`, `stats_reset`) | OK. |
| Archiver and basebackup | `archiver.go` | `pg_stat_archiver` (all seven columns), `pg_stat_progress_basebackup` (`phase`, `backup_total`, `backup_streamed`), `pg_ls_archive_statusdir()` | OK. |
| Settings and cluster | `instance_info.go`, `settings.go`, `cmd/pglens-agent/main.go` | `pg_control_system().system_identifier`; `pg_settings` (ten columns); `pg_database` (`oid`, `datname`) | OK. |
| On-demand pgstattuple | `internal/agent/exec_pgstattuple.go` | `pgstattuple(regclass)`: `table_len`, `tuple_count`, `tuple_len`, `dead_tuple_*`, `free_space`, `free_percent` | OK on 1.5 (the same version on all five). |

The 141 (view, column) pairs were listed by reading the queries in the files above, not with an automated parser, so a column built by string concatenation could be missing from the list. Every listed pair exists on the versions where the code reads it. Columns that the code does not read are not listed here. Nothing removed in 19 is read by pglens.

### 3.6 Functions pglens calls

Names found by a regex over non-test `.go` and embedded `.sql` files under `internal/` and `cmd/` (a name followed by an opening parenthesis). Every name exists on 15 to 19. The Tier 0 role has EXECUTE on each one on 19rc1 (checked as the role). The `pg_advisory_*` calls run on the pglens storage database, not on a monitored database.

| Function | Called from | Exists 15 to 19 | Tier 0 EXECUTE on 19rc1 |
|---|---|---|---|
| `age()` | `internal/check/activity.go`, `internal/check/table_stats.go` | yes | true |
| `current_setting()` | `cmd/pglens-agent/main.go`, `internal/agent/conn.go`, `internal/check/activity.go`, `archiver.go`, `instance_info.go`, `io.go`, `bloat_estimate.sql` | yes | true |
| `inet_server_addr()`, `inet_server_port()` | `internal/check/instance_info.go` | yes | true |
| `pg_backend_pid()` | `internal/agent/exec_signal.go`, `internal/ash/sampler.go`, `internal/check/activity.go`, `ash.go`, `locks.go` | yes | true |
| `pg_blocking_pids()` | `internal/check/locks.go` | yes | true |
| `pg_control_system()` | `cmd/pglens-agent/main.go`, `internal/agent/conn.go`, `internal/check/instance_info.go`, `internal/pgtype/role.go` | yes | true |
| `pg_current_wal_lsn()` | `internal/check/replication_slots.go`, `replication_streaming.go`, `wal.go` | yes | true |
| `pg_is_in_recovery()` | `cmd/pglens-agent/main.go`, `internal/agent/conn.go`, `internal/check/instance_info.go`, `replication_slots.go`, `replication_streaming.go`, `wal.go`, `internal/pgtype/role.go` | yes | true |
| `pg_last_wal_receive_lsn()` | `internal/check/replication_receiver.go`, `replication_streaming.go` | yes | true |
| `pg_last_wal_replay_lsn()` | `internal/check/replication_receiver.go`, `replication_slots.go`, `wal.go` | yes | true |
| `pg_last_xact_replay_timestamp()` | `internal/check/replication_receiver.go` | yes | true |
| `pg_get_indexdef()` | `internal/check/index_stats.go` | yes | true |
| `pg_ls_archive_statusdir()` | `internal/check/archiver.go` | yes | true |
| `pg_postmaster_start_time()` | `internal/check/instance_info.go` | yes | true |
| `pg_relation_size()` | `internal/check/index_stats.go`, `bloat_estimate.sql` | yes | true |
| `pg_table_size()` | `internal/check/table_stats.go` | yes | true |
| `pg_total_relation_size()` | `internal/check/table_stats.go`, `bloat_estimate.sql` | yes | true |
| `pg_wal_lsn_diff()` | `internal/check/replication_slots.go`, `replication_streaming.go`, `wal.go` | yes | true |
| `pg_advisory_lock()`, `pg_advisory_unlock()`, `pg_try_advisory_lock()` | `internal/store/migrate.go`, `internal/advisor/engine.go`, `internal/alert/engine.go`, `internal/server/staleness.go` | yes | true (storage database only) |
| `pgstattuple(regclass)` | `internal/agent/exec_pgstattuple.go` | extension 1.5 on all images | true (section 3.1 finding) |

## 4. PostgreSQL 19 diff

### 4.1 Column-level diff for monitored objects, 15 to 16 to 17 to 18 to 19rc1

Monitored objects are the `pg_stat*` family, `pg_locks`, `pg_prepared_xacts`, `pg_sequences`, `pg_replication_slots`, `pg_shmem_allocations*`, `pg_backend_memory_contexts`, `pg_stats*`, the catalog tables that pglens reads, and the extension views `pg_stat_statements` and PostGIS `geometry_columns` and `geography_columns`. Renames marked "inferred" are based on the same attribute number and type, not on release notes.

#### PostgreSQL 15 to 16

pg_stat_statements extension: 1.10 to 1.10. New predefined roles: pg_create_subscription, pg_use_reserved_connections. Memberships of pg_monitor and the other monitoring roles: no change.

| Object | Columns added | Columns removed | Type changed | Notes |
|---|---|---|---|---|
| `pg_database` | daticurules |  |  |  |
| `pg_replication_slots` | conflicting |  |  |  |
| `pg_stat_all_indexes` | last_idx_scan |  |  |  |
| `pg_stat_all_tables` | last_seq_scan, last_idx_scan, n_tup_newpage_upd |  |  |  |
| `pg_stat_database_conflicts` | confl_active_logicalslot |  |  |  |
| `pg_stat_gssapi` | credentials_delegated |  |  |  |
| `pg_stat_io` | (new view) |  |  | new view (pg_stat_io) |
| `pg_stat_subscription` | leader_pid |  |  |  |
| `pg_stat_sys_indexes` | last_idx_scan |  |  |  |
| `pg_stat_sys_tables` | last_seq_scan, last_idx_scan, n_tup_newpage_upd |  |  |  |
| `pg_stat_user_indexes` | last_idx_scan |  |  |  |
| `pg_stat_user_tables` | last_seq_scan, last_idx_scan, n_tup_newpage_upd |  |  |  |
| `pg_stat_xact_all_tables` | n_tup_newpage_upd |  |  |  |
| `pg_stat_xact_sys_tables` | n_tup_newpage_upd |  |  |  |
| `pg_stat_xact_user_tables` | n_tup_newpage_upd |  |  |  |

Catalog functions in pg_catalog: 60 identity strings new, 7 gone. No name removed. New names are mostly aggregate and JSON support functions (`*_strict`, `*_unique`), plus `erf`, `erfc`, `date_add`, `date_subtract`, `array_shuffle`, `array_sample` and `any_value`. The 7 gone are the old signatures of 7 changed functions, among them `pg_stat_get_activity` (the function behind `pg_stat_activity`, whose 22 columns are the same on 15 and 16, section 3.1).

#### PostgreSQL 16 to 17

pg_stat_statements extension: 1.10 to 1.11. New predefined roles: pg_maintain. Memberships of pg_monitor and the other monitoring roles: no change.

| Object | Columns added | Columns removed | Type changed | Notes |
|---|---|---|---|---|
| `pg_database` | dathasloginevt, datlocale | daticulocale |  |  |
| `pg_replication_slots` | inactive_since, invalidation_reason, failover, synced |  |  |  |
| `pg_stat_bgwriter` |  | checkpoints_timed, checkpoints_req, checkpoint_write_time, checkpoint_sync_time, buffers_checkpoint, buffers_backend, buffers_backend_fsync |  | checkpoint columns moved to pg_stat_checkpointer |
| `pg_stat_checkpointer` | (new view) |  |  | new view |
| `pg_stat_progress_copy` | tuples_skipped |  |  |  |
| `pg_stat_progress_vacuum` | max_dead_tuple_bytes, dead_tuple_bytes, num_dead_item_ids, indexes_total, indexes_processed | max_dead_tuples, num_dead_tuples |  | inferred renames at same position: max_dead_tuples to max_dead_tuple_bytes, num_dead_tuples to dead_tuple_bytes (code probes information_schema) |
| `pg_stat_subscription` | worker_type |  |  |  |
| `pg_statistic_ext` |  |  | stxstattarget integer→smallint |  |
| `pg_stats` | range_length_histogram, range_empty_frac, range_bounds_histogram |  |  |  |
| `public.pg_stat_statements` | shared_blk_read_time, shared_blk_write_time, local_blk_read_time, local_blk_write_time, jit_deform_count, jit_deform_time, stats_since, minmax_stats_since | blk_read_time, blk_write_time |  | inferred renames: blk_read_time to shared_blk_read_time, blk_write_time to shared_blk_write_time |
| `pg_wait_events` | (new view) | | | new view |

Catalog functions in pg_catalog: 63 identity strings new, 30 gone. 12 names are gone: the `pg_stat_get_bgwriter_*` checkpoint counters (moved to `pg_stat_checkpointer`), `pg_stat_get_buf_*_backend`, `pg_stat_get_checkpoint_*_time`, `interval_accum*`, `interval_combine`, `pg_read_file_old` and `pg_rotate_logfile_old`. 16 signatures changed, among them `pg_stat_reset_shared`, `pg_stat_reset_slru`, `pg_stat_get_subscription` and `pg_get_replication_slots`.

#### PostgreSQL 17 to 18

pg_stat_statements extension: 1.11 to 1.12. New predefined roles: pg_signal_autovacuum_worker. Memberships of pg_monitor and the other monitoring roles: no change.

| Object | Columns added | Columns removed | Type changed | Notes |
|---|---|---|---|---|
| `pg_backend_memory_contexts` | type, path | parent |  | parent replaced by type and path (inferred) |
| `pg_replication_slots` | two_phase_at |  |  |  |
| `pg_shmem_allocations_numa` | (new view) |  |  | new view (errors here: libnuma) |
| `pg_stat_all_tables` | total_vacuum_time, total_autovacuum_time, total_analyze_time, total_autoanalyze_time |  |  |  |
| `pg_stat_checkpointer` | num_done, slru_written |  |  |  |
| `pg_stat_database` | parallel_workers_to_launch, parallel_workers_launched |  |  |  |
| `pg_stat_io` | read_bytes, write_bytes, extend_bytes | op_bytes |  | op_bytes removed (read/write/extend_bytes added) |
| `pg_stat_progress_analyze` | delay_time |  |  |  |
| `pg_stat_progress_vacuum` | delay_time |  |  |  |
| `pg_stat_subscription_stats` | confl_insert_exists, confl_update_origin_differs, confl_update_exists, confl_update_missing, confl_delete_origin_differs, confl_delete_missing, confl_multiple_unique_conflicts |  |  |  |
| `pg_stat_sys_tables` | total_vacuum_time, total_autovacuum_time, total_analyze_time, total_autoanalyze_time |  |  |  |
| `pg_stat_user_tables` | total_vacuum_time, total_autovacuum_time, total_analyze_time, total_autoanalyze_time |  |  |  |
| `pg_stat_wal` |  | wal_write, wal_sync, wal_write_time, wal_sync_time |  | wal_write/wal_sync(+_time) removed |
| `public.pg_stat_statements` | wal_buffers_full, parallel_workers_to_launch, parallel_workers_launched |  |  |  |
| `pg_aios` | (new view) | | | new view |

Catalog functions in pg_catalog: 122 identity strings new, 39 gone. One name is no longer a function in 18 (`pg_timezone_abbrevs`, not monitoring-relevant). 69 names are new, mostly skip-support and support functions, plus `crc32` and `crc32c`, `gamma`, `lgamma`, `bytea` and the `has_largeobject_privilege` family. 18 signatures changed, among them `pg_stat_get_io`, `pg_stat_get_wal`, `pg_stat_have_stats`, `pg_control_init`, `pg_get_backend_memory_contexts`, `pg_stat_get_subscription_stats` and the `regexp_*` family.

#### PostgreSQL 18 to 19rc1

pg_stat_statements extension: 1.12 to 1.13. New predefined roles: none. Memberships of pg_monitor and the other monitoring roles: no change.

| Object | Columns added | Columns removed | Type changed | Notes |
|---|---|---|---|---|
| `pg_available_extensions` | location |  |  |  |
| `pg_replication_slots` | slotsync_skip_reason |  |  |  |
| `pg_stat_all_indexes` | stats_reset |  |  |  |
| `pg_stat_all_tables` | stats_reset |  |  |  |
| `pg_stat_autovacuum_scores` | (new view) |  |  | new view |
| `pg_stat_database_conflicts` | stats_reset |  |  |  |
| `pg_stat_lock` | (new view) |  |  | new view |
| `pg_stat_progress_analyze` | started_by |  |  |  |
| `pg_stat_progress_basebackup` | backup_type |  |  |  |
| `pg_stat_progress_repack` | (new view) |  |  | new view |
| `pg_stat_progress_vacuum` | mode, started_by |  |  |  |
| `pg_stat_recovery` | (new view) |  |  | new view |
| `pg_stat_replication_slots` | mem_exceeded_count, slotsync_skip_count, slotsync_last_skip |  |  |  |
| `pg_stat_subscription_stats` | sync_seq_error_count, sync_table_error_count, confl_update_deleted | sync_error_count |  | sync_error_count split into sync_seq_error_count and sync_table_error_count (not a clean rename; pglens does not read it) |
| `pg_stat_sys_indexes` | stats_reset |  |  |  |
| `pg_stat_sys_tables` | stats_reset |  |  |  |
| `pg_stat_user_functions` | stats_reset |  |  |  |
| `pg_stat_user_indexes` | stats_reset |  |  |  |
| `pg_stat_user_tables` | stats_reset |  |  |  |
| `pg_stat_wal` | wal_fpi_bytes |  |  |  |
| `pg_statio_all_indexes` | stats_reset |  |  |  |
| `pg_statio_all_sequences` | stats_reset |  |  |  |
| `pg_statio_all_tables` | stats_reset |  |  |  |
| `pg_statio_sys_indexes` | stats_reset |  |  |  |
| `pg_statio_sys_sequences` | stats_reset |  |  |  |
| `pg_statio_sys_tables` | stats_reset |  |  |  |
| `pg_statio_user_indexes` | stats_reset |  |  |  |
| `pg_statio_user_sequences` | stats_reset |  |  |  |
| `pg_statio_user_tables` | stats_reset |  |  |  |
| `pg_stats` | tableid, attnum |  |  | tableid and attnum added |
| `pg_stats_ext` | tableid, statistics_id |  |  |  |
| `pg_stats_ext_exprs` | tableid, statistics_id, range_length_histogram, range_empty_frac, range_bounds_histogram |  |  |  |
| `public.pg_stat_statements` | generic_plan_calls, custom_plan_calls |  |  |  |
| `pg_dsm_registry_allocations` | (new view) | | | new view |
| `pg_publication_sequences` | (new view) | | | new view |

Catalog functions in pg_catalog: 59 identity strings new, 42 gone. The 42 gone are the 31 removed names (30 encoding conversions and one binary upgrade helper) plus the old signatures of the 11 changed functions. The 59 new identities are mostly the PG19 monitoring helpers, the type-support functions, and new overloads of `bytea`, `max`, `min` and `random`. The monitoring-relevant ones are in section 4.3.


### 4.2 Objects new in 19rc1


**New in PostgreSQL 19rc1 that is relevant to monitoring**

- New views, all readable by the T0 role through the public SELECT grant:
  - `pg_stat_autovacuum_scores` (12 columns, one row per relation, 1,112 rows on the test cluster)
  - `pg_stat_lock` (5 columns, lock wait statistics)
  - `pg_stat_progress_repack` (14 columns, progress of REPACK, the new command)
  - `pg_stat_recovery` (9 columns, empty on a primary)
  - `pg_dsm_registry_allocations`, `pg_publication_sequences` (not monitoring-relevant)
- New columns in views pglens reads: `pg_stat_progress_vacuum.mode` and `started_by`; `pg_stat_progress_analyze.started_by`; `pg_stat_progress_basebackup.backup_type`; `pg_stat_wal.wal_fpi_bytes`; `pg_replication_slots.slotsync_skip_reason`; `pg_stat_replication_slots.mem_exceeded_count`, `slotsync_skip_count`, `slotsync_last_skip`; `stats_reset` on the `pg_stat_all/sys/user_tables`, `pg_stat_all/sys/user_indexes`, `pg_statio_*` (tables, indexes, sequences), `pg_stat_user_functions` and `pg_stat_database_conflicts` views; `pg_available_extensions.location` (not read by pglens).
- Changed function signatures (not read by pglens): `pg_control_checkpoint()` adds `logical_decoding boolean` and changes `next_multi_offset` from `xid` to `bigint`; `pg_get_replication_slots()` adds `slotsync_skip_reason`; `pg_available_extension_versions()` and `pg_available_extensions()` add `location`.
- pg_stat_statements 1.13 adds `generic_plan_calls` and `custom_plan_calls`. Its `pg_stat_statements_reset()` signature is unchanged from 1.12 and still superuser-only.
- No view or column that pglens reads is removed or renamed in 19rc1.


### 4.3 Functions: new, removed and changed in 19rc1

| Function | 18 to 19rc1 change | Read by pglens |
|---|---|---|
| `pg_control_checkpoint()` | adds `logical_decoding boolean`; `next_multi_offset` changes from `xid` to `bigint` | no (only `pg_control_system()` is read) |
| `pg_get_replication_slots()` | adds `slotsync_skip_reason` | no |
| `pg_available_extensions()`, `pg_available_extension_versions()` | add `location` | no |
| `pg_stat_statements()` (1.13) | gains the new OUT columns of the view | no (the view is read) |
| `pg_stat_statements_reset()` | unchanged from 1.12 (`userid, dbid, queryid, minmax_only`), still superuser-only | no |
| `pg_ls_summariesdir()` | new in 18, unchanged in 19 | no |
| `pg_stat_get_wal()`, `pg_stat_get_backend_wal()` | add `wal_fpi_bytes` (view `pg_stat_wal` has 6 columns on 19rc1, 5 on 18) | `pg_stat_wal` is read with discovered columns (`wal.go`) |
| `pg_stat_get_replication_slot()` | adds `mem_exceeded_count`, `slotsync_skip_count`, `slotsync_last_skip` | no |
| `pg_stat_get_subscription_stats()` | `sync_error_count` split into `sync_seq_error_count` and `sync_table_error_count`; adds `confl_update_deleted` | no (the view is not read; no subscriptions in the test) |

Predefined roles added: `pg_create_subscription` and `pg_use_reserved_connections` (16), `pg_maintain` (17), `pg_signal_autovacuum_worker` (18). None of them is a member of `pg_monitor`. From 17, the owner's ACL entry on these views includes MAINTAIN (`m`). The PUBLIC SELECT grant is unchanged, and the new 19 views get the same grant. No predefined role was added in 19rc1, and the `pg_monitor` membership did not change.

Totals at name level, from the `allfn` lists of the 18 and 19rc1 dumps: 41 names are new, 31 names are gone, 11 names have one identity removed and one added (changed signature), and 4 names (`bytea`, `max`, `min`, `random`) gained overloads only. At identity level there are 59 new and 42 gone.

**New in 19rc1, monitoring-relevant.** The helpers behind `pg_stat_lock`, `pg_stat_autovacuum_scores`, `pg_stat_recovery`, `pg_dsm_registry_allocations` and `pg_publication_sequences` were confirmed with `pg_get_viewdef`. The other rows are listed as found, without a matching view check.

| Function | Serves | Tier 0 EXECUTE on 19rc1 | Read by pglens |
|---|---|---|---|
| `pg_stat_get_lock()` | `pg_stat_lock` (12 rows here) | true | no |
| `pg_stat_get_autovacuum_scores()` | `pg_stat_autovacuum_scores` (1,112 rows here) | true | no |
| `pg_stat_get_recovery()` | `pg_stat_recovery` (0 rows on a primary) | true | no |
| `pg_get_dsm_registry_allocations()` | `pg_dsm_registry_allocations` | true | no |
| `pg_get_multixact_stats()` | multixact statistics (1 row here) | true | no |
| `pg_stat_get_function_stat_reset_time(oid)` | per-function stats reset time | true | no |
| `pg_stat_get_stat_reset_time()` | cluster-wide stats reset time | false | no |
| `pg_get_publication_sequences(oid)` | `pg_publication_sequences` | false | no |

Not monitoring-relevant: `pg_clear_extended_stats()` and `pg_restore_extended_stats()` (extended statistics maintenance), `binary_upgrade_check_logical_slot_pending_wal()` and `binary_upgrade_create_conflict_detection_slot()` (upgrade tooling), and type-support functions (`oid8*`, `btoid8*`, `hashoid8*`, `hashoid8extended`, `regdatabase*`, `to_regdatabase`, `uuid`, `tid_block`, `tid_offset`, `error_on_null`, `range_minus_multi`, `multirange_minus_multi`).

**Removed in 19rc1 (31 names).** 30 encoding conversion functions (15 `*_to_mic` such as `euc_jp_to_mic`, `sjis_to_mic`, `win866_to_mic`, and 15 `mic_to_*` such as `mic_to_euc_jp`, `mic_to_sjis`, `mic_to_win1251`) and `binary_upgrade_logical_slot_has_caught_up`. None is monitoring-relevant and none is read by pglens.

**Changed signatures (11 names).** Eight of the 11 are in the first table of this section. The other three:

| Function | 18 to 19rc1 change | Read by pglens |
|---|---|---|
| `pg_get_sequence_data(regclass)` | adds `page_lsn pg_lsn` | no |
| `pg_get_publication_tables()` | adds an overload with `target_relid`; the VARIADIC parameter is renamed from `pubname` to `pubnames` | no |
| `pg_replication_origin_session_setup()` | `(text)` becomes `(node_name text, pid integer)` | no |

### 4.4 PostGIS 3.6.4 on PostgreSQL 17 and 19rc1

- Extension set and versions are identical on both images: plpgsql 1.0; postgis, postgis_topology and postgis_tiger_geocoder 3.6.4; fuzzystrmatch 1.2; pgstattuple 1.5. `pg_stat_statements` is 1.11 on 17 and 1.13 on 19rc1, which is the core change in section 4.1.
- `geometry_columns` and `geography_columns` (7 columns each) and `spatial_ref_sys` (5 columns): the same column names and types on both images. No change.
- `postgis_full_version()`: `text` on both. The output differs only in the PostgreSQL build number (`PGSQL="170"` against `PGSQL="190"`).
- Visibility is the same on both images. Tier 0 sees 0 rows in `geometry_columns` and `geography_columns`, and the superuser sees 13 and 1. With `pg_read_all_data` granted temporarily, Tier 0 sees 13 and 1 rows. `spatial_ref_sys` has 8,500 rows for the superuser and for Tier 0.

## 5. Recommendations

1. Correct the header of `deploy/sql/monitoring_user.sql`: it says PostgreSQL 15 to 18, and the file applies cleanly on 19rc1. Also say that `default_transaction_read_only` is a session default that clients can change, and correct the claim that the server refuses every write (see item 8).
2. Remove the `GRANT EXECUTE ... pg_control_system()` and `pg_control_checkpoint()` lines, or state that they are redundant on 15 to 19. The stable cluster ID still works.
3. Document that `pgstattuple` is executable at Tier 0 once installed, because `pg_monitor` includes `pg_stat_scan_tables`. That makes the Tier 3 extension the only gate. Decide whether this matches the intended tier model.
4. Keep the agent tolerant of `pg_shmem_allocations_numa` errors. Do not read it in a required check.
5. For PostGIS inventory, Tier 0 sees no geometry or geography columns (0 rows, against 13 and 1 for the superuser, on 17 and 19rc1). Tier 1 (`pg_read_all_data`) does see them: this was tested with a temporary grant. Decide whether the PostGIS inventory belongs to Tier 0. The views filter on table SELECT, so showing them at Tier 0 needs a per-table SELECT grant. That also exposes the table data, not only the catalog entry. Record the decision in the tier documentation.
6. No code change is needed to support 19rc1. The new monitoring views are candidates for future checks (`pg_stat_lock`, `pg_stat_autovacuum_scores`, `pg_stat_recovery`).
7. The bloat estimate reads `pg_stats`, so it silently skips tables the monitoring role cannot SELECT. The SQL comment mentions missing column statistics, but not that the role's privileges cause the gap. Either document this in `bloat_estimate.sql` and in the check description, or add a fallback that does not depend on `pg_stats`. Consider also reporting how many tables were skipped.
8. **Close the two write paths in section 3.3 (tested on 19rc1).** In each monitored database, as the owner: `REVOKE EXECUTE ON FUNCTION lo_create(oid), lo_from_bytea(oid, bytea) FROM PUBLIC;` and `REVOKE TEMPORARY ON DATABASE <db> FROM PUBLIC;`. Then add the same to `monitoring_user.sql` for the database it configures. Before doing it, check that no pglens check needs temp tables or large objects on a monitored database. A search of `internal/check`, `internal/agent`, `internal/ash` and `cmd/pglens-agent` found none. The only write-looking SQL there is `SET default_transaction_read_only = on` in `internal/agent/conn.go`.

## 6. Limits

- One run per probe on an idle cluster. Timings are indicative only.
- No remote database was contacted. Subscriptions, standbys and real replication lag were not tested.
- `pg_shmem_allocations_numa` could not be read on either image (environment).
- Live progress was tested for VACUUM only.
- `get_changes` and `advance` on logical slots were not executed on purpose.
- The temporary `pg_read_all_data` grant for the PostGIS test was made in throwaway containers and revoked afterwards. The role then returned to `pg_monitor` only.
- The function list in section 3.6 comes from a regex. SQL built by string concatenation would be missed.
- Write paths: only `lo_create`, `lo_from_bytea` and temp tables were tested with the session setting off. Other PUBLIC-executable functions were not tested one by one. Server-side `lo_import` and `lo_export` were not tested.
- Scripts and raw probe output are in `/tmp/claude-1000/-mnt-fabio-dati-Git-SperimentazioniAI-postgres-analyze/613631cf-4ae9-42d7-9ca9-e33f7ce4a509/scratchpad/cov/` (not in the repository).

## 7. Cleanup

The seven containers of the first set were removed explicitly by name (`pglens-review-cov-pg15`, `-pg16`, `-pg17`, `-pg18`, `-pg19rc1`, `-postgis17`, `-postgis19rc1`). The three verification containers (`pglens-review-cov2-pg19rc1`, `pglens-review-cov2-postgis17`, `pglens-review-cov2-postgis19rc1`) and the three large-object containers (`pglens-review-lo-pg19rc1`, `pglens-review-lo2-pg19rc1`, `pglens-review-lo3-pg19rc1`) were removed the same way. `docker ps -a --filter name=pglens-review-cov` and `--filter name=pglens-review-lo` returned nothing afterwards. The host also has other containers whose names start with `pglens-review-` (for example `pglens-review-cr-*`, `pglens-review-dr-*`, `pglens-review-ro-*`, `pglens-review-ar-*` and `pglens-review-sa11-verify01`). This session did not start them, so they were not touched.
