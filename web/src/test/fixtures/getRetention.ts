import type { Schemas } from '@/api/types'

type RetentionFamily = Schemas['RetentionFamily']

export const responseStatus = 200

/** What migrations 0003 and 0007 leave: 30 days each, 90 for metrics_bloat. */
export const base: RetentionFamily[] = [
  {
    family: 'metrics',
    hypertable: 'metrics',
    days: 30,
    source: 'default',
    min_days: 3,
    max_days: 365,
  },
  {
    family: 'metrics_statements',
    hypertable: 'metrics_statements',
    days: 30,
    source: 'default',
    min_days: 3,
    max_days: 365,
  },
  {
    family: 'metrics_ash',
    hypertable: 'metrics_ash',
    days: 30,
    source: 'default',
    min_days: 3,
    max_days: 365,
  },
  {
    family: 'metrics_replication',
    hypertable: 'metrics_replication',
    days: 30,
    source: 'default',
    min_days: 3,
    max_days: 365,
  },
  {
    family: 'metrics_tables',
    hypertable: 'metrics_tables',
    days: 30,
    source: 'default',
    min_days: 3,
    max_days: 365,
  },
  {
    family: 'metrics_indexes',
    hypertable: 'metrics_indexes',
    days: 30,
    source: 'default',
    min_days: 3,
    max_days: 365,
  },
  {
    family: 'metrics_bloat',
    hypertable: 'metrics_bloat',
    days: 90,
    source: 'default',
    min_days: 8,
    max_days: 365,
  },
]

/** metrics_ash shortened to 14 days and metrics_tables without a policy. */
export const custom: RetentionFamily[] = base.map((row) => {
  if (row.family === 'metrics_ash') return { ...row, days: 14, source: 'custom' }
  if (row.family === 'metrics_tables') return { ...row, days: null, source: 'custom' }
  return row
})

export function makeGetRetention(
  overrides: Partial<Record<RetentionFamily['family'], Partial<RetentionFamily>>> = {},
) {
  return base.map((row) => ({ ...row, ...overrides[row.family] }))
}
