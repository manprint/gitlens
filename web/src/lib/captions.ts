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
} as const satisfies Record<string, CaptionEntry>

export type CaptionId = keyof typeof CAPTIONS

/** Short hints for table column headers (D30), shown by the column's ⓘ. */
export const columnHelp = {
  'statements.mean-time': 'Average execution time of one call, from pg_stat_statements.',
  'statements.total-time':
    'Execution time of all calls in the selected range, from pg_stat_statements.',
  'indexes.scans': 'Index scans counted by pg_stat_user_indexes.idx_scan since the last reset.',
} as const satisfies Record<string, string>

export type ColumnHelpId = keyof typeof columnHelp

export function captionOf(id: CaptionId): CaptionEntry {
  return CAPTIONS[id]
}
