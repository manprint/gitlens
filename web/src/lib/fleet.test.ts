import { describe, expect, it } from 'vitest'

import type { Cluster } from '@/api/types'
import { CLUSTER } from '@/test/fixture-helpers'

import { describeHealth } from './fleet'

const cluster = (overrides: Partial<Cluster> = {}): Cluster =>
  ({ ...CLUSTER, ...overrides }) as Cluster

describe('describeHealth', () => {
  it('UI-FLEET-030 says an ok cluster has all instances up and no lag', () => {
    const sentence = describeHealth(cluster({ health: 'ok' }))

    expect(sentence).toMatch(/primary is present/i)
    expect(sentence).toMatch(/all instances are up/i)
    expect(sentence).toMatch(/no replication lag/i)
  })

  it('UI-FLEET-031 names a down instance or lagging standby for degraded health', () => {
    const down = describeHealth(
      cluster({
        health: 'degraded',
        instances: [{ ...CLUSTER.instances[0], addr: 'postgres-down', up: false }],
      }),
    )
    const lagging = describeHealth(cluster({ health: 'degraded', max_replay_lag_seconds: 12.5 }))
    const needsAttention = describeHealth(cluster({ health: 'degraded' }))

    expect(down).toMatch(/postgres-down.*down/i)
    expect(lagging).toMatch(/standby replay lag.*12\.5 seconds/i)
    expect(needsAttention).toMatch(/health check requires attention/i)
  })

  it('UI-FLEET-032 says split brain for critical health with two primaries', () => {
    const sentence = describeHealth(
      cluster({
        health: 'critical',
        instances: [
          CLUSTER.instances[0],
          { ...CLUSTER.instances[0], instance_id: '00000000-0000-4000-8000-000000000002' },
        ],
      }),
    )

    expect(sentence).toMatch(/split brain/i)
  })

  it('UI-FLEET-033 says no primary for critical health without a primary', () => {
    const sentence = describeHealth(
      cluster({
        health: 'critical',
        primary: null,
        instances: [{ ...CLUSTER.instances[0], role: 'standby' }],
      }),
    )

    expect(sentence).toMatch(/no primary/i)
  })

  it('UI-FLEET-034 never contradicts the server health field', () => {
    const fixtures = [
      cluster({ health: 'ok' }),
      cluster({ health: 'degraded', max_replay_lag_seconds: 3 }),
      cluster({ health: 'critical', primary: null, instances: [] }),
      cluster({
        health: 'critical',
        instances: [
          CLUSTER.instances[0],
          { ...CLUSTER.instances[0], instance_id: '00000000-0000-4000-8000-000000000002' },
        ],
      }),
    ]

    for (const cluster of fixtures) {
      const sentence = describeHealth(cluster)
      if (cluster.health === 'ok') expect(sentence).toMatch(/healthy/i)
      if (cluster.health === 'degraded') expect(sentence).toMatch(/degraded/i)
      if (cluster.health === 'critical') expect(sentence).toMatch(/critical/i)
    }
  })
})
