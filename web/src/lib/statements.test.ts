import { describe, expect, it } from 'vitest'

import {
  comparableWithin,
  normaliseQueryText,
  rankStatements,
  shareOfTotal,
  summariseStatement,
  type StatementRow,
} from './statements'

function statement(overrides: Partial<StatementRow> = {}): StatementRow {
  return {
    queryid: '100',
    query_text: 'select 1',
    ...overrides,
  }
}

describe('rankStatements', () => {
  it('UI-QRY-001 keeps equal values deterministic with a queryid tiebreak', () => {
    const rows = [
      statement({ queryid: '20', total_exec_time_ms: 10 }),
      statement({ queryid: '10', total_exec_time_ms: 10 }),
      statement({ queryid: '30', total_exec_time_ms: 20 }),
    ]

    expect(rankStatements(rows, 'total_exec_time').map((row) => row.queryid)).toEqual([
      '30',
      '10',
      '20',
    ])
    expect(rows.map((row) => row.queryid)).toEqual(['20', '10', '30'])
  })
})

describe('shareOfTotal', () => {
  it('UI-QRY-002 returns null for every row when total execution time is zero', () => {
    expect(
      shareOfTotal([
        statement({ total_exec_time_ms: 0 }),
        statement({ queryid: '200', total_exec_time_ms: null }),
      ]),
    ).toEqual([null, null])
  })
})

describe('summariseStatement', () => {
  it('UI-QRY-003 keeps rows per call null when calls is zero', () => {
    expect(
      summariseStatement(
        statement({
          calls: 0,
          rows: 10,
          total_exec_time_ms: 100,
          shared_blks_hit: 1,
          shared_blks_read: 1,
          temp_blks_written: 2,
        }),
      ),
    ).toEqual({
      cacheHitRatio: 0.5,
      calls: 0,
      mean: null,
      rowsPerCall: null,
      tempBytesPerCall: null,
      total: 100,
    })
  })

  it('derives the remaining metrics only from available values', () => {
    expect(
      summariseStatement(
        statement({
          calls: 4,
          rows: 20,
          total_exec_time_ms: 100,
          shared_blks_hit: 6,
          shared_blks_read: 4,
          temp_blks_written: 2,
        }),
      ),
    ).toEqual({
      cacheHitRatio: 0.6,
      calls: 4,
      mean: 25,
      rowsPerCall: 5,
      tempBytesPerCall: 4096,
      total: 100,
    })
  })
})

describe('normaliseQueryText', () => {
  it('collapses whitespace without changing the source value', () => {
    const text = '  select\n  *\nfrom   users  '
    expect(normaliseQueryText(text)).toBe('select * from users')
    expect(text).toBe('  select\n  *\nfrom   users  ')
  })
})

describe('comparableWithin', () => {
  it('UI-QRY-004 is false across clusters', () => {
    expect(
      comparableWithin(
        { cluster_id: 'cluster-a', pg_version: 16 },
        { cluster_id: 'cluster-b', pg_version: 16 },
      ),
    ).toBe(false)
  })

  it('UI-QRY-005 is false across major versions', () => {
    expect(
      comparableWithin(
        { cluster_id: 'cluster-a', pg_version: 16 },
        { cluster_id: 'cluster-a', pg_version: 17 },
      ),
    ).toBe(false)
  })

  it('UI-QRY-006 preserves a string queryid throughout ranking', () => {
    const queryid = '9223372036854775807'
    const [ranked] = rankStatements(
      [statement({ queryid, total_exec_time_ms: 1 })],
      'total_exec_time',
    )

    expect(ranked?.queryid).toBe(queryid)
    expect(typeof ranked?.queryid).toBe('string')
  })

  it('requires both scope fields before enabling comparison', () => {
    expect(
      comparableWithin(
        { cluster_id: 'cluster-a', pg_version: null },
        { cluster_id: 'cluster-a', pg_version: null },
      ),
    ).toBe(false)
  })
})
