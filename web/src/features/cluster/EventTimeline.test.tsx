import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderWithProviders } from '@/test/render'

import { EventTimeline } from './EventTimeline'

const ids = {
  newPrimary: '22222222-2222-2222-2222-222222222222',
  oldPrimary: '11111111-1111-1111-1111-111111111111',
}

describe('EventTimeline', () => {
  it('UI-CLUS-031 failover_detected renders old and new primary as instance links', () => {
    renderWithProviders(
      <EventTimeline
        events={[
          {
            cluster_id: '1',
            event_id: 1,
            instance_id: ids.newPrimary,
            payload: { new_primary: ids.newPrimary, old_primary: ids.oldPrimary },
            ts: '2026-08-31T10:00:00Z',
            type: 'failover_detected',
          },
        ]}
      />,
    )

    expect(screen.getByRole('link', { name: ids.oldPrimary })).toHaveAttribute(
      'href',
      `/instances/${ids.oldPrimary}`,
    )
    expect(screen.getByRole('link', { name: ids.newPrimary })).toHaveAttribute(
      'href',
      `/instances/${ids.newPrimary}`,
    )
  })

  it('UI-CLUS-032 cluster_id_changed is rendered as a critical invariant violation', () => {
    renderWithProviders(
      <EventTimeline
        events={[
          {
            cluster_id: '1',
            event_id: 2,
            instance_id: null,
            payload: {},
            ts: '2026-08-31T10:00:00Z',
            type: 'cluster_id_changed',
          },
        ]}
      />,
    )

    expect(screen.getByRole('alert')).toHaveTextContent('invariant violation I-1')
    expect(screen.getByRole('article')).toHaveAttribute('data-severity', 'critical')
  })

  it('UI-CLUS-033 an unrecognised type renders its raw string rather than being dropped', () => {
    renderWithProviders(
      <EventTimeline
        events={[
          {
            cluster_id: '1',
            event_id: 3,
            instance_id: null,
            payload: {},
            ts: '2026-08-31T10:00:00Z',
            type: 'future_event',
          },
        ]}
      />,
    )

    const article = screen.getByRole('article')
    expect(within(article).getByText('future_event')).toBeInTheDocument()
    expect(article).toHaveAttribute('data-severity', 'info')
  })

  it('UI-CLUS-034 the type filter round-trips through the URL', () => {
    const { router } = renderWithProviders(
      <EventTimeline
        events={[
          {
            cluster_id: '1',
            event_id: 4,
            instance_id: null,
            payload: {},
            ts: '2026-08-31T10:00:00Z',
            type: 'agent_up',
          },
          {
            cluster_id: '1',
            event_id: 5,
            instance_id: null,
            payload: {},
            ts: '2026-08-31T09:00:00Z',
            type: 'agent_down',
          },
        ]}
      />,
      { route: '/clusters/1?type=agent_down' },
    )

    expect(screen.getByText('Agent down')).toBeInTheDocument()
    expect(screen.queryByText('Agent recovered')).not.toBeInTheDocument()

    fireEvent.change(screen.getByRole('combobox', { name: 'Filter events by type' }), {
      target: { value: 'agent_up' },
    })
    expect(router.state.location.search).toBe('?type=agent_up')
    expect(screen.getByText('Agent recovered')).toBeInTheDocument()
  })

  it('UI-CLUS-036 describes every event type the server emits instead of the unknown fallback', () => {
    // The literals the server writes to the events table: staleness.go,
    // inventory.go and pipeline.go.
    const emitted = [
      'instance_unreachable',
      'instance_reachable',
      'cardinality_truncated',
      'cluster_identity_conflict',
    ]
    renderWithProviders(
      <EventTimeline
        events={emitted.map((type, index) => ({
          cluster_id: '1',
          event_id: index + 1,
          instance_id: null,
          payload: {},
          ts: '2026-08-31T10:00:00Z',
          type,
        }))}
      />,
    )

    expect(screen.queryByText('Unknown event type')).not.toBeInTheDocument()
    expect(screen.getByText('Instance unreachable')).toBeInTheDocument()
    expect(screen.getByText('Instance reachable')).toBeInTheDocument()
    expect(screen.getByText('Series truncated by cardinality budget')).toBeInTheDocument()
    const identityConflict = screen
      .getAllByRole('article')
      .find((article) => within(article).queryByText('Cluster identity conflict') !== null)
    expect(identityConflict).toHaveAttribute('data-severity', 'warning')
  })
})
