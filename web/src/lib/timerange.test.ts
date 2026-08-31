import { describe, expect, it } from 'vitest'

import {
  MAX_RANGE_MS,
  RANGE_PRESETS,
  chooseStep,
  parseRange,
  stepMilliseconds,
  toParams,
  type RangePreset,
} from './timerange'

const NOW = new Date('2026-08-31T12:00:00.000Z')

describe('parseRange', () => {
  it.each(Object.keys(RANGE_PRESETS) as RangePreset[])(
    'UI-TIME-001 parses %s against the frozen clock',
    (preset) => {
      const result = parseRange(new URLSearchParams({ range: preset }), NOW)

      expect(result.valid).toBe(true)
      expect(result.kind).toBe('relative')
      expect(result.to).toEqual(NOW)
      expect(result.from).toEqual(new Date(NOW.getTime() - RANGE_PRESETS[preset].durationMs))
    },
  )

  it('UI-TIME-002 parses an absolute pair', () => {
    const result = parseRange(
      new URLSearchParams({
        from: '2026-08-31T10:00:00+02:00',
        to: '2026-08-31T11:00:00+02:00',
      }),
      NOW,
    )

    expect(result).toMatchObject({ kind: 'absolute', label: 'Custom range', valid: true })
    expect(result.from).toEqual(new Date('2026-08-31T08:00:00.000Z'))
    expect(result.to).toEqual(new Date('2026-08-31T09:00:00.000Z'))
  })

  it('UI-TIME-003 rejects an inverted range and falls back to 1h', () => {
    const result = parseRange(
      new URLSearchParams({ from: '2026-08-31T12:00:00Z', to: '2026-08-31T11:00:00Z' }),
      NOW,
    )

    expect(result).toMatchObject({ fallback: true, parameter: 'from/to', valid: false })
    expect(result.from).toEqual(new Date(NOW.getTime() - RANGE_PRESETS['1h'].durationMs))
  })

  it('UI-TIME-004 rejects a span above 30d', () => {
    const result = parseRange(
      new URLSearchParams({ from: '2026-07-01T00:00:00Z', to: '2026-08-31T00:00:01Z' }),
      NOW,
    )

    expect(result).toMatchObject({ fallback: true, parameter: 'from/to', valid: false })
    if (result.valid) throw new Error('expected an invalid time range')
    expect(result.reason).toContain('30 days')
  })

  it('UI-TIME-005 round-trips relative and absolute ranges through URL params', () => {
    const relative = parseRange(new URLSearchParams({ range: '7d' }), NOW)
    const absolute = parseRange(
      new URLSearchParams({ from: '2026-08-30T00:00:00Z', to: '2026-08-30T01:00:00Z' }),
      NOW,
    )

    expect(parseRange(toParams(relative), NOW)).toMatchObject({
      from: relative.from,
      kind: 'relative',
      to: relative.to,
      valid: true,
    })
    expect(parseRange(toParams(absolute), NOW)).toMatchObject({
      from: absolute.from,
      kind: 'absolute',
      to: absolute.to,
      valid: true,
    })
  })
})

describe('chooseStep', () => {
  it.each([
    ['15m', 15 * 60_000, '15s'],
    ['1h', 60 * 60_000, '15s'],
    ['24h', 24 * 60 * 60_000, '5m'],
    ['7d', 7 * 24 * 60 * 60_000, '15m'],
    ['30d', MAX_RANGE_MS, '1h'],
  ])('UI-TIME-006 chooses the exact bounded step for %s', (_label, span, expected) => {
    const from = new Date(0)
    const to = new Date(span)
    const step = chooseStep(from, to)

    expect(step).toBe(expected)
    expect(span / stepMilliseconds(step)).toBeLessThanOrEqual(1_000)
  })

  it('UI-TIME-007 keeps the 422-producing point count unreachable', () => {
    const span = MAX_RANGE_MS
    const step = chooseStep(new Date(0), new Date(span))

    expect(span / stepMilliseconds(step)).toBeLessThanOrEqual(10_000)
  })
})
