import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  command_id: '00000000-0000-4000-8000-000000000002',
  state: 'pending',
}
export function makeGetCommand(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
