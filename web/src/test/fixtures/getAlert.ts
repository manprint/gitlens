import { NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  alert_key: 'replication-lag',
  rule_id: 'replication-lag',
  severity: 'warning',
  state: 'firing',
  datname: 'app',
  labels: { cluster: 'production' },
  value: 12.5,
  summary: 'Replication lag is above the threshold',
  started_at: NOW,
  last_eval_at: NOW,
  resolved_at: null,
  suppressed: false,
}
export function makeGetAlert(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
