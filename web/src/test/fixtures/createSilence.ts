import { NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 201
export const base = {
  silence_id: '00000000-0000-4000-8000-000000000003',
  matchers: [],
  reason: 'maintenance window',
  starts_at: NOW,
  ends_at: NOW,
}
export function makeCreateSilence(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
