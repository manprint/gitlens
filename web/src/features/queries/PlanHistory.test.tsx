import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import PlanHistory from './PlanHistory'
import { dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const CAPTURES = 'Plan history entries'

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

// total_shapes counts distinct plan hashes, as the server does: plain and ANALYZE
// captures of one shape share a hash.
function plansHandler(plans: { plan_hash: string }[]) {
  const total_shapes = new Set(plans.map((plan) => plan.plan_hash)).size
  return http.get('*/api/v1/plans', () => HttpResponse.json({ plans, total_shapes }))
}

function selectCaptures(...indexes: number[]) {
  const boxes = screen.getAllByRole('checkbox', { name: /Select plan captured/ })
  for (const index of indexes) fireEvent.click(boxes[index]!)
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

  it('UI-QRY-031 lists captures newest first with their shape and counts the shapes', async () => {
    vi.useRealTimers()
    const changed = { ...plan(2, '2024-01-02T00:00:00Z', 'hash-new', 'Index Scan'), changed: true }
    server.use(plansHandler([plan(1, '2024-01-01T00:00:00Z', 'hash-old'), changed]))

    renderHistory()

    await waitFor(() => expect(dataRows(CAPTURES)).toHaveLength(2))
    const rows = dataRows(CAPTURES)
    expect(rows[0]).toHaveTextContent('hash-new')
    expect(rows[0]).toHaveTextContent('New shape')
    expect(rows[0]).toHaveTextContent('Index Scan')
    expect(rows[1]).toHaveTextContent('hash-old')
    expect(rows[1]).not.toHaveTextContent('New shape')
    expect(screen.getByText(/2 captures · 2 distinct shapes/)).toBeInTheDocument()
  })

  it('UI-QRY-032 compares two captures older to newer and marks the changed operation', async () => {
    vi.useRealTimers()
    server.use(
      plansHandler([
        plan(1, '2024-01-01T00:00:00Z', 'hash-old', 'Seq Scan', 10),
        plan(2, '2024-01-02T00:00:00Z', 'hash-new', 'Index Scan', 12),
      ]),
    )

    const { container } = renderHistory()
    await waitFor(() => expect(dataRows(CAPTURES)).toHaveLength(2))
    selectCaptures(0, 1)

    const comparison = screen.getByRole('region', { name: 'Plan comparison' })
    expect(within(comparison).getByRole('note')).toHaveTextContent('Different shape: 1 operation')
    expect(within(comparison).getByLabelText('Compared captures')).toHaveTextContent(
      /Older.*shape hash-old.*Newer.*shape hash-new/,
    )
    expect(screen.getAllByTestId('plan-diff')).toHaveLength(1)
    const row = dataRows('Plan comparison')[0]!
    expect(row).toHaveTextContent('Seq Scan')
    expect(row).toHaveTextContent('Index Scan')
    expect(row).toHaveTextContent('10.00 → 12.00')
    expect(row).toHaveTextContent('+20%')
    await expectNoA11yViolations(container)
  })

  it('UI-QRY-033 reads a single selected capture and asks for two to compare', async () => {
    vi.useRealTimers()
    server.use(plansHandler([plan(1, '2024-01-01T00:00:00Z', 'hash-only')]))

    renderHistory()

    expect(await screen.findByText(/or two to compare them/)).toBeInTheDocument()
    selectCaptures(0)
    expect(screen.getByRole('table', { name: 'Query plan tree' })).toHaveTextContent('Seq Scan')
    expect(screen.queryByRole('region', { name: 'Plan comparison' })).not.toBeInTheDocument()
  })

  it('UI-QRY-034 keeps the two newest selections when a third capture is ticked', async () => {
    vi.useRealTimers()
    server.use(
      plansHandler([
        plan(1, '2024-01-01T00:00:00Z', 'hash-a'),
        plan(2, '2024-01-02T00:00:00Z', 'hash-b'),
        plan(3, '2024-01-03T00:00:00Z', 'hash-c'),
      ]),
    )

    renderHistory()
    await waitFor(() => expect(dataRows(CAPTURES)).toHaveLength(3))
    selectCaptures(0, 1, 2)

    const boxes = screen.getAllByRole('checkbox', { name: /Select plan captured/ })
    expect(boxes.map((box) => (box as HTMLInputElement).checked)).toEqual([false, true, true])
    expect(screen.getByLabelText('Compared captures')).toHaveTextContent(
      /Older.*shape hash-a.*Newer.*shape hash-b/,
    )
  })
})

describe('PlanHistory comparison table controls', () => {
  it('TBL-UI-108 plan-history pages, searches, filters and sorts the compared operations', async () => {
    vi.useRealTimers()
    // A root with 24 child scans: 25 operations; the newer plan differs in the first three.
    const tree = (differing: boolean) => ({
      'Node Type': 'Append',
      'Total Cost': 100,
      Plans: seq(24).map((n, index) => ({
        'Node Type': differing && index < 3 ? `Index Scan ${n}` : `Seq Scan ${n}`,
        'Total Cost': index + 1,
      })),
    })
    const older = { ...plan(1, '2024-01-01T00:00:00Z', 'hash-old'), plan: tree(false) }
    const newer = { ...plan(2, '2024-01-02T00:00:00Z', 'hash-new'), plan: tree(true) }
    server.use(plansHandler([older, newer]))
    renderHistory()
    await waitFor(() => expect(dataRows(CAPTURES)).toHaveLength(2))
    selectCaptures(0, 1)
    const name = 'Plan comparison'
    const operations = () =>
      dataRows(name).map((row) => within(row).getByRole('rowheader').textContent)

    expect(dataRows(name)).toHaveLength(20)
    filterTable(name, 'Difference', 'differs')
    expect(dataRows(name)).toHaveLength(3)
    filterTable(name, 'Difference', '')
    searchTable(name, 'Scan 2')
    expect(dataRows(name)).toHaveLength(5)
    searchTable(name, '')
    sortTable(name, 'Total cost', 'descending')
    expect(operations()[0]).toBe('Append')
    expect(operations()[1]).toBe('Seq Scan 24')
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(dataRows(name)).toHaveLength(5)
  })
})
