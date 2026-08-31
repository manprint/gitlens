import { CLUSTER_ID, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { cluster_id: CLUSTER_ID, edges: [] }
export const empty = base
export function makeGetClusterReplication(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
