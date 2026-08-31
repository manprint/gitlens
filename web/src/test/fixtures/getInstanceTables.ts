import { INSTANCE_ID, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  instance_id: INSTANCE_ID,
  items: [],
  truncated: false,
  relations_not_reported: 0,
}
export const truncated = { ...base, truncated: true, relations_not_reported: 3 }
export function makeGetInstanceTables(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
