import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
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
  it('CLNAME-UI-007 shows the cluster name, else Unnamed cluster', () => {
    const { unmount } = renderWithProviders(
      <ClusterCard cluster={makeCluster({ name: 'odoo-prod' })} />,
    )
    const named = screen.getByRole('link', { name: 'odoo-prod (9007199254740993)' })
    expect(named).toHaveTextContent('odoo-prod')
    expect(named).toHaveAttribute('title', 'odoo-prod')
    unmount()

    renderWithProviders(<ClusterCard cluster={makeCluster({ name: null })} />)
    expect(
      screen.getByRole('link', { name: 'Unnamed cluster (9007199254740993)' }),
    ).toHaveTextContent('Unnamed cluster')
  })

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

  it('RDY-UI-011 no readiness prop, no badge; with one, the badge', () => {
    const first = renderWithProviders(<ClusterCard cluster={makeCluster()} />)
    expect(screen.queryByTestId('readiness-badge')).toBeNull()
    first.unmount()
    renderWithProviders(
      <ClusterCard
        cluster={makeCluster()}
        readiness={{ level: 'partial', warnings: 2, infos: 0 }}
      />,
    )
    expect(screen.getByTestId('readiness-badge')).toHaveTextContent('Partial · 2 warnings')
  })

  it('RDY-UI-009 a card with the readiness badge has no accessibility violations', async () => {
    // The card is a list item: it needs its list, as the fleet page gives it.
    const view = renderWithProviders(
      <div role="list">
        <ClusterCard
          cluster={makeCluster()}
          readiness={{ level: 'partial', warnings: 2, infos: 1 }}
        />
      </div>,
    )
    expect(screen.getByRole('status', { name: /^Monitoring: partial/ })).toBeInTheDocument()
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })
})
