/**
 * The closed list of units a summary card may show (D31): a tile's figure is
 * one number in one of these units, never free text.
 */
export const TILE_UNITS = [
  'ms',
  's',
  'min',
  'h',
  'days',
  '%',
  'bytes',
  'KiB/s',
  'MiB/s',
  'per second',
  'per minute',
  'rows',
  'rows/s',
  'connections',
  'sessions',
  'queries',
  'statements',
  'indexes',
  'tables',
  'databases',
  'findings',
  'alerts',
  'checks',
  'checkpoints',
  'files',
  'deadlocks',
  'foreign keys',
  'instances',
  'clusters',
  'CPUs',
  'transactions',
] as const

export type TileUnit = (typeof TILE_UNITS)[number]

/**
 * The text shown after a tile's figure. Units are spelled out (D30), so today
 * this is the unit itself; it is the one place to change a unit's display.
 */
export function formatUnit(unit: TileUnit, value?: unknown): string {
  if (value === 1 || value === '1') return SINGULAR_UNIT[unit] ?? unit
  return unit
}

/** The unit of a countable noun when the figure is exactly 1 (`1 index`, not `1 indexes`). */
const SINGULAR_UNIT: Partial<Record<TileUnit, string>> = {
  rows: 'row',
  connections: 'connection',
  sessions: 'session',
  queries: 'query',
  statements: 'statement',
  indexes: 'index',
  tables: 'table',
  databases: 'database',
  findings: 'finding',
  alerts: 'alert',
  checks: 'check',
  checkpoints: 'checkpoint',
  files: 'file',
  deadlocks: 'deadlock',
  'foreign keys': 'foreign key',
  instances: 'instance',
  clusters: 'cluster',
  CPUs: 'CPU',
  transactions: 'transaction',
}

export function isTileUnit(value: string): value is TileUnit {
  return (TILE_UNITS as readonly string[]).includes(value)
}
