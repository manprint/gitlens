import { describe, expect, it } from 'vitest'

import {
  CONNECTION_SATURATION_THRESHOLD,
  breakdown,
  connectionSaturation,
  counterRate,
  deadlockRate,
  hasReported,
  latestSampleAt,
  latestValue,
  oldestStateAge,
} from './activity'

const first = '2026-08-27T01:59:50.000Z'
const second = '2026-08-27T02:00:00.000Z'

function metric(value: number | null, ts = second, labels: Record<string, string> = {}) {
  return { ts, value, labels }
}

describe('activity derivation', () => {
  it('selects the newest value and sample across metric series', () => {
    const metrics = {
      pg_backends: [metric(2, first), metric(5, second)],
      pg_connections_used: [metric(null, second)],
    }

    expect(latestValue(metrics, 'pg_backends')).toBe(5)
    expect(latestValue(metrics, 'pg_connections_used')).toBeNull()
    expect(latestSampleAt(metrics)).toBe(second)
    expect(latestSampleAt({})).toBeNull()
  })

  it('aggregates state and database labels from their newest samples', () => {
    const metrics = {
      pg_backends: [
        metric(2, first, { state: 'active', wait_event_type: 'CPU' }),
        metric(3, second, { state: 'active', wait_event_type: 'Lock' }),
        metric(4, second, { state: 'idle', wait_event_type: 'CPU' }),
      ],
      pg_max_state_age_seconds: [
        metric(10, second, { state: 'active' }),
        metric(20, second, { state: 'idle' }),
      ],
    }

    expect(breakdown(metrics, 'pg_backends', 'state')).toEqual([
      { label: 'active', value: 5 },
      { label: 'idle', value: 4 },
    ])
    expect(oldestStateAge(metrics)).toBe(20)
  })

  it('uses the same strict above-80-percent saturation threshold as the advisor', () => {
    expect(
      connectionSaturation({ pg_connections_used_ratio: [metric(CONNECTION_SATURATION_THRESHOLD)] })
        .saturated,
    ).toBe(false)
    expect(connectionSaturation({ pg_connections_used_ratio: [metric(0.81)] }).saturated).toBe(true)
    expect(connectionSaturation({}).ratio).toBeNull()
  })

  it('derives counter rates, including a reset, and aggregates deadlock label groups', () => {
    expect(counterRate([metric(10, first), metric(12, second)])).toBeCloseTo(0.2)
    expect(counterRate([metric(12, second)])).toBeNull()
    expect(
      deadlockRate({
        pg_deadlocks_total: [
          metric(10, first, { database: 'app' }),
          metric(12, second, { database: 'app' }),
          metric(4, first, { database: 'jobs' }),
          metric(5, second, { database: 'jobs' }),
        ],
      }),
    ).toBeCloseTo(0.3)
    expect(deadlockRate({})).toBeNull()
  })

  it('reports whether an opt-in metric was actually returned', () => {
    expect(
      hasReported({ pg_connections_by_application: [] }, 'pg_connections_by_application'),
    ).toBe(false)
    expect(
      hasReported({ pg_connections_by_application: [metric(1)] }, 'pg_connections_by_application'),
    ).toBe(true)
  })
})
