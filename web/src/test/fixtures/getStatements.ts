import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { statements: [], truncated: false, comparable_scope: 'cluster' }
export const empty = base
export const truncated = { ...base, truncated: true }
export function makeGetStatements(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
