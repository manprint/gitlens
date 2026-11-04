/**
 * The caption catalogue (D30): what each chart, summary card, table and
 * section shows, where the figure comes from, its unit and window. Writing
 * rules are in the plan (phase 17): `what` says what is measured in one plain
 * sentence, `source` names the PostgreSQL view or pglens computation checked
 * against the code, estimates say "estimated", and every acronym is in the
 * glossary. A wrong id fails the type check.
 */

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
    what: 'Share of block reads served from shared buffers in the selected database.',
    source:
      'pg_stat_database.blks_hit / (blks_hit + blks_read), from pg_blks_hit_total and pg_blks_read_total.',
    unit: '%',
    window: 'Selected time range.',
    howToRead:
      'Reads that miss shared buffers may still come from the operating system cache, so a lower ratio is not always disk I/O.',
  },
  'index.duplicate': {
    what: 'Groups of indexes on the same table with identical definitions; each group wastes the space and write cost of all but one index.',
    source:
      'pglens advisor rule index.duplicate: indexes of one table with the same access method and the same normalised text between the first and last parenthesis (columns, expressions, predicate); uniqueness is not compared.',
    unit: 'index groups',
    window: 'Latest advisor evaluation.',
  },
  'index.redundant-prefix': {
    what: 'Indexes whose columns are the leading part of a longer index on the same table; the longer index can serve the same lookups.',
    source:
      'pglens advisor rule index.redundant_prefix: non-unique btree indexes whose columns lead a longer index.',
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
    thresholds: 'The advisor rule conn.saturation warns above 80 % of max_connections.',
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
    what: 'Days of history kept per metrics family before TimescaleDB drops its oldest chunks.',
    source:
      'The retention and compression policies of each hypertable (timescaledb_information.jobs), read with getRetention and replaced with setRetention.',
    unit: 'days',
    window:
      'Live policies. A policy runs once a day and drops whole chunks (6 hours of data; 1 day for bloat) older than its retention.',
    howToRead:
      'Preview shows what a value would delete; a shorter retention asks for confirmation and deletes that history at the next policy run.',
  },
  'settings.retention-table': {
    what: 'One row per metrics family: its retention now, the days it accepts, and a new value to preview and apply.',
    source: 'getRetention for the rows, previewRetention for the preview, setRetention to apply.',
    unit: 'days',
    window: 'Live policies.',
    howToRead:
      'The shortest retention is one day past the family’s compression horizon and never under 3 days; the longest is 365 days.',
  },
  'settings.advisor-rules-table': {
    what: 'Switched off, a rule is skipped and its findings are resolved.',
    source:
      'getAdvisorRules for the rows; the switch calls setAdvisorRuleOverride, stored in advisor_rule_overrides.',
    window: 'Live catalogue. A change takes effect at the next advisor evaluation.',
    howToRead:
      'Which advisor rules run. Switched off, a rule is not evaluated and its findings are resolved at the next evaluation. Switched on again, the rule runs at the next evaluation and its findings reopen if the condition still holds. custom marks a stored override; switching back deletes it. Search and filters change no rule; thresholds are not editable.',
  },
  'settings.advisor-windows': {
    what: 'How far back the unused-index and backup-history checks look before they report a finding.',
    source:
      'The windows stored by setAdvisorWindows (server_settings), read with getAdvisorWindows; without a stored value, 7 days.',
    unit: 'hours or days',
    window: 'Between 1 hour and 90 days.',
    howToRead:
      'A shorter window reports sooner and can raise false positives. The advisor applies a saved window at its next evaluation. An index is judged unused once pglens history covers the unused-index window and pglens recorded no scan of it inside the window; a scan older than the window does not count. On PostgreSQL 16 and later the statistics can judge an index at once, without pglens history: one never scanned since a reset a window old (or at an unknown time), or last scanned before the window with no scan pglens recorded inside it. Indexes the check never reports (10 MiB or smaller, primary key, unique) get no not-enough-history finding either. pglens keeps index metrics for their retention (30 days by default): a longer unused-index window is never covered by pglens history, so an index judged on that history alone stays degraded, with a reason naming both lengths; the scans of a standby are read from the same kept history. The backup-history window only sets how long an instance must be monitored before a missing base backup is reported; whatever the window, a base backup counts as recent for 7 days (No recent base backup) and as a backup strategy for 30 days (No visible backup strategy), and one seen longer ago than that is reported at once.',
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
} as const satisfies Record<string, CaptionEntry>

export type CaptionId = keyof typeof CAPTIONS

/** Short hints for table column headers (D30), shown by the column's ⓘ. */
export const columnHelp = {
  'statements.mean-time': 'Average execution time of one call, from pg_stat_statements.',
  'statements.total-time':
    'Execution time of all calls in the selected range, from pg_stat_statements.',
  'indexes.scans': 'Index scans counted by pg_stat_user_indexes.idx_scan since the last reset.',
  'advisor.enabled':
    'Off: the rule is not evaluated and its findings are resolved at the next evaluation. On again: they reopen if the condition still holds.',
} as const satisfies Record<string, string>

export type ColumnHelpId = keyof typeof columnHelp

export function captionOf(id: CaptionId): CaptionEntry {
  return CAPTIONS[id]
}
