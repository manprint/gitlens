import { CLUSTER_ID, INSTANCE_SUMMARY, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  ...INSTANCE_SUMMARY,
  cluster_id: CLUSTER_ID,
  databases: [],
  databases_not_monitored: 0,
}
export function makeGetInstance(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
