import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  entries: [],
  resolution_seconds: 30,
  statistical: false,
}
export const empty = base
export const disabled = {
  ...base,
  enabled: false,
  warning: 'ASH sampling is disabled',
}
export function makeGetAshTop(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
