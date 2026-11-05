/**
 * The caption catalogue (D30): what each chart, summary card, table and
 * section shows, where the figure comes from, its unit and window. Writing
 * rules are in the plan (phase 17): `what` says what is measured in one plain
 * sentence, `source` names the PostgreSQL view or pglens computation checked
 * against the code, estimates say "estimated", and every acronym is in the
 * glossary. A wrong id fails the type check.
 */

import { instanceCaptions } from './captionsets/instance'
import { fleetCaptions } from './captionsets/fleet'
import { queriesCaptions } from './captionsets/queries'
import { alertsCaptions } from './captionsets/alerts'
import { indexesCaptions } from './captionsets/indexes'
import { activityCaptions } from './captionsets/activity'
import { settingsCaptions } from './captionsets/settings'

export interface CaptionEntry {
  /** What is measured, one sentence (at most 160 characters). */
  what: string
  /** The PostgreSQL view/column or pglens computation the figure comes from. */
  source: string
  unit?: string
  window?: string
  /** How to read the figure or the shape of the chart; never claims a cause. */
  howToRead?: string
  /** The product's own thresholds (advisor rules, heuristics), when there are any. */
  thresholds?: string
}

/** Each route group keeps its entries in `captionsets/`; ids must be unique across them. */
export const CAPTIONS = {
  'overview.tps': {
    what: 'Transactions committed per second in the selected database.',
    source: 'pg_stat_database.xact_commit (pg_xact_commit_total), as a rate per second.',
    unit: 'per second',
    window: 'Selected time range.',
    howToRead:
      'A flat line at zero means no committed work; a gap means no sample was stored for that time.',
  },
  'overview.cache-hit-ratio': {
    what: "Share of page reads found in PostgreSQL's own memory cache (shared buffers) rather than requested from the operating system.",
    source:
      'pg_stat_database.blks_hit / (blks_hit + blks_read), from pg_blks_hit_total and pg_blks_read_total.',
    unit: '%',
    window: 'Newest sample of the selected range.',
    howToRead:
      'Reads that miss shared buffers may still come from the operating system cache, so a lower ratio is not always disk I/O.',
  },
  'index.duplicate': {
    what: 'Indexes that share an identical definition with another index on the same table; each group wastes the space and write cost of all but one.',
    source:
      'pglens advisor rule index.duplicate: indexes of one table with the same access method and the same normalised text between the first and last parenthesis (columns, expressions, predicate); uniqueness is not compared.',
    unit: 'indexes, in groups of two or more',
    window: 'Latest advisor evaluation.',
  },
  'index.redundant-prefix': {
    what: 'Indexes whose columns are the leading part of a longer index on the same table; the longer index can serve the same lookups.',
    source:
      'pglens advisor rule index.redundant_prefix: non-unique btree indexes whose key columns are a strict leading part of another btree index on the same table with the same predicate.',
    unit: 'indexes',
    window: 'Latest advisor evaluation.',
    howToRead:
      'Check that the longer index covers the same queries before removing the shorter one.',
  },
  'activity.connections-used': {
    what: 'Client connections open now compared with max_connections.',
    source:
      'Backends counted from pg_stat_activity (pg_connections_used) and the max_connections setting (pg_connections_limit).',
    unit: 'connections',
    window: 'Latest sample.',
    thresholds: 'The advisor warns when connections go above 80 % of max_connections.',
  },
  'settings.names': {
    what: 'Names for clusters and instances, shown in titles, breadcrumbs, server pickers, filters and alert and event tables instead of identifiers and addresses.',
    source:
      'clusters.name and instances.display_name, set with renameCluster and renameInstance and read from the cluster and instance lists.',
    unit: 'names of up to 64 characters',
    window: 'Current names; the lists refresh after each rename.',
    howToRead:
      'In this table a muted label marked default means no name is set: the cluster shows as Cluster and its identifier, the instance as its address and port.',
  },
  'settings.names-table': {
    what: 'One row per cluster followed by one row per instance in it; Rename opens the name editor.',
    source: 'The cluster list (getClusters) and the instance list (getInstances).',
    window: 'Current names.',
    howToRead: 'Names are unique without regard to case; an empty name restores the default label.',
  },
  'settings.preferences': {
    what: 'Console preferences kept in this browser: the theme, the default time range, the refresh interval and the table page size.',
    source:
      'The pglens.preferences entry of this browser’s local storage; nothing is sent to the server.',
    window: 'Applied at once to every open page of this browser and kept after a reload.',
    howToRead:
      'Each control states its effect. A range or page size in the address still wins over the default; Reset to defaults restores every preference.',
  },
  'settings.retention': {
    what: 'Days of history pglens keeps for each group of metrics; older data is deleted once a day.',
    source:
      'The retention and compression policies of each hypertable (timescaledb_information.jobs), read with getRetention and replaced with setRetention.',
    unit: 'days',
    window:
      'Live policies. A policy runs once a day and deletes whole blocks of stored data (chunks of 6 hours; 1 day for bloat) that are older than its retention.',
    howToRead:
      'Preview shows what a value would delete; a shorter retention asks for confirmation and deletes that history at the next policy run.',
  },
  'settings.retention-table': {
    what: 'One row per group of metrics: its retention now, the days it accepts, and a new value to preview and apply.',
    source: 'getRetention for the rows, previewRetention for the preview, setRetention to apply.',
    unit: 'days',
    window: 'Live policies.',
    howToRead:
      'The shortest retention is one day past the family’s compression horizon and never under 3 days; the longest is 365 days.',
  },
  'settings.advisor-rules-table': {
    what: 'The advisor rules, one per row with category, severity, scope and minimum permission tier; a switch makes pglens run or skip each one.',
    source:
      'getAdvisorRules for the rows; the switch calls setAdvisorRuleOverride, stored in advisor_rule_overrides.',
    window: 'Live catalogue. A change takes effect at the next advisor evaluation.',
    howToRead:
      'Which advisor rules run. Minimum tier is the permission tier (T0 to T2) an instance needs for the rule to be evaluated. Switched off, a rule is not evaluated and its findings are resolved at the next evaluation. Switched on again, the rule runs at the next evaluation and its findings reopen if the condition still holds. custom marks a stored override; switching back deletes it. Search and filters change no rule; thresholds are not editable.',
  },
  'settings.advisor-windows': {
    what: 'How far back the unused-index and backup-history checks look before they report a finding.',
    source:
      'The windows stored by setAdvisorWindows (server_settings), read with getAdvisorWindows; without a stored value, 7 days.',
    unit: 'hours or days',
    window: 'Between 1 hour and 90 days.',
    howToRead:
      'A shorter window reports sooner and can raise false positives. The advisor applies a saved window at its next evaluation. An index counts as unused once the history pglens has kept covers the whole window and pglens saw no scan of it inside the window; a scan older than the window does not count. On PostgreSQL 16 and later the scan counters of the server itself can judge an index at once, without that history: one never scanned since a counter reset a window old (or at an unknown time), or last scanned before the window with no scan pglens saw inside it. Indexes the check never reports (10 MiB or smaller, primary key, unique) get no not-enough-history finding either. pglens keeps index history for its retention (30 days by default): a longer unused-index window is never covered by that history, so an index judged on it alone stays degraded, with a reason naming both lengths; the scans of a standby are read from the same kept history. The backup-history window only sets how long an instance must be monitored before a missing base backup (a full copy of the data files, the starting point for restoring) is reported; whatever the window, a base backup counts as recent for 7 days (No recent base backup) and as a backup strategy for 30 days (No visible backup strategy), and one seen longer ago than that is reported at once.',
  },
  'settings.notifications': {
    what: 'Which alert delivery channels are configured, from the server environment or the console, the host each posts to, and a test message per channel.',
    source:
      'getNotificationChannels reads the environment and the addresses stored from the console (an address itself is never sent); setNotificationChannel and deleteNotificationChannel store and remove one; Send test calls testNotificationChannel.',
    window:
      'Read when the tab opens. An address stored from the console applies at once and overrides the environment; a change of the environment needs a server restart.',
    howToRead:
      'Send test posts "pglens test notification" through the same channel the alerts use and waits at most 10 s; a failure shows the error with the address removed.',
  },
  'instance.readiness': {
    what: 'What is not set up on this server, so some pglens features show less than they could, and how to close each gap.',
    source:
      'getReadiness: pglens rules over the settings, extensions, role membership, skipped checks and agent switches it already stores (settings facts, check_skip, agent_flags).',
    window: 'Latest values; recomputed at most every 30 s.',
    howToRead:
      'A warning means a feature is lost or degraded; a note is off by design or optional. Each fix names where to make the change and whether it needs a restart.',
  },
  'fleet.readiness': {
    what: 'Whether the agent can monitor every server of the cluster in full, or what is missing.',
    source:
      'getReadinessSummary: the readiness level of each instance, counted per cluster (warnings and notes added up).',
    window: 'Latest values; refreshed every 15 s.',
    howToRead:
      'Partial means at least one server has a warning: open it, then its Readiness tab, for the fixes.',
  },
  'instance.coverage': {
    what: 'How many of the checks the agent schedules for this instance have stored a result since the agent last started.',
    source:
      'getCoverage: the start time and check intervals the agent sends with every push, merged with the newest stored result of each check (check_results).',
    unit: 'checks',
    window: 'Since the agent started; refreshed every 15 s.',
    howToRead:
      'After a start every check runs once within 30 s to 10 min, depending on its interval; until then pages may show gaps. A failed check returned an error on its last run, so it has no current result: the banner stays and names it until a run succeeds.',
  },
  'indexes.search': {
    what: 'Indexes, tables, columns and statements whose name, table, definition, column type, text, queryid or database contains the term, in the picked database if any.',
    source:
      'searchInstance: the index definitions and table columns the agent reported (index_def and table_columns facts) and the statement texts of pg_stat_statements (query_texts), matched ignoring case.',
    window:
      'Objects seen and statements run in the selected time range. Each group lists at most 50 matches and counts them all.',
    howToRead:
      'The marked text is what matched. A link opens the index or table, expanded, on its tab, or the statement search on Slowest queries, in the same time range.',
  },
  'indexes.suggestions': {
    what: 'Candidate btree indexes for the columns your statements filter or sort on and no index leads with, best first. Every figure is an estimate.',
    source:
      'getIndexSuggestions: the normalised statements of pg_stat_statements (query_texts) parsed for equality, range, prefix and ordering predicates, with the pg_stat_statements rates, the reported index definitions and column statistics, and the table counters.',
    unit: 'weight in % of the database statement time; read cost in ms per second',
    window: 'The window picked on the tab (1 hour to 7 days).',
    howToRead:
      'Weight and read cost are an upper bound of the time the statements spend today: an index cannot save more. Size comes from planner row counts. Write rates are inserts, updates and deletes per second. pglens never creates an index; review the statement before you run it.',
    thresholds:
      'Verdict: read/write ratio of 10 or more is strong, 1 or more moderate, below 1 write-heavy; a table with no index writes is at least moderate. Tables under 1,000 rows are skipped.',
  },
  'locks.sessions-application': {
    what: 'Sessions opened by your applications: one row per client session of the latest lock sample, oldest transaction first.',
    source:
      'getLocks sessions: client backends of pg_stat_activity (at most 100), classed as application by the server from application_name and the monitoring role.',
    unit: 'sessions',
    window: 'Latest lock sample, taken every 10 s.',
    howToRead:
      'Expand a row for every field of the session and its signal actions. Query is the normalised statement unless marked raw.',
  },
  'locks.sessions-pglens': {
    what: 'Connections opened by pglens itself to collect these metrics; they are not your workload.',
    source:
      'getLocks sessions classed as pglens by the server: application_name pglens, pglens/… or pglens-agent/…, or the monitoring role.',
    unit: 'sessions',
    window: 'Latest lock sample, taken every 10 s.',
    howToRead:
      'A session of your application never shows here, and a pglens session never shows in the application table.',
  },
  'locks.connection-summary': {
    what: 'Client connections counted by origin, application, user, client address, database and state, with each share of the total.',
    source:
      'getLocks connections: counted from the very sessions of the two tables; when the sample is cut, the counts of the activity scrape (pg_connections_* and pg_backends).',
    unit: 'connections',
    window: 'Latest lock sample, taken every 10 s.',
    howToRead:
      'A client address of local is a Unix socket. Counts per application need checks.activity.by_application.',
  },
  'locks.origin': {
    what: 'Who opened the session: app is a connection of your application, pglens a connection pglens opened itself.',
    source: 'The origin the server sets on each session of getLocks.',
    window: 'Latest lock sample.',
  },
  'index.history-scans': {
    what: 'How often queries scanned this index, per second, averaged over each bucket of the selected range.',
    source:
      'getIndexHistory: pg_stat_user_indexes.idx_scan, stored as a per-second rate every 5 minutes and averaged per bucket.',
    unit: 'scans per second',
    window: 'Selected time range, at most 30 days; buckets of 10 minutes or more.',
    howToRead:
      'A gap means no sample was stored for that bucket; nothing is filled in. A flat line at zero means no scan, which is not a verdict that the index is unused.',
  },
  'index.history-size': {
    what: 'Size of this index at the end of each bucket of the selected range.',
    source:
      'getIndexHistory: pg_relation_size of the index (index_bytes) as stored every 5 minutes; the last value of each bucket.',
    unit: 'bytes',
    window: 'Selected time range, at most 30 days; buckets of 10 minutes or more.',
    howToRead:
      'A gap means no sample was stored for that bucket. A rebuild shows as a drop; growth is the table growing or the index bloating.',
  },
  'indexes.tax': {
    what: 'What the indexes of each table cost: their number, size, unused space and the index entries each write adds. Every figure is an estimate.',
    source:
      'The index statistics (pg_stat_user_indexes) and the table statistics (pg_stat_user_tables) of the latest collection, combined in the browser; the unused size follows the advisor verdict or the raw scan counter.',
    unit: 'bytes, writes per second, index entries per second',
    window: 'Latest collection; rates are per second over the last interval.',
    howToRead:
      'Write amplification is inserts, deletes and updates that are not heap-only per second, times the number of indexes. Unique and primary key indexes are never counted as unused. Unused size is a candidate to review, not a verdict.',
  },
  ...instanceCaptions,
  ...fleetCaptions,
  ...queriesCaptions,
  ...alertsCaptions,
  ...indexesCaptions,
  ...activityCaptions,
  ...settingsCaptions,
} as const satisfies Record<string, CaptionEntry>

export type CaptionId = keyof typeof CAPTIONS

/** Short hints for table column headers (D30), shown by the column's ⓘ. */
export const columnHelp = {
  'statements.mean-time': 'Average execution time of one call, from pg_stat_statements.',
  'statements.total-time':
    'Execution time of all calls in the selected range, from pg_stat_statements.',
  'slots.retained-bytes':
    "WAL kept for this slot: bytes between the server's current WAL position and the oldest position the slot may still need.",
  'slots.wal-before-invalidation':
    'Bytes of WAL that can still be written before PostgreSQL may remove WAL this slot needs. Unlimited: no value reported, as when max_slot_wal_keep_size is -1.',
  'slots.age-inactive':
    "Time since the slot's last active reading in the stored history; a dash means it is active now.",
  'logical.errors':
    'Errors counted by pg_stat_subscription_stats since its last reset: apply worker errors first, table synchronization errors second.',
  'logical.conflicts':
    'Conflicts the subscription detected, from pg_stat_subscription_stats since its last reset; not available before PostgreSQL 18.',
  'catalog.sequence-limit':
    "Last value the sequence can give that its column accepts: the nearer of the sequence's own bound and the column type's bound.",
  'catalog.sequence-used':
    'Share of the range from the start value to the limit already handed out, from the last value read. A reason shows when it cannot be read.',
  'catalog.partitions-reach':
    "Highest upper bound among the table's range partitions, in UTC. No end means a partition has no upper limit.",
  'catalog.extension-packages':
    "Default version of the extension in the server's installed packages, from pg_available_extensions.",
  'catalog.extension-update':
    'Available when the packaged version is newer than the installed one; unknown when the packages lack the extension or the versions cannot be compared.',
  'catalog.function-own-time':
    "Milliseconds per second spent in the function's own code, not in functions it calls, averaged over the last hour. Total time includes them.",
  'internals.slru-zeroed':
    'Blocks per second the cache filled with zeros, averaged over the last hour, from pg_stat_slru.blks_zeroed.',
  'internals.slru-flushes':
    'Flushes of dirty cache data to disk per second, averaged over the last hour, from pg_stat_slru.flushes.',
  'internals.slru-truncations':
    'Times per second the cache was truncated, averaged over the last hour, from pg_stat_slru.truncates.',
  'relations.relation':
    'Schema and name of the table; on the Indexes tab, the table the index belongs to. The same name can appear once per database.',
  'relations.live-tuples':
    'Rows the statistics count as live, from pg_stat_user_tables.n_live_tup; an estimate kept by PostgreSQL.',
  'relations.dead-tuples':
    'Row versions left by updates and deletes that vacuum has not yet removed, from pg_stat_user_tables.n_dead_tup.',
  'relations.dead-ratio':
    'Dead rows divided by live rows, so it can pass 100 %; it is zero when the table has no live rows.',
  'relations.seq-scan-rate':
    'Times per second the whole table was read from start to end, averaged over the last scrape interval.',
  'relations.idx-scan-rate':
    'Times per second the index was used to find rows, averaged over the last scrape interval.',
  'relations.last-vacuum':
    'Newer of the manual and the automatic vacuum time; auto marks an automatic one.',
  'relations.last-analyze':
    'Newer of the manual and the automatic analyze time; auto marks an automatic one.',
  'relations.last-scan':
    'When a query last used the index since the statistics were reset; PostgreSQL 15 does not report it.',
  'relations.valid':
    'An index that is not valid is not used by queries, for example after a failed concurrent build.',
  'relations.real-size': 'Size of the table or index on disk when the agent measured it.',
  'relations.expected-size':
    'Size it would have with no wasted space, estimated from row counts and column statistics.',
  'relations.bloat':
    'Real size minus expected size: space the estimate says holds no live data; zero when it is not larger than expected.',
  'relations.bloat-ratio': 'Bloat divided by real size.',
  'relations.method':
    'Estimate is a statistical estimate of the whole table or index; pgstattuple is an exact measurement of one table.',
  'instance.setting-source':
    'Where the current value was set, from pg_settings.source, such as default or configuration file.',
  'instance.setting-context':
    'From pg_settings.context: postmaster means a change needs a restart, sighup means a configuration reload.',
  'instance.setting-changed': 'When pglens last saw the value of this setting change.',
  'instance.setting-state':
    'Pending restart: the configuration holds a new value that only a restart puts in force, from pg_settings.pending_restart.',
  'instance.durability-fsync':
    'off: PostgreSQL does not force its writes to disk, so a crash or power loss can corrupt the data.',
  'instance.durability-full-page-writes':
    'off: changed pages are not written whole to the WAL after a checkpoint, so a crash can leave a page that recovery cannot repair.',
  'instance.durability-archive-mode':
    'off: finished WAL files are not copied anywhere, so restoring a backup to a point in time is not possible.',
  'instance.durability-wal-level':
    'minimal: the WAL keeps only what crash recovery needs, so there is no replication and no point-in-time recovery.',
  'instance.durability-synchronous-commit':
    'off: a commit is confirmed before its WAL is flushed to disk, so a crash can lose the latest commits.',
  'internals.temp-files-rate':
    'Temporary files created per minute in this database, averaged over the last hour, from pg_stat_database.temp_files.',
  'internals.recovery-cancelled':
    'Queries cancelled per minute on this standby for this reason, averaged over the last hour, from pg_stat_database_conflicts.',
  'statements.share-of-listed':
    "This statement's total time divided by the total time of all statements listed in the table; unlisted statements are not counted.",
  'statements.cache-hit-ratio':
    "Shared-buffer block hits divided by hits plus reads for this statement over the range; a read may still come from the operating system's cache.",
  'statements.wal':
    'WAL bytes written by this statement over the selected range, estimated from per-second rates.',
  'statements.temp-bytes-per-call':
    'Temporary bytes written per call over the selected range: temporary blocks written, 8 KiB each, divided by calls.',
  'indexes.scans':
    'Index scans per second over the last collection interval, from pg_stat_user_indexes.idx_scan.',
  'indexes.flags-column':
    'Labels on the index: its kind, such as primary, unique or partial, and the advisor findings on it.',
  'indexes.cache-hit':
    "Share of the index's block reads served from PostgreSQL's own memory cache, from pg_statio_user_indexes.",
  'indexes.last-scan':
    'When a query last scanned the index; PostgreSQL 16 or later records it, older servers show Not available.',
  'indexes.fk-columns': 'The referencing columns of the foreign key; no index leads with them.',
  'indexes.fk-references':
    'The table the foreign key points to; deleting or re-keying its rows scans the table that holds the key.',
  'indexes.fk-severity-column':
    'Warning when the referencing table is 10 MiB or larger, info below that.',
  'indexes.cleanup-reason-column':
    'Why the index is listed: advisor findings for invalid, duplicate, redundant prefix and unused; raw counters only for never scanned.',
  'indexes.cleanup-writes':
    'Entries per second that inserts and updates that are not HOT add to this index; a drop saves them.',
  'indexes.dead-rows-column':
    'Row versions left by updates and deletes that vacuum has not removed yet, with the estimated space they take.',
  'indexes.dead-live':
    'Dead rows divided by live rows; above 100% the table holds more dead than live row versions.',
  'indexes.vacuum-due':
    'Dead rows divided by the count at which automatic vacuum starts; 1.0× or more means vacuum is due.',
  'indexes.dead-last-vacuum':
    'Time of the latest manual or automatic vacuum; Never means none is recorded.',
  'indexes.hot-share':
    "Share of the table's updates that were HOT: they wrote no entry in any index.",
  'indexes.dead-writes': 'Inserts, updates and deletes per second on the table, added together.',
  'indexes.dead-trend':
    'Dead rows of the table over the selected time range; only the 10 worst tables have a trend.',
  'indexes.suggest-weight':
    "Share of the database's tracked statement time that the statements which would use this index account for.",
  'indexes.suggest-read-cost':
    'Statement time per second spent in the statements that would use the index; an upper bound of what it could save.',
  'indexes.suggest-size':
    "Estimated size of the new index, from the planner's row count and the column widths.",
  'indexes.suggest-writes': 'Inserts, updates and deletes per second on the table, in that order.',
  'indexes.suggest-verdict':
    'The reads the index would serve compared with the writes it would cost: strong, moderate or write-heavy.',
  'indexes.suggest-statements':
    'Number of tracked statements that filter or sort on the key columns.',
  'indexes.tax-index-size':
    "Combined size of the table's indexes that the agent reported; indexes it did not report are not counted.",
  'indexes.tax-table-size': "Size of the table's heap alone, without its indexes and TOAST data.",
  'indexes.tax-ratio':
    'Combined index size as a percentage of the heap size; above 100% the indexes are larger than the table.',
  'indexes.tax-unused':
    'Space of indexes with no scan: advisor means the advisor judged them unused, raw counters means no scan since the statistics reset.',
  'indexes.tax-writes':
    'Inserts, deletes and updates that are not HOT, per second: each one writes an entry in every index of the table.',
  'indexes.tax-amplification':
    "Writes per second times the number of indexes: the index entries the table's writes add each second, estimated.",
  'sessions.pid': 'Process ID of the PostgreSQL server process that serves this session.',
  'sessions.state':
    'State of the session in pg_stat_activity: active, idle, idle in transaction and so on.',
  'sessions.wait':
    'What the session is waiting for now, as wait event type and wait event; empty when it is not waiting.',
  'sessions.transaction-age':
    'Time since the current transaction of this session began; zero when it has none open.',
  'sessions.query-age':
    'Time since the latest query of this session started; for an idle session, the age of its last query.',
  'advisor.enabled':
    'Off: the rule is not evaluated and its findings are resolved at the next evaluation. On again: they reopen if the condition still holds.',
  'settings.permission-tier':
    'T0 reads statistics only, T1 also runs EXPLAIN for query plans, T2 also cancels or ends other sessions.',
  'settings.instance-state':
    'Up: the instance reported within the last 60 seconds. Down: it did not report within that time.',
  'settings.agent-state':
    'Down when the instance has not reported within 60 seconds or an agent-down alert is open for it; otherwise Reporting.',
  'settings.skip-reason':
    'Why the agent does not monitor this database; db_budget means it already monitors as many databases as its budget allows. A dash: monitored.',
  'settings.audit-requested-by':
    'The credential that asked: api-token, or ui-session with a short hash of the session, then the client address; not a person.',
  'settings.audit-state':
    'requested, ok, error, rejected or expired. A request the agent never claimed shows as expired once it ran out in the queue.',
  'rules.rule': 'What the rule checks, in one sentence, followed by its rule id.',
  'rules.needs': 'The data the rule reads, in plain words.',
  'rules.tier': 'Permission tier the rule needs: T0 reads statistics only. T0 rules are always on.',
  'rules.threshold':
    'The value the metric must pass for the alert to fire, with its unit. A rule raised by an event has no threshold.',
  'rules.duration': 'How long the condition must hold before the alert fires; 0 s fires at once.',
  'findings.object':
    'The table, index or other object the finding is about, then its database, instance and cluster, by name.',
  'findings.scope':
    'What the finding covers: an instance, a cluster, a database, or one database object such as a table or index.',
  'findings.state':
    'open: the problem is present now. muted: you silenced it for a time. degraded: the rule could not be evaluated. resolved: the problem is gone.',
  'events.details':
    'What to check for that event; a failover shows the old and the new primary, a counter reset shows how many counters reset.',
  'alerts.notes':
    'The silence that suppresses the alert, with the time it has left, and a link to the checklist for agent alerts.',
  'alerts.suppression':
    'Suppressed: a silence stops its notifications. Unknown: the silences could not be loaded.',
  'silences.matchers':
    'A label of the alert compared with a value: = is exact, ~ is a regular expression, not reverses it. An alert must match all of them.',
} as const satisfies Record<string, string>

export type ColumnHelpId = keyof typeof columnHelp

export function captionOf(id: CaptionId): CaptionEntry {
  return CAPTIONS[id]
}
