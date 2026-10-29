import { describe, expect, it } from 'vitest'

import { sliderLabelFormatter, spanOf, timeAxisOption } from './timeAxis'

const HOUR = 3_600_000
const at = Date.UTC(2026, 9, 29, 9, 41)

describe('timeAxisOption', () => {
  it('TAXIS-001 hides overlapping labels and formats a 1 h span as HH:mm', () => {
    const axis = timeAxisOption(HOUR)
    expect(axis.type).toBe('time')
    expect(axis.axisLabel.hideOverlap).toBe(true)
    expect(axis.axisLabel.showMinLabel).toBe(false)
    expect(axis.axisLabel.showMaxLabel).toBe(false)
    expect(axis.axisLabel.formatter(at)).toBe('09:41')
    expect(sliderLabelFormatter(HOUR)(at)).toBe('09:41')
  })

  it('TAXIS-002 adds the date for a 48 h span', () => {
    expect(timeAxisOption(48 * HOUR).axisLabel.formatter(at)).toBe('Oct 29 09:41')
    expect(sliderLabelFormatter(48 * HOUR)(at)).toBe('Oct 29 09:41')
  })

  it('TAXIS-003 keeps every label within 12 characters from 5 min to 30 d', () => {
    for (const span of [5 * 60_000, HOUR, 6 * HOUR, 24 * HOUR, 7 * 24 * HOUR, 30 * 24 * HOUR]) {
      for (const offset of [0, 9 * HOUR + 59 * 60_000, 75 * 24 * HOUR]) {
        const label = timeAxisOption(span).axisLabel.formatter(Date.UTC(2026, 0, 1) + offset)
        expect(label.length, `${span} ${label}`).toBeLessThanOrEqual(12)
      }
    }
    // Same span, same formatter: options built twice compare equal.
    expect(timeAxisOption(HOUR)).toBe(timeAxisOption(HOUR))
    expect(timeAxisOption(HOUR).axisLabel.formatter(NaN)).toBe('')
    expect(spanOf(['2026-01-01T00:00:00Z', 'bad', '2026-01-01T01:00:00Z'])).toBe(HOUR)
    expect(spanOf([])).toBe(0)
  })
})
