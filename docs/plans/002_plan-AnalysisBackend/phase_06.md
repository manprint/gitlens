# Phase 5 — Configuration and durability: settings, WAL, checkpointer, I/O, archiver

> **Intent:** collect what is needed to say "this server is configured wrong" and
> "your backups are not really working" — the GUC snapshot, WAL generation,
> checkpoint behavior, I/O accounting and archiver health.
> **Shippable alone?** yes — five new instance-scope checks and two read
> endpoints.
> **Preconditions:** phase 2 `DONE`. Phase 4 is not required.

## State contract (mandatory)

1. Before touching anything: read [STATE.md](STATE.md). If §1 `Status` is `OPEN`,
   finish or revert that unit first (§6 says how far it got). Run the gate
   commands in STATE.md **§3** and check the result against what §1, §7, and §11
   claim; the repo wins, so correct the file when they disagree.
2. **Open the sub-phase in STATE.md §1 before editing any code**: `Type:
   sub-phase`, its `ID`, `Status: OPEN`, `Intent`, `Next action:`, and §6 set to
   `claimed — nothing written yet`.
3. **Close it after the gates are green**: append the §4 ledger row, reset §6 to
   `none — tree consistent`, update §5 §7 §8 §9 §10 and the §11 board, point §1
   at the next unit with `Status: none`, bump the timestamp. When STATE.md §3 has
   WIP commits on, commit the closed sub-phase and put its sha in the §4 row. A
   sub-phase is not done until this is written.
4. If the session ends mid-sub-phase, leave §1 `OPEN` and write exactly what is
   half-finished into §6 before stopping — plus a `wip(<N.Y>)` commit when WIP
   commits are on.

## The version problem, stated once

This phase touches the three system views that changed most across PostgreSQL
15 to 18. Two of the changes are known and are handled by declaring version
bounds; one is not known and is verified first, in sub-phase 5.1, before any
code depends on it.

| View | Change | Handling |
|------|--------|----------|
| `pg_stat_bgwriter` / `pg_stat_checkpointer` | checkpoint counters moved out of `pg_stat_bgwriter` into `pg_stat_checkpointer` in PG 17 (R1) | one check, two queries, branch on `pgtype.PG17`, identical metric names on both sides |
| `pg_stat_io` | introduced in PG 16 (R2) | `Requirements.MinPG = pgtype.PG16`; excluded upstream on PG 15 with a reason |
| `pg_stat_wal` | `UNVERIFIED` — the WAL I/O columns may have moved to `pg_stat_io` in PG 18 (D23, Q-A) | verified in 5.1 against a live container before 5.3 is written |

Never branch on a version you have not verified. The pattern the whole phase
uses instead is: **declare the bound in `Requires()` when the whole check is
version-specific, and probe `information_schema.columns` once per connection
when only some columns are**. The probe pattern is already established by
sub-phase 4.3.

---

## Sub-phases

### 5.1 Verify the PG 18 WAL statistics shape

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — this settles an `UNVERIFIED` external fact and
  is a review gate; the answer changes what 5.3 collects
- **Files:** `internal/check/wal_probe_integration_test.go` (new, temporary
  scaffolding that becomes part of the permanent test set)
- **Change:** write an integration test that starts one container per supported
  version through the existing `test/pgtest` harness and, for each, prints and
  asserts the column set of the two views:

  ```sql
  SELECT column_name, data_type
  FROM information_schema.columns
  WHERE table_schema = 'pg_catalog' AND table_name = 'pg_stat_wal'
  ORDER BY column_name;

  SELECT column_name, data_type
  FROM information_schema.columns
  WHERE table_schema = 'pg_catalog' AND table_name = 'pg_stat_io'
  ORDER BY column_name;
  ```

  Record the observed column set for each version in `STATE.md` §8 as a
  deviation row titled "R7 resolved", and update the `overview.md` References
  table row R7 from `UNVERIFIED` to the verified fact with the date — a
  superseding note, not an edit that erases the original uncertainty. Then close
  Q-A in `overview.md` **Open questions** and in `STATE.md` §9.

  Keep the test permanently, renamed `INT-WAL-000`, asserting the exact column
  set per version. It is cheap and it turns the next PostgreSQL major release
  from a production surprise into a red test.

  > Do not write `internal/check/wal.go` in this sub-phase. The whole point is
  > that 5.3 is written against a measured fact.

- **Unit tests:** none.
- **e2e tests:** `INT-WAL-000` — the column-set assertion above, on all four
  versions.
- **Done:** `INT-WAL-000` green; R7 updated in `overview.md`; Q-A closed in both
  `overview.md` and `STATE.md` §9; the deviation recorded in `STATE.md` §8;
  gates green; closed in `STATE.md`.

### 5.2 The `settings` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/settings.go` (new),
  `internal/check/settings_test.go` (new),
  `internal/check/settings_integration_test.go` (new)
- **Change:** implement a check named `settings`.

  ```go
  Requirements{
      Scope:    ScopeInstance,
      PermTier: pgtype.TierReadOnly,
  }
  DefaultInterval() = 1 * time.Hour
  Timeout()         = 10 * time.Second
  ```

  ```sql
  SELECT name, setting, unit, source, boot_val, reset_val, pending_restart,
         vartype, context, short_desc
  FROM pg_settings
  ```

  `pg_settings` has around 350 rows. Emitting all of them as facts every hour
  for every instance is 350 rows per instance in `object_facts`, which is
  acceptable because `object_facts` is keyed and upserted, not appended — but it
  is still noise. Emit a fact for a setting when **either** of these holds:

  1. its `name` is in a curated allowlist, declared as a sorted `[]string`
     constant in the file, or
  2. `setting IS DISTINCT FROM boot_val`, meaning an operator changed it.

  The curated allowlist, at minimum, and kept in this exact order so a diff is
  readable:

  ```
  archive_command, archive_mode, archive_timeout, autovacuum,
  autovacuum_analyze_scale_factor, autovacuum_max_workers,
  autovacuum_naptime, autovacuum_vacuum_cost_delay,
  autovacuum_vacuum_cost_limit, autovacuum_vacuum_scale_factor,
  autovacuum_work_mem, checkpoint_completion_target, checkpoint_timeout,
  default_statistics_target, effective_cache_size, effective_io_concurrency,
  fsync, full_page_writes, hot_standby, hot_standby_feedback, huge_pages,
  maintenance_work_mem, max_connections, max_parallel_workers,
  max_parallel_workers_per_gather, max_replication_slots,
  max_standby_streaming_delay, max_wal_senders, max_wal_size,
  max_worker_processes, min_wal_size, random_page_cost, shared_buffers,
  statement_timeout, synchronous_commit, synchronous_standby_names,
  temp_buffers, track_io_timing, wal_buffers, wal_compression,
  wal_keep_size, wal_level, work_mem
  ```

  Each emitted fact has `Kind: "setting"`, `Key: name`, `ValueText: setting`,
  and labels `{unit, source, vartype, context, pending_restart}`.

  Also emit three gauges so the advisor can do arithmetic without parsing text:
  `pg_setting_bytes{name}` for settings whose `unit` is a byte unit, normalised
  to bytes; `pg_setting_seconds{name}` for time units, normalised to seconds;
  and `pg_settings_pending_restart` as a count. Normalisation must handle the
  units PostgreSQL actually reports — `8kB`, `kB`, `MB`, `GB`, `ms`, `s`, `min`
  — and a unit it does not recognise must be **skipped with a warning**, never
  guessed.

  > **`archive_command` can contain credentials.** Redact it: emit the fact with
  > `ValueText` set to the first token of the command only, followed by
  > ` [redacted]` when there was more. State the redaction in the README.

- **Unit tests:** `TestSettings_AllowlistIsSorted`,
  `TestSettings_EmitsNonDefaultOutsideAllowlist`,
  `TestSettings_SkipsDefaultOutsideAllowlist`,
  `TestSettings_ByteUnitNormalisation` (table over `8kB`, `kB`, `MB`, `GB`),
  `TestSettings_TimeUnitNormalisation` (table over `ms`, `s`, `min`),
  `TestSettings_UnknownUnitIsSkipped`,
  `TestSettings_ArchiveCommandIsRedacted`,
  `TestSettings_PendingRestartCount`.
- **e2e tests:** `INT-SET-001` — against a real container, `ALTER SYSTEM SET
  work_mem = '64MB'` followed by `SELECT pg_reload_conf()`, then `Scrape`
  reports `pg_setting_bytes{name="work_mem"} = 67108864` and a `setting` fact
  whose `ValueText` is `64MB`.
  `INT-SET-002` — a setting requiring a restart reports `pending_restart` in its
  labels and increments the count gauge.
- **Done:** gates green + closed in `STATE.md`.

### 5.3 The `wal` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/wal.go` (new),
  `internal/check/wal_test.go` (new),
  `internal/check/wal_integration_test.go` (new)
- **Change:** implement a check named `wal`, written **against the column set
  measured in 5.1**, not against an assumption.

  ```go
  Requirements{
      Scope:    ScopeInstance,
      PermTier: pgtype.TierReadOnly,
  }
  DefaultInterval() = 15 * time.Second
  Timeout()         = 5 * time.Second
  ```

  Always collected, on every version and both roles:

  ```sql
  SELECT CASE WHEN pg_is_in_recovery()
              THEN pg_last_wal_replay_lsn()
              ELSE pg_current_wal_lsn()
         END AS lsn
  ```

  Convert the LSN to a `float64` byte offset — `pg_wal_lsn_diff(lsn, '0/0')`
  returns a `numeric`, which is the safe way to get it — and emit
  `pg_wal_lsn_bytes` as a **counter**, so the delta engine turns it into a WAL
  generation rate in bytes per second. That single metric is what the checkpoint
  and `wal_keep_size` advisor rules need.

  Then collect `pg_stat_wal`, selecting only the columns the probe reported for
  this connection, and emit each as `pg_wal_<column>` with counter kind for the
  `*_count` and `*_bytes` columns and gauge kind for the rest. Use the same
  probe-once-per-connection pattern as sub-phase 4.3. A column absent on this
  version is simply not emitted; the advisor rule that wants it degrades rather
  than failing (invariant I-3).

  Emit `pg_wal_stats_columns_available` as a gauge carrying the number of
  `pg_stat_wal` columns actually found, so an operator can see at a glance that
  a version gave less data rather than that the check is broken.

- **Unit tests:** `TestWAL_UsesReplayLSNOnStandby`,
  `TestWAL_UsesCurrentLSNOnPrimary`,
  `TestWAL_LSNIsACounter`,
  `TestWAL_ProbesColumnsOnce`,
  `TestWAL_SkipsAbsentColumns`,
  `TestWAL_EmitsColumnAvailabilityGauge`.
- **e2e tests:** `INT-WAL-001` — on a primary, generate WAL with the existing
  `test/workload/` helpers and assert `pg_wal_lsn_bytes` increases between two
  scrapes.
  `INT-WAL-002` — on a standby from `pgtest.PrimaryStandby()`, the check
  succeeds and reports the replay LSN, not an error.
  `INT-WAL-003` — the emitted metric name set is exactly as expected for each
  version, derived from `INT-WAL-000`'s measured column set.
- **Done:** gates green + `INT-WAL-003` green on all four versions + closed in
  `STATE.md`.

### 5.4 The `checkpointer` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/checkpointer.go` (new),
  `internal/check/checkpointer_test.go` (new),
  `internal/check/checkpointer_integration_test.go` (new)
- **Change:** implement a check named `checkpointer` that presents one stable
  metric set across the PG 17 view split (R1).

  ```go
  Requirements{
      Scope:    ScopeInstance,
      PermTier: pgtype.TierReadOnly,
  }
  DefaultInterval() = 30 * time.Second
  Timeout()         = 5 * time.Second
  ```

  On `PGVersion() >= pgtype.PG17`, read `pg_stat_checkpointer` for the
  checkpoint counters and `pg_stat_bgwriter` for what remains there. Below
  PG 17, read `pg_stat_bgwriter` alone. Map both onto the same names:

  | Stable metric | Kind | PG 15/16 source | PG 17/18 source |
  |---------------|------|-----------------|-----------------|
  | `pg_checkpoints_timed_total` | counter | `pg_stat_bgwriter.checkpoints_timed` | `pg_stat_checkpointer.num_timed` |
  | `pg_checkpoints_requested_total` | counter | `pg_stat_bgwriter.checkpoints_req` | `pg_stat_checkpointer.num_requested` |
  | `pg_checkpoint_write_time_ms_total` | counter | `pg_stat_bgwriter.checkpoint_write_time` | `pg_stat_checkpointer.write_time` |
  | `pg_checkpoint_sync_time_ms_total` | counter | `pg_stat_bgwriter.checkpoint_sync_time` | `pg_stat_checkpointer.sync_time` |
  | `pg_bgwriter_buffers_clean_total` | counter | `pg_stat_bgwriter.buffers_clean` | `pg_stat_bgwriter.buffers_clean` |
  | `pg_bgwriter_maxwritten_clean_total` | counter | `pg_stat_bgwriter.maxwritten_clean` | same |
  | `pg_buffers_alloc_total` | counter | `pg_stat_bgwriter.buffers_alloc` | same |

  > **Verify the PG 17/18 column names with the same probe technique as 5.1
  > before relying on them.** R1 establishes that the split happened; it does not
  > relieve the implementer of confirming the exact new column names against a
  > live container. If a name differs from the table above, use the measured one
  > and record the correction in `STATE.md` §8.

  The ratio `requested / (timed + requested)` is what the advisor uses to say
  "your `max_wal_size` is too small"; compute it in the advisor, not here. This
  check emits raw counters only.

- **Unit tests:** `TestCheckpointer_PG16UsesBgwriter`,
  `TestCheckpointer_PG17UsesCheckpointer`,
  `TestCheckpointer_MetricNamesIdenticalAcrossVersions`,
  `TestCheckpointer_AllCountersAreCounterKind`.
- **e2e tests:** `INT-BGW-001` — force a checkpoint with `CHECKPOINT` and assert
  `pg_checkpoints_requested_total` increased.
  `INT-BGW-002` — the metric name set is byte-identical on all four versions.
- **Done:** gates green + `INT-BGW-002` green on all four versions + closed in
  `STATE.md`.

### 5.5 The `io` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/io.go` (new), `internal/check/io_test.go` (new),
  `internal/check/io_integration_test.go` (new)
- **Change:** implement a check named `io`.

  ```go
  Requirements{
      Scope:    ScopeInstance,
      PermTier: pgtype.TierReadOnly,
      MinPG:    pgtype.PG16,   // R2
  }
  DefaultInterval() = 30 * time.Second
  Timeout()         = 5 * time.Second
  ```

  ```sql
  SELECT backend_type, object, context,
         reads, read_bytes, writes, write_bytes, extends, hits, evictions, fsyncs,
         read_time, write_time, fsync_time
  FROM pg_stat_io
  WHERE reads > 0 OR writes > 0 OR extends > 0 OR hits > 0
  ```

  Select only the columns `INT-WAL-000` confirmed exist on the version in hand —
  `pg_stat_io` gained columns between 16 and 18, and the same probe cache
  applies.

  Emit `pg_io_<column>` with labels `{backend_type, object, context}`, counters
  throughout. The `WHERE` clause matters: `pg_stat_io` has around 200 rows of
  which most are all-zero on a normal instance, and emitting them would burn a
  quarter of the instance's series budget on nothing.

  `read_time`, `write_time` and `fsync_time` are zero unless `track_io_timing`
  is on. Emit `pg_io_timing_enabled` as a gauge from `current_setting`, so a
  reader can tell "no I/O time" from "I/O time not measured" — the advisor uses
  it to emit a `degraded` finding rather than concluding the disk is instant.

- **Unit tests:** `TestIO_ExcludedBelowPG16`,
  `TestIO_FiltersAllZeroRows`,
  `TestIO_LabelsFromBackendTypeObjectContext`,
  `TestIO_EmitsTimingEnabledGauge`,
  `TestIO_SkipsAbsentColumns`.
- **e2e tests:** `INT-IO-001` — on PG 16, 17 and 18, generate reads and assert
  `pg_io_reads` is positive for `backend_type="client backend"`.
  `INT-IO-002` — on PG 15, the registry excludes the check with the reason
  `requires PG >= 16`, and the check never runs.
- **Done:** gates green + `INT-IO-002` proves the exclusion path + closed in
  `STATE.md`.

### 5.6 The `archiver` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/archiver.go` (new),
  `internal/check/archiver_test.go` (new),
  `internal/check/archiver_integration_test.go` (new)
- **Change:** implement a check named `archiver` (R3).

  ```go
  Requirements{
      Scope:    ScopeInstance,
      PermTier: pgtype.TierReadOnly,
  }
  DefaultInterval() = 60 * time.Second
  Timeout()         = 5 * time.Second
  ```

  ```sql
  SELECT archived_count, last_archived_wal, last_archived_time,
         failed_count, last_failed_wal, last_failed_time, stats_reset
  FROM pg_stat_archiver;

  SELECT pid, phase, backup_total, backup_streamed,
         tablespaces_total, tablespaces_streamed
  FROM pg_stat_progress_basebackup;
  ```

  Metrics:

  | Metric | Kind | Meaning |
  |--------|------|---------|
  | `pg_archiver_archived_total` | counter | successfully archived segments |
  | `pg_archiver_failed_total` | counter | failed attempts |
  | `pg_archiver_last_archived_age_seconds` | gauge | omitted when `last_archived_time` is null |
  | `pg_archiver_last_failed_age_seconds` | gauge | omitted when null |
  | `pg_archiver_failed_ratio` | gauge | failures since the last success: `1` when `last_failed_time > last_archived_time`, otherwise `0`. Feeds the seeded `archive.failing` rule |
  | `pg_archive_mode_enabled` | gauge | `1` when `archive_mode` is `on` or `always` |
  | `pg_basebackup_jobs_running` | gauge | rows in the progress view |
  | `pg_basebackup_progress_ratio` | gauge | `backup_streamed / NULLIF(backup_total, 0)`, labelled by `phase`, omitted when the denominator is zero |

  `stats_reset` goes into `Result.StatsReset` so the delta engine handles a
  `pg_stat_reset_shared('archiver')` correctly — this is the same mechanism the
  existing checks use and it is what keeps invariant I-2 of plan 001 true.

  When `archive_mode` is off, emit `pg_archive_mode_enabled = 0` and **nothing
  else**. A disabled archiver reporting zero failures reads as healthy, which is
  precisely the wrong conclusion; the advisor turns `enabled = 0` into an
  informational finding instead.

- **Unit tests:** `TestArchiver_FailedRatioWhenLastFailedIsNewer`,
  `TestArchiver_FailedRatioZeroWhenLastArchivedIsNewer`,
  `TestArchiver_OmitsAgesWhenNull`,
  `TestArchiver_ArchiveModeOffEmitsOnlyTheFlag`,
  `TestArchiver_StatsResetIsPropagated`,
  `TestArchiver_BasebackupRatioOmittedWhenTotalZero`.
- **e2e tests:** `INT-ARCH-001` — a container started with `archive_mode = on`
  and an `archive_command` that always fails reports
  `pg_archiver_failed_ratio = 1` and a growing `pg_archiver_failed_total`.
  `INT-ARCH-002` — with `archive_command = '/bin/true'`,
  `pg_archiver_failed_ratio` is 0 and `pg_archiver_archived_total` grows after
  `SELECT pg_switch_wal()`.
  `INT-ARCH-003` — with `archive_mode = off`, only `pg_archive_mode_enabled` is
  emitted.
- **Done:** gates green + closed in `STATE.md`.

### 5.7 Settings API and cluster drift

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/api_settings.go` (new),
  `internal/server/api_settings_test.go` (new),
  `internal/server/api_settings_integration_test.go` (new),
  `internal/server/http.go` (modified)
- **Change:** two endpoints.

  `GET /api/v1/instances/{id}/settings` returns the `object_facts` rows of kind
  `setting` for that instance: `name`, `value`, `unit`, `source`, `context`,
  `pending_restart`, `first_seen`, `last_seen`, `changed_at`. Support
  `?changed_since=<RFC3339>` to answer "what changed before the incident", which
  is the question this table exists for.

  `GET /api/v1/clusters/{id}/settings-drift` compares every instance in the
  cluster and returns only the settings where they differ (plan 002 D11):

  ```json
  [
    {"name": "hot_standby_feedback",
     "values": [{"instance_id": "…", "role": "primary", "value": "off"},
                {"instance_id": "…", "role": "standby", "value": "on"}]}
  ]
  ```

  Exclude from the comparison the settings that are legitimately per-instance:
  `listen_addresses`, `port`, `data_directory`, `cluster_name`,
  `synchronous_standby_names`, `primary_conninfo`, `primary_slot_name`,
  `hot_standby`, and anything whose `context` is `internal`. Declare that list
  as a sorted constant beside the allowlist of 5.2, with a one-line comment per
  entry saying why it differs legitimately. A drift report that flags `port`
  every time is a report nobody reads.

- **Unit tests:** `TestSettingsAPI_ChangedSinceFilter`,
  `TestDriftAPI_ReportsOnlyDifferences`,
  `TestDriftAPI_ExcludesPerInstanceSettings`,
  `TestDriftAPI_SingleInstanceClusterReturnsEmpty`.
- **e2e tests:** `INT-SET-003` — with a primary and a standby whose
  `hot_standby_feedback` differ, the drift endpoint reports exactly that one
  setting.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 5.8 Integration sweep and permission verification

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `internal/check/registry_integration_test.go` (modified),
  `deploy/sql/monitoring_user.sql` (modified only if a grant is genuinely
  missing)
- **Change:** extend the tier table so the five new checks are covered as a
  T0-only user across all four versions, exactly as sub-phase 4.7 did.

  Expect one real finding here: `pg_stat_archiver` and the progress views are
  readable by `pg_monitor`, but `archive_command` is only visible to a superuser
  or to a role granted `pg_read_all_settings`. If the shipped monitoring script
  does not already grant it, add it and record the change in `STATE.md` §8 — and
  make sure the check degrades gracefully when it is absent rather than failing,
  since a fleet may have installations created by an older version of the
  script.

- **Unit tests:** none (this sub-phase is the tests).
- **e2e tests:** `INT-CHECK-017` — the extended tier table covering `settings`,
  `wal`, `checkpointer`, `io`, `archiver`.
  `INT-SET-004` — as a role without `pg_read_all_settings`, the `settings` check
  still succeeds and simply omits the settings it cannot read, reporting a skip
  reason.
- **Done:** both green on all four versions + `SYS-PERM-001` still passes +
  closed in `STATE.md`.

### 5.9 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** update these sections for what this phase actually made usable:
  - **Checks** — add `settings`, `wal`, `checkpointer`, `io` and `archiver` with
    interval, scope, tier and, for `io`, the PG 16 minimum.
  - **Configuration** — the five intervals with defaults.
  - **Usage** — `curl` examples for `/api/v1/instances/{id}/settings` including
    `changed_since`, and for `/api/v1/clusters/{id}/settings-drift`, with
    expected JSON.
  - **Limitations** — `archive_command` is **redacted** to its first token
    because it can contain credentials. I/O timings are zero unless
    `track_io_timing` is enabled, and pglens reports which case applies rather
    than assuming. `pg_stat_io` is unavailable below PostgreSQL 16. pglens
    reports whether WAL archiving is working; it does **not** verify that a
    backup can be restored, and it does not integrate with pgBackRest, Barman or
    WAL-G.
  - **Requirements** — if sub-phase 5.8 added a grant, document that existing
    installations should re-run `deploy/sql/monitoring_user.sql`, and that the
    script is idempotent.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the `curl` examples were executed and produced the
  documented output.
- **Done:** a new user can see which settings differ between their primary and
  standby from the README alone; the "we do not verify restorability" limit is
  stated plainly; gates green; closed in `STATE.md` with the §11 docs row for
  phase 5 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `make test-integration`
- **Coverage:** `make coverage-gate`
- **Regression guard:** `INT-CHECK-015`, `INT-CHECK-016`, `SYS-PERM-001` and
  every phase 2 test must still pass.
- **README:** the redaction, the `track_io_timing` caveat and the restorability
  limit are documented.

## Phase done criterion

R7 is no longer `UNVERIFIED`: the `pg_stat_wal` and `pg_stat_io` column sets are
measured and asserted per version by `INT-WAL-000`. The five new checks emit an
identical metric name set on every supported version, an instance with a broken
`archive_command` reports `pg_archiver_failed_ratio = 1`, and the drift endpoint
reports a `hot_standby_feedback` mismatch between primary and standby.
`INT-WAL-000` to `INT-WAL-003`, `INT-SET-001` to `INT-SET-004`, `INT-BGW-001`,
`INT-BGW-002`, `INT-IO-001`, `INT-IO-002`, `INT-ARCH-001` to `INT-ARCH-003` and
`INT-CHECK-017` are green. README.md reflects this phase's shipped behavior, and
`STATE.md` §11 shows phase 5 `DONE` with every sub-phase closed.
