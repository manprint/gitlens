import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import {
  describeDocumentedEvent,
  DOCUMENTED_EVENT_TYPES,
  describeEvent,
  isDocumentedEventType,
  sortEventsNewestFirst,
  type ClusterEvent,
} from './events'

const GO_SOURCE_ROOT = join(import.meta.dirname, '..', '..', '..', 'internal')
// An event type reaches the store where it is passed to emitEvent or
// emitRepeatable, set as the Type of an event, declared as an EventType
// constant, or listed as the typ of a staleness transition emitted later.
const GO_EVENT_SITE = /emitEvent\(|emitRepeatable\(|Type:|EventType|\btyp:/
const QUOTED_LOWERCASE = /"([a-z][a-z0-9_]*)"/g
// Words that share an event site line but are not event types: payload keys,
// values of other records, and SQL or catalog words on the same lines.
const NOT_EVENT_TYPES: ReadonlySet<string> = new Set([
  'attempted_cluster_id',
  'attempted_source',
  'cluster_id',
  'instance_id',
  'last_seen',
  'slot_name',
  'event',
  'metric',
  'other',
  'streaming',
  'column',
  'column_type',
  'constraint',
  'constraint_type',
  'data_type',
  'link',
  'schemaname',
  'sequence',
  'subtype',
  'table',
  'table_schema',
  'wait_event',
  'wait_event_type',
])

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

  it('UI-CLUS-052 every quoted word on a Go event site is a documented type or a known non-event word', () => {
    const named = new Set<string>()
    for (const line of goSourceLines(GO_SOURCE_ROOT).filter((line) => GO_EVENT_SITE.test(line))) {
      for (const [, word] of line.matchAll(QUOTED_LOWERCASE)) {
        if (word) named.add(word)
      }
    }

    // The scan has to reach the emitters, or the check below passes on nothing.
    expect(named.has('counter_reset_detected')).toBe(true)
    expect(named.has('failover_detected')).toBe(true)
    const undocumented = [...named].filter(
      (word) => !isDocumentedEventType(word) && !NOT_EVENT_TYPES.has(word),
    )
    expect(
      undocumented,
      'a Go event site names a type the taxonomy does not document, so the UI would show it as unknown',
    ).toEqual([])
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
