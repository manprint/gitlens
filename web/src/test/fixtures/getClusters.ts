import { CLUSTER, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = [CLUSTER]
export const empty = []
export const withNullLag = [{ ...CLUSTER, max_replay_lag_seconds: null }]
export function makeGetClusters(overrides: DeepPartial<typeof base> = []) {
  return withOverrides(base, overrides)
}
