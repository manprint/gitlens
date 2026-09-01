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

  it('ranks every supported metric and falls back to queryid for unavailable values', () => {
    const rows = [
      statement({
        queryid: '2',
        total_exec_time_ms: 20,
        mean_exec_time: 10,
        calls: 2,
        rows: 8,
        shared_blks_read: 4,
        temp_blks_written: 3,
      }),
      statement({
        queryid: '10',
        total_exec_time_ms: 10,
        calls: 1,
        rows: 4,
        shared_blks_read: 2,
        temp_blks_written: 1,
      }),
      statement({ queryid: 'alpha' }),
    ]

    expect(rankStatements(rows, 'mean_exec_time').map((row) => row.queryid)).toEqual([
      '2',
      '10',
      'alpha',
    ])
    expect(rankStatements(rows, 'calls').map((row) => row.queryid)).toEqual(['2', '10', 'alpha'])
    expect(rankStatements(rows, 'rows').map((row) => row.queryid)).toEqual(['2', '10', 'alpha'])
    expect(rankStatements(rows, 'shared_blks_read').map((row) => row.queryid)).toEqual([
      '2',
      '10',
      'alpha',
    ])
    expect(rankStatements(rows, 'temp_blks_written').map((row) => row.queryid)).toEqual([
      '2',
      '10',
      'alpha',
    ])
    expect(
      rankStatements(
        [statement({ queryid: '10' }), statement({ queryid: '2', calls: 1 })],
        'calls',
      ),
    ).toMatchObject([{ queryid: '2' }, { queryid: '10' }])
  })

  it('orders numeric and textual query ids without losing integer precision', () => {
    const tie = (queryid: number | string) => statement({ queryid, calls: 1 })

    expect(rankStatements([tie('10'), tie('2')], 'calls').map((row) => row.queryid)).toEqual([
      '2',
      '10',
    ])
    expect(rankStatements([tie('-10'), tie('-2')], 'calls').map((row) => row.queryid)).toEqual([
      '-10',
      '-2',
    ])
    expect(rankStatements([tie('-1'), tie('1')], 'calls').map((row) => row.queryid)).toEqual([
      '-1',
      '1',
    ])
    expect(rankStatements([tie('01'), tie('1')], 'calls').map((row) => row.queryid)).toEqual([
      '01',
      '1',
    ])
    expect(rankStatements([tie('z'), tie('a')], 'calls').map((row) => row.queryid)).toEqual([
      'a',
      'z',
    ])
    expect(rankStatements([tie('1'), tie('1')], 'calls').map((row) => row.queryid)).toEqual([
      '1',
      '1',
    ])
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

  it('preserves unavailable values while deriving shares from available time', () => {
    expect(
      shareOfTotal([
        statement({ total_exec_time_ms: 100 }),
        statement({ queryid: '200', total_exec_time_ms: null }),
        statement({ queryid: '300', total_exec_time_ms: 50 }),
      ]),
    ).toEqual([2 / 3, null, 1 / 3])
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

  it('prefers an explicit mean and leaves incomplete metrics unavailable', () => {
    expect(
      summariseStatement(
        statement({
          mean_exec_time: 12.5,
          calls: 2,
          rows: null,
          shared_blks_hit: 0,
          shared_blks_read: 0,
          temp_blks_written: null,
        }),
      ),
    ).toEqual({
      cacheHitRatio: null,
      calls: 2,
      mean: 12.5,
      rowsPerCall: null,
      tempBytesPerCall: null,
      total: null,
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

  it('normalises numeric and textual major-version representations', () => {
    expect(
      comparableWithin(
        { cluster_id: 'cluster-a', major_version: 160000 },
        { cluster_id: 'cluster-a', pg_version: '16.4' },
      ),
    ).toBe(true)
    expect(
      comparableWithin(
        { cluster_id: 'cluster-a', major_version: 'unknown' },
        { cluster_id: 'cluster-a', pg_version: 'unknown' },
      ),
    ).toBe(false)
    expect(
      comparableWithin(
        { cluster_id: 'cluster-a', major_version: '  ' },
        { cluster_id: 'cluster-a', pg_version: 16 },
      ),
    ).toBe(false)
  })
})
