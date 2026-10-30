import { describe, expect, it } from 'vitest'

import { TILE_UNITS, formatUnit, isTileUnit } from './units'

describe('tile units (D31)', () => {
  it('CARD-005 formats every unit of the closed list', () => {
    // Units are spelled out (D30).
    expect(formatUnit('per second')).toBe('per second')
    expect(formatUnit('per minute')).toBe('per minute')
    expect(formatUnit('ms')).toBe('ms')
    expect(formatUnit('%')).toBe('%')
    expect(formatUnit('connections')).toBe('connections')
    for (const unit of TILE_UNITS) expect(formatUnit(unit)).not.toBe('')
  })

  it('CARD-005 recognises only the units of the list', () => {
    expect(isTileUnit('bytes')).toBe(true)
    expect(isTileUnit('9 redundant prefix')).toBe(false)
    expect(new Set(TILE_UNITS).size).toBe(TILE_UNITS.length)
  })
})
