import { describe, expect, it } from 'vitest'

import type { Schemas } from '@/api/types'
import { base } from '@/test/fixtures/getIndexSuggestions'

import type { IndexSuggestion } from './indexSuggestions'
import {
  buildMissingIndexes,
  createdIndexName,
  ident,
  indexName,
  missingIndexScript,
  splitTable,
  summariseMissing,
  type MissingInput,
} from './missingIndexes'

type Finding = Schemas['Finding']

const MIB = 1024 * 1024
const FIRST = base.suggestions[0]! as unknown as IndexSuggestion

function suggestion(extra: Partial<IndexSuggestion> = {}): IndexSuggestion {
  return { ...FIRST, verdict: 'strong', confidence: 'high', ...extra }
}

function fk(extra: Record<string, unknown> = {}, datname = 'odoo'): Finding {
  return {
    finding_id: `fk-${datname}`,
    rule_id: 'index.fk_unindexed',
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname,
    object_name: 'public.orders.orders_customer_fk',
    title: 'Foreign key without an index',
    evidence: {
      table: 'public.orders',
      constraint: 'orders_customer_fk',
      columns: 'customer_id',
      referenced: 'public.customers',
      table_bytes: 20 * MIB,
      ...extra,
    },
  }
}

function spatial(extra: Record<string, unknown> = {}): Finding {
  return {
    finding_id: 'geo-1',
    rule_id: 'postgis.spatial_unindexed',
    severity: 'info',
    state: 'open',
    scope: 'relation',
    datname: 'odoo',
    object_name: 'public.places.geom',
    title: 'Spatial column without an index',
    evidence: { table: 'public.places', column: 'geom', rows: 5000, type: 'geometry', ...extra },
  }
}

function table(name: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'odoo',
    schemaname: 'public',
    relname: name,
    table_bytes: 1 * MIB,
    seq_tup_read_rate: 0,
    seq_scan_rate: 0,
    n_tup_del_rate: 0,
    n_tup_upd_rate: 0,
    ...extra,
  }
}

function build(input: Partial<MissingInput>) {
  return buildMissingIndexes({ suggestions: [], findings: [], tables: [], database: '', ...input })
}

describe('missing index helpers', () => {
  it('MISS-LIB-001 identifiers are bare when plain lower case and quoted otherwise', () => {
    expect(ident('orders')).toBe('orders')
    expect(ident('Orders')).toBe('"Orders"')
    expect(ident('a"b')).toBe('"a""b"')
    expect(splitTable('sales.orders')).toEqual(['sales', 'orders'])
    expect(splitTable('orders')).toEqual(['public', 'orders'])
  })

  it('MISS-LIB-002 a generated index name stays within 63 bytes and keeps its suffix', () => {
    expect(indexName('orders', ['customer_id'], 'idx')).toBe('orders_customer_id_idx')
    const long = indexName('t'.repeat(60), ['column_name'], 'gist')
    expect(long).toHaveLength(63)
    expect(long.endsWith('_gist')).toBe(true)
    expect(indexName('Mixed-Case', ['a b'], 'idx')).toBe('mixed_case_a_b_idx')
  })

  it('MISS-LIB-003 the index name is read from a create statement, quoted or not', () => {
    expect(createdIndexName('CREATE INDEX CONCURRENTLY idx_a ON public.t (a);')).toBe('idx_a')
    expect(createdIndexName('create index if not exists "Big Idx" on t (a)')).toBe('"Big Idx"')
    expect(createdIndexName('SELECT 1')).toBeNull()
  })
})

describe('buildMissingIndexes', () => {
  it('MISS-LIB-004 a strong suggestion is Create; low gain is Check; write heavy is Skip', () => {
    const [strong] = build({ suggestions: [suggestion()] })
    expect(strong!.recommendation).toBe('create')
    expect(strong!.steps.map((step) => step.title)).toEqual(['Check', 'Create', 'Verify', 'Undo'])
    expect(strong!.steps[3]!.sql).toBe(
      'DROP INDEX CONCURRENTLY IF EXISTS public.idx_res_partner_ref;',
    )

    const low = build({ suggestions: [suggestion({ read_cost_ms_per_s: 0.2, weight_pct: 0.5 })] })
    expect(low[0]!.recommendation).toBe('check')
    expect(low[0]!.why).toContain('That gain is small')

    const heavy = build({ suggestions: [suggestion({ verdict: 'write_heavy' })] })
    expect(heavy[0]!.recommendation).toBe('skip')

    const rough = build({ suggestions: [suggestion({ confidence: 'low' })] })
    expect(rough[0]!.recommendation).toBe('test')
    expect(rough[0]!.why).toContain('Column statistics are missing')
  })

  it('MISS-LIB-005 an unindexed foreign key is Create only on a big table with deletes', () => {
    const parentDeletes = [table('customers', { n_tup_del_rate: 0.5 })]
    const [big] = build({ findings: [fk()], tables: parentDeletes })
    expect(big!.recommendation).toBe('create')
    expect(big!.command).toBe(
      'CREATE INDEX CONCURRENTLY IF NOT EXISTS orders_customer_id_idx ON public.orders (customer_id);',
    )
    expect(big!.why).toContain('are being deleted now')

    const [small] = build({ findings: [fk({ table_bytes: 1 * MIB })], tables: parentDeletes })
    expect(small!.recommendation).toBe('test')

    const [quiet] = build({ findings: [fk()] })
    expect(quiet!.recommendation).toBe('check')
    expect(quiet!.why).toContain('Create it if rows of public.customers are ever deleted')
  })

  it('MISS-LIB-006 an unindexed spatial column gets a GiST index to check first', () => {
    const [item] = build({ findings: [spatial()] })
    expect(item!.source).toBe('spatial')
    expect(item!.recommendation).toBe('check')
    expect(item!.command).toBe(
      'CREATE INDEX CONCURRENTLY IF NOT EXISTS places_geom_gist ON public.places USING gist (geom);',
    )
    expect(item!.why).toContain('reads all 5,000 rows')
    expect(build({ findings: [spatial({ column: '' })] })).toEqual([])
    expect(build({ findings: [{ ...spatial(), state: 'resolved' }] })).toEqual([])
  })

  it('MISS-LIB-007 a full-scanned table is listed only above both floors and without another row', () => {
    const hot = table('events', {
      table_bytes: 64 * MIB,
      seq_tup_read_rate: 5000,
      seq_scan_rate: 2,
    })
    const [item] = build({ tables: [hot] })
    expect(item!.source).toBe('scans')
    expect(item!.command).toBeNull()
    expect(item!.why).toContain('about 2,500 rows per scan')

    expect(build({ tables: [{ ...hot, table_bytes: 1 * MIB }] })).toEqual([])
    expect(build({ tables: [{ ...hot, seq_tup_read_rate: 10 }] })).toEqual([])
    const covered = build({
      suggestions: [suggestion({ table: 'events' })],
      tables: [hot],
    })
    expect(covered.map((row) => row.source)).toEqual(['queries'])
  })

  it('MISS-LIB-008 the picked database scopes every source; rows sort by recommendation then weight', () => {
    const rows = build({
      suggestions: [suggestion(), suggestion({ id: 'other', database: 'crm' })],
      findings: [fk(), fk({}, 'crm'), spatial()],
      tables: [table('customers', { n_tup_del_rate: 1 })],
      database: 'odoo',
    })
    expect(rows.every((row) => row.database === 'odoo')).toBe(true)
    expect(rows.map((row) => row.recommendation)).toEqual(['create', 'create', 'check'])
    expect(rows[0]!.weight).toBeGreaterThanOrEqual(rows[1]!.weight)
  })
})

describe('summary and script', () => {
  const rows = build({
    suggestions: [suggestion(), suggestion({ id: 'heavy', verdict: 'write_heavy' })],
    findings: [fk(), spatial()],
    tables: [table('events', { table_bytes: 64 * MIB, seq_tup_read_rate: 5000 })],
  })

  it('MISS-LIB-009 the summary counts rows by recommendation and by source', () => {
    const summary = summariseMissing(rows)
    expect(summary.total).toBe(5)
    expect(summary.byRecommendation).toEqual({ create: 1, test: 0, check: 3, skip: 1 })
    expect(summary.bySource).toEqual({ queries: 2, 'foreign-keys': 1, spatial: 1, scans: 1 })
  })

  it('MISS-LIB-010 the script runs Create rows, comments out Check rows, leaves out the rest', () => {
    const script = missingIndexScript(rows, 'prod-db')
    expect(script).toContain('-- Missing indexes for prod-db, generated by pglens.')
    expect(script).toContain('\\connect odoo')
    expect(script).toContain(
      '\nCREATE INDEX CONCURRENTLY idx_res_partner_ref ON public.res_partner (ref);',
    )
    expect(script).toContain(
      '-- Enable after the check described on the page:\n-- CREATE INDEX CONCURRENTLY IF NOT EXISTS orders_customer_id_idx',
    )
    expect(script.match(/idx_res_partner_ref ON/g)).toHaveLength(1)
    expect(script).not.toContain('events_col_idx')
    expect(missingIndexScript([], 'prod-db')).toContain('-- No index to create in this list.')
  })
})
