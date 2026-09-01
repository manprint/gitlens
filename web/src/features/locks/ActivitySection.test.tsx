import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { NOW } from '@/test/time'

import { ActivitySection, type ActivityResponseLike } from './ActivitySection'

function metric(value: number | null, secondsAgo = 0, labels: Record<string, string> = {}) {
  return {
    ts: new Date(NOW.getTime() - secondsAgo * 1000).toISOString(),
    value,
    labels,
  }
}

function response(metrics: ActivityResponseLike['metrics']): ActivityResponseLike {
  return { stale: false, metrics }
}

describe('ActivitySection', () => {
  it('UI-LOCK-020 uses the conn.near_max 80 percent threshold', () => {
    render(
      <ActivitySection
        response={response({
          pg_connections_used: [metric(81)],
          pg_connections_limit: [metric(100)],
          pg_connections_used_ratio: [metric(0.81)],
        })}
        now={NOW}
      />,
    )

    expect(screen.getByText(/Near max_connections/)).toBeInTheDocument()
    expect(screen.getByText(/The conn.near_max advisor threshold/)).toHaveTextContent(
      'Connections above 80 percent of max_connections',
    )
  })

  it('UI-LOCK-021 renders Disabled when per-application counts are not reported', () => {
    render(<ActivitySection response={response({})} now={NOW} />)

    expect(screen.getByRole('status')).toHaveTextContent('checks.activity.by_application')
    expect(screen.getByText(/breakdown is opt-in/i)).toBeInTheDocument()
  })

  it('UI-LOCK-022 does not offer a per-user control', () => {
    render(<ActivitySection response={response({})} now={NOW} />)

    expect(screen.queryByRole('button', { name: /user/i })).not.toBeInTheDocument()
    expect(
      screen.getByText(/breakdowns are available per state and per database/i),
    ).toBeInTheDocument()
  })

  it('UI-LOCK-023 renders Unknown for null age gauges', () => {
    render(
      <ActivitySection
        response={response({
          pg_max_xact_age_seconds: [metric(null)],
          pg_max_idle_in_transaction_seconds: [metric(null)],
          pg_max_state_age_seconds: [metric(null, 0, { state: 'idle' })],
          pg_oldest_prepared_xact_seconds: [metric(null)],
        })}
        now={NOW}
      />,
    )

    expect(
      within(screen.getByRole('article', { name: 'Longest transaction' })).getByLabelText(
        'not measured',
      ),
    ).toBeInTheDocument()
    expect(
      within(screen.getByRole('article', { name: 'Longest idle-in-transaction' })).getByLabelText(
        'not measured',
      ),
    ).toBeInTheDocument()
  })

  it('UI-LOCK-024 presents deadlocks as a rate and names the log-analysis limit', () => {
    render(
      <ActivitySection
        response={response({
          pg_deadlocks_total: [metric(10, 10), metric(12)],
        })}
        now={NOW}
      />,
    )

    expect(screen.getByRole('article', { name: 'Deadlock rate' })).toHaveTextContent('0.20')
    expect(
      screen.getByText(/identifying involved statements requires PostgreSQL log analysis/i),
    ).toBeInTheDocument()
  })

  it('renders reported application counts and remains accessible', async () => {
    vi.useRealTimers()
    const { container } = render(
      <ActivitySection
        response={response({
          pg_connections_by_application: [metric(4, 0, { application_name: 'api' })],
          pg_connections_by_database: [metric(4, 0, { datname: 'app' })],
          pg_backends: [metric(4, 0, { state: 'active', wait_event_type: 'CPU' })],
        })}
        now={NOW}
      />,
    )

    expect(screen.getByRole('list', { name: 'Connections by application' })).toHaveTextContent(
      'api',
    )
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('renders active session metadata and signal availability', () => {
    renderWithProviders(
      <ActivitySection
        currentTier="T2"
        instanceId="instance-1"
        now={NOW}
        response={response({})}
        sessions={[
          {
            pid: 1234,
            state: 'active',
            datname: null,
            database: 'app',
            usename: null,
            username: 'alice',
            query: 'select 1\nfrom dual',
            backend_type: 'client backend',
          },
          {
            pid: '',
            state: true,
            datname: '',
            usename: false,
            query: null,
            backend_type: 'background worker',
          },
        ]}
      />,
    )

    expect(screen.getByRole('list', { name: 'Active sessions' })).toHaveTextContent('PID 1234')
    expect(screen.getByRole('list', { name: 'Active sessions' })).toHaveTextContent(
      'database app; user alice',
    )
    expect(screen.getByRole('list', { name: 'Active sessions' })).toHaveTextContent('select 1')
    expect(screen.getByRole('list', { name: 'Active sessions' })).toHaveTextContent(
      'Signal actions unavailable: PID — is not a client backend.',
    )
    expect(screen.getByRole('button', { name: 'Cancel query' })).toBeInTheDocument()
  })
})
