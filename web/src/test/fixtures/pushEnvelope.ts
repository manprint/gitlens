import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 202
export const base = { accepted: 1, rejected: 0 }
export function makePushEnvelope(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
