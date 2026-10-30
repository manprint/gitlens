import { CLUSTER_ID, INSTANCE_SUMMARY, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { ...INSTANCE_SUMMARY, cluster_id: CLUSTER_ID, display_name: 'odoo-db-1' }
export const cleared = { ...INSTANCE_SUMMARY, cluster_id: CLUSTER_ID, display_name: null }
export function makeRenameInstance(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
