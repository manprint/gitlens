import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

vi.mock('echarts-for-react/esm/core', () => ({
  default: () => <div data-testid="mock-echarts" />,
}))

import type { TimeRangeResult } from '@/lib/timerange'
import type { ReplicationMetricEdge } from '@/lib/replication'
import { NOW } from '@/test/time'
import { renderWithProviders } from '@/test/render'

import { LagSection } from './LagSection'

const edges: ReplicationMetricEdge[] = [
  {
    from: '00000000-0000-0000-0000-000000000001',
    to: '00000000-0000-0000-0000-000000000002',
    metric: 'write_lag_sec',
    series: [{ ts: '2026-08-31T10:00:00Z', value: 1 }],
  },
  {
    from: '00000000-0000-0000-0000-000000000001',
    to: '00000000-0000-0000-0000-000000000002',
    metric: 'flush_lag_sec',
    series: [{ ts: '2026-08-31T10:00:00Z', value: 2 }],
  },
  {
    from: '00000000-0000-0000-0000-000000000001',
    to: '00000000-0000-0000-0000-000000000002',
    metric: 'replay_lag_sec',
    series: [{ ts: '2026-08-31T10:00:00Z', value: null }],
  },
]

function longRange(): TimeRangeResult {
  return {
    fallback: false,
    from: new Date(NOW.getTime() - 31 * 24 * 60 * 60 * 1_000),
    kind: 'absolute',
    label: 'Custom range',
    to: NOW,
    valid: true,
  }
}

describe('LagSection', () => {
  it('UI-CLUS-018 renders Degraded and names the 30-day raw retention', () => {
    renderWithProviders(<LagSection edges={edges} range={longRange()} />)

    expect(screen.getByRole('status')).toHaveTextContent('30-day raw retention')
  })

  it('UI-CLUS-019 renders an empty state instead of a flat zero line', () => {
    renderWithProviders(<LagSection edges={[]} />)

    expect(screen.getByRole('status')).toHaveTextContent('No replication lag data in this range')
    expect(screen.queryByTestId('mock-echarts')).not.toBeInTheDocument()
  })

  it('renders Unknown when an edge has no samples in the selected range', () => {
    renderWithProviders(
      <LagSection
        edges={[
          {
            from: edges[0]?.from ?? 'from',
            to: edges[0]?.to ?? 'to',
            metric: 'replay_lag_sec',
            series: [],
          },
        ]}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent('No data in this range')
    expect(screen.queryByTestId('mock-echarts')).not.toBeInTheDocument()
  })

  it('renders one chart per edge and explicitly reports missing samples', () => {
    renderWithProviders(<LagSection edges={edges} />)

    expect(
      screen.getByRole('heading', { name: /00000000-0000-0000-0000-000000000001/ }),
    ).toBeInTheDocument()
    expect(screen.getByRole('note')).toHaveTextContent('Gaps in this range')
    expect(screen.getByTestId('mock-echarts')).toBeInTheDocument()
  })

  it('names each end of an edge after its instance, or the reported standby name', () => {
    renderWithProviders(
      <LagSection
        edges={[
          { ...edges[0]!, from: 'reporting replica', standby_name: 'reporting replica' },
          edges[1]!,
        ]}
        instances={[
          {
            instance_id: '00000000-0000-0000-0000-000000000001',
            addr: 'standby.example.test',
            port: 5433,
            role: 'standby',
            pg_version: 160000,
            perm_tier: 'T1',
            last_seen: '2026-08-31T10:00:00Z',
            up: true,
          },
          {
            instance_id: '00000000-0000-0000-0000-000000000002',
            addr: 'primary.example.test',
            port: 5432,
            role: 'primary',
            pg_version: 160000,
            perm_tier: 'T1',
            last_seen: '2026-08-31T10:00:00Z',
            up: true,
          },
        ]}
      />,
    )

    expect(
      screen.getByRole('heading', {
        name: 'standby.example.test:5433 → primary.example.test:5432 (Unknown)',
      }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('article', {
        name: 'reporting replica → primary.example.test:5432 (Unknown)',
      }),
    ).toBeInTheDocument()
  })
})
