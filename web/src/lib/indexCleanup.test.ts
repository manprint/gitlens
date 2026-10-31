import { describe, expect, it } from 'vitest'

import { formatBytes } from './format'
import { buildIndexRows } from './indexAnalysis'
import {
  buildCleanup,
  cleanupScript,
  dropStatement,
  indexWriteRates,
  observationBasis,
  quoteIdentifier,
  recreateStatement,
  summariseCleanup,
  UNUSED_INDEX_MIN_BYTES,
  unusedWindowPhrase,
} from './indexCleanup'

function item(name: string, definition: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    indexrelname: name,
    index_bytes: 1000,
    idx_scan_rate: 1,
    is_primary: false,
    is_unique: false,
    is_valid: true,
    last_scan_status: 'scanned',
    definition,
    ...extra,
  }
}

function finding(rule: string, object: string, extra: Record<string, unknown> = {}) {
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

const def = (name: string, cols = 'a') =>
  `CREATE INDEX ${name} ON public.orders USING btree (${cols})`

function candidates(
  items: Record<string, unknown>[],
  findings: Record<string, unknown>[] = [],
  rates = new Map<string, number>(),
  configuredWindow: number | null = null,
) {
  return buildCleanup(buildIndexRows(items, findings as never), rates, configuredWindow)
}

describe('identifiers and statements', () => {
  it('CLEAN-001 quotes identifiers and doubles embedded quotes', () => {
    expect(quoteIdentifier('plain')).toBe('"plain"')
    expect(quoteIdentifier('we"ird')).toBe('"we""ird"')
  })

  it('CLEAN-002 drops without blocking writes', () => {
    const [row] = buildIndexRows([item('Orders_idx', def('Orders_idx'))], [])
    expect(dropStatement(row!)).toBe('DROP INDEX CONCURRENTLY IF EXISTS "public"."Orders_idx";')
  })

  it('CLEAN-003 rebuilds the index as it was, concurrently', () => {
    const rows = buildIndexRows(
      [
        item('plain', def('plain')),
        item('uniq', 'CREATE UNIQUE INDEX uniq ON public.orders USING btree (a)'),
        item('already', 'CREATE INDEX CONCURRENTLY already ON public.orders USING btree (a)'),
        item('none', '', { definition: '' }),
        item('odd', 'not an index'),
      ],
      [],
    )
    expect(recreateStatement(rows[0]!)).toBe(
      'CREATE INDEX CONCURRENTLY plain ON public.orders USING btree (a);',
    )
    expect(recreateStatement(rows[1]!)).toMatch(/^CREATE UNIQUE INDEX CONCURRENTLY uniq/)
    expect(recreateStatement(rows[2]!)).toBe(
      'CREATE INDEX CONCURRENTLY already ON public.orders USING btree (a);',
    )
    expect(recreateStatement(rows[3]!)).toBeNull()
    expect(recreateStatement(rows[4]!)).toBeNull()
  })
})

describe('indexWriteRates', () => {
  it('CLEAN-004 counts inserts and updates that are not heap-only', () => {
    const rates = indexWriteRates([
      {
        datname: 'app',
        schemaname: 'public',
        relname: 'orders',
        n_tup_ins_rate: 2,
        n_tup_upd_rate: 5,
        n_tup_hot_upd_rate: 3,
        n_tup_del_rate: 9,
      },
      { datname: 'app', schemaname: 'public', relname: 'ins_only', n_tup_ins_rate: 4 },
      {
        datname: 'app',
        schemaname: 'public',
        relname: 'odd',
        n_tup_upd_rate: 1,
        n_tup_hot_upd_rate: 7,
      },
      { datname: 'app', schemaname: 'public', relname: 'silent' },
      { relname: 'unnamed', n_tup_ins_rate: 1 },
    ])
    expect(rates.get('app.public.orders')).toBe(4)
    expect(rates.get('app.public.ins_only')).toBe(4)
    // More heap-only updates than updates cannot happen; the figure is capped, not negative.
    expect(rates.get('app.public.odd')).toBe(0)
    expect(rates.has('app.public.silent')).toBe(false)
    expect(rates.get('..unnamed')).toBe(1)
  })
})

describe('buildCleanup', () => {
  it('CLEAN-005 never proposes an index that enforces a constraint', () => {
    const result = candidates(
      [
        item('pk', def('pk'), {
          is_primary: true,
          is_unique: true,
          last_scan_status: 'never_since_reset',
        }),
        item('uq', def('uq'), { is_unique: true, last_scan_status: 'never_since_reset' }),
      ],
      [finding('index.unused', 'public.pk'), finding('index.unused', 'public.uq')],
    )
    expect(result).toEqual([])
  })

  it('CLEAN-006 proposes an invalid index', () => {
    const [candidate] = candidates([item('bad', def('bad'), { is_valid: false })])
    expect(candidate).toMatchObject({ reason: 'invalid', basis: 'advisor', coveredBy: null })
  })

  it('CLEAN-007 keeps the used one of two duplicates and names it', () => {
    const items = [
      item('a_idx', def('a_idx'), { idx_scan_rate: 0 }),
      item('b_idx', def('b_idx'), { idx_scan_rate: 9 }),
    ]
    const result = candidates(items, [
      finding('index.duplicate', 'public.a_idx', {
        evidence: { indexes: ['public.a_idx', 'public.b_idx'] },
      }),
    ])
    expect(result).toHaveLength(1)
    expect(result[0]).toMatchObject({ reason: 'duplicate', coveredBy: 'public.b_idx' })
    expect(result[0]?.row.name).toBe('a_idx')
  })

  it('CLEAN-008 keeps the constraint of a duplicate group, then the larger, then the first name', () => {
    const withUnique = candidates(
      [item('x', def('x'), { is_unique: true }), item('y', def('y'), { idx_scan_rate: 50 })],
      [finding('index.duplicate', 'public.x', { evidence: { indexes: ['public.x', 'public.y'] } })],
    )
    expect(withUnique.map((c) => c.row.name)).toEqual(['y'])

    const bySize = candidates(
      [item('x', def('x'), { index_bytes: 10 }), item('y', def('y'), { index_bytes: 20 })],
      [finding('index.duplicate', 'public.x', { evidence: { indexes: ['public.x', 'public.y'] } })],
    )
    expect(bySize.map((c) => c.row.name)).toEqual(['x'])

    const byName = candidates(
      [item('x', def('x')), item('y', def('y'))],
      [finding('index.duplicate', 'public.x', { evidence: { indexes: ['public.x', 'public.y'] } })],
    )
    expect(byName.map((c) => c.row.name)).toEqual(['y'])
  })

  it('CLEAN-009 proposes a redundant prefix and names the longer index', () => {
    const [candidate] = candidates(
      [item('short', def('short', 'a'))],
      [
        finding('index.redundant_prefix', 'public.short', {
          evidence: { prefix: 'public.short', extended: 'public.long' },
        }),
      ],
    )
    expect(candidate).toMatchObject({ reason: 'redundant', coveredBy: 'public.long' })
    const [bare] = candidates(
      [item('short', def('short'))],
      [finding('index.redundant_prefix', 'public.short')],
    )
    expect(bare).toMatchObject({ reason: 'redundant', coveredBy: null })
  })

  it('CLEAN-010 tells the advisor verdict from the raw counter', () => {
    const result = candidates(
      [
        item('judged', def('judged'), { last_scan_status: 'never_since_reset' }),
        item('raw', def('raw'), { last_scan_status: 'never_since_reset' }),
        item('busy', def('busy')),
      ],
      [finding('index.unused', 'public.judged')],
    )
    expect(result.map((c) => [c.row.name, c.reason, c.basis])).toEqual([
      ['judged', 'unused', 'advisor'],
      ['raw', 'never-scanned', 'statistics'],
    ])
  })

  it('CLEAN-011 gives each index one reason, the strongest', () => {
    const result = candidates(
      [item('both', def('both'), { is_valid: false, last_scan_status: 'never_since_reset' })],
      [finding('index.unused', 'public.both')],
    )
    expect(result).toHaveLength(1)
    expect(result[0]?.reason).toBe('invalid')
  })

  it('CLEAN-012 orders by reason, then by the space given back', () => {
    const result = candidates(
      [
        item('small_unused', def('small_unused'), {
          index_bytes: 10,
          last_scan_status: 'never_since_reset',
        }),
        item('big_unused', def('big_unused'), {
          index_bytes: 90,
          last_scan_status: 'never_since_reset',
        }),
        item('bad', def('bad'), { is_valid: false, index_bytes: null }),
      ],
      [
        finding('index.unused', 'public.small_unused'),
        finding('index.unused', 'public.big_unused'),
      ],
    )
    expect(result.map((c) => c.row.name)).toEqual(['bad', 'big_unused', 'small_unused'])
    expect(result[0]?.reclaimBytes).toBe(0)
  })

  it('CLEAN-013 attaches the table write rate when it is known', () => {
    const rates = new Map([['app.public.orders', 6]])
    const [known] = candidates([item('bad', def('bad'), { is_valid: false })], [], rates)
    expect(known?.writeRate).toBe(6)
    const [unknown] = candidates([item('bad', def('bad'), { is_valid: false })])
    expect(unknown?.writeRate).toBeNull()
  })
})

describe('summariseCleanup', () => {
  it('CLEAN-014 keeps what the advisor confirms apart from what rests on raw counters', () => {
    const result = candidates(
      [
        item('bad', def('bad'), { is_valid: false, index_bytes: 300 }),
        item('raw', def('raw'), {
          index_bytes: 100,
          last_scan_status: 'never_since_reset',
          relname: 'other',
        }),
        item('raw2', def('raw2'), { index_bytes: 50, last_scan_status: 'never_since_reset' }),
      ],
      [],
      new Map([['app.public.orders', 4]]),
    )
    const summary = summariseCleanup(result, 2000)
    expect(summary.count).toBe(3)
    expect(summary.confirmed).toEqual({
      count: 1,
      reclaimBytes: 300,
      writeRate: 4,
      writeRateUnknown: 0,
    })
    expect(summary.provisional).toEqual({
      count: 2,
      reclaimBytes: 150,
      writeRate: 4,
      writeRateUnknown: 1,
    })
    expect(summary.confirmedShare).toBeCloseTo(0.15)
    expect(summary.byReason.invalid).toBe(1)
    expect(summary.byReason['never-scanned']).toBe(2)
    expect(summariseCleanup([], 0).confirmedShare).toBeNull()
  })
})

describe('unused-index wording (13.3)', () => {
  const evidence = (extra: Record<string, unknown>) => ({
    evidence: { peak_scan_rate: 0, history_days: 0.1, size_bytes: 1000, ...extra },
  })
  const big = { index_bytes: 64 * 1024 * 1024 }

  it('COVUI-005 the reason uses the observation and the window of the finding', () => {
    const [stats] = candidates(
      [item('idle', def('idle'), { last_scan_status: 'never_since_reset' })],
      [
        finding(
          'index.unused',
          'public.idle',
          evidence({
            observed_source: 'postgres_stats',
            observed_for_seconds: 9 * 86_400 + 3_000,
            window_seconds: 7 * 86_400,
          }),
        ),
      ],
    )
    expect(stats?.why).toBe(
      'The advisor saw no scans of this index: PostgreSQL statistics cover 9 days; the window is 7 days.',
    )

    const [own] = candidates(
      [item('idle', def('idle'))],
      [
        finding(
          'index.unused',
          'public.idle',
          evidence({
            observed_source: 'pglens',
            observed_for_seconds: 2 * 3_600 + 59,
            window_seconds: 3_600,
          }),
        ),
      ],
    )
    expect(own?.why).toBe(
      'The advisor saw no scans of this index: pglens history covers 2 hours; the window is 1 hour.',
    )
  })

  it('COVUI-005 the observed span is written in the unit of the window, never below it', () => {
    // 1.6 days against a 36-hour window: "1 day" would read shorter than the window.
    expect(
      observationBasis({
        observed_source: 'pglens',
        observed_for_seconds: 1.6 * 86_400,
        window_seconds: 36 * 3_600,
      }),
    ).toBe('pglens history covers 38 hours; the window is 36 hours')
    expect(
      observationBasis({
        observed_source: 'pglens',
        observed_for_seconds: 36 * 3_600,
        window_seconds: 36 * 3_600,
      }),
    ).toBe('pglens history covers 36 hours; the window is 36 hours')
    expect(
      observationBasis({
        observed_source: 'postgres_stats',
        observed_for_seconds: 7 * 86_400 + 86_399,
        window_seconds: 7 * 86_400,
      }),
    ).toBe('PostgreSQL statistics cover 7 days; the window is 7 days')
  })

  it('COVUI-005 a verdict since the statistics reset is worded from the reset', () => {
    // The advisor's evidence for an index never scanned since a reset of
    // unknown time: pglens history may be minutes old, and is not the basis.
    const unknown = evidence({
      window: 'since statistics reset',
      observed_source: 'pglens',
      observed_for_seconds: 0,
      window_seconds: 7 * 86_400,
    })
    const [unknownReset] = candidates(
      [item('idle', def('idle'), { last_scan_status: 'never_since_reset' })],
      [finding('index.unused', 'public.idle', unknown)],
    )
    expect(unknownReset?.why).toBe(
      'The advisor saw no scans of this index: PostgreSQL statistics show no scan since their last reset (time unknown); the window is 7 days.',
    )
    expect(unknownReset?.why).not.toContain('pglens history')

    const known = evidence({
      window: 'since statistics reset',
      stats_reset: '2026-10-01T08:30:00Z',
      observed_source: 'postgres_stats',
      observed_for_seconds: 29 * 86_400,
      window_seconds: 7 * 86_400,
    })
    expect(observationBasis(known.evidence)).toBe(
      'PostgreSQL statistics show no scan since their reset on 2026-10-01; the window is 7 days',
    )
    expect(observationBasis({ window: 'since statistics reset' })).toBe(
      'PostgreSQL statistics show no scan since their last reset (time unknown)',
    )
  })

  it('COVUI-005 a verdict from a last scan before the window is worded from that scan', () => {
    // PostgreSQL 16 recorded the last scan ten days ago and pglens saw none
    // inside the window: the advisor judged at once, on the statistics.
    const lastScan = evidence({
      last_scan: '2026-10-20T06:30:00Z',
      observed_source: 'postgres_stats',
      observed_for_seconds: 10 * 86_400,
      window_seconds: 7 * 86_400,
    })
    const [stale] = candidates(
      [item('idle', def('idle'))],
      [finding('index.unused', 'public.idle', lastScan)],
    )
    expect(stale?.why).toBe(
      'The advisor saw no scans of this index: PostgreSQL statistics show the last scan on 2026-10-20; the window is 7 days.',
    )
    expect(
      observationBasis({ last_scan: 'not a time', observed_source: 'postgres_stats' }),
    ).toBeNull()
  })

  it('COVUI-005 a finding without the numbers keeps the general sentence', () => {
    const [old] = candidates([item('idle', def('idle'))], [finding('index.unused', 'public.idle')])
    expect(old?.why).toBe('The advisor saw no scans of this index within its observation window.')
    expect(observationBasis({ observed_source: 'pglens', observed_for_seconds: 60 })).toBeNull()
    expect(observationBasis({ observed_for_seconds: 'x', window_seconds: 3_600 })).toBeNull()
    expect(observationBasis(undefined)).toBeNull()
    // An observation shorter than the window is no basis for a verdict.
    expect(
      observationBasis({
        observed_source: 'pglens',
        observed_for_seconds: 90,
        window_seconds: 3_600,
      }),
    ).toBeNull()
  })

  it('COVUI-005 a raw counter names the window of the findings, not a fixed 7 days', () => {
    const result = candidates(
      [
        item('judged', def('judged')),
        item('raw', def('raw'), { last_scan_status: 'never_since_reset', ...big }),
      ],
      [
        finding(
          'index.unused',
          'public.judged',
          evidence({
            observed_source: 'pglens',
            observed_for_seconds: 5_400,
            window_seconds: 3_600,
          }),
        ),
      ],
      new Map(),
      7 * 86_400,
    )
    const raw = result.find((candidate) => candidate.row.name === 'raw')
    expect(raw?.why).toBe(
      'No scan since the statistics were last reset. The advisor calls an index unused only after 1 hour with no scan, here and on any standby, so this is the raw counter, not a verdict.',
    )
    expect(cleanupScript(result)).toContain(
      '-- Raw counters only. The advisor has not called these indexes unused (that takes 1 hour',
    )
  })

  it('COVUI-005 without a finding the configured window is named', () => {
    const raw = [item('raw', def('raw'), { last_scan_status: 'never_since_reset', ...big })]
    const [configured] = candidates(raw, [], new Map(), 36 * 3_600)
    expect(configured?.why).toBe(
      'No scan since the statistics were last reset. The advisor calls an index unused only after 36 hours with no scan, here and on any standby, so this is the raw counter, not a verdict.',
    )
    expect(cleanupScript([configured!], 36 * 3_600)).toContain('(that takes 36 hours')
    expect(unusedWindowPhrase(buildIndexRows(raw, []), 36 * 3_600)).toBe('36 hours')

    // Neither a finding nor the settings: the window is unknown, and no length is claimed.
    const [alone] = candidates(raw)
    expect(alone?.why).toBe(
      'No scan since the statistics were last reset. The advisor calls an index unused only after its full observation window with no scan, here and on any standby, so this is the raw counter, not a verdict.',
    )
    expect(cleanupScript([alone!])).not.toContain('7 days')
  })

  it("COVUI-005 a small index is out of the advisor's reach, not short of history", () => {
    const [small] = candidates(
      [item('raw', def('raw'), { last_scan_status: 'never_since_reset', index_bytes: 8192 })],
      [],
      new Map(),
      3_600,
    )
    expect(small?.why).toBe(
      `No scan since the statistics were last reset. The advisor leaves out indexes of ${formatBytes(UNUSED_INDEX_MIN_BYTES)} or less, so this is the raw counter, not a verdict.`,
    )
    expect(small?.why).not.toContain('history')
    // At the limit the index is still left out; one byte more and the window applies.
    const [limit] = candidates([
      item('raw', def('raw'), {
        last_scan_status: 'never_since_reset',
        index_bytes: UNUSED_INDEX_MIN_BYTES,
      }),
    ])
    expect(limit?.why).toContain('leaves out indexes')
    const [over] = candidates([
      item('raw', def('raw'), {
        last_scan_status: 'never_since_reset',
        index_bytes: UNUSED_INDEX_MIN_BYTES + 1,
      }),
    ])
    expect(over?.why).toContain('only after its full observation window')
  })
})

describe('cleanupScript', () => {
  const lineOf = (script: string, text: string) =>
    script.split('\n').find((line) => line.includes(text))

  it('CLEAN-015 lists every confirmed drop and how to undo it', () => {
    const result = candidates([
      item('bad', def('bad'), { is_valid: false }),
      item('nodef', '', { is_valid: false, definition: null }),
    ])
    const script = cleanupScript(result)
    expect(script).toContain('pglens does not execute this script')
    expect(lineOf(script, '"public"."bad"')).toBe(
      'DROP INDEX CONCURRENTLY IF EXISTS "public"."bad";',
    )
    expect(lineOf(script, '"public"."nodef"')).toBe(
      'DROP INDEX CONCURRENTLY IF EXISTS "public"."nodef";',
    )
    expect(script).toContain('-- CREATE INDEX CONCURRENTLY bad ON public.orders USING btree (a);')
  })

  it('CLEAN-016 leaves out the undo section when no definition is known', () => {
    const script = cleanupScript(
      candidates([item('nodef', '', { is_valid: false, definition: null })]),
    )
    expect(script).not.toContain('To undo a drop')
  })

  it('CLEAN-017 comments out the drops that rest on raw counters', () => {
    const script = cleanupScript(
      candidates([
        item('bad', def('bad'), { is_valid: false }),
        item('raw', def('raw'), { last_scan_status: 'never_since_reset' }),
      ]),
    )
    expect(lineOf(script, '"public"."bad"')).toBe(
      'DROP INDEX CONCURRENTLY IF EXISTS "public"."bad";',
    )
    expect(lineOf(script, '"public"."raw"')).toBe(
      '-- DROP INDEX CONCURRENTLY IF EXISTS "public"."raw";',
    )
    expect(script).toContain('Raw counters only')
    // No active statement for the raw one: every line that mentions it is a comment.
    expect(
      script.split('\n').filter((line) => line.includes('raw') && !line.startsWith('--')),
    ).toEqual([])
  })

  it('CLEAN-018 has no raw-counter section when the advisor confirmed everything', () => {
    const script = cleanupScript(candidates([item('bad', def('bad'), { is_valid: false })]))
    expect(script).not.toContain('Raw counters only')
  })
})
