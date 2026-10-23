# What pglens does not do

pglens is an observability and analysis backend. Its measurements and advice
are deliberately bounded: a missing observation is not evidence that the
underlying PostgreSQL condition is healthy.

## 1. Backups and recovery

pglens observes PostgreSQL archiving: archived and failed WAL segments, the age
of the last successful and failed archive, and the segments waiting in
`archive_status`. It sees a base backup only while one runs, in
`pg_stat_progress_basebackup`, sampled once a minute: a shorter backup, or one
taken outside the replication protocol (file-level tools, storage snapshots),
is never seen. It does not verify that a base backup succeeded, that a restore
works, that an archive destination is readable, or that a backup is
restorable. No pglens archiver metric is proof of recovery readiness.

## 2. Bloat is an estimate by default

Scheduled bloat is a statistics-based estimate of table heaps, computed from
`pg_stats`. It can be wrong in either direction, especially for wide rows,
heavily TOASTed rows, or stale statistics. It skips relations under 1 MiB,
tables never analysed, and every table with a column the monitoring role
cannot read (PostgreSQL hides that column's statistics), which at T0 is usually
every table. Indexes are not estimated, so the `index.bloat_high` rule has no
data to evaluate. The exact figure requires the optional `pgstattuple`
extension, installed by an administrator, and is collected on demand only, at
T1; that operation performs a full relation scan.

## 3. Query plans are captured on request

Plans are captured on demand from normalized statement text in
`pg_stat_statements`, which replaces every constant with a placeholder. On
PostgreSQL 16 and later such a statement is planned with `GENERIC_PLAN`: a plan
for unknown values, which can differ from the one PostgreSQL picks for
particular values. On PostgreSQL 15 it cannot be explained and is reported as
such. pglens does not invent parameter values, so a returned plan describes a
statement that PostgreSQL actually recorded, not an imagined query with guessed
parameters.

## 4. `EXPLAIN ANALYZE` executes the statement

`EXPLAIN ANALYZE` is refused unless the agent sets
`allow_explain_analyze: true`. Even then it runs only a statement that starts
with `SELECT`, `VALUES`, `WITH`, or `TABLE` and has no parameter placeholder;
because `pg_stat_statements` replaces every constant with one, that means a
statement recorded without constants. It runs in a `READ ONLY` transaction
under a 30-second statement timeout and is always rolled back, so PostgreSQL
itself rejects writes, including data-modifying CTEs and `nextval()`, and a
notification is discarded with the rollback.

A read-only transaction does not contain a function whose effect lies outside
it: `pg_cancel_backend()` and `pg_terminate_backend()` when the monitoring role
holds `pg_signal_backend` (the T2 grant), session-level advisory locks (which
survive `ROLLBACK`), `pg_logical_emit_message()`, `txid_current()`, remote work
through `dblink`, and anything an untrusted procedural language can do. Before
executing, the agent therefore inspects the statement, the database and the
`EXPLAIN (VERBOSE)` plan — where views are expanded and SQL functions inlined —
and refuses `EXPLAIN ANALYZE`, naming the reason, when:

- the plan calls a user-defined function (its body is not part of the plan, and
  the volatility it declares is not enforced by PostgreSQL), a function of an
  extension outside a short list of extensions that only compute (PostGIS,
  `pg_trgm`, `pgcrypto`, `hstore`, `citext`, ...), a volatile built-in function
  outside a list of built-ins that only read or compute, or a function it
  cannot identify;
- the plan contains a `Foreign Scan` or a `Custom Scan`;
- the database has a user-defined operator, cast, domain constraint or
  aggregate that calls such a function, because a plan does not always show
  those calls (`CASE x WHEN y` and `NULLIF` hide the equality operator, an
  implicit cast shows as its argument);
- the monitoring session is a superuser.

Two defences do not depend on reading a plan. Planning can itself run code —
PostgreSQL folds calls to `IMMUTABLE` functions with constant arguments and
evaluates `STABLE` ones for estimates — so a session that holds
`pg_signal_backend` plans as `pg_read_all_data` (and is refused if it cannot
switch), and the connection an `EXPLAIN`, with or without `ANALYZE`, used is
closed afterwards instead of returning to the pool, so a lock or setting left in
the session ends with it. What remains outside every check is a function its
owner declared `IMMUTABLE` or `STABLE` although it has effects outside the
transaction (it runs while the statement is planned, before anything can be
inspected, and can switch the role back with `set_config('role', ...)`), and a
function or view replaced concurrently between the inspected plan and the
executed one.

The statement still executes. It takes the locks a normal run takes, can wait
behind a conflicting lock and so queue ahead of later requests, uses CPU,
memory, temporary files, and I/O until it finishes or times out, and can write
WAL for hint bits or page pruning. Keep `allow_explain_analyze: false` unless
running the recorded statements is acceptable for the target.

## 5. Relation cardinality is bounded

Above each relation check's `top_n` budget (per database), relation-level
series are truncated by rank. The API reports `truncated: true` with a
`relations_not_reported` of at least one: the agent does not count what it
left out, so the figure is a lower bound. Truncated relations are not sampled
elsewhere and are not backfilled later.

## 6. Host metrics require a local agent

Host metrics are available only for an instance beside the agent, as indicated
by `host_local`. For a remote instance the host endpoint returns
`available: false` with a reason. pglens does not infer host memory, CPU, disk,
or cgroup limits from inside PostgreSQL.

## 7. Advisor findings are heuristics

Advisor thresholds are tuned for general-purpose OLTP workloads. A data
warehouse or another unusual workload can legitimately trigger several rules.
Findings are advice with an attached reason, not verdicts; review the reason
and disagree with it when the workload justifies doing so.

## 8. Scrapes do not see sub-interval events

There is no sampling below a check's scrape interval. A lock held for 200 ms
between two 15-second scrapes is invisible, and post-processing cannot recover
it. Active-session-history sampling narrows this window for the checks where it
is enabled; it does not eliminate the gap.

## 9. Supported PostgreSQL versions are limited

The supported and verified range is PostgreSQL 15 through 19; 19 is verified
on its release candidate (19rc1) and is verified again at its release. Older
versions lack views or columns used by this plan. Newer versions are untested
until their system-view column sets have been probed and verified in the same
way as the PostgreSQL 18 and 19 compatibility work.

## 10. Tenant separation is not an isolation boundary

The schema carries `tenant_id` through the data model, but the current product
does not provide per-tenant isolation or a user/tenant role model. There is no
per-tenant data boundary or per-user audit trail; in practice this is a
single-tenant deployment. Do not treat `tenant_id` as an authorization boundary.

## 11. Authentication is shared

Browser and API sessions use one deployment-wide `PGLENS_UI_PASSWORD`. pglens
does not provide user accounts, roles, or per-user permissions. The session
cookie is temporary and is lost when the server restarts, so operators must sign
in again. Protect the service with the network controls appropriate to the
deployment; a session is not a substitute for an authorization boundary.

## 12. Pooler state is outside the product view

pglens connects to PostgreSQL instances and does not expose a pooler view. It
does not report pool sizes, queue depth, transaction-pooling state, or pooler
health. When PgBouncer or another pooler is in use, inspect and alert on that
component separately.

## 13. Every in-memory structure is bounded, and a bound that trips drops data

A monitoring system that grows without limit under load fails exactly when it is
most needed, so every long-lived structure on both sides has a ceiling. Each
ceiling is a deliberate trade: past it, data is dropped or rejected rather than
queued forever.

**Server, per request:**

- A push body is capped at **32 MiB on the wire**. Larger is rejected with 413.
- A gzipped push is capped at **256 MiB after decompression**. `MaxBytesReader`
  only bounds what arrives on the wire, and gzip's ratio on the repetitive JSON
  an envelope is made of is easily three orders of magnitude, so without this
  second cap a single 32 MiB request could expand to tens of gigabytes and take
  the server out of memory. Exceeding it is a 413 and a
  `pglens_ingest_rejected_total{reason="decompressed_too_large"}` increment.
- Request headers are capped at 64 KiB, and the server enforces read, write, and
  idle timeouts of 2 minutes with a 10-second header timeout. A client that
  opens a connection and sends nothing holds it for 10 seconds, not forever.

**Server, over time:**

- Delta and topology state for an instance is evicted **one hour** after the last
  sample for it. An instance that stops reporting stops costing memory; when it
  comes back, its first sample is a fresh baseline and emits no rate, the same
  as a cold start.
- Browser/API sessions are swept on creation past **1024** live sessions and hard
  capped at **4096**. At the cap, the sessions closest to expiry are evicted
  first, so the practical effect of hitting it is that the oldest sign-ins are
  asked to sign in again.

**Agent:**

- Without a disk buffer, at most **1024 envelopes** are held in memory. Past
  that the oldest are dropped and counted the same way every other drop is —
  they reach the server as `pglens_samples_dropped_rate`, which is what makes
  the `agent_buffer_full` alert rule fire — rather than the queue growing until
  the process is OOM-killed. With a disk buffer configured, this queue is not
  the durability mechanism; the buffer is.
- Per-scope cardinality state (one selector per instance × database) is dropped
  after **6 hours** without use, so a database that is dropped or removed from
  the target list does not keep its top-N state alive for the lifetime of the
  process.

These numbers are not configurable. They are chosen to be far above any real
deployment and to exist purely so that an abnormal one degrades visibly instead
of dying.
