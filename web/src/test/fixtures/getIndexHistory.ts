import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const empty = {
  index: { datname: 'odoo', schemaname: 'public', indexrelname: 'res_partner_name_idx' },
  step_seconds: 1200,
  points: [],
}
export const base = {
  ...empty,
  points: [
    {
      ts: '2026-08-27T01:00:00.000Z',
      idx_scan_rate: 2,
      idx_tup_read_rate: 20,
      index_bytes: 1380000,
    },
    {
      ts: '2026-08-27T01:20:00.000Z',
      idx_scan_rate: null,
      idx_tup_read_rate: null,
      index_bytes: null,
    },
    {
      ts: '2026-08-27T01:40:00.000Z',
      idx_scan_rate: 6,
      idx_tup_read_rate: 0,
      index_bytes: 1400000,
    },
  ],
}
export function makeGetIndexHistory(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
