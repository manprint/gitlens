import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'
import type { LockNode } from '@/lib/locks'

import { SignalActions } from './SignalActions'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const COMMAND_ID = '22222222-2222-4222-8222-222222222222'

const node: LockNode = {
  pid: 42,
  backend_type: 'client backend',
  datname: 'app',
  usename: 'alice',
  application_name: 'web-api',
  query: 'SELECT pg_sleep(30)\n-- private second line',
}

function renderActions(overrides: Partial<React.ComponentProps<typeof SignalActions>> = {}) {
  return renderWithProviders(
    <SignalActions node={node} instanceId={INSTANCE_ID} currentTier="T2" {...overrides} />,
  )
}

function commandHandlers(response: object) {
  server.use(
    http.post('*/api/v1/instances/:id/commands', () =>
      HttpResponse.json({ command_id: COMMAND_ID }, { status: 202 }),
    ),
    http.get('*/api/v1/commands/:id', () => HttpResponse.json(response)),
  )
}

function StaleAfterOpen() {
  const [sampleStale, setSampleStale] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setSampleStale(true)}>
        Sample goes stale
      </button>
      <SignalActions
        node={node}
        instanceId={INSTANCE_ID}
        currentTier="T2"
        sampleStale={sampleStale}
      />
    </>
  )
}

function captureCommands() {
  const bodies: unknown[] = []
  server.use(
    http.post('*/api/v1/instances/:id/commands', async ({ request }) => {
      bodies.push(await request.json())
      return HttpResponse.json({ command_id: COMMAND_ID }, { status: 202 })
    }),
    http.get('*/api/v1/commands/:id', () =>
      HttpResponse.json({ command_id: COMMAND_ID, state: 'done', result: { signalled: true } }),
    ),
  )
  return bodies
}

// The sample refreshes while the confirmation is open: the same PID now
// belongs to another session.
function RefreshingSample() {
  const [refreshed, setRefreshed] = useState(false)
  const current: LockNode = refreshed
    ? {
        ...node,
        usename: 'bob',
        query: 'SELECT 2',
        backend_start: '2026-10-23T10:00:00.000001Z',
      }
    : { ...node, backend_start: '2026-10-23T09:00:00.123456Z' }
  return (
    <>
      <button type="button" onClick={() => setRefreshed(true)}>
        Sample refreshes
      </button>
      <SignalActions node={current} instanceId={INSTANCE_ID} currentTier="T2" />
    </>
  )
}

function openDialog(kind: 'cancel' | 'terminate') {
  fireEvent.click(
    screen.getByRole('button', {
      name: kind === 'cancel' ? 'Cancel query' : 'Terminate backend',
    }),
  )
  return screen.getByRole('dialog')
}

describe('SignalActions', () => {
  it('UI-LOCK-030 disables both actions below T2 with the grant named', () => {
    renderActions({ currentTier: 'T1' })

    expect(screen.getByRole('button', { name: /Cancel query/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Terminate backend/ })).toBeDisabled()
    expect(screen.getByText(/monitoring_user\.sql -v tier1=1 -v tier2=1/)).toBeInTheDocument()
  })

  it('UI-LOCK-031 names pid, database, user and application in terminate confirmation', () => {
    renderActions()
    const dialog = openDialog('terminate')

    expect(dialog).toHaveTextContent('PID 42')
    expect(dialog).toHaveTextContent('database app')
    expect(dialog).toHaveTextContent('user alice')
    expect(dialog).toHaveTextContent('application web-api')
    expect(dialog).toHaveTextContent('SELECT pg_sleep(30)')
  })

  it('UI-LOCK-032 states rollback and connection closure for terminate', () => {
    renderActions()
    expect(openDialog('terminate')).toHaveTextContent(
      "client's connection will be closed and its open transaction will be rolled back",
    )
  })

  it('UI-LOCK-033 keeps the confirm button out of default focus', () => {
    renderActions()
    const dialog = openDialog('terminate')

    expect(within(dialog).getByRole('button', { name: 'Terminate backend' })).not.toHaveFocus()
    expect(within(dialog).getByRole('button', { name: 'Keep running' })).toHaveFocus()
  })

  it('UI-LOCK-034 offers no action for a non-client backend and states why', () => {
    renderActions({ node: { ...node, backend_type: 'autovacuum worker' } })

    expect(screen.queryByRole('button', { name: 'Cancel query' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Terminate backend' })).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('not a client backend')
  })

  it('UI-LOCK-035 surfaces an agent rejection verbatim', async () => {
    const rejection = 'allow_signal is false on target'
    commandHandlers({ command_id: COMMAND_ID, error: rejection, state: 'failed' })
    renderActions()
    const dialog = openDialog('cancel')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel query' }))
    vi.useRealTimers()

    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent(rejection))
  })

  it('UI-LOCK-036 links a successful command to the command audit', async () => {
    commandHandlers({ command_id: COMMAND_ID, state: 'done', result: { signalled: true } })
    renderActions()
    const dialog = openDialog('cancel')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel query' }))
    vi.useRealTimers()

    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'View command audit' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('link', { name: 'View command audit' })).toHaveAttribute(
      'href',
      '/settings',
    )
  })

  it('UI-LOCK-037 has no bulk action', () => {
    renderActions()

    expect(screen.queryByRole('button', { name: /terminate all/i })).not.toBeInTheDocument()
  })

  it('UI-LOCK-054 says signals remain subject to allow_signal on the target', () => {
    renderActions()

    expect(
      screen.getByText('Signals remain subject to allow_signal on the target.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Terminate backend/ })).toBeEnabled()
  })

  it('keeps both confirmation dialogs accessible', async () => {
    renderActions()
    vi.useRealTimers()

    const cancelDialog = openDialog('cancel')
    expect(cancelDialog).toBeInTheDocument()
    await expectNoA11yViolations(cancelDialog)
    fireEvent.click(within(cancelDialog).getByRole('button', { name: 'Keep running' }))

    const terminateDialog = openDialog('terminate')
    expect(terminateDialog).toBeInTheDocument()
    await expectNoA11yViolations(terminateDialog)
  })

  it('UI-LOCK-047 disables both actions while the lock sample is stale', () => {
    renderActions({ sampleStale: true })

    expect(screen.getByRole('button', { name: /Cancel query/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Terminate backend/ })).toBeDisabled()
    expect(screen.getByText(/disabled until a fresh lock sample arrives/)).toBeInTheDocument()
  })

  it('UI-LOCK-048 a confirmation opened on a fresh sample cannot submit once it goes stale', () => {
    renderWithProviders(<StaleAfterOpen />)
    openDialog('terminate')

    fireEvent.click(screen.getByText('Sample goes stale'))

    expect(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Terminate backend' }),
    ).toBeDisabled()
  })

  it('UI-LOCK-061 sends the start time the operator saw, unchanged to the microsecond', async () => {
    const bodies = captureCommands()
    renderActions({ node: { ...node, backend_start: '2026-10-23T09:00:00.123456Z' } })
    const dialog = openDialog('cancel')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel query' }))
    vi.useRealTimers()

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({
      kind: 'cancel',
      args: { pid: 42, backend_start: '2026-10-23T09:00:00.123456Z' },
    })
  })

  it('UI-LOCK-062 sends the PID alone when the sample carries no start time', async () => {
    const bodies = captureCommands()
    renderActions()
    const dialog = openDialog('terminate')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Terminate backend' }))
    vi.useRealTimers()

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ kind: 'terminate', args: { pid: 42 } })
  })

  it('UI-LOCK-063 confirms the session the dialog was opened for when the sample refreshes', async () => {
    const bodies = captureCommands()
    renderWithProviders(<RefreshingSample />)
    const dialog = openDialog('terminate')
    expect(dialog).toHaveTextContent('user alice')

    fireEvent.click(screen.getByText('Sample refreshes'))
    expect(within(dialog).getByText(/user alice/)).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Terminate backend' }))
    vi.useRealTimers()

    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({
      kind: 'terminate',
      args: { pid: 42, backend_start: '2026-10-23T09:00:00.123456Z' },
    })
  })
})
