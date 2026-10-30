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
] as const

export type TileUnit = (typeof TILE_UNITS)[number]

/**
 * The text shown after a tile's figure. Units are spelled out (D30), so today
 * this is the unit itself; it is the one place to change a unit's display.
 */
export function formatUnit(unit: TileUnit): string {
  return unit
}

export function isTileUnit(value: string): value is TileUnit {
  return (TILE_UNITS as readonly string[]).includes(value)
}
