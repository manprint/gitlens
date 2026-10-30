import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200

export const base = {
  family: 'metrics_ash' as const,
  hypertable: 'metrics_ash',
  days: 14 as number | null,
  source: 'custom' as 'default' | 'custom',
  min_days: 3,
  max_days: 365 as const,
}

export function makeSetRetention(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
