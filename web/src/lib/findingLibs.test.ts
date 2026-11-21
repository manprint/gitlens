import { describe, expect, it } from 'vitest'

import { evidenceRows } from './findingEvidence'
import { formatBytes, formatDuration, formatMilliseconds, formatTimestamp } from './format'
import { findingCheck, sqlLiteral } from './findingCheck'
import { findingSubject, subjectLabel } from './findingSubject'
import { ADVISOR_RULE_IDS } from './ruleText'

const UUID = '0b1c2d3e-0000-4000-8000-000000000001'

// The object each advisor rule names, as the advisor writes it.
const OBJECTS: [RegExp, string][] = [
  [/^index\.divergence$/, `public.t1_v_idx@${UUID}`],
  [/^(index|partition\.index)/, 'public.t1_v_idx'],
  [/^sequence\./, 'public.t1_id_seq'],
  [/^(function|trigger)\./, 'public.f1'],
  [/^(table|partition|postgis\.spatial|bloat)\./, 'public.t1'],
  [/^config\.drift$/, 'setting:work_mem'],
  [/^subscription\./, 'subscription:sub1'],
  [/^logical_slot\./, 'slot:s1'],
  [/^query\./, 'queryid:42'],
]
const base = { instance_id: UUID, scope: 'instance', datname: 'shop' } as const

describe('findingSubject', () => {
  it('UI-FIND-054 names a statement by its text and keeps the exact queryid', () => {
    const subject = findingSubject({
      ...base,
      object_name: 'queryid:-4617329483752201933',
      query_text: 'SELECT 1\n  FROM t',
    })
    expect(subject).toEqual({
      kind: 'statement',
      queryid: '-4617329483752201933',
      text: 'SELECT 1\n  FROM t',
    })
    expect(subjectLabel(subject)).toBe('SELECT 1 FROM t')
    expect(
      subjectLabel(findingSubject({ ...base, object_name: 'queryid:7', query_text: ' ' })),
    ).toBe('Statement 7 (text no longer kept)')
  })

  it('UI-FIND-055 hides instance ids and names settings, slots, siblings and placeholders', () => {
    const label = (object_name: string) =>
      subjectLabel(findingSubject({ ...base, object_name }), (id) => (id === UUID ? 'db-1' : id))
    expect(label(UUID)).toBe('')
    expect(label('')).toBe('')
    expect(label('setting:work_mem')).toBe('Setting work_mem')
    expect(label('slot:s1')).toBe('Replication slot s1')
    expect(label('subscription:sub')).toBe('Subscription sub')
    expect(label(`public.t_idx@${UUID}`)).toBe('public.t_idx (compared with instance db-1)')
    expect(label('pg_wal')).toBe('pg_wal directory')
    expect(label('public.orders')).toBe('public.orders')
  })
})

describe('evidenceRows', () => {
  it('UI-FIND-056 formats units, ratios, booleans and times, in key order, without omitted keys', () => {
    const rows = evidenceRows(
      {
        queryid: -4617329483752202000,
        share: 0.425,
        threshold: 0.3,
        used_ratio: 1,
        dead_tuples: 1234567,
        primary: false,
        empty: null,
        tags: ['a', 'b'],
      },
      'query.total_time_share',
      ['queryid'],
    )
    expect(rows.map((row) => [row.key, row.label, row.value])).toEqual([
      ['share', 'Share of the execution time', '42.5%'],
      ['threshold', 'Threshold', '30%'],
      ['used_ratio', 'Used', '100%'],
      ['dead_tuples', 'Dead rows', '1,234,567'],
      ['primary', 'Primary', 'no'],
      ['empty', 'Empty', '—'],
      ['tags', 'Tags', 'a, b'],
    ])
    expect(evidenceRows({ threshold: 5 }, 'table.dead_tuples_high')[0]!.value).toBe('5')
    expect(evidenceRows(undefined, 'x')).toEqual([])
    expect(
      evidenceRows(
        {
          observed_ms: '761.3274609999993',
          table_bytes: 1048576,
          last_archived_age_seconds: 90,
          seq_scan_rate: 1.234,
          at: '2026-11-20T10:00:00Z',
          detail: { a: 1 },
          pairs: [{ a: 1 }],
          queryid: '123',
        },
        'x',
      ).map((row) => row.value),
    ).toEqual([
      formatMilliseconds(761.3274609999993),
      formatBytes(1048576),
      formatDuration(90),
      '1.23/s',
      formatTimestamp('2026-11-20T10:00:00Z'),
      '{"a":1}',
      '[{"a":1}]',
      '123',
    ])
  })
})

describe('findingCheck', () => {
  const sqlOf = (rule_id: string, object_name: string | null, query_text?: string) =>
    findingCheck({
      ...base,
      rule_id,
      ...(object_name === null ? {} : { object_name }),
      ...(query_text ? { query_text } : {}),
    })

  it('UI-FIND-057 wraps every script in a read-only transaction that rolls back', () => {
    for (const [rule, object] of [
      ['table.dead_tuples_high', 'public.orders'],
      ['index.unused', 'public.orders_idx'],
      ['config.fsync_off', null],
      ['txn.long_running', null],
      ['query.slow_mean', 'queryid:42'],
    ] as const) {
      const check = sqlOf(rule, object, 'SELECT 1')
      expect(check, rule).not.toBeNull()
      expect(check!.sql.startsWith('BEGIN TRANSACTION READ ONLY;'), rule).toBe(true)
      expect(check!.sql.trimEnd().endsWith('ROLLBACK;'), rule).toBe(true)
      expect(check!.sql, rule).not.toMatch(/\b(INSERT|UPDATE|DELETE|ALTER|DROP|VACUUM|ANALYZE;)\b/)
    }
  })

  it('UI-FIND-058 quotes the object and the statement id, and plans $n statements generically', () => {
    expect(sqlOf('table.dead_tuples_high', 'Sales.Order Lines')).toBeNull()
    expect(sqlOf('table.dead_tuples_high', 'Sales.Orders')!.sql).toContain(`'"Sales"."Orders"'`)
    expect(sqlLiteral("it's")).toBe("'it''s'")

    const statement = sqlOf(
      'query.slow_mean',
      'queryid:-42',
      'SELECT * FROM t WHERE a = $1 AND b = $2;',
    )!
    expect(statement.database).toBe('shop')
    expect(statement.sql).toContain('WHERE queryid = -42')
    expect(statement.sql).toContain('SET LOCAL plan_cache_mode = force_generic_plan;')
    expect(statement.sql).toContain('EXPLAIN EXECUTE pglens_check(NULL, NULL);')
    expect(statement.sql).toContain('DEALLOCATE pglens_check;')
    expect(sqlOf('query.slow_mean', 'queryid:1', 'SELECT 1')!.sql).toContain('EXPLAIN\nSELECT 1;')
    expect(sqlOf('query.slow_mean', 'queryid:1')!.sql).toContain('EXPLAIN <statement>;')
  })

  it('UI-FIND-060 gives every advisor rule a read-only check with steps', () => {
    for (const rule of ADVISOR_RULE_IDS) {
      const object = OBJECTS.find(([re]) => re.test(rule))?.[1] ?? null
      for (const text of ['SELECT * FROM t1 WHERE id = $1', undefined]) {
        const check = sqlOf(rule, object, text)
        expect(check, rule).not.toBeNull()
        expect(check!.steps.length, rule).toBeGreaterThan(0)
        expect(check!.sql.startsWith('BEGIN TRANSACTION READ ONLY;'), rule).toBe(true)
        expect(check!.sql.trimEnd().endsWith('ROLLBACK;'), rule).toBe(true)
        expect(check!.sql, rule).not.toMatch(
          /^\s*(INSERT|UPDATE|DELETE|ALTER|DROP|CREATE|VACUUM|ANALYZE|TRUNCATE|GRANT)\b/im,
        )
      }
    }
  })

  it('UI-FIND-061 has no object check without a qualified object name', () => {
    for (const rule of ['table.dead_tuples_high', 'table.bloat_high', 'index.unused']) {
      expect(sqlOf(rule, null), rule).toBeNull()
    }
    const any = findingCheck({ ...base, datname: '', rule_id: 'config.fsync_off' })
    expect(any!.database).toBeNull()
  })

  it('UI-FIND-059 has no check for a rule it does not know', () => {
    expect(sqlOf('made.up_rule', 'public.orders')).toBeNull()
  })
})
