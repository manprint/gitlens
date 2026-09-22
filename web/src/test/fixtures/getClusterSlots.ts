import { CLUSTER_ID, INSTANCE_ID, NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { cluster_id: CLUSTER_ID, slots: [] }
export const empty = base
export const inactive = {
  cluster_id: CLUSTER_ID,
  slots: [
    {
      instance_id: INSTANCE_ID,
      slot_name: 'standby_1',
      slot_type: 'physical',
      active: false,
      wal_status: 'extended',
      retained_bytes: 2 * 1024 ** 3,
      safe_wal_size_bytes: null,
      last_active_at: '2026-08-27T01:00:00.000Z',
      ts: NOW,
    },
  ],
}
export function makeGetClusterSlots(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
