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

  it('CARD-006 a figure of exactly 1 takes the singular unit', () => {
    expect(formatUnit('indexes', '1')).toBe('index')
    expect(formatUnit('statements', 1)).toBe('statement')
    expect(formatUnit('foreign keys', '1')).toBe('foreign key')
    expect(formatUnit('indexes', '12')).toBe('indexes')
    expect(formatUnit('indexes', '0')).toBe('indexes')
    expect(formatUnit('indexes', '1,000')).toBe('indexes')
    expect(formatUnit('per second', '1')).toBe('per second')
    expect(formatUnit('%', '1')).toBe('%')
  })
})
