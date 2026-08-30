# What pglens does not do

pglens is an observability and analysis backend. Its measurements and advice
are deliberately bounded: a missing observation is not evidence that the
underlying PostgreSQL condition is healthy.

## 1. Backups and recovery

pglens observes PostgreSQL archiving: `archived_count`, `failed_count`, and the
age of the last successful archive. It does not verify that `pg_basebackup`
succeeded, that a restore works, that an archive destination is readable, or
that a backup is restorable. No pglens archiver metric is proof of recovery
readiness.

## 2. Bloat is an estimate by default

Scheduled bloat is a statistics-based estimate. It can be wrong in either
direction, especially for wide rows, heavily TOASTed rows, or stale
statistics. The exact figure requires the optional `pgstattuple` extension and
is collected on demand only; that operation performs a full relation scan.

## 3. Query plans are captured on request

Plans are captured on demand from normalized statement text in
`pg_stat_statements`. A statement whose placeholders make it unexplainable is
reported as such. pglens does not invent parameter values, so a returned plan
describes a statement that PostgreSQL actually recorded, not an imagined query
with guessed parameters.

## 4. `EXPLAIN ANALYZE` executes the statement

`EXPLAIN ANALYZE` is run inside a transaction and rolled back, but the work
still happens. Locks are taken, triggers fire, and WAL can be written. Effects
outside the transaction, such as `NOTIFY`, `COPY TO PROGRAM`, or work done via
`dblink`, are not undone by that rollback. Keep `allow_explain_analyze: false`
unless this behavior is acceptable for the target.

## 5. Relation cardinality is bounded

Above the configured relation budget, relation-level series are truncated by
rank. The API reports `truncated: true` and the number of relations not
reported. Truncated relations are not sampled elsewhere and are not backfilled
later.

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

The supported and verified range is PostgreSQL 15 through 18. Older versions
lack views or columns used by this plan. Newer versions are untested until
their system-view column sets have been probed and verified in the same way as
the PostgreSQL 18 compatibility work.

## 10. Tenant separation is not an isolation boundary

The schema carries `tenant_id` through the data model, but the current product
does not authenticate users or separate tenants. In practice this is a single-
tenant deployment. Do not treat `tenant_id` as an authorization boundary.
