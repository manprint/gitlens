import { describe, expect, it } from 'vitest'

import type { Schemas } from '@/api/types'

import {
  autovacuumTrigger,
  buildDeadRows,
  compareDeadRows,
  DEFAULT_AUTOVACUUM,
  matchesDeadFilter,
  matchesDeadSearch,
  readAutovacuumSettings,
  summariseDeadRows,
  trendDirection,
  trendValues,
  WORST_FLOOR,
  type DeadFilter,
  type DeadSortKey,
} from './deadRows'

type Finding = Schemas['Finding']

const DEFAULTS = { ...DEFAULT_AUTOVACUUM, source: 'default' as const }

function table(name: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: name,
    n_live_tup: 1000,
    n_dead_tup: 100,
    total_bytes: 10_000,
    table_bytes: 8_000,
    ...extra,
  }
}

function finding(rule: string, object: string, extra: Partial<Finding> = {}): Finding {
  return {
    finding_id: `${rule}:${object}`,
    rule_id: rule,
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: object,
    title: rule,
    ...extra,
  }
}

describe('readAutovacuumSettings', () => {
  it('DEAD-001 reads the trigger settings of the instance', () => {
    const settings = readAutovacuumSettings([
      { name: 'autovacuum_vacuum_threshold', value: '100' },
      { name: 'autovacuum_vacuum_scale_factor', value: '0.05' },
      { name: 'max_connections', value: '200' },
    ])
    expect(settings).toEqual({ threshold: 100, scaleFactor: 0.05, source: 'instance' })
    expect(autovacuumTrigger(10_000, settings)).toBe(600)
  })

  it('DEAD-002 falls back to the PostgreSQL defaults and says so', () => {
    expect(readAutovacuumSettings([])).toEqual({
      threshold: 50,
      scaleFactor: 0.2,
      source: 'default',
    })
    const partial = readAutovacuumSettings([{ name: 'autovacuum_vacuum_threshold', value: '75' }])
    expect(partial).toEqual({ threshold: 75, scaleFactor: 0.2, source: 'default' })
    const garbage = readAutovacuumSettings([
      { name: 'autovacuum_vacuum_threshold', value: 'many' },
      { name: 'autovacuum_vacuum_scale_factor', value: '-1' },
    ])
    expect(garbage.source).toBe('default')
    expect(autovacuumTrigger(-5, DEFAULTS)).toBe(50)
  })
})

describe('buildDeadRows', () => {
  it('DEAD-003 derives ratios, the autovacuum trigger and the estimated dead space', () => {
    const [row] = buildDeadRows([table('orders', { n_dead_tup: 450 })], [], DEFAULTS)
    expect(row?.key).toBe('app.public.orders')
    expect(row?.deadRatio).toBeCloseTo(0.45)
    // 50 + 0.2 * 1000
    expect(row?.trigger).toBe(250)
    expect(row?.triggerShare).toBeCloseTo(1.8)
    // 8000 bytes of heap, 450 of 1450 tuples are dead.
    expect(row?.deadBytes).toBeCloseTo(8_000 * (450 / 1450))
  })

  it('DEAD-004 uses the later of the manual and the automatic vacuum', () => {
    const rows = buildDeadRows(
      [
        table('a', {
          last_vacuum_at: '2026-08-27T01:00:00Z',
          last_autovacuum_at: '2026-08-27T02:00:00Z',
        }),
        table('b', {
          last_vacuum_at: '2026-08-27T03:00:00Z',
          last_autovacuum_at: '2026-08-27T02:00:00Z',
        }),
        table('c', { last_autoanalyze_at: '2026-08-27T01:30:00Z' }),
      ],
      [],
      DEFAULTS,
    )
    expect(rows[0]).toMatchObject({ lastVacuumAt: '2026-08-27T02:00:00Z', lastVacuumKind: 'auto' })
    expect(rows[1]).toMatchObject({
      lastVacuumAt: '2026-08-27T03:00:00Z',
      lastVacuumKind: 'manual',
    })
    expect(rows[2]).toMatchObject({ lastVacuumAt: null, neverVacuumed: true })
    expect(rows[2]?.lastAnalyzeAt).toBe('2026-08-27T01:30:00Z')
  })

  it('DEAD-005 sums the write rates and turns the vacuum rate into runs per day', () => {
    const [row] = buildDeadRows(
      [
        table('t', {
          n_tup_ins_rate: 1,
          n_tup_upd_rate: 2,
          n_tup_del_rate: 0.5,
          hot_update_ratio: 0.75,
          autovacuum_rate: 1 / 3600,
          n_mod_since_analyze: 12,
          relfrozenxid_age: 99,
          seq_scan_rate: 0.1,
        }),
      ],
      [],
      DEFAULTS,
    )
    expect(row).toMatchObject({
      writeRate: 3.5,
      updateRate: 2,
      hotRatio: 0.75,
      modSinceAnalyze: 12,
      frozenAge: 99,
      seqScanRate: 0.1,
    })
    expect(row?.autovacuumPerDay).toBeCloseTo(24)
  })

  it('DEAD-006 does not invent figures the agent did not send', () => {
    const [row] = buildDeadRows([{ datname: 'app', relname: 'bare' }], [], DEFAULTS)
    expect(row).toMatchObject({
      dead: 0,
      live: 0,
      deadRatio: null,
      bytes: null,
      deadBytes: null,
      writeRate: null,
      hotRatio: null,
      autovacuumPerDay: null,
      neverVacuumed: false,
    })
  })

  it('DEAD-007 attaches only the open advisor findings of the table', () => {
    const rows = buildDeadRows(
      [table('a'), table('b')],
      [
        finding('table.dead_tuples_high', 'public.a'),
        finding('table.autoanalyze_stale', 'public.a', { state: 'resolved' }),
        finding('table.never_autovacuumed', 'public.b', { datname: 'other' }),
        finding('index.unused', 'public.a'),
      ],
      DEFAULTS,
    )
    expect(rows[0]?.findings.map((f) => f.rule_id)).toEqual(['table.dead_tuples_high'])
    expect(rows[1]?.findings).toEqual([])
  })
})

describe('summariseDeadRows', () => {
  it('DEAD-008 counts the tables that need a look', () => {
    const rows = buildDeadRows(
      [
        table('big', {
          n_live_tup: 30_000,
          n_dead_tup: 12_000,
          last_autovacuum_at: '2026-08-27T01:00:00Z',
        }),
        table('clean', { n_dead_tup: 0, last_autovacuum_at: '2026-08-27T01:00:00Z' }),
        table('never', { n_dead_tup: 10 }),
      ],
      [],
      DEFAULTS,
    )
    const summary = summariseDeadRows(rows)
    expect(summary).toMatchObject({
      tables: 3,
      withDead: 2,
      totalDead: 12_010,
      overTrigger: 1,
      neverVacuumed: 1,
    })
    expect(summary.worst?.table).toBe('big')
    expect(summary.deadBytes).toBeGreaterThan(0)
    expect(summariseDeadRows([]).worst).toBeNull()
  })

  it('DEAD-014 does not call a tiny table the worst one', () => {
    const rows = buildDeadRows(
      [
        // A catalog-sized table at 300% dead: noise, not the worst table.
        table('tiny', { n_live_tup: 3, n_dead_tup: 9 }),
        table('almost', { n_live_tup: 40_000, n_dead_tup: WORST_FLOOR - 1 }),
        table('real', { n_live_tup: 100_000, n_dead_tup: WORST_FLOOR }),
      ],
      [],
      DEFAULTS,
    )
    expect(summariseDeadRows(rows).worst?.table).toBe('real')
    expect(summariseDeadRows(rows.slice(0, 2)).worst).toBeNull()
  })
})

describe('sorting and filtering', () => {
  const rows = buildDeadRows(
    [
      table('alpha', {
        n_dead_tup: 5,
        hot_update_ratio: 0.9,
        n_tup_ins_rate: 1,
        last_vacuum_at: '2026-08-27T01:00:00Z',
      }),
      table('beta', { n_dead_tup: 500, total_bytes: 50_000, hot_update_ratio: 0.1 }),
      table('gamma', {
        n_dead_tup: 50,
        n_tup_ins_rate: 9,
        last_autovacuum_at: '2026-08-27T05:00:00Z',
      }),
    ],
    [finding('table.dead_tuples_high', 'public.beta')],
    DEFAULTS,
  )
  const order = (key: DeadSortKey, descending: boolean) =>
    [...rows].sort((a, b) => compareDeadRows(a, b, key, descending)).map((row) => row.table)

  it('DEAD-009 orders by every column, missing values last', () => {
    expect(order('dead', true)).toEqual(['beta', 'gamma', 'alpha'])
    expect(order('dead', false)).toEqual(['alpha', 'gamma', 'beta'])
    expect(order('table', false)).toEqual(['alpha', 'beta', 'gamma'])
    expect(order('size', true)[0]).toBe('beta')
    expect(order('ratio', true)[0]).toBe('beta')
    expect(order('trigger', true)[0]).toBe('beta')
    expect(order('hot', true)).toEqual(['alpha', 'beta', 'gamma'])
    expect(order('hot', false)).toEqual(['beta', 'alpha', 'gamma'])
    expect(order('writes', true)).toEqual(['gamma', 'alpha', 'beta'])
    expect(order('vacuum', true)).toEqual(['gamma', 'alpha', 'beta'])
    expect(order('vacuum', false)).toEqual(['alpha', 'gamma', 'beta'])
  })

  it('DEAD-010 breaks ties by key so the order is stable', () => {
    const twins = buildDeadRows([table('b'), table('a')], [], DEFAULTS)
    expect(twins.sort((x, y) => compareDeadRows(x, y, 'dead', true)).map((r) => r.table)).toEqual([
      'a',
      'b',
    ])
    const bare = buildDeadRows(
      [
        { datname: 'app', relname: 'y' },
        { datname: 'app', relname: 'x' },
      ],
      [],
      DEFAULTS,
    )
    expect(bare.sort((x, y) => compareDeadRows(x, y, 'hot', true)).map((r) => r.table)).toEqual([
      'x',
      'y',
    ])
  })

  it('DEAD-011 filters and searches', () => {
    const names = (filter: DeadFilter) =>
      rows.filter((row) => matchesDeadFilter(row, filter)).map((row) => row.table)
    expect(names('all')).toHaveLength(3)
    expect(names('dead')).toHaveLength(3)
    expect(names('due')).toEqual(['beta'])
    expect(names('never')).toEqual(['beta'])
    expect(names('flagged')).toEqual(['beta'])
    expect(rows.filter((row) => matchesDeadSearch(row, ' ALP ')).map((row) => row.table)).toEqual([
      'alpha',
    ])
    expect(rows.filter((row) => matchesDeadSearch(row, 'app'))).toHaveLength(3)
    expect(rows.filter((row) => matchesDeadSearch(row, ''))).toHaveLength(3)
    expect(rows.filter((row) => matchesDeadSearch(row, 'zzz'))).toHaveLength(0)
  })
})

describe('trend', () => {
  it('DEAD-012 drops the buckets with no reading', () => {
    expect(
      trendValues([
        { ts: 'a', n_dead_tup: 1 },
        { ts: 'b', n_dead_tup: null },
        { ts: 'c', n_dead_tup: 3 },
      ]),
    ).toEqual([1, 3])
  })

  it('DEAD-013 tells growing from shrinking and flat', () => {
    expect(trendDirection([100, 150, 400])).toBe('growing')
    expect(trendDirection([400, 150, 100])).toBe('shrinking')
    expect(trendDirection([100, 90, 102])).toBe('flat')
    expect(trendDirection([0, 0, 40])).toBe('growing')
    expect(trendDirection([0, 0])).toBe('flat')
    expect(trendDirection([5])).toBe('flat')
    expect(trendDirection([])).toBe('flat')
  })
})
