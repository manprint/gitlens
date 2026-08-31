import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  finding_id: 'finding-001',
  rule_id: 'replication-lag',
  severity: 'warning',
  state: 'open',
  scope: 'cluster',
  title: 'Replication lag requires attention',
}
export function makeGetFinding(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
