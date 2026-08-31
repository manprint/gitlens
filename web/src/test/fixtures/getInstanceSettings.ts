import { INSTANCE_ID, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { instance_id: INSTANCE_ID, settings: [] }
export const empty = base
export function makeGetInstanceSettings(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
