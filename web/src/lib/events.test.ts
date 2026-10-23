import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import {
  describeDocumentedEvent,
  DOCUMENTED_EVENT_TYPES,
  describeEvent,
  sortEventsNewestFirst,
  type ClusterEvent,
} from './events'

const GO_SOURCE_ROOT = join(import.meta.dirname, '..', '..', '..', 'internal')
// An event type reaches the store where it is passed to emitEvent or
// emitRepeatable, set as the Type of an event, declared as an EventType
// constant, or listed as the typ of a staleness transition emitted later.
const GO_EVENT_SITE = /emitEvent\(|emitRepeatable\(|Type:|EventType|\btyp:/

function goSourceLines(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) return goSourceLines(path)
    if (!entry.name.endsWith('.go') || entry.name.endsWith('_test.go')) return []
    return readFileSync(path, 'utf8').split('\n')
  })
}

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

  it('UI-CLUS-036 every documented event type is emitted by a Go source file', () => {
    const eventSites = goSourceLines(GO_SOURCE_ROOT).filter((line) => GO_EVENT_SITE.test(line))

    for (const type of DOCUMENTED_EVENT_TYPES) {
      expect(
        eventSites.some((line) => line.includes(`"${type}"`)),
        `${type} has no emitter, so the taxonomy would describe an event that never occurs`,
      ).toBe(true)
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
