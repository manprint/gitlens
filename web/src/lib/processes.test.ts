import { describe, expect, it } from 'vitest'

import {
  formatByteRate,
  formatCoreShare,
  isProcessGrouping,
  PROCESS_GROUPINGS,
  snapshotIsEmpty,
} from './processes'

describe('processes', () => {
  it('formats CPU as a share of one core, above 100 % for several cores', () => {
    expect(formatCoreShare(0.256, 'en-US')).toBe('25.6%')
    expect(formatCoreShare(2, 'en-US')).toBe('200.0%')
    expect(formatCoreShare(0.5, 'it-IT')).toBe('50,0%')
  })

  it('formats a byte rate and keeps an unknown one null', () => {
    expect(formatByteRate(2048, 'en-US')).toBe('2.0 KiB/s')
    expect(formatByteRate(null, 'en-US')).toBeNull()
    expect(formatByteRate(Number.NaN, 'en-US')).toBeNull()
  })

  it('knows the groupings', () => {
    expect(PROCESS_GROUPINGS[0]).toBe('none')
    expect(isProcessGrouping('datname')).toBe(true)
    expect(isProcessGrouping('query')).toBe(false)
  })

  it('an answer without a sample is empty', () => {
    expect(snapshotIsEmpty(undefined)).toBe(false)
    expect(snapshotIsEmpty({ ts: null, processes: [] })).toBe(true)
    expect(snapshotIsEmpty({ ts: '2026-11-21T10:00:00Z', processes: [] })).toBe(true)
  })
})
