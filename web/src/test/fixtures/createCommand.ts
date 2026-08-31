import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 202
export const base = { command_id: '00000000-0000-4000-8000-000000000002' }
export function makeCreateCommand(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
