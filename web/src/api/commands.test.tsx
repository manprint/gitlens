import { act, fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import { useCommand, useCreateCommand } from './commands'
import { REFRESH } from './policy'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

const commandId = '00000000-0000-4000-8000-000000000002'

function settle() {
  return act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

function flushQueryNotification() {
  return act(async () => {
    await vi.runOnlyPendingTimersAsync()
  })
}

function CommandProbe({ id = commandId }: { id?: string }) {
  const query = useCommand(id)
  return <output data-testid="command-state">{query.data?.state ?? 'loading'}</output>
}

describe('command lifecycle query', () => {
  it('UI-CMD-001 polls at one second until a terminal state', async () => {
    const responses = [
      { command_id: commandId, state: 'pending' },
      { command_id: commandId, state: 'claimed' },
      { command_id: commandId, state: 'done', result: { plan: [] } },
    ]
    let calls = 0
    server.use(
      http.get('*/api/v1/commands/:id', () => {
        const response = responses[Math.min(calls, responses.length - 1)]
        calls += 1
        return HttpResponse.json(response)
      }),
    )

    renderWithProviders(<CommandProbe />)
    await settle()
    expect(screen.getByTestId('command-state')).toHaveTextContent('pending')
    expect(calls).toBe(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.command.interval - 1)
    })
    expect(calls).toBe(1)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1)
    })
    await flushQueryNotification()
    expect(screen.getByTestId('command-state')).toHaveTextContent('done')
    expect(calls).toBe(3)
    const terminalCalls = calls
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.command.interval * 3)
    })
    expect(calls).toBe(terminalCalls)
  })

  it.each(['done', 'succeeded', 'failed', 'expired', 'rejected'] as const)(
    'UI-CMD-002 stops polling on %s',
    async (state) => {
      let calls = 0
      server.use(
        http.get('*/api/v1/commands/:id', () => {
          calls += 1
          return HttpResponse.json({ command_id: commandId, state, error: 'agent reason' })
        }),
      )

      renderWithProviders(<CommandProbe />)
      await settle()
      const terminalCalls = calls
      await act(async () => {
        await vi.advanceTimersByTimeAsync(REFRESH.command.interval * 3)
      })
      expect(calls).toBe(terminalCalls)
    },
  )

  it('UI-CMD-004 expires locally after TTL plus grace when the server remains pending', async () => {
    let calls = 0
    server.use(
      http.get('*/api/v1/commands/:id', () => {
        calls += 1
        return HttpResponse.json({ command_id: commandId, state: 'pending' })
      }),
    )

    renderWithProviders(<CommandProbe />)
    await settle()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5 * 60_000 + 5_000)
    })
    await flushQueryNotification()
    expect(screen.getByTestId('command-state')).toHaveTextContent('expired')
    const expiredCalls = calls
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.command.interval * 3)
    })
    expect(calls).toBe(expiredCalls)
  })
})

function CreateProbe() {
  const mutation = useCreateCommand('instance-a')
  return (
    <>
      <button
        type="button"
        onClick={() => mutation.mutate({ kind: 'explain', args: { queryid: 42 } })}
      >
        create
      </button>
      <output data-testid="create-state">
        {mutation.commandId ?? mutation.error?.kind ?? mutation.status}
      </output>
      <output data-testid="polled-state">{mutation.command.data?.state ?? 'none'}</output>
    </>
  )
}

describe('command creation mutation', () => {
  it('hands the accepted command id to the polling hook', async () => {
    let postedBody: unknown
    server.use(
      http.post('*/api/v1/instances/:id/commands', async ({ request }) => {
        postedBody = await request.json()
        return HttpResponse.json({ command_id: commandId }, { status: 202 })
      }),
      http.get('*/api/v1/commands/:id', () =>
        HttpResponse.json({ command_id: commandId, state: 'done', result: { plan: [] } }),
      ),
    )

    renderWithProviders(<CreateProbe />)
    fireEvent.click(screen.getByRole('button', { name: 'create' }))
    await settle()
    expect(postedBody).toEqual({ kind: 'explain', args: { queryid: 42 } })
    expect(screen.getByTestId('create-state')).toHaveTextContent(commandId)
    await flushQueryNotification()
    expect(screen.getByTestId('polled-state')).toHaveTextContent('done')
  })

  it('UI-CMD-006 does not retry a failed mutation', async () => {
    let calls = 0
    server.use(
      http.post('*/api/v1/instances/:id/commands', () => {
        calls += 1
        return HttpResponse.json(
          { error: 'commands unavailable', detail: 'database pool is not configured' },
          { status: 503 },
        )
      }),
    )

    renderWithProviders(<CreateProbe />)
    fireEvent.click(screen.getByRole('button', { name: 'create' }))
    await settle()
    expect(calls).toBe(1)
    expect(screen.getByTestId('create-state')).toHaveTextContent('server')
  })
})
