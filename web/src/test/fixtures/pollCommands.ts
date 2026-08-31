import { CLUSTER_ID, INSTANCE_ID, NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  command_id: '00000000-0000-4000-8000-000000000002',
  kind: 'explain',
  args: {},
  instance_id: INSTANCE_ID,
  cluster_id: CLUSTER_ID,
  claim_token: '00000000-0000-4000-8000-000000000004',
  expires_at: NOW,
}
export function makePollCommands(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
