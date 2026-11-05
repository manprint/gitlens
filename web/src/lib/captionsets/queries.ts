import type { CaptionEntry } from '../captions'

/** Captions of the queries route group (D30); rules in the plan, phase 17. */
export const queriesCaptions = {
  'queries.statements-section': {
    what: 'Statements recorded by pg_stat_statements on this instance, ranked by the chosen measure and cut to the top 50.',
    source:
      'Server statements endpoint over metrics_statements, written by the stat_statements check from pg_stat_statements; one row per queryid and database, ranked and limited on the server.',
    unit: 'Statements; each row carries calls, milliseconds, rows, blocks and bytes.',
    window: 'Selected time range.',
    howToRead:
      'PostgreSQL evicts entries beyond pg_stat_statements.max, so a statement can appear and disappear between ranges. The search box matches query text, queryid or database over the whole range.',
  },
  'queries.statements-table': {
    what: 'Per-statement totals over the selected range: calls, execution time, rows, cache hit ratio, WAL written and temporary blocks written.',
    source:
      'Server statements endpoint: calls, time, rows, shared block hits and reads, WAL and temporary blocks are rates summed over the range; shares and ratios are computed in the browser.',
    unit: 'Calls, milliseconds, rows, percent, bytes.',
    window: 'Selected time range.',
    howToRead:
      'Mean time is total time divided by calls. Share of listed rows is a statement total time over the total of the listed rows. Cache hit ratio is shared block hits over hits plus reads. Temporary bytes/call is temporary blocks written, 8192 bytes each, divided by calls. The column sort buttons reorder the loaded rows only.',
  },
  'queries.ranking': {
    what: 'The measure the server ranks statements by before keeping the top 50; the column sort buttons only reorder the rows already loaded.',
    source:
      'Server statements endpoint order_by parameter: total_exec_time, mean_exec_time, calls, rows, shared_blks_read, temp_blks_written or wal_bytes, summed over the range.',
    unit: 'Measure (milliseconds, calls, rows, blocks or bytes), highest first.',
    window: 'Selected time range.',
  },
  'slow.slowest-mean': {
    what: 'Highest mean execution time per call among the ranked statements.',
    source:
      'Maximum, over the listed statements, of total execution time divided by calls, from the server statements endpoint over metrics_statements.',
    unit: 'Milliseconds, shown in the largest fitting unit.',
    window: 'Selected time range.',
    howToRead:
      'Only the statements loaded by the page are compared; with the Total time ranking a slower statement may be outside the list.',
  },
  'slow.total-time': {
    what: 'Execution time summed over all ranked statements, not over the whole server.',
    source:
      'Sum of each listed statement total execution time (pg_stat_statements.total_exec_time, stored as a rate and multiplied by sample seconds).',
    unit: 'Milliseconds, shown in the largest fitting unit.',
    window: 'Selected time range.',
  },
  'slow.calls': {
    what: 'Number of calls summed over the ranked statements.',
    source:
      'Sum of each listed statement calls (pg_stat_statements.calls, stored as a rate and multiplied by sample seconds).',
    unit: 'Calls.',
    window: 'Selected time range.',
  },
  'slow.statements-ranked': {
    what: 'Number of statements the page loaded and ranked, up to 200.',
    source:
      'Count of the rows returned by the server statements endpoint, requested with a limit of 200.',
    unit: 'Statements.',
    window: 'Selected time range.',
    howToRead: 'At 200 the list is cut at the page limit.',
  },
  'slow.table': {
    what: 'Statements ranked by mean time per call or by total time, with their calls, mean time, total time and share of the listed time.',
    source:
      'Server statements endpoint over metrics_statements; Share of listed time is computed in the browser as a statement total time over the total of the ranked statements.',
    unit: 'Calls, milliseconds, percent.',
    window: 'Selected time range.',
    howToRead:
      'Rank 1 has the highest value of the chosen ranking. The share is relative to the listed statements, not to all database time.',
  },
  'slow.rank-by': {
    what: 'Chooses whether statements are ranked by mean time per call or by total time.',
    source:
      'Server statements endpoint order_by: mean_exec_time (total time divided by calls) or total_exec_time.',
    unit: 'Milliseconds, highest first.',
    window: 'Selected time range.',
  },
  'queries.detail-mean-time': {
    what: 'Average execution time of one call of this statement.',
    source:
      'Total execution time divided by calls for this queryid and database, from the server statements endpoint over metrics_statements.',
    unit: 'Milliseconds, shown in the largest fitting unit.',
    window: 'Selected time range.',
  },
  'queries.detail-total-time': {
    what: 'Execution time of all calls of this statement.',
    source:
      'pg_stat_statements.total_exec_time for this queryid and database, stored as a rate and summed over the range by the server statements endpoint.',
    unit: 'Milliseconds, shown in the largest fitting unit.',
    window: 'Selected time range.',
  },
  'queries.detail-calls': {
    what: 'Number of times this statement ran.',
    source:
      'pg_stat_statements.calls for this queryid and database, stored as a rate and summed over the range by the server statements endpoint.',
    unit: 'Calls.',
    window: 'Selected time range.',
  },
  'queries.detail-rows-per-call': {
    what: 'Rows returned or affected per call of this statement, rounded to one decimal.',
    source:
      'pg_stat_statements.rows divided by calls for this queryid and database, from the server statements endpoint.',
    unit: 'Rows per call.',
    window: 'Selected time range.',
  },
  'queries.detail-cache-hit-ratio': {
    what: 'Share of the shared blocks this statement accessed that were found in shared buffers.',
    source:
      'pg_stat_statements.shared_blks_hit divided by shared_blks_hit plus shared_blks_read, computed in the browser from the server statements endpoint.',
    unit: '%',
    window: 'Selected time range.',
    howToRead:
      'Reads that miss shared buffers may still come from the operating system cache, so a lower ratio is not always disk I/O.',
  },
  'queries.detail-wal': {
    what: 'Write-ahead log generated by all calls of this statement.',
    source:
      'pg_stat_statements.wal_bytes for this queryid and database, stored as a rate and summed over the range by the server statements endpoint.',
    unit: 'Bytes.',
    window: 'Selected time range.',
  },
  'findings.list': {
    what: 'Problems found by the advisor rules on the collected statistics, one row per finding with its severity, state and evidence.',
    source:
      'Server findings endpoint: rows of the findings table written by the advisor engine after each rule evaluation; open and degraded findings come first, at most 1000.',
    unit: 'Findings.',
    window: 'State at the latest advisor evaluation of each rule.',
    howToRead:
      'Open: the rule condition held at its last evaluation. Degraded: the rule could not be evaluated, which is not a healthy result. Muted: silenced until its expiry. Resolved: the condition has cleared.',
  },
  'findings.summary': {
    what: 'Counts of open findings per severity and of findings whose rule could not be evaluated; each button filters the list.',
    source:
      'Counted in the browser from every finding the server returned for the current search, before the state, scope, cluster and instance filters.',
    unit: 'Findings.',
    window: 'Latest advisor evaluation.',
    howToRead:
      'Muted and resolved findings are not counted. Rules not evaluated counts degraded findings, not rules.',
  },
  'findings.filter-state': {
    what: 'Shows findings in one state; the default lists open and degraded ones and hides muted and resolved ones.',
    source:
      'findings.state, set by the advisor engine and by mute actions, filtered in the browser.',
    unit: 'Findings.',
    window: 'State at the latest advisor evaluation.',
    howToRead:
      'Degraded means the rule could not run, for lack of a permission tier or an input. A muted finding returns to open when the mute expires and the condition still holds.',
  },
  'findings.filter-severity': {
    what: 'Shows findings of one severity, critical, warning or info, as set by the advisor rule that raised them.',
    source:
      'Rule severity declared in the advisor registry (internal/advisor) and stored in findings.severity, filtered in the browser.',
    unit: 'Findings.',
    window: 'State at the latest advisor evaluation.',
  },
  'findings.filter-scope': {
    what: 'Shows findings whose rule judges a whole cluster or a single instance.',
    source:
      'findings.scope, set by the advisor rule that raised the finding, filtered in the browser.',
    unit: 'Findings.',
    window: 'State at the latest advisor evaluation.',
    howToRead:
      'Findings on a database or a table have their own scope and show only under All scopes.',
  },
  'findings.rule-catalogue': {
    what: 'Every advisor rule with its severity, scope, inputs and permission tier, and how many targets it currently flags or cannot evaluate.',
    source:
      'Server advisor rules endpoint, the rule registry in internal/advisor; the firing and cannot-evaluate counts are computed in the browser from the findings table.',
    unit: 'Rules; counts in targets.',
    window: 'Registry as built; counts at the latest advisor evaluation.',
    howToRead:
      'A disabled rule is not evaluated, so its counts stay at zero. Rules are enabled or disabled in Settings, Advisor rules.',
  },
  'findings.rule-catalogue-table': {
    what: 'One row per advisor rule: severity, scope, needed inputs, minimum permission tier, and the targets with an open or degraded finding.',
    source:
      'Server advisor rules endpoint for the rule columns; the counts are distinct targets in the findings the page loaded, at most 1000.',
    unit: 'Rules; counts in targets.',
    window: 'Registry as built; counts at the latest advisor evaluation.',
    howToRead:
      'Firing instances counts distinct targets with an open finding of the rule, instances for instance rules and clusters for cluster rules. Cannot evaluate counts targets whose finding is degraded.',
  },
  'findings.catalogue-tier': {
    what: 'Shows the rules that run at or below the chosen permission tier of the monitoring role.',
    source:
      'Rule minimum tier from the advisor registry: T0 pg_monitor and pg_control_system(), T1 adds pg_read_all_data, T2 adds pg_signal_backend (internal/pgtype/role.go).',
    unit: 'Permission tier, T0 to T2.',
    window: 'Registry as built.',
    howToRead:
      'On an instance whose tier is lower than the rule minimum, the rule reports a degraded finding instead of being evaluated.',
  },
} as const satisfies Record<string, CaptionEntry>
