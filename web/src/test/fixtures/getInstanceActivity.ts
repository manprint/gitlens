import { INSTANCE_ID, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { instance_id: INSTANCE_ID, stale: false, metrics: {}, xmin_horizon: null }
export const stale = { ...base, stale: true }
export const withXminHolder = {
  ...base,
  xmin_horizon: {
    holder_kind: 'session',
    identity: '4242',
    age_xids: 910,
    age_seconds: 33.5,
    sampled_at: '2026-10-23T10:00:00Z',
  },
}
export const withConnectionMetrics = {
  ...base,
  metrics: {
    pg_connections_by_origin: [
      { ts: '2026-10-23T10:00:00Z', value: 240, labels: { origin: 'application' } },
      { ts: '2026-10-23T10:00:00Z', value: 4, labels: { origin: 'pglens' } },
    ],
    pg_connections_by_user: [
      { ts: '2026-10-23T10:00:00Z', value: 244, labels: { usename: 'app' } },
    ],
    pg_connections_by_client: [
      { ts: '2026-10-23T10:00:00Z', value: 244, labels: { client_addr: 'local' } },
    ],
  },
}
export function makeGetInstanceActivity(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
