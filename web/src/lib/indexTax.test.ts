import { describe, expect, it } from 'vitest'

import { buildIndexRows } from './indexAnalysis'
import { buildIndexTax, summariseIndexTax } from './indexTax'

function item(name: string, extra: Record<string, unknown> = {}, table = 'orders') {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: table,
    indexrelname: name,
    index_bytes: 1000,
    idx_scan_rate: 1,
    is_primary: false,
    is_unique: false,
    is_valid: true,
    last_scan_status: 'scanned',
    ...extra,
  }
}

function tableStats(extra: Record<string, unknown> = {}, table = 'orders') {
  return { datname: 'app', schemaname: 'public', relname: table, ...extra }
}

const unusedFinding = (object: string) => ({
  finding_id: `index.unused:${object}`,
  rule_id: 'index.unused',
  severity: 'warning',
  state: 'open',
  scope: 'relation',
  datname: 'app',
  object_name: object,
  title: 'unused',
})

function tax(
  items: Record<string, unknown>[],
  tables: Record<string, unknown>[] = [],
  findings: ReturnType<typeof unusedFinding>[] = [],
) {
  return buildIndexTax(buildIndexRows(items, findings as never), tables)
}

describe('buildIndexTax', () => {
  it('INDEXTAX-001 counts indexes and sizes and divides by the table size', () => {
    const [orders, lines] = tax(
      [
        item('a', { index_bytes: 3000 }),
        item('b', { index_bytes: 1000 }),
        item('c', { index_bytes: 500 }, 'lines'),
      ],
      [tableStats({ table_bytes: 2000 }), tableStats({ table_bytes: 500 }, 'lines')],
    )
    expect(orders).toMatchObject({
      table: 'orders',
      indexCount: 2,
      indexBytes: 4000,
      tableBytes: 2000,
      indexToTableRatio: 2,
    })
    expect(lines).toMatchObject({ table: 'lines', indexCount: 1, indexToTableRatio: 1 })
  })

  it('INDEXTAX-002 never counts a unique or primary index as unused', () => {
    const [row] = tax(
      [
        item('pk', { is_primary: true, is_unique: true, last_scan_status: 'never_since_reset' }),
        item('uq', { is_unique: true, last_scan_status: 'never_since_reset' }),
        item('raw', { last_scan_status: 'never_since_reset', index_bytes: 700 }),
        item('verdict', { index_bytes: 300 }),
      ],
      [],
      [unusedFinding('public.verdict')],
    )
    expect(row).toMatchObject({
      indexCount: 4,
      unusedCount: 2,
      unusedBytes: 1000,
      unusedConfirmedBytes: 300,
      unusedBasis: 'statistics',
    })
    const [confirmed] = tax(
      [item('verdict', { index_bytes: 300 })],
      [],
      [unusedFinding('public.verdict')],
    )
    expect(confirmed?.unusedBasis).toBe('advisor')
    const [none] = tax([item('used')])
    expect(none).toMatchObject({ unusedCount: 0, unusedBytes: 0, unusedBasis: null })
  })

  it('INDEXTAX-003 leaves heap-only updates out of the write amplification', () => {
    const [row] = tax(
      [item('a'), item('b'), item('c')],
      [
        tableStats({
          n_tup_ins_rate: 2,
          n_tup_upd_rate: 10,
          n_tup_hot_upd_rate: 8,
          n_tup_del_rate: 1,
        }),
      ],
    )
    expect(row?.writeRate).toBe(5)
    expect(row?.writeAmplification).toBe(15)
    const [capped] = tax([item('a')], [tableStats({ n_tup_upd_rate: 1, n_tup_hot_upd_rate: 7 })])
    expect(capped?.writeAmplification).toBe(0)
  })

  it('INDEXTAX-004 gives zero write amplification for a table with no writes', () => {
    const [row] = tax(
      [item('a'), item('b')],
      [tableStats({ n_tup_ins_rate: 0, n_tup_upd_rate: 0, n_tup_del_rate: 0 })],
    )
    expect(row?.writeRate).toBe(0)
    expect(row?.writeAmplification).toBe(0)
  })

  it('INDEXTAX-005 has no ratio and no amplification without table statistics', () => {
    const rows = tax([item('a'), item('b', {}, 'empty')], [tableStats({ table_bytes: 0 }, 'empty')])
    const unknown = rows.find((row) => row.table === 'orders')
    const zero = rows.find((row) => row.table === 'empty')
    expect(unknown?.indexToTableRatio).toBeNull()
    expect(unknown?.writeAmplification).toBeNull()
    expect(zero?.indexToTableRatio).toBeNull()
  })
})

describe('summariseIndexTax', () => {
  it('INDEXTAX-006 sums the tables and skips unknown rates', () => {
    const summary = summariseIndexTax(
      tax(
        [item('a'), item('b', { last_scan_status: 'never_since_reset' }), item('c', {}, 'lines')],
        [tableStats({ n_tup_ins_rate: 3 })],
      ),
    )
    expect(summary).toMatchObject({
      tables: 2,
      indexCount: 3,
      indexBytes: 3000,
      unusedBytes: 1000,
      unusedConfirmedBytes: 0,
      writeAmplification: 6,
    })
  })
})
