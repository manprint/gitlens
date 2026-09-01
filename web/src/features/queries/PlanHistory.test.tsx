import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import PlanHistory from './PlanHistory'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'

function plan(
  planId: number,
  capturedAt: string,
  hash: string,
  nodeType = 'Seq Scan',
  totalCost = 10,
  analyzed = false,
) {
  return {
    plan_id: planId,
    plan_hash: hash,
    captured_at: capturedAt,
    analyzed,
    plan: { 'Node Type': nodeType, 'Total Cost': totalCost },
    changed: false,
  }
}

function renderHistory() {
  return renderWithProviders(
    <PlanHistory instanceId={INSTANCE_ID} queryid="9223372036854775807" />,
    { route: `/instances/${INSTANCE_ID}/queries/9223372036854775807` },
  )
}

function plansHandler(plans: object[]) {
  return http.get('*/api/v1/plans', () => HttpResponse.json({ plans, total_shapes: plans.length }))
}

describe('PlanHistory', () => {
  it('UI-QRY-030 explains that an empty history is expected', async () => {
    vi.useRealTimers()
    server.use(plansHandler([]))

    const { container } = renderHistory()

    expect(await screen.findByText(/No plan history yet/)).toBeInTheDocument()
    expect(screen.getByText(/does not sample plans automatically/)).toBeInTheDocument()
    await expectNoA11yViolations(container)
  })

  it('UI-QRY-031 renders entries newest first', async () => {
    vi.useRealTimers()
    server.use(
      plansHandler([
        plan(1, '2024-01-01T00:00:00Z', 'hash-old'),
        plan(2, '2024-01-02T00:00:00Z', 'hash-new'),
      ]),
    )

    renderHistory()

    await waitFor(() =>
      expect(screen.getByRole('button', { name: /hash-new/ })).toBeInTheDocument(),
    )
    const entries = screen.getAllByRole('button', { name: /hash-/ })
    expect(entries[0]).toHaveTextContent('hash-new')
    expect(entries[1]).toHaveTextContent('hash-old')
  })

  it('UI-QRY-032 highlights differing nodes and costs in a comparison', async () => {
    vi.useRealTimers()
    server.use(
      plansHandler([
        plan(1, '2024-01-01T00:00:00Z', 'hash-old', 'Seq Scan', 10),
        plan(2, '2024-01-02T00:00:00Z', 'hash-new', 'Index Scan', 12),
      ]),
    )

    renderHistory()

    await waitFor(() =>
      expect(screen.getByRole('button', { name: /hash-new/ })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('button', { name: /hash-old/ }))
    fireEvent.click(screen.getByRole('button', { name: /hash-new/ }))

    expect(screen.getByRole('heading', { name: 'Plan comparison' })).toBeInTheDocument()
    expect(screen.getAllByTestId('plan-diff')).toHaveLength(2)
    expect(screen.getByText(/Seq Scan · cost 10/)).toBeInTheDocument()
    expect(screen.getByText(/Index Scan · cost 12/)).toBeInTheDocument()
  })

  it('UI-QRY-033 explains why comparison is unavailable with fewer than two entries', async () => {
    vi.useRealTimers()
    server.use(plansHandler([plan(1, '2024-01-01T00:00:00Z', 'hash-only')]))

    renderHistory()

    expect(await screen.findByText(/Select two plan entries/)).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Plan comparison' })).not.toBeInTheDocument()
  })
})
