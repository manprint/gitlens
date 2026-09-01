import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import ExplainPanel from './ExplainPanel'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const COMMAND_ID = '22222222-2222-4222-8222-222222222222'

function renderPanel(props: Partial<React.ComponentProps<typeof ExplainPanel>> = {}) {
  return renderWithProviders(
    <ExplainPanel
      currentTier="T2"
      datname="app"
      instanceId={INSTANCE_ID}
      queryid={123}
      {...props}
    />,
    { route: `/instances/${INSTANCE_ID}/queries/123` },
  )
}

function commandHandlers(response: object) {
  server.use(
    http.post('*/api/v1/instances/:id/commands', () =>
      HttpResponse.json({ command_id: COMMAND_ID }),
    ),
    http.get('*/api/v1/commands/:id', () => HttpResponse.json(response)),
  )
}

describe('ExplainPanel', () => {
  it('UI-QRY-020 disables plan-only below T1 and names the required tier', () => {
    renderPanel({ currentTier: 'T0' })

    const button = screen.getByRole('button', { name: /Run plan only/i })
    expect(button).toBeDisabled()
    expect(screen.getByText('Requires T1 permission; current tier is T0.')).toBeInTheDocument()
  })

  it('UI-QRY-021 explains rollback, resource and lock impact before ANALYZE', () => {
    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: 'Plan with ANALYZE' }))

    expect(screen.getByRole('dialog')).toHaveTextContent('executes the statement')
    expect(screen.getByRole('dialog')).toHaveTextContent('rolled back')
    expect(screen.getByRole('dialog')).toHaveTextContent('consumes database resources')
    expect(screen.getByRole('dialog')).toHaveTextContent('can take locks')
  })

  it('UI-QRY-022 does not put the ANALYZE confirmation button in default focus', () => {
    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: 'Plan with ANALYZE' }))

    expect(screen.getByRole('button', { name: 'Run EXPLAIN ANALYZE' })).not.toHaveFocus()
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
  })

  it('UI-QRY-023 sends queryid, datname and options without query text', async () => {
    let requestBody: unknown
    server.use(
      http.post('*/api/v1/instances/:id/commands', async ({ request }) => {
        requestBody = await request.json()
        return HttpResponse.json({ command_id: COMMAND_ID })
      }),
    )

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /Run plan only/i }))
    vi.useRealTimers()

    await waitFor(() => expect(requestBody).toBeDefined())
    expect(requestBody).toEqual({
      args: { analyze: false, datname: 'app', queryid: '123' },
      kind: 'explain',
    })
    expect(JSON.stringify(requestBody)).not.toContain('query_text')
  })

  it('UI-QRY-024 surfaces an agent rejection verbatim', async () => {
    const rejection = 'allow_explain_analyze is false on target'
    commandHandlers({
      command_id: COMMAND_ID,
      error: rejection,
      state: 'failed',
    })

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: 'Plan with ANALYZE' }))
    fireEvent.click(screen.getByRole('button', { name: 'Run EXPLAIN ANALYZE' }))
    vi.useRealTimers()

    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent(rejection))
  })

  it('UI-QRY-025 renders plan node types, estimated and actual rows, and costs', async () => {
    commandHandlers({
      command_id: COMMAND_ID,
      result: [
        {
          Plan: {
            'Actual Rows': 4,
            'Node Type': 'Nested Loop',
            'Plan Rows': 8,
            Plans: [{ 'Node Type': 'Index Scan', 'Total Cost': 12.5 }],
            'Total Cost': 34.5,
          },
        },
      ],
      state: 'done',
    })

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /Run plan only/i }))
    vi.useRealTimers()

    await waitFor(() => expect(screen.getByText('Nested Loop')).toBeInTheDocument())
    expect(screen.getByText('Index Scan')).toBeInTheDocument()
    expect(screen.getByText('Estimated rows')).toBeInTheDocument()
    expect(screen.getByText('Actual rows')).toBeInTheDocument()
    expect(screen.getAllByText('34.5').length).toBeGreaterThan(0)
    expect(screen.getByText('Raw JSON')).toBeInTheDocument()
  })

  it('UI-QRY-026 warns that normalised placeholders can change the plan', () => {
    renderPanel({ normalized: true })

    expect(screen.getByRole('note')).toHaveTextContent('normalised with placeholders')
    expect(screen.getByRole('note')).toHaveTextContent('parameter-sensitive')
  })

  it('gates ANALYZE at T2 and preserves the disabled control', () => {
    renderPanel({ currentTier: 'T1' })

    const button = screen.getByRole('button', { name: /Plan with ANALYZE/ })
    expect(button).toBeDisabled()
    expect(screen.getByText('Requires T2 permission; current tier is T1.')).toBeInTheDocument()
  })

  it('UI-QRY-045 renders the server detail for an unprocessable EXPLAIN request', async () => {
    server.use(
      http.post('*/api/v1/instances/:id/commands', () =>
        HttpResponse.json(
          { error: 'invalid command', detail: 'query was evicted before execution' },
          { status: 422 },
        ),
      ),
    )

    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /Run plan only/i }))
    vi.useRealTimers()

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByRole('alert')).toHaveTextContent('invalid command: query was evicted')
  })

  it('keeps the confirmation dialog accessible', async () => {
    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: 'Plan with ANALYZE' }))

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    vi.useRealTimers()
    await expectNoA11yViolations(document.body)
  })

  it('keeps a rendered plan tree accessible', async () => {
    commandHandlers({
      command_id: COMMAND_ID,
      result: [{ Plan: { 'Node Type': 'Seq Scan', 'Total Cost': 1.2 } }],
      state: 'done',
    })

    const { container } = renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /Run plan only/i }))
    vi.useRealTimers()
    await waitFor(() => expect(screen.getByText('Seq Scan')).toBeInTheDocument())

    vi.useRealTimers()
    await expectNoA11yViolations(container)
  })
})
