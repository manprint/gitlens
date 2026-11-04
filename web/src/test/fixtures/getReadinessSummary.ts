import type { Schemas } from '@/api/types'

import { INSTANCE_ID } from '../fixture-helpers'

type ReadinessSummary = Schemas['ReadinessSummary']

export const responseStatus = 200

/** No instance. */
export const base: ReadinessSummary = { instances: [], totals: { full: 0, partial: 0 } }
export const empty = base

/** One instance monitored in part. */
export const partial: ReadinessSummary = {
  instances: [{ instance_id: INSTANCE_ID, level: 'partial', warnings: 1, infos: 2 }],
  totals: { full: 0, partial: 1 },
}
