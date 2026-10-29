import { describe, expect, it } from 'vitest'

import {
  countResets,
  formatAxisTime,
  formatRangeLabel,
  formatStepLabel,
  formatTooltipTime,
} from './chartTime'

const HOUR = 3_600_000
const at = Date.UTC(2026, 9, 29, 9, 41)

describe('chart time labels', () => {
  it('CTIME-001 shows HH:mm for a 1 h span', () => {
    expect(formatAxisTime(at, HOUR)).toBe('09:41')
  })

  it('CTIME-002 adds the date for a 48 h span', () => {
    expect(formatAxisTime(at, 48 * HOUR)).toBe('Oct 29 09:41')
  })

  it('CTIME-003 keeps HH:mm at exactly 36 h', () => {
    expect(formatAxisTime(at, 36 * HOUR)).toBe('09:41')
    expect(formatAxisTime(at, 36 * HOUR + 1)).toBe('Oct 29 09:41')
  })

  it('CTIME-004 formats the tooltip time in UTC with seconds', () => {
    expect(formatTooltipTime(at)).toBe('2026-10-29 09:41:00 UTC')
  })

  it('CTIME-005 states the range and the step', () => {
    expect(formatRangeLabel(new Date(at), new Date(at + HOUR), 60)).toBe(
      'Oct 29 09:41 → 10:41 UTC · step 1m',
    )
    expect(formatRangeLabel(new Date(at), new Date(at + 24 * HOUR), 300)).toBe(
      'Oct 29 09:41 → Oct 30 09:41 UTC · step 5m',
    )
  })

  it('CTIME-006 labels steps with the shortest exact unit', () => {
    expect(formatStepLabel(15)).toBe('15s')
    expect(formatStepLabel(60)).toBe('1m')
    expect(formatStepLabel(300)).toBe('5m')
    expect(formatStepLabel(3600)).toBe('1h')
    expect(formatStepLabel(86_400)).toBe('1d')
  })

  it('CTIME-007 counts only null buckets with values on both sides', () => {
    expect(countResets([null, 1, null, null, 2, null])).toBe(2)
    expect(countResets([null, null, 3])).toBe(0)
    expect(countResets([])).toBe(0)
  })

  it('CTIME-008 drops the step when it is unknown', () => {
    const from = new Date('2026-10-29T09:41:00Z')
    const to = new Date('2026-10-29T10:41:00Z')
    expect(formatRangeLabel(from, to, 0)).toBe('Oct 29 09:41 → 10:41 UTC')
    expect(formatRangeLabel(from, to, -5)).toBe('Oct 29 09:41 → 10:41 UTC')
  })
})
