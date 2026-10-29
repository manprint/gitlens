import { describe, expect, it } from 'vitest'

import type { Schemas } from '@/api/types'

import {
  buildIndexRows,
  foreignKeyGaps,
  groupByTable,
  matchesFilter,
  matchesSearch,
  needsAttention,
  summariseIndexes,
} from './indexAnalysis'

type Finding = Schemas['Finding']

function item(name: string, extra: Record<string, unknown> = {}) {
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
    last_scan_at: '2026-08-27T01:00:00Z',
    definition: `CREATE INDEX ${name} ON public.orders USING btree (customer_id)`,
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

describe('buildIndexRows', () => {
  it('IDX-AN-001 attaches advisor findings to the index they name, never recomputing them', () => {
    const rows = buildIndexRows(
      [item('a_idx'), item('b_idx'), item('c_idx', { is_valid: false })],
      [
        finding('index.unused', 'public.a_idx'),
        finding('index.redundant_prefix', 'public.b_idx'),
        finding('index.invalid', 'public.c_idx'),
      ],
    )
    expect(rows[0]?.unusedFinding).not.toBeNull()
    expect(rows[1]?.redundantFinding).not.toBeNull()
    expect(rows[2]?.invalidFinding).not.toBeNull()
    expect(rows.every(needsAttention)).toBe(true)
  })

  it('IDX-AN-002 marks every member of a duplicate group with the others', () => {
    const rows = buildIndexRows(
      [item('a_idx'), item('b_idx'), item('c_idx')],
      [
        finding('index.duplicate', 'public.orders', {
          evidence: { indexes: ['public.a_idx', 'public.b_idx'] },
        }),
      ],
    )
    expect(rows[0]?.duplicateOf).toEqual(['public.b_idx'])
    expect(rows[1]?.duplicateOf).toEqual(['public.a_idx'])
    expect(rows[2]?.duplicateOf).toEqual([])
  })

  it('IDX-AN-003 ignores muted and resolved findings and findings of another database', () => {
    const rows = buildIndexRows(
      [item('a_idx')],
      [
        finding('index.unused', 'public.a_idx', { state: 'muted' }),
        finding('index.unused', 'public.a_idx', { state: 'resolved' }),
        finding('index.unused', 'public.a_idx', { datname: 'other' }),
      ],
    )
    expect(rows[0]?.unusedFinding).toBeNull()
    expect(needsAttention(rows[0]!)).toBe(false)
  })

  it('IDX-AN-004 tolerates missing fields', () => {
    const [row] = buildIndexRows([{ datname: 'app', indexrelname: 'x' }], [])
    expect(row?.bytes).toBeNull()
    expect(row?.scanRate).toBeNull()
    expect(row?.cacheHitRatio).toBeNull()
    expect(row?.lastScanStatus).toBe('not_available')
    expect(row?.parsed.columns).toEqual([])
    expect(row?.valid).toBe(true)
  })
})

describe('summariseIndexes', () => {
  it('IDX-AN-005 counts kinds, sizes and groups', () => {
    const rows = buildIndexRows(
      [
        item('orders_pkey', {
          is_primary: true,
          is_unique: true,
          definition: 'CREATE UNIQUE INDEX orders_pkey ON public.orders USING btree (id)',
        }),
        item('a_idx', {
          last_scan_status: 'never_since_reset',
          last_scan_at: null,
          index_bytes: 4000,
          definition: 'CREATE INDEX a_idx ON public.orders USING btree (a, b) WHERE (a > 0)',
        }),
        item('g_idx', {
          relname: 'docs',
          definition: 'CREATE INDEX g_idx ON public.docs USING gin (body)',
        }),
        item('e_idx', {
          is_valid: false,
          definition: 'CREATE INDEX e_idx ON public.orders USING btree (lower((name)::text))',
        }),
      ],
      [],
    )
    const summary = summariseIndexes(rows)
    expect(summary).toMatchObject({
      total: 4,
      bytes: 7000,
      primary: 1,
      unique: 0,
      partial: 1,
      expression: 1,
      composite: 1,
      invalid: 1,
      neverScanned: 1,
      neverScannedBytes: 4000,
    })
    expect(summary.byMethod.map((entry) => [entry.label, entry.count])).toEqual([
      ['btree', 3],
      ['gin', 1],
    ])
    expect(groupByTable(rows)[0]).toMatchObject({ table: 'orders' })
    expect(groupByTable(rows)[0]?.indexes).toHaveLength(3)
  })

  it('IDX-AN-006 does not call a unique or primary index never scanned', () => {
    const rows = buildIndexRows(
      [item('u_idx', { is_unique: true, last_scan_status: 'never_since_reset' })],
      [],
    )
    expect(summariseIndexes(rows).neverScanned).toBe(0)
    expect(matchesFilter(rows[0]!, 'unused')).toBe(false)
  })
})

describe('filters and search', () => {
  const rows = buildIndexRows(
    [
      item('a_idx', {
        definition: 'CREATE INDEX a_idx ON public.orders USING btree (customer_id)',
      }),
      item('p_idx', {
        definition: 'CREATE INDEX p_idx ON public.orders USING btree (id) WHERE (id > 0)',
      }),
    ],
    [finding('index.unused', 'public.a_idx')],
  )

  it('IDX-AN-007 filters by kind', () => {
    expect(rows.filter((row) => matchesFilter(row, 'unused')).map((row) => row.name)).toEqual([
      'a_idx',
    ])
    expect(rows.filter((row) => matchesFilter(row, 'partial')).map((row) => row.name)).toEqual([
      'p_idx',
    ])
    expect(rows.filter((row) => matchesFilter(row, 'all'))).toHaveLength(2)
  })

  it('IDX-AN-008 searches names, columns and methods', () => {
    expect(rows.filter((row) => matchesSearch(row, 'CUSTOMER')).map((row) => row.name)).toEqual([
      'a_idx',
    ])
    expect(rows.filter((row) => matchesSearch(row, 'btree'))).toHaveLength(2)
    expect(rows.filter((row) => matchesSearch(row, 'nothing'))).toHaveLength(0)
  })
})

describe('foreignKeyGaps', () => {
  it('IDX-AN-009 lists open foreign keys without an index', () => {
    const gaps = foreignKeyGaps([
      finding('index.fk_unindexed', 'public.orders.orders_customer_fk', {
        evidence: {
          table: 'public.orders',
          constraint: 'orders_customer_fk',
          columns: 'customer_id',
          referenced: 'public.customers',
        },
      }),
      finding('index.unused', 'public.a_idx'),
    ])
    expect(gaps).toHaveLength(1)
    expect(gaps[0]).toMatchObject({
      table: 'public.orders',
      constraint: 'orders_customer_fk',
      columns: 'customer_id',
      referenced: 'public.customers',
    })
  })
})
