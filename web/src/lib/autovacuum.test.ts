import { describe, expect, it } from 'vitest'

import { AUTOVACUUM_SETTINGS, runBadges, runKey, runsQuery, summaryKey } from './autovacuum'

describe('autovacuum', () => {
  it('lists the wraparound badge before the aggressive one', () => {
    expect(runBadges({ aggressive: true, wraparound: true })).toEqual(['wraparound', 'aggressive'])
    expect(runBadges({ aggressive: true, wraparound: false })).toEqual(['aggressive'])
    expect(runBadges({ aggressive: false, wraparound: false })).toEqual([])
  })

  it('keys tables and runs without collisions across names', () => {
    const a = { datname: 'a.b', schemaname: 'c', relname: 'd' }
    const b = { datname: 'a', schemaname: 'b.c', relname: 'd' }
    expect(summaryKey(a)).not.toBe(summaryKey(b))
    expect(runKey({ ...a, kind: 'vacuum', ts: 't' })).not.toBe(
      runKey({ ...a, kind: 'analyze', ts: 't' }),
    )
  })

  it('filters the runs of one table', () => {
    expect(runsQuery('i1', { datname: 'app', schemaname: 'public', relname: 'orders' })).toEqual({
      instance_id: 'i1',
      database: 'app',
      schema: 'public',
      relation: 'orders',
    })
    expect(AUTOVACUUM_SETTINGS).toEqual(['checks.logs.enabled', 'log_autovacuum_min_duration'])
  })
})
