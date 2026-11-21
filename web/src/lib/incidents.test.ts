import { describe, expect, it } from 'vitest'

import { deadlockPayload, incidentEvent } from '@/test/incidents'

import { DOCUMENTED_EVENT_TYPES } from './events'
import { INCIDENT_EVENT_TYPES, lockCtid, parseIncident, parseIncidents } from './incidents'

describe('captured incidents', () => {
  it('INC-UI-001 lists only documented capture types', () => {
    for (const type of INCIDENT_EVENT_TYPES) expect(DOCUMENTED_EVENT_TYPES).toContain(type)
  })

  it('INC-UI-002 reads every field of a deadlock capture', () => {
    const incident = parseIncident(incidentEvent(7, deadlockPayload))
    expect(incident).toMatchObject({
      key: '7',
      type: 'deadlock',
      title: 'Deadlock captured',
      capturedAt: '2026-11-20T16:02:03Z',
      database: 'app',
      object: 'public.orders, row ctid (3,7)',
      lockMode: 'ShareLock',
      longestWait: '1.2s',
      omitted: 2,
      suppressed: 3,
    })
    expect(incident?.explanation).toMatch(/^The sessions waited for each other in a cycle/)
    expect(incident?.sessions).toHaveLength(2)
    expect(incident?.sessions[0]).toEqual({
      pid: 101,
      user: 'app_rw',
      application: 'web',
      client: '10.1.1.5',
      state: 'active',
      timing: 'transaction open for 4s · statement running for 1.5s',
      waits: 'ShareLock on transaction 900 (1.2s)',
      blockedBy: [102],
      holdsRow: '(3,7) of public.orders',
      holdsTables: ['public.orders', 'public.order_lines'],
      xminAge: '1,200',
      query: 'UPDATE orders SET qty = 1 WHERE id = 7',
    })
    expect(incident?.sessions[1]?.timing).toBe('idle for 30s')
    expect(incident?.sequence).toEqual([
      'pid 101 waits for ShareLock on transaction 900 (1.2s), blocked by pid 102',
      'pid 102 waits for ExclusiveLock on public.orders (3,7), blocked by pid 101',
    ])
  })

  it('INC-UI-003 explains a rebuilt deadlock and names its window', () => {
    const rebuilt = parseIncident(
      incidentEvent(8, {
        ...deadlockPayload,
        ctid: '',
        rebuilt: true,
        deadlocks: 2,
        since: '2026-11-20T16:01:00Z',
      }),
    )
    expect(rebuilt?.object).toBe('public.orders')
    expect(rebuilt?.explanation).toBe(
      'PostgreSQL counted 2 deadlocks and broke them within deadlock_timeout, before the agent saw the cycle: listed are the sessions that waited on a lock, or blocked one, in this database since 2026-11-20 16:01:00 UTC.',
    )
    const one = parseIncident(incidentEvent(9, { type: 'deadlock', rebuilt: true, sessions: [] }))
    expect(one?.explanation).toMatch(
      /counted one deadlock and broke it within deadlock_timeout.*in this database\.$/,
    )
    expect(one?.capturedAt).toBe('2026-11-20T16:02:05Z')
    expect(one?.object).toBe('')
  })

  it('INC-UI-004 explains each capture type and skips other payloads', () => {
    for (const type of ['lock_wait', 'idle_in_transaction', 'long_transaction', 'long_query']) {
      expect(parseIncident(incidentEvent(1, { type, sessions: [] }))?.explanation).not.toBe('')
    }
    expect(parseIncident(incidentEvent(1, { type: 'other' }))?.explanation).toBe('')
    expect(parseIncident(incidentEvent(1, {}))).toBeNull()
    expect(
      parseIncident({ ...incidentEvent(1, {}), payload: [] as unknown as Record<string, unknown> }),
    ).toBeNull()
  })

  it('INC-UI-005 names lock targets without a relation', () => {
    const waits = (wait: Record<string, unknown>) =>
      parseIncident(incidentEvent(1, { type: 'lock_wait', sessions: [{ pid: 1, wait }] }))
        ?.sessions[0]?.waits
    expect(waits({ locktype: 'transactionid', mode: 'ShareLock' })).toBe(
      'ShareLock on a transaction',
    )
    expect(waits({ locktype: 'advisory', mode: 'ExclusiveLock' })).toBe('ExclusiveLock on advisory')
    expect(waits({ locktype: 'relation', relation: 'public.t' })).toBe('a lock on public.t')
    expect(waits({})).toBe('a lock on a lock')
    expect(lockCtid(null)).toBe('')
    expect(lockCtid({ page: 1 })).toBe('')
  })

  it('INC-UI-006 sorts newest first', () => {
    const list = parseIncidents([
      incidentEvent(1, { type: 'lock_wait', captured_at: '2026-11-20T10:00:00Z' }),
      incidentEvent(2, { not: 'an incident' }),
      incidentEvent(3, { type: 'deadlock', captured_at: '2026-11-20T12:00:00Z' }),
    ])
    expect(list.map((i) => i.key)).toEqual(['3', '1'])
  })
})
