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
      'A shorter window reports sooner and can raise false positives. The advisor keeps its 7-day windows until a later release reads these values.',
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
