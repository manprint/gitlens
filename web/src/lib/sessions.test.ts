import { describe, expect, it } from 'vitest'

import {
  SESSION_FILTERS,
  SESSION_SEARCH_FIELDS,
  SESSION_SORT,
  blockedByThis,
  connectionGroup,
  connectionKeyLabel,
  countOf,
  firstLine,
  hasOrigin,
  originSentence,
  sessionApplication,
  sessionOrigin,
  sessionQueryText,
  sessionUser,
  sessionWait,
  splitByOrigin,
  usesOriginTables,
  withShare,
  type ConnectionsSummary,
  type Session,
} from './sessions'

const app: Session = { pid: 1, origin: 'application', state: 'active' }
const lens: Session = { pid: 2, origin: 'pglens', state: 'idle' }
const old: Session = { pid: 3, state: 'idle' }

const summary: ConnectionsSummary = {
  by_application_enabled: true,
  truncated: false,
  by_origin: [
    { key: 'application', count: 3 },
    { key: 'pglens', count: 1 },
  ],
  by_application: [],
  by_user: [],
  by_client: [],
  by_database: [],
  by_state: [],
}

describe('sessions library', () => {
  it('SESSLIB-001 takes the origin from the server only and picks the table mode', () => {
    expect(sessionOrigin({ pid: 1, origin: 'pglens', application_name: 'api' })).toBe('pglens')
    expect(sessionOrigin({ pid: 1, application_name: 'pglens/check' })).toBeNull()
    expect(sessionOrigin({ pid: 1, origin: 'other' })).toBeNull()
    expect(hasOrigin([old, app])).toBe(true)
    expect(hasOrigin([old])).toBe(false)
    expect(usesOriginTables([old], summary)).toBe(false)
    expect(usesOriginTables([old, lens], undefined)).toBe(true)
    expect(usesOriginTables([], summary)).toBe(true)
    expect(usesOriginTables([], undefined)).toBe(false)
  })

  it('SESSLIB-002 splits sessions by origin and counts a session without one as application', () => {
    const split = splitByOrigin([app, lens, old])
    expect(split.application.map((s) => s.pid)).toEqual([1, 3])
    expect(split.pglens.map((s) => s.pid)).toEqual([2])
  })

  it('SESSLIB-003 reads session fields with their fallbacks', () => {
    expect(sessionUser({ pid: 1, usename: '', username: 'u2', user: 'u3' })).toBe('u2')
    expect(sessionApplication({ pid: 1, application: 'api' })).toBe('api')
    expect(sessionWait({ pid: 1, wait_event_type: 'Lock', wait_event: 'relation' })).toBe(
      'Lock: relation',
    )
    expect(sessionWait({ pid: 1, wait_event_type: 'Client' })).toBe('Client')
    expect(sessionWait({ pid: 1 })).toBeNull()
    expect(sessionQueryText({ pid: 1, query_text: 'SELECT 1' })).toEqual({
      text: 'SELECT 1',
      source: 'normalised',
    })
    expect(sessionQueryText({ pid: 1, query_text: 'x', query_text_source: 'raw' })?.source).toBe(
      'raw',
    )
    expect(sessionQueryText({ pid: 1, query_text: '  ' })).toBeNull()
    expect(firstLine('SELECT 1\nFROM t')).toBe('SELECT 1')
    expect(firstLine('  \nx')).toBeNull()
    expect(firstLine(null)).toBeNull()
  })

  it('SESSLIB-004 sorts, searches and filters the table rows', () => {
    const waiting: Session = { pid: 9, blocked_by: [1], state: 'idle in transaction (aborted)' }
    const byId = (id: string) => SESSION_FILTERS.find((filter) => filter.id === id)
    expect(byId('waiting')?.predicate?.(waiting, 'only')).toBe(true)
    expect(byId('waiting')?.predicate?.(app, 'only')).toBe(false)
    expect(byId('waiting')?.predicate?.(app, '')).toBe(true)
    expect(byId('idle-tx')?.predicate?.(waiting, 'only')).toBe(true)
    expect(byId('idle-tx')?.predicate?.(lens, 'only')).toBe(false)
    expect(SESSION_SORT.xact_age({ pid: 1, xact_age: 7 })).toBe(7)
    expect(SESSION_SORT.pid({ pid: 12 })).toBe(12)
    const haystack = SESSION_SEARCH_FIELDS.map((field) =>
      field({ pid: 5, usename: 'ann', application_name: 'api', query_text: 'SELECT secret' }),
    )
    expect(haystack).toContain('5')
    expect(haystack).toContain('ann')
    expect(haystack).toContain('SELECT secret')
  })

  it('SESSLIB-005 summarises connection counts, shares and the sessions a session blocks', () => {
    expect(countOf(summary.by_origin, 'pglens')).toBe(1)
    expect(countOf(summary.by_origin, 'missing')).toBe(0)
    expect(originSentence(summary)).toBe('application 3 · pglens 1')
    const cut: ConnectionsSummary = {
      ...summary,
      truncated: true,
      totals: {
        ...summary,
        by_origin: [
          { key: 'application', count: 240 },
          { key: 'pglens', count: 4 },
        ],
      },
    }
    expect(connectionGroup(cut, 'by_origin')[0]?.count).toBe(240)
    expect(originSentence(cut)).toBe('application 240 · pglens 4')
    expect(
      withShare([
        { key: 'a', count: 3 },
        { key: 'b', count: 1 },
      ]).map((r) => r.share),
    ).toEqual([0.75, 0.25])
    expect(withShare([{ key: 'a', count: 0 }])[0]?.share).toBe(0)
    expect(connectionKeyLabel('')).toBe('(empty name)')
    expect(
      blockedByThis(
        [
          { pid: 2, blocked_by: [1] },
          { pid: 3, blocked_by: [] },
        ],
        1,
      ),
    ).toEqual(['2'])
  })
})
