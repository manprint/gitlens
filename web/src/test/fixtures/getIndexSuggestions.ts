import { INSTANCE_ID, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const empty = {
  instance_id: INSTANCE_ID,
  generated_at: '2026-11-05T12:00:00.000Z',
  window_seconds: 86400,
  estimates_note: 'Every figure is an estimate from the statistics of the window.',
  coverage: { statements_considered: 0, statements_analysed: 0, skipped: {} },
  suggestions: [],
  notes: ['no statements in the window'],
}
export const base = {
  ...empty,
  coverage: { statements_considered: 3, statements_analysed: 2, skipped: { no_text: 1 } },
  suggestions: [
    {
      id: '2d4629f99baf',
      database: 'odoo',
      schema: 'public',
      table: 'res_partner',
      columns: [{ name: 'ref', desc: false }],
      create_statement: 'CREATE INDEX CONCURRENTLY idx_res_partner_ref ON public.res_partner (ref);',
      weight_pct: 40,
      read_cost_ms_per_s: 40,
      calls_rate: 12,
      statements: [{ queryid: 11, share_pct: 100, query_text: 'SELECT id FROM res_partner WHERE ref = $1' }],
      overlapping_indexes: [],
      selectivity: { column: 'ref', n_distinct: -0.8, label: 'high' },
      est_size_bytes: 1380000,
      write: { insert_rate: 0.5, update_rate: 1, delete_rate: 0.1, hot_update_ratio: 0.5 },
      read_write_ratio: 10.9,
      verdict: 'strong',
      confidence: 'high',
      notes: [],
    },
  ],
  notes: [],
}
export function makeGetIndexSuggestions(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
