import { expectTypeOf, test } from 'vitest'

import type { Cluster, SeriesPoint } from './types'

test('keeps contract-sensitive API types stable', () => {
  expectTypeOf<Cluster['cluster_id']>().toEqualTypeOf<string>()
  expectTypeOf<SeriesPoint['value']>().toEqualTypeOf<number | null>()
  expectTypeOf<Cluster['max_replay_lag_seconds']>().toEqualTypeOf<number | null | undefined>()
})
