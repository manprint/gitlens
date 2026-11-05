import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

vi.mock('echarts-for-react/esm/core', () => ({
  default: () => <div data-testid="mock-echarts" />,
}))
vi.mock('@xyflow/react', () => ({
  Background: () => null,
  Controls: () => null,
  ReactFlow: () => <div data-testid="mock-react-flow" />,
}))

import type { Schemas } from '@/api/types'
import { captionOf, type CaptionId } from '@/lib/captions'
import type { ReplicationMetricEdge } from '@/lib/replication'
import { INSTANCE_ID, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { expectCaptionedCards } from '@/test/clearCard'
import { renderWithProviders } from '@/test/render'

import { DriftSection } from './DriftSection'
import { EventTimeline } from './EventTimeline'
import { LagSection } from './LagSection'
import { SlotsSection, type SlotRow } from './SlotsSection'
import { TopologyGraph } from './TopologyGraph'

const STANDBY_ID = '00000000-0000-4000-8000-000000000002'

function visibleCaptions(id: CaptionId) {
  return screen.getAllByText(captionOf(id).what).filter((element) => {
    try {
      expect(element).toBeVisible()
      return true
    } catch {
      return false
    }
  })
}

describe('cluster, topology and lag captions', () => {
  it('CAPTION-PAGE-010 the drift table is captioned', () => {
    const { container } = renderWithProviders(
      <DriftSection
        entries={[
          {
            name: 'max_connections',
            values: [
              { instance_id: 'primary', role: 'primary', value: '100' },
              { instance_id: 'standby-1', role: 'standby', value: '200' },
            ],
          },
        ]}
      />,
    )

    expect(expectCaptionedCards(container)).toEqual(['cluster.drift-table'])
    expect(visibleCaptions('cluster.drift-table')).toHaveLength(1)
  })

  it('CAPTION-PAGE-011 the slots table is captioned', () => {
    const slots: SlotRow[] = [
      {
        active: false,
        inactiveAgeSeconds: 600,
        name: 'slot_a',
        owningInstance: 'primary',
        retainedBytes: 1_536,
        safeWalSizeBytes: null,
        slotType: 'physical',
        walStatus: null,
      },
    ]
    const { container } = renderWithProviders(<SlotsSection slots={slots} />)

    expect(expectCaptionedCards(container)).toEqual(['cluster.slots-table'])
    expect(visibleCaptions('cluster.slots-table')).toHaveLength(1)
  })

  it('CAPTION-PAGE-012 the topology section is captioned', () => {
    const instances: Schemas['InstanceSummary'][] = [
      INSTANCE_SUMMARY,
      {
        ...INSTANCE_SUMMARY,
        addr: 'standby.example.test',
        instance_id: STANDBY_ID,
        role: 'standby',
      },
    ]
    const { container } = renderWithProviders(
      <TopologyGraph
        instances={instances}
        topology={[
          {
            confidence: 'high',
            from: STANDBY_ID,
            sync_state: 'async',
            to: INSTANCE_ID,
            type: 'streaming',
          },
        ]}
      />,
    )

    expect(expectCaptionedCards(container)).toEqual(['topology.graph'])
    expect(visibleCaptions('topology.graph')).toHaveLength(1)
  })

  it('CAPTION-PAGE-013 the event timeline is captioned', () => {
    const { container } = renderWithProviders(
      <EventTimeline
        events={[
          {
            cluster_id: '1',
            event_id: 1,
            instance_id: INSTANCE_ID,
            payload: { new_primary: INSTANCE_ID, old_primary: STANDBY_ID },
            ts: '2026-08-31T10:00:00Z',
            type: 'failover_detected',
          },
        ]}
      />,
    )

    expect(expectCaptionedCards(container)).toEqual(['cluster.events'])
    expect(visibleCaptions('cluster.events')).toHaveLength(1)
  })

  it('CAPTION-PAGE-014 each lag chart is captioned', () => {
    const edges: ReplicationMetricEdge[] = ['write_lag_sec', 'flush_lag_sec', 'replay_lag_sec'].map(
      (metric) => ({
        from: STANDBY_ID,
        to: INSTANCE_ID,
        metric: metric as ReplicationMetricEdge['metric'],
        series: [{ ts: '2026-08-31T10:00:00Z', value: 1 }],
      }),
    )
    const { container } = renderWithProviders(<LagSection edges={edges} />)

    expect(expectCaptionedCards(container)).toEqual(['lag.edge'])
    expect(screen.getByRole('button', { name: /^About / })).toBeInTheDocument()
    expect(visibleCaptions('lag.edge')).toHaveLength(1)
  })

  it('CARD-ALL-001 the group has no summary tile, so no card breaks the card contract', () => {
    renderWithProviders(<LagSection edges={[]} />)

    expect(screen.queryAllByRole('article')).toHaveLength(0)
  })
})
