import { describe, expect, it } from 'vitest'

import type { ReplicationInstance, ReplicationMetricEdge, TopologyEdge } from './replication'
import {
  alignSeries,
  buildGraph,
  detectAnomalies,
  hasGaps,
  layoutGraph,
  summariseSlots,
} from './replication'

const PRIMARY: ReplicationInstance = {
  addr: 'primary.example.test',
  instance_id: '00000000-0000-4000-8000-000000000001',
  last_seen: '2026-08-31T12:00:00.000Z',
  perm_tier: 'T1',
  pg_version: 16,
  port: 5432,
  role: 'primary',
  up: true,
}

function standby(instance_id: string, addr: string): ReplicationInstance {
  return {
    ...PRIMARY,
    addr,
    instance_id,
    role: 'standby',
  }
}

function topology(overrides: Partial<TopologyEdge> = {}): TopologyEdge {
  return {
    confidence: 'high',
    from: PRIMARY.instance_id,
    to: '00000000-0000-4000-8000-000000000002',
    type: 'streaming',
    ...overrides,
  }
}

function metric(
  name: ReplicationMetricEdge['metric'],
  series: ReplicationMetricEdge['series'],
): ReplicationMetricEdge {
  return {
    from: PRIMARY.instance_id,
    metric: name,
    series,
    to: '00000000-0000-4000-8000-000000000002',
  }
}

describe('buildGraph', () => {
  it('UI-REPL-001 builds a node per instance and an edge per topology entry', () => {
    const graph = buildGraph(
      [PRIMARY, standby('00000000-0000-4000-8000-000000000002', 'standby.example.test')],
      [topology({ sync_state: 'sync' })],
    )

    expect(graph.nodes).toEqual([
      {
        address: PRIMARY.addr,
        id: PRIMARY.instance_id,
        port: 5432,
        role: 'primary',
        tier: 'T1',
        up: true,
        version: 16,
      },
      {
        address: 'standby.example.test',
        id: '00000000-0000-4000-8000-000000000002',
        port: 5432,
        role: 'standby',
        tier: 'T1',
        up: true,
        version: 16,
      },
    ])
    expect(graph.edges).toMatchObject([
      {
        confidence: 'high',
        from: PRIMARY.instance_id,
        sync_state: 'sync',
        to: '00000000-0000-4000-8000-000000000002',
        type: 'streaming',
      },
    ])
  })

  it('UI-REPL-002 makes an unresolved endpoint a distinct node and keeps its note', () => {
    const unknown = '00000000-0000-4000-8000-000000000099'
    const graph = buildGraph(
      [PRIMARY],
      [
        topology({ confidence: 'low', note: 'Endpoint could not be resolved', to: unknown }),
        topology({ confidence: 'low', to: unknown }),
      ],
    )

    expect(graph.nodes).toHaveLength(2)
    expect(graph.nodes[1]).toMatchObject({
      id: `unresolved:${unknown}`,
      role: 'unresolved',
      unresolved: true,
    })
    expect(graph.edges).toHaveLength(2)
    expect(graph.edges[0]).toMatchObject({
      from: PRIMARY.instance_id,
      note: 'Endpoint could not be resolved',
      to: `unresolved:${unknown}`,
    })
  })
})

describe('layoutGraph', () => {
  it('UI-REPL-003 is deterministic for the same graph input', () => {
    const standbyA = standby('00000000-0000-4000-8000-000000000002', 'z.example.test')
    const standbyB = standby('00000000-0000-4000-8000-000000000003', 'a.example.test')
    const graph = buildGraph(
      [PRIMARY, standbyA, standbyB],
      [topology({ to: standbyA.instance_id }), topology({ to: standbyB.instance_id })],
    )

    expect(layoutGraph(graph)).toEqual(layoutGraph(graph))
    expect(
      layoutGraph(graph).nodes.find((node) => node.id === standbyB.instance_id)?.position,
    ).toEqual({ x: 0, y: 1 })
  })

  it('UI-REPL-004 places cascading standbys one level below their upstream', () => {
    const direct = standby('00000000-0000-4000-8000-000000000002', 'standby.example.test')
    const cascade = standby('00000000-0000-4000-8000-000000000003', 'cascade.example.test')
    const graph = buildGraph(
      [PRIMARY, direct, cascade],
      [
        topology({ from: direct.instance_id, to: PRIMARY.instance_id }),
        topology({ from: cascade.instance_id, to: direct.instance_id }),
      ],
    )
    const laidOut = layoutGraph(graph)
    const directPosition = laidOut.nodes.find((node) => node.id === direct.instance_id)?.position
    const cascadePosition = laidOut.nodes.find((node) => node.id === cascade.instance_id)?.position

    expect(directPosition?.y).toBe(1)
    expect(cascadePosition?.y).toBe(2)
  })
})

describe('detectAnomalies', () => {
  it('UI-REPL-005 detects a graph without a primary', () => {
    const onlyStandby = standby('00000000-0000-4000-8000-000000000002', 'standby.example.test')
    const graph = buildGraph([onlyStandby], [])

    expect(detectAnomalies(graph)).toMatchObject({
      multiplePrimary: [],
      noPrimary: [onlyStandby.instance_id],
      orphanStandbys: [onlyStandby.instance_id],
    })
  })

  it('UI-REPL-006 reports all primary ids for split brain', () => {
    const secondPrimary = { ...PRIMARY, instance_id: '00000000-0000-4000-8000-000000000002' }
    const graph = buildGraph([PRIMARY, secondPrimary], [])

    expect(detectAnomalies(graph).multiplePrimary).toEqual([
      PRIMARY.instance_id,
      secondPrimary.instance_id,
    ])
  })
})

describe('alignSeries', () => {
  it('UI-REPL-007 preserves null and never interpolates across a gap', () => {
    const aligned = alignSeries([
      metric('write_lag_sec', [
        { ts: '2026-08-31T12:00:00Z', value: 1 },
        { ts: '2026-08-31T12:02:00Z', value: null },
      ]),
      metric('flush_lag_sec', [{ ts: '2026-08-31T12:00:00Z', value: 0.5 }]),
      metric('replay_lag_sec', [{ ts: '2026-08-31T12:02:00Z', value: 0.25 }]),
    ])

    expect(aligned.write_lag_sec).toEqual([1, null])
    expect(aligned.flush_lag_sec).toEqual([0.5, null])
    expect(aligned.replay_lag_sec).toEqual([null, 0.25])
  })

  it('UI-REPL-008 aligns all three metrics onto one sorted timestamp axis', () => {
    const aligned = alignSeries([
      metric('replay_lag_sec', [{ ts: '2026-08-31T12:02:00Z', value: 3 }]),
      metric('write_lag_sec', [{ ts: '2026-08-31T12:01:00Z', value: 1 }]),
      metric('flush_lag_sec', [{ ts: '2026-08-31T12:02:00Z', value: 2 }]),
    ])

    expect(aligned).toEqual({
      flush_lag_sec: [null, 2],
      replay_lag_sec: [null, 3],
      timestamps: ['2026-08-31T12:01:00Z', '2026-08-31T12:02:00Z'],
      write_lag_sec: [1, null],
    })
  })

  it('UI-REPL-009 reports gaps only when a bucket is null', () => {
    expect(hasGaps([1, null, 2])).toBe(true)
    expect(hasGaps([{ ts: '2026-08-31T12:00:00Z', value: null }])).toBe(true)
    expect(hasGaps([0, 1, 2])).toBe(false)
  })
})

describe('summariseSlots', () => {
  it('UI-REPL-010 totals retained bytes and names the worst slot', () => {
    const summary = summariseSlots([
      { active: false, name: 'inactive', retainedBytes: 100 },
      { active: true, name: 'worst', retainedBytes: 250 },
      { retained_bytes: 50, slot_active: 0, slot_name: 'server-slot' },
      { retained_bytes: -1, slot_active: Number.NaN, slot_name: 'unknown' },
    ])

    expect(summary).toEqual({ inactive: 2, retainedBytesTotal: 400, worst: 'worst' })
  })
})
