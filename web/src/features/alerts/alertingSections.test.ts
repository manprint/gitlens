import { describe, expect, it } from 'vitest'

import {
  ALERTS_SECTIONS,
  EVENTS_SECTIONS,
  readAlertsSection,
  readEventsSection,
  withSection,
} from './alertingSections'

describe('alertingSections', () => {
  it('FHD-APPLY-029 reads ?section= with a default and never writes the default tab', () => {
    expect(ALERTS_SECTIONS.map((section) => section.id)).toEqual(['alerts', 'delivery'])
    expect(EVENTS_SECTIONS.map((section) => section.id)).toEqual(['events', 'alerts'])
    expect(readAlertsSection(null)).toBe('alerts')
    expect(readAlertsSection('delivery')).toBe('delivery')
    expect(readAlertsSection('nope')).toBe('alerts')
    expect(readEventsSection(null)).toBe('events')
    expect(readEventsSection('alerts')).toBe('alerts')
    expect(readEventsSection('delivery')).toBe('events')

    const current = new URLSearchParams('range=1h&section=alerts&events.page=2')
    expect(withSection(current, 'events', 'events').toString()).toBe('range=1h&events.page=2')
    expect(withSection(current, 'alerts', 'events').toString()).toBe(
      'range=1h&section=alerts&events.page=2',
    )
  })
})
