import { describe, expect, it } from 'vitest'

import {
  describeDocumentedEvent,
  DOCUMENTED_EVENT_TYPES,
  describeEvent,
  sortEventsNewestFirst,
  type ClusterEvent,
} from './events'

const event = (overrides: Partial<ClusterEvent>): ClusterEvent => ({
  cluster_id: '7381927364512345678',
  event_id: 1,
  instance_id: null,
  payload: {},
  ts: '2026-08-31T10:00:00Z',
  type: 'role_change',
  ...overrides,
})

describe('event taxonomy', () => {
  it('UI-CLUS-030 every documented event type has a title and a what-to-check line', () => {
    for (const type of DOCUMENTED_EVENT_TYPES) {
      const description = describeDocumentedEvent(type)
      expect(description.title).not.toBe('')
      expect(description.whatToCheck).not.toBe('')
    }
  })

  it('keeps unknown event types neutral and visible', () => {
    expect(describeEvent('future_event')).toEqual({
      severity: 'info',
      title: 'Unknown event type',
      whatToCheck: 'Review the raw event type and payload before taking action.',
    })
  })

  it('UI-CLUS-035 orders events newest first', () => {
    const ordered = sortEventsNewestFirst([
      event({ event_id: 1, ts: '2026-08-30T10:00:00Z' }),
      event({ event_id: 2, ts: '2026-08-31T10:00:00Z' }),
    ])
    expect(ordered.map(({ event_id }) => event_id)).toEqual([2, 1])
  })
})
