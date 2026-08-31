import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { Cluster } from '@/api/types'
import { renderWithProviders } from '@/test/render'
import { CLUSTER } from '@/test/fixture-helpers'

import { ClusterCard } from './ClusterCard'

const baseCluster = CLUSTER as unknown as Cluster

type ClusterOverrides = Omit<Partial<Cluster>, 'standby_count'> & {
  standby_count?: number | undefined
}

const makeCluster = (overrides: ClusterOverrides = {}): Cluster => {
  const merged = { ...baseCluster, ...overrides }
  if ('standby_count' in overrides && overrides.standby_count === undefined) {
    delete merged.standby_count
  }
  return merged as unknown as Cluster
}

describe('ClusterCard', () => {
  it('renders the exact cluster id and Unknown for an unmeasured lag', () => {
    renderWithProviders(<ClusterCard cluster={baseCluster} />)

    expect(screen.getByText('9007199254740993')).toBeInTheDocument()
    expect(screen.getByLabelText('not measured')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /production \(9007199254740993\)/ })).toHaveAttribute(
      'href',
      '/clusters/9007199254740993',
    )
  })

  it('renders fallback values, critical health, and the identity guidance link', () => {
    renderWithProviders(
      <ClusterCard
        cluster={makeCluster({
          name: null,
          id_source: 'agent_reported',
          primary: null,
          instance_count: 0,
          health: 'critical',
          instances: [],
          standby_count: undefined,
          max_replay_lag_seconds: 0,
        })}
      />,
    )

    expect(screen.getByText('Unnamed cluster')).toBeInTheDocument()
    expect(screen.getByRole('status', { name: /Health: critical/i })).toBeInTheDocument()
    expect(screen.getAllByLabelText('not measured')).toHaveLength(2)
    expect(screen.getByText('0.00 s')).toBeInTheDocument()
    expect(screen.getByText('agent_reported')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: /monitoring role grant instructions/i }),
    ).toHaveAttribute('href', '/README.md#setting-up-the-monitoring-role')
  })

  it('renders degraded health, measured lag, and a singular firing alert', () => {
    renderWithProviders(
      <ClusterCard
        cluster={makeCluster({ health: 'degraded', max_replay_lag_seconds: 12.5 })}
        alertCount={1}
      />,
    )

    expect(screen.getByRole('status', { name: /Health: degraded/i })).toBeInTheDocument()
    expect(screen.getByText('12.5 s')).toBeInTheDocument()
    expect(screen.getByText('1 firing alert')).toBeInTheDocument()
  })

  it('pluralizes firing alerts', () => {
    renderWithProviders(<ClusterCard cluster={baseCluster} alertCount={2} />)

    expect(screen.getByText('2 firing alerts')).toBeInTheDocument()
  })
})
