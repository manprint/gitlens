import { describe, expect, it } from 'vitest'

import { assertMatchesContract } from '@/test/contract'
import { base as preview, lengthening } from '@/test/fixtures/previewRetention'

import {
  chunkCount,
  confirmationText,
  formatMiB,
  isShortening,
  previewSummary,
  RETENTION_FAMILY_LABELS,
  retentionErrorMessage,
  validateRetentionDays,
} from './retention'

describe('retention rules', () => {
  it('RET-UI-005 days are a whole number between the family minimum and 365', () => {
    expect(validateRetentionDays('14', 3, 365)).toEqual({ ok: true, value: 14 })
    expect(validateRetentionDays(' 8 ', 8, 365)).toEqual({ ok: true, value: 8 })
    expect(validateRetentionDays('365', 3, 365)).toEqual({ ok: true, value: 365 })
    expect(validateRetentionDays('', 3, 365)).toEqual({
      ok: false,
      error: 'Enter a number of days.',
    })
    for (const text of ['1.5', '-3', '1e2', 'ten', '0x10']) {
      expect(validateRetentionDays(text, 3, 365), text).toEqual({
        ok: false,
        error: 'Enter a whole number of days.',
      })
    }
    expect(validateRetentionDays('2', 3, 365)).toEqual({
      ok: false,
      error: 'Enter between 3 and 365 days.',
    })
    expect(validateRetentionDays('7', 8, 365).ok).toBe(false)
    expect(validateRetentionDays('366', 3, 365).ok).toBe(false)
  })

  it('RET-UI-005 a shorter retention, or one for a family without a policy, is a shortening', () => {
    expect(isShortening(30, 14)).toBe(true)
    expect(isShortening(null, 365)).toBe(true)
    expect(isShortening(30, 30)).toBe(false)
    expect(isShortening(30, 60)).toBe(false)
  })

  it('RET-UI-005 the confirmation names chunks, MiB, family and days as the contract writes it', () => {
    expect(confirmationText(preview)).toBe(
      'This will permanently delete 64 chunks (~40 MiB) of metrics_ash data older than 14 days at the next policy run. Take a backup first.',
    )
    expect(
      confirmationText({ ...preview, chunks_to_drop: 1, bytes_to_free: 3 * 1024 * 1024 }),
    ).toBe(
      'This will permanently delete 1 chunk (~3.0 MiB) of metrics_ash data older than 14 days at the next policy run. Take a backup first.',
    )
    expect(formatMiB(0)).toBe('0.0')
    expect(formatMiB(1000)).toBe('0.1')
    expect(formatMiB(1536 * 1024)).toBe('1.5')
    expect(formatMiB(2048 * 1024 * 1024)).toBe('2,048')
    expect(chunkCount(1200)).toBe('1,200 chunks')
  })

  it('RET-UI-005 the preview summary says what Apply would delete', () => {
    expect(previewSummary(preview, 30)).toBe('Deletes 64 chunks (~40 MiB) at the next policy run.')
    expect(previewSummary(lengthening, 30)).toBe('Keeps more history: nothing is deleted.')
    expect(previewSummary({ ...preview, chunks_to_drop: 0, bytes_to_free: 0 }, 30)).toBe(
      'Nothing older than 14 days is stored now.',
    )
  })

  it('RET-UI-005 errors read as what happened; the retention was not changed', () => {
    expect(
      retentionErrorMessage({
        kind: 'server',
        status: 400,
        error: 'invalid days',
        detail: 'days of metrics_bloat must be between 8 and 365',
      }),
    ).toBe('invalid days: days of metrics_bloat must be between 8 and 365')
    expect(
      retentionErrorMessage({
        kind: 'server',
        status: 409,
        error: 'confirmation required',
        detail: '',
      }),
    ).toMatch(/Preview again/)
    expect(
      retentionErrorMessage({
        kind: 'server',
        status: 503,
        error: 'store unavailable',
        detail: '',
      }),
    ).toBe('The store is unavailable. Try again later.')
    expect(retentionErrorMessage({ kind: 'not_found' })).toMatch(/does not know this family/)
    expect(retentionErrorMessage({ kind: 'network', message: 'Failed to fetch' })).toBe(
      'Failed to fetch',
    )
    expect(retentionErrorMessage(new Error('x'))).toBe('The retention was not changed. Try again.')
  })

  it('RET-UI-005 every family of the contract has a label, and the 409 body matches it', () => {
    expect(Object.keys(RETENTION_FAMILY_LABELS)).toEqual([
      'metrics',
      'metrics_statements',
      'metrics_ash',
      'metrics_replication',
      'metrics_tables',
      'metrics_indexes',
      'metrics_bloat',
    ])
    expect(() =>
      assertMatchesContract('setRetention', 409, {
        error: 'confirmation required',
        detail: 'a retention of 14 days for metrics_ash deletes history at the next policy run',
        preview,
      }),
    ).not.toThrow()
  })
})
