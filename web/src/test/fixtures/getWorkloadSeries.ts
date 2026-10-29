import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  step_seconds: 120,
  source: 'statements',
  series: [
    {
      name: 'calls',
      unit: 'calls/s',
      kind: 'rate',
      points: [
        { ts: '2026-08-27T01:00:00.000Z', value: 16.5 },
        { ts: '2026-08-27T01:02:00.000Z', value: null },
        { ts: '2026-08-27T01:04:00.000Z', value: 33 },
      ],
    },
  ],
}
export const empty = { ...base, series: [] }
export function makeGetWorkloadSeries(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
