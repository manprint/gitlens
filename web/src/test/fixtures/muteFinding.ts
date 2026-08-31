import { NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  finding_id: 'finding-001',
  state: 'muted',
  muted_until: NOW,
  mute_reason: 'maintenance window',
}
export function makeMuteFinding(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
