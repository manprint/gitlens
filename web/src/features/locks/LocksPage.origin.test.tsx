import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { withOrigins, withTruncatedConnections } from '@/test/fixtures/getLocks'
import { renderWithProviders } from '@/test/render'
import { columnTexts } from '@/test/table'
import { NOW } from '@/test/time'

const mocks = vi.hoisted(() => ({
  instance: vi.fn(),
  locks: vi.fn(),
  activity: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useInstance: mocks.instance,
  useLocks: mocks.locks,
  useInstanceActivity: mocks.activity,
}))

import { ActivitySection } from './ActivitySection'
import { BlockingTree, LocksPage } from './BlockingTree'

function success<T>(data: T) {
  return {
    data,
    dataAge: 0,
    dataUpdatedAt: Date.now(),
    error: null,
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }
}

const sampledAt = new Date().toISOString()
const originResponse = { ...withOrigins, sampled_at: sampledAt }
const oldResponse = {
  sampled_at: sampledAt,
  stale: false,
  nodes: [],
  sessions: [{ pid: 42, backend_type: 'client backend', state: 'active' }],
}

function renderPage(response: object, view = '') {
  mocks.instance.mockReturnValue(success({ perm_tier: 'T2' }))
  mocks.locks.mockReturnValue(success(response))
  mocks.activity.mockReturnValue(success({ stale: false, metrics: {} }))
  return renderWithProviders(<LocksPage instanceId="instance-1" />, {
    route: `/instances/instance-1/locks${view}`,
  })
}

beforeEach(() => {
  mocks.instance.mockReset()
  mocks.locks.mockReset()
  mocks.activity.mockReset()
})

const activity = { stale: false, metrics: {} }

describe('origin sessions on the Locks page', () => {
  it('SORIGIN-UI-002 shows the origin split on the connections tile', () => {
    render(<ActivitySection connections={withOrigins.connections} now={NOW} response={activity} />)

    expect(
      within(screen.getByRole('article', { name: 'Connections used' })).getByText(
        '2 application connections · 1 pglens connection',
      ),
    ).toBeInTheDocument()
  })

  it('SORIGIN-UI-002 states the activity totals when the sample was cut', () => {
    render(
      <ActivitySection connections={withTruncatedConnections.connections} response={activity} />,
    )

    expect(
      screen.getByText('240 application connections · 4 pglens connections'),
    ).toBeInTheDocument()
  })

  it('SORIGIN-UI-006 badges the blocking tree nodes with their origin', () => {
    const nodes = [
      { pid: 10, blocked_by: [], origin: 'application', wait_duration: null },
      {
        pid: 11,
        blocked_by: [10],
        origin: 'pglens',
        wait_duration: 3,
        wait_started_at: '2026-09-01T09:59:57Z',
      },
    ]
    const { unmount } = render(
      <BlockingTree
        now={new Date('2026-09-01T10:00:00Z')}
        response={{ sampled_at: '2026-09-01T10:00:00Z', stale: false, nodes }}
      />,
    )
    const items = screen.getAllByRole('treeitem')
    expect(within(items[0]!).getAllByText('app')).not.toHaveLength(0)
    expect(within(items[0]!).getAllByText('pglens')).not.toHaveLength(0)
    unmount()

    render(
      <BlockingTree
        now={new Date('2026-09-01T10:00:00Z')}
        response={{
          sampled_at: '2026-09-01T10:00:00Z',
          stale: false,
          nodes: nodes.map((node) => ({ ...node, origin: undefined })),
        }}
      />,
    )
    expect(screen.queryByText('app')).not.toBeInTheDocument()
    expect(screen.queryByText('pglens')).not.toBeInTheDocument()
  })

  it('SORIGIN-UI-007 keeps the previous list, its buttons and a note for sessions without origin', () => {
    renderWithProviders(
      <ActivitySection
        currentTier="T2"
        instanceId="instance-1"
        now={NOW}
        response={activity}
        sessions={oldResponse.sessions}
      />,
    )

    const list = screen.getByRole('list', { name: 'Active sessions' })
    expect(within(list).getByRole('button', { name: 'Cancel query' })).toBeInTheDocument()
    expect(within(list).getByRole('button', { name: 'Terminate backend' })).toBeInTheDocument()
    expect(screen.getByText(/does not report the origin of a session/)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('SORIGIN-UI-007 never renders the previous list once a session carries an origin', () => {
    renderWithProviders(
      <ActivitySection
        currentTier="T2"
        instanceId="instance-1"
        now={NOW}
        response={activity}
        sessions={[...oldResponse.sessions, { pid: 43, origin: 'pglens' }]}
      />,
    )

    expect(screen.queryByRole('list', { name: 'Active sessions' })).not.toBeInTheDocument()
    expect(screen.queryByText(/does not report the origin/)).not.toBeInTheDocument()
    expect(screen.getByRole('table', { name: 'Application sessions' })).toBeInTheDocument()
  })

  it('SORIGIN-UI-007 keeps the old page for a response without origins', () => {
    renderPage(oldResponse)

    expect(screen.getByRole('list', { name: 'Active sessions' })).toBeInTheDocument()
    expect(screen.queryByRole('tablist')).not.toBeInTheDocument()
  })

  it('SORIGIN-UI-008 names checks.activity.by_application only when the counts are off', () => {
    render(
      <ActivitySection
        connections={withTruncatedConnections.connections}
        now={NOW}
        response={activity}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent('checks.activity.by_application')
  })

  it('SORIGIN-UI-011 shows no callout when the counts are on or the field is absent', () => {
    const { unmount } = render(
      <ActivitySection connections={withOrigins.connections} now={NOW} response={activity} />,
    )
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText(/breakdown is opt-in/i)).not.toBeInTheDocument()
    unmount()

    const absent: Partial<typeof withOrigins.connections> = { ...withOrigins.connections }
    delete absent.by_application_enabled
    render(
      <ActivitySection
        connections={absent as unknown as typeof withOrigins.connections}
        now={NOW}
        response={activity}
      />,
    )
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByText(/breakdown is opt-in/i)).not.toBeInTheDocument()
  })

  it('SORIGIN-UI-001 opens on the blocking view and switches views from the tab list', () => {
    const { router } = renderPage(originResponse)

    expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
      'Blocking',
      'Application sessions',
      'pglens sessions',
      'Activity',
    ])
    expect(screen.getByRole('tab', { name: 'Blocking' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()

    fireEvent.mouseDown(screen.getByRole('tab', { name: 'pglens sessions' }), { button: 0 })
    expect(router.state.location.search).toBe('?view=pglens')
    expect(columnTexts('pglens monitoring sessions', 1)).toEqual(['4103'])
  })

  it('SORIGIN-UI-001 shows each origin on its own view', () => {
    const { unmount } = renderPage(originResponse, '?view=application')
    expect(columnTexts('Application sessions', 1)).toEqual(['4102', '4101'])
    expect(screen.queryByRole('table', { name: 'pglens monitoring sessions' })).toBeNull()
    unmount()

    renderPage(originResponse, '?view=activity')
    expect(screen.getByRole('heading', { name: 'Connection summary' })).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Oldest cleanup blocker (xmin holder)' }),
    ).toBeInTheDocument()
  })

  it('SORIGIN-UI-001 mounts the tables and summary for an empty sample that carries connections', () => {
    renderPage({ ...originResponse, sessions: [] }, '?view=application')

    expect(screen.getByText('No application sessions at this moment')).toBeInTheDocument()
  })

  it(
    'SORIGIN-UI-010 has no accessibility violations on any view',
    { timeout: 60_000 },
    async () => {
      vi.useRealTimers()
      for (const view of ['', '?view=application', '?view=pglens', '?view=activity']) {
        const { container, unmount } = renderPage(originResponse, view)
        await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
        unmount()
      }
      const { container } = renderPage(originResponse, '?view=application')
      fireEvent.click(screen.getByRole('button', { name: 'Show details of session 4101' }))
      await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
    },
  )
})
