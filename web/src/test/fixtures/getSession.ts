import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { authenticated: true, configured: true }
export const unauthenticated = { authenticated: false, configured: true }
export function makeGetSession(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
