import { describe, expect, it } from 'vitest'

import {
  formatChange,
  formatCompareValue,
  formatRatio,
  isWorse,
  periodB,
  periodParams,
  presetPeriods,
  readComparePreset,
} from './compare'

const bFrom = new Date('2026-11-21T10:00:00Z')
const bTo = new Date('2026-11-21T12:00:00Z')

describe('presetPeriods', () => {
  it('UI-CMP-001 A is the period before B, the same hours a day or a week earlier, or the custom period', () => {
    expect(presetPeriods('previous', bFrom, bTo)).toEqual({
      from: new Date('2026-11-21T08:00:00Z'),
      to: new Date('2026-11-21T10:00:00Z'),
    })
    expect(presetPeriods('yesterday', bFrom, bTo)).toEqual({
      from: new Date('2026-11-20T10:00:00Z'),
      to: new Date('2026-11-20T12:00:00Z'),
    })
    expect(presetPeriods('week', bFrom, bTo)).toEqual({
      from: new Date('2026-11-14T10:00:00Z'),
      to: new Date('2026-11-14T12:00:00Z'),
    })
    const custom = { from: new Date('2026-11-01T00:00:00Z'), to: new Date('2026-11-02T00:00:00Z') }
    expect(presetPeriods('custom', bFrom, bTo, custom)).toEqual(custom)
    expect(presetPeriods('custom', bFrom, bTo, { from: custom.from })).toBeNull()
    expect(presetPeriods('custom', bFrom, bTo, { from: custom.to, to: custom.from })).toBeNull()
    expect(readComparePreset('week')).toBe('week')
    expect(readComparePreset('nope')).toBe('previous')
    expect(readComparePreset(null)).toBe('previous')
  })

  it('B ends on whole minutes and the four bounds go out as ISO instants', () => {
    const b = periodB(new Date('2026-11-21T10:00:42.5Z'), new Date('2026-11-21T11:00:59Z'))
    expect(periodParams(presetPeriods('previous', b.from, b.to) ?? b, b)).toEqual({
      a_from: '2026-11-21T09:00:00.000Z',
      a_to: '2026-11-21T10:00:00.000Z',
      b_from: '2026-11-21T10:00:00.000Z',
      b_to: '2026-11-21T11:00:00.000Z',
    })
  })
})

describe('formatRatio', () => {
  it('UI-CMP-002 a ratio, new, gone or none', () => {
    expect(formatRatio(3)).toBe('×3.0')
    expect(formatRatio(0.456, 'both')).toBe('×0.5')
    expect(formatRatio(null, 'new')).toBe('new')
    expect(formatRatio(0, 'gone')).toBe('gone')
    expect(formatRatio(null)).toBe('—')
    expect(formatRatio(Number.NaN)).toBe('—')
    expect(isWorse(2, 'both')).toBe(true)
    expect(isWorse(1.99, 'both')).toBe(false)
    expect(isWorse(null, 'new')).toBe(false)
  })

  it('formats values per second and signed changes', () => {
    expect(formatCompareValue('calls', 1.5)).toBe('1.5/s')
    expect(formatCompareValue('mean_exec_time', 2.5)).toBe('2.5 ms')
    expect(formatCompareValue('wal_bytes', 1536)).toBe('1.5 KiB/s')
    expect(formatCompareValue('rows', null)).toBe('—')
    const calls = (size: number) => formatCompareValue('calls', size)
    expect(formatChange(1.5, calls)).toBe('+1.5/s')
    expect(formatChange(-1.5, calls)).toBe('−1.5/s')
    expect(formatChange(0, calls)).toBe('0/s')
    expect(formatChange(null, calls)).toBe('—')
  })
})
