import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  rule_id: 'replication-lag',
  enabled: true,
  severity: 'warning',
  threshold: 10,
  for_seconds: 60,
}
export function makeUpdateAlertRule(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
