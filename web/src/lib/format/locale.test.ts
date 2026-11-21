import { afterEach, describe, expect, it } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'

import { formatBytes, formatCount, formatMilliseconds, formatRelative } from '.'

describe('formatters follow the console language (plan 006 D7)', () => {
  afterEach(async () => {
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-022 numbers and relative times in Italian, unit symbols unchanged', async () => {
    await setServerLanguage('it')
    expect(formatBytes(1536)).toBe('1,5 KiB')
    expect(formatCount(12345)).toBe('12.345')
    expect(formatMilliseconds(2.5)).toBe('2,5 ms')
    expect(formatRelative('2026-11-20T10:00:00Z', new Date('2026-11-20T10:05:00Z'))).toBe(
      '5 minuti fa',
    )
    expect(formatBytes(1536, 'en-US')).toBe('1.5 KiB')
  })
})
