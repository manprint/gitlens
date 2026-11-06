import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { INSTANCE_ID, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'

import { CommandAudit } from './CommandAudit'

type AuditEntry = Record<string, unknown>

async function settleAuditQuery() {
  vi.useRealTimers()
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
}

function renderAudit(entries: AuditEntry[]) {
  server.use(ok('getInstanceCommandAudit', entries))
  return renderWithProviders(<CommandAudit instances={[INSTANCE_SUMMARY]} />)
}

// Scoped to the table: the State and Command filters list the same words.
function auditTable() {
  return within(screen.getByRole('table', { name: /^Command audit for / }))
}

describe('CommandAudit', () => {
  it('UI-SET-060 shows one instance at a time, picked in the URL (GAP-9)', async () => {
    server.use(ok('getInstanceCommandAudit', []))
    const others = ['b', 'c'].map((suffix, index) => ({
      ...INSTANCE_SUMMARY,
      addr: `${suffix}.example.test`,
      instance_id: `00000000-0000-0000-0000-00000000000${index + 2}`,
    }))
    const { router } = renderWithProviders(
      <CommandAudit instances={[INSTANCE_SUMMARY, ...others]} />,
    )
    const picker = screen.getByRole('combobox', { name: 'Command audit instance' })
    expect(within(picker).getAllByRole('option')).toHaveLength(3)
    await settleAuditQuery()
    expect(await screen.findAllByText('No command activity')).toHaveLength(1)
    expect(router.state.location.search).toBe('')

    fireEvent.change(picker, { target: { value: others[1]?.instance_id } })
    expect(router.state.location.search).toBe(`?auditInstance=${others[1]?.instance_id}`)
    expect(picker).toHaveValue(others[1]?.instance_id)
    fireEvent.change(picker, { target: { value: INSTANCE_ID } })
    expect(router.state.location.search).toBe('')
  })

  it('UI-SET-061 has no instance picker with one instance', () => {
    renderAudit([])
    expect(screen.queryByRole('combobox', { name: 'Command audit instance' })).toBeNull()
  })

  it('UI-SET-037 shows a queryid above 2^53 with every digit', async () => {
    // The server writes queryid as a string, since a JSON number above 2^53
    // loses its last digits when the browser parses it.
    const body =
      '[{"audit_id":1,"command_id":"00000000-0000-4000-8000-000000000001","instance_id":"' +
      INSTANCE_ID +
      '","kind":"explain","args":{"analyze":false,"queryid":"1234567890123456789"},"requested_by":"api-token from 192.0.2.10","executed_at":"2026-09-01T01:00:00Z","outcome":"ok"}]'
    server.use(
      http.get('*/api/v1/instances/:id/command-audit', () =>
        HttpResponse.text(body, { headers: { 'Content-Type': 'application/json' } }),
      ),
    )
    renderWithProviders(<CommandAudit instances={[INSTANCE_SUMMARY]} />)
    await settleAuditQuery()

    const table = await waitFor(() =>
      screen.getByRole('table', { name: 'Command audit for postgres.example.test:5432' }),
    )
    expect(within(table).getByText(/"queryid":"1234567890123456789"/)).toBeInTheDocument()
  })

  it('UI-SET-020 displays entries newest first', async () => {
    renderAudit([
      {
        audit_id: 1,
        command_id: '00000000-0000-4000-8000-000000000001',
        instance_id: INSTANCE_ID,
        kind: 'explain',
        args: { pid: 41 },
        requested_by: 'api-token from 192.0.2.10',
        executed_at: '2026-09-01T01:00:00Z',
        outcome: 'ok',
      },
      {
        audit_id: 2,
        command_id: '00000000-0000-4000-8000-000000000002',
        instance_id: INSTANCE_ID,
        kind: 'cancel',
        args: { pid: 42 },
        requested_by: 'api-token from 192.0.2.10',
        executed_at: '2026-09-01T02:00:00Z',
        outcome: 'ok',
      },
    ])
    await settleAuditQuery()

    const table = await waitFor(() =>
      screen.getByRole('table', { name: 'Command audit for postgres.example.test:5432' }),
    )
    const rows = within(table).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('cancel')
    expect(rows[2]).toHaveTextContent('explain')
  })

  it('UI-SET-021 shows the rejection reason', async () => {
    renderAudit([
      {
        audit_id: 3,
        command_id: '00000000-0000-4000-8000-000000000003',
        instance_id: INSTANCE_ID,
        kind: 'terminate',
        args: { pid: 43 },
        requested_by: 'api-token from 192.0.2.10',
        executed_at: '2026-09-01T02:00:00Z',
        outcome: 'rejected',
        detail: 'agent lacks permission',
      },
    ])
    await settleAuditQuery()

    await waitFor(() => expect(auditTable().getByText('rejected')).toBeInTheDocument())
    expect(screen.getByText('agent lacks permission')).toBeInTheDocument()
  })

  it('UI-SET-022 shows an expired command, when it expired and why', async () => {
    renderAudit([
      {
        audit_id: 4,
        command_id: '00000000-0000-4000-8000-000000000004',
        instance_id: INSTANCE_ID,
        kind: 'explain',
        args: {},
        requested_by: 'api-token from 192.0.2.10',
        executed_at: '2026-09-01T02:01:00Z',
        outcome: 'expired',
        detail: 'expired before the agent claimed it',
      },
    ])
    await settleAuditQuery()

    await waitFor(() => expect(auditTable().getByText('expired')).toBeInTheDocument())
    expect(screen.getByText('2026-09-01 02:01:00 UTC')).toBeInTheDocument()
    expect(screen.getByText('expired before the agent claimed it')).toBeInTheDocument()
  })

  it('UI-SET-023 explains that the audit records the credential, not the person', async () => {
    renderAudit([
      {
        audit_id: 5,
        command_id: '00000000-0000-4000-8000-000000000005',
        instance_id: INSTANCE_ID,
        kind: 'cancel',
        args: { pid: 44 },
        requested_by: 'ui-session 9f86d081 from 192.0.2.10',
        executed_at: '2026-09-01T02:00:00Z',
        outcome: 'requested',
      },
    ])
    await settleAuditQuery()

    expect(await screen.findByText('ui-session 9f86d081 from 192.0.2.10')).toBeInTheDocument()
    expect(auditTable().getByText('requested')).toBeInTheDocument()
    expect(screen.getByText(/Recorded:/)).toBeInTheDocument()
    expect(screen.queryByText(/Executed:/)).not.toBeInTheDocument()
    // The caption line is the visible one; the hint tooltip holds the same sentence.
    expect(
      screen.getByText(/credential that requested it/i, { ignore: '[role="tooltip"] *' }),
    ).toBeVisible()
    expect(screen.getByText(/not the person/i, { ignore: '[role="tooltip"] *' })).toBeVisible()
  })

  it('UI-SET-024 shows an empty state when no commands were recorded', async () => {
    renderAudit([])
    await settleAuditQuery()

    await waitFor(() => expect(screen.getByText('No command activity')).toBeInTheDocument())
  })

  it('UI-SET-038 reports a 200 body that is not JSON as an invalid response', async () => {
    // A proxy can answer 200 with an HTML page. The read must fail as a
    // malformed API response, not as a bare SyntaxError the alert cannot render.
    server.use(
      http.get('*/api/v1/instances/:id/command-audit', () =>
        HttpResponse.text('<html><body>Gateway</body></html>', {
          headers: { 'Content-Type': 'text/html' },
        }),
      ),
    )
    renderWithProviders(<CommandAudit instances={[INSTANCE_SUMMARY]} />)
    await settleAuditQuery()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Could not load command audit')
    expect(alert).toHaveTextContent('The server returned an invalid response')
  })
})

describe('CommandAudit table controls', () => {
  it('TBL-UI-109 command-audit pages, searches, filters and sorts the entries', async () => {
    renderAudit(
      seq(25).map((n, index) => ({
        audit_id: index + 1,
        command_id: `00000000-0000-4000-8000-0000000000${n}`,
        instance_id: INSTANCE_ID,
        kind: index < 4 ? 'cancel' : 'explain',
        args: { queryid: `q${n}` },
        requested_by: 'api-token from 192.0.2.10',
        executed_at: new Date(Date.UTC(2026, 7, 27, 0, index)).toISOString(),
        outcome: index < 2 ? 'rejected' : 'ok',
      })),
    )
    await settleAuditQuery()
    const name = `Command audit for ${INSTANCE_SUMMARY.addr}:${INSTANCE_SUMMARY.port}`
    await waitFor(() => expect(dataRows(name)).toHaveLength(20))

    // Newest first stays the default order.
    expect(columnTexts(name, 2)[0]).toContain('q25')
    searchTable(name, 'q2')
    expect(dataRows(name)).toHaveLength(6)
    searchTable(name, '')
    filterTable(name, 'Command', 'cancel')
    expect(dataRows(name)).toHaveLength(4)
    filterTable(name, 'Command', '')
    filterTable(name, 'State', 'rejected')
    expect(dataRows(name)).toHaveLength(2)
    filterTable(name, 'State', '')
    sortTable(name, 'Time', 'ascending')
    expect(columnTexts(name, 2)[0]).toContain('q01')
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(dataRows(name)).toHaveLength(5)
  })
})
