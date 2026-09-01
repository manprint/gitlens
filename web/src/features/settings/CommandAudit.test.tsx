import { act, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { INSTANCE_ID, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'

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

describe('CommandAudit', () => {
  it('UI-SET-020 displays entries newest first', async () => {
    renderAudit([
      {
        audit_id: 1,
        command_id: '00000000-0000-4000-8000-000000000001',
        instance_id: INSTANCE_ID,
        kind: 'explain',
        args: { pid: 41 },
        executed_at: '2026-09-01T01:00:00Z',
        outcome: 'ok',
      },
      {
        audit_id: 2,
        command_id: '00000000-0000-4000-8000-000000000002',
        instance_id: INSTANCE_ID,
        kind: 'cancel',
        args: { pid: 42 },
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
        executed_at: '2026-09-01T02:00:00Z',
        outcome: 'rejected',
        detail: 'agent lacks permission',
      },
    ])
    await settleAuditQuery()

    await waitFor(() => expect(screen.getByText('rejected')).toBeInTheDocument())
    expect(screen.getByText('agent lacks permission')).toBeInTheDocument()
  })

  it('UI-SET-022 shows an expired command and its expiry time', async () => {
    renderAudit([
      {
        audit_id: 4,
        command_id: '00000000-0000-4000-8000-000000000004',
        instance_id: INSTANCE_ID,
        kind: 'explain',
        args: {},
        state: 'expired',
        executed_at: '2026-09-01T02:00:00Z',
        expires_at: '2026-09-01T02:01:00Z',
      },
    ])
    await settleAuditQuery()

    await waitFor(() => expect(screen.getByText('expired')).toBeInTheDocument())
    expect(screen.getByText('2026-09-01 02:01:00 UTC')).toBeInTheDocument()
  })

  it('UI-SET-023 explains that the audit records actions, not identities', async () => {
    renderAudit([
      {
        audit_id: 5,
        command_id: '00000000-0000-4000-8000-000000000005',
        instance_id: INSTANCE_ID,
        kind: 'cancel',
        outcome: 'ok',
      },
    ])
    await settleAuditQuery()

    expect(screen.getByText(/records the action, not the person/i)).toBeInTheDocument()
  })

  it('UI-SET-024 shows an empty state when no commands were recorded', async () => {
    renderAudit([])
    await settleAuditQuery()

    await waitFor(() => expect(screen.getByText('No command activity')).toBeInTheDocument())
  })
})
