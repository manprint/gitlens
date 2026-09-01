import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { qk } from '@/api/keys'
import { REFRESH } from '@/api/policy'
import type { Alert } from '@/lib/alerts'
import type { ClusterEvent } from '@/lib/events'
import { NOW } from '@/test/time'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { EventsPage } from './EventsPage'

const baseAlert: Alert = {
  alert_key: 'alert-1',
  cluster_id: '1',
  datname: 'postgres',
  instance_id: '00000000-0000-4000-8000-000000000001',
  labels: {},
  last_eval_at: '2026-09-01T04:00:00Z',
  resolved_at: null,
  rule_id: 'replication_lag',
  severity: 'warning',
  started_at: '2026-09-01T03:00:00Z',
  state: 'resolved',
  summary: 'Replication lag',
  suppressed: false,
  value: 1,
}

const baseEvent: ClusterEvent = {
  cluster_id: '1',
  event_id: 1,
  instance_id: null,
  payload: {},
  ts: '2026-09-01T04:00:00Z',
  type: 'agent_up',
}

function alert(overrides: Partial<Alert> = {}): Alert {
  return { ...baseAlert, ...overrides }
}

function event(overrides: Partial<ClusterEvent> = {}): ClusterEvent {
  return { ...baseEvent, ...overrides }
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

async function renderPage(
  route = '/events?range=1h',
  events: ClusterEvent[] = [baseEvent],
  alerts: Alert[] = [baseAlert],
) {
  server.use(ok('getEvents', events), ok('getAlerts', alerts), ok('getSilences', []))
  const view = renderWithProviders(<EventsPage />, { route })
  await settle()
  return view
}

describe('EventsPage', () => {
  it('UI-ALERT-050 renders a positive no-firing state with the last evaluation', async () => {
    await renderPage('/events?range=1h', [baseEvent], [alert()])

    expect(
      screen.getByText('No alerts firing. Last evaluated: 2026-09-01T04:00:00Z.'),
    ).toBeInTheDocument()
  })

  it('UI-ALERT-051 states that the event list is capped at the server maximum', async () => {
    const events = Array.from({ length: 1_000 }, (_, index) =>
      event({
        event_id: index + 1,
        ts: `2026-09-01T04:${String(index % 60).padStart(2, '0')}:00Z`,
      }),
    )
    await renderPage('/events?limit=1000&range=1h', events)

    expect(screen.getByRole('note')).toHaveTextContent(/capped at 1,000 results/i)
  })

  it('UI-ALERT-052 renders suppression as Unknown when silences fail', async () => {
    server.use(
      ok('getEvents', [baseEvent]),
      ok('getAlerts', [alert()]),
      status('getSilences', 500, { error: 'internal_error', detail: 'silences unavailable' }),
    )
    renderWithProviders(<EventsPage />, { route: '/events?range=1h' })
    await settle()

    expect(screen.getByText('Suppression: Unknown')).toBeInTheDocument()
    expect(screen.queryByText(/Not suppressed/i)).not.toBeInTheDocument()
  })

  it('UI-ALERT-053 marks an old event snapshot as Stale', async () => {
    const view = await renderPage()
    view.queryClient.setQueryData(
      qk.events(
        undefined,
        undefined,
        new Date(NOW.getTime() - 60 * 60 * 1_000).toISOString(),
        NOW.toISOString(),
        100,
      ),
      [baseEvent],
      { updatedAt: Date.now() - REFRESH.activity.staleAfter },
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })

    expect(screen.getByRole('status', { name: /Stale data:/i })).toBeInTheDocument()
  })

  it('UI-ALERT-054 navigates a 401 to login exactly once', async () => {
    server.use(status('getEvents', 401), ok('getAlerts', [baseAlert]), ok('getSilences', []))
    const view = renderWithProviders(<EventsPage />, { route: '/events?range=1h' })
    await settle()

    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe('?next=%2Fevents%3Frange%3D1h')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.alerts.interval * 2)
    })
    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe('?next=%2Fevents%3Frange%3D1h')
  })

  it('UI-ALERT-055 retries a server error and renders the recovered timeline', async () => {
    server.use(
      sequence(
        'getEvents',
        { status: 500, body: { error: 'internal_error', detail: 'events unavailable' } },
        [baseEvent],
      ),
      ok('getAlerts', [baseAlert]),
      ok('getSilences', []),
    )
    renderWithProviders(<EventsPage />, { route: '/events?range=1h' })
    await settle()

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load events')
    fireEvent.click(screen.getByRole('button', { name: 'Retry events' }))
    await settle()

    expect(screen.getByRole('heading', { name: 'Event timeline' })).toBeInTheDocument()
  })

  it('UI-ALERT-056 polls alerts at the alerts interval', async () => {
    server.use(
      ok('getEvents', [baseEvent]),
      sequence('getAlerts', [baseAlert], [alert({ state: 'firing', resolved_at: null })]),
      ok('getSilences', []),
    )
    renderWithProviders(<EventsPage />, { route: '/events?range=1h' })
    await settle()

    expect(screen.getByText(/No alerts firing/)).toBeInTheDocument()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.alerts.interval + 1)
    })
    await settle()

    expect(screen.getByText('Firing alerts: 1')).toBeInTheDocument()
  })
})
