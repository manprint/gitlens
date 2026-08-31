import { INSTANCE_SUMMARY, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = [INSTANCE_SUMMARY]
export const empty = []
export const down = [{ ...INSTANCE_SUMMARY, up: false }]
export function makeGetInstances(overrides: DeepPartial<typeof base> = []) {
  return withOverrides(base, overrides)
}
