import { CLUSTER, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { ...CLUSTER, name: 'prod-eu' }
export const cleared = { ...CLUSTER, name: null }
export function makeRenameCluster(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
