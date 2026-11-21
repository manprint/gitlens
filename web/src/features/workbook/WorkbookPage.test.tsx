import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse, delay } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Workbook, WorkbookVariant } from '@/api/workbooks'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { VariantEditor } from './VariantEditor'
import { WorkbookView } from './WorkbookPage'

const PLAN_A = [{ Plan: { 'Node Type': 'Seq Scan', 'Total Cost': 200 }, 'Planning Time': 2 }]
const PLAN_B = [{ Plan: { 'Node Type': 'Index Scan', 'Total Cost': 50 }, 'Planning Time': 1 }]

function variant(patch: Partial<WorkbookVariant> & { name: string }): WorkbookVariant {
  return {
    variant_id: `v-${patch.name}`,
    position: 0,
    settings: {},
    hints: '',
    params: [],
    analyze: false,
    status: 'never',
    command_id: null,
    error: null,
    plan_hash: null,
    total_cost: null,
    planning_ms: null,
    execution_ms: null,
    ran_at: null,
    ...patch,
  } as WorkbookVariant
}

const WORKBOOK: Workbook = {
  workbook_id: 'w-1',
  instance_id: '11111111-1111-4111-8111-111111111111',
  database: 'app',
  queryid: '42',
  name: 'Tuning 42',
  notes: '',
  created_at: '2026-11-21T10:00:00Z',
  updated_at: '2026-11-21T10:00:00Z',
  variants: [
    variant({
      name: 'Baseline',
      status: 'done',
      total_cost: 200,
      planning_ms: 2,
      plan_hash: 'aaaaaaaaaaaaaaaa',
      plan: PLAN_A,
    }),
    variant({
      name: 'No seqscan',
      position: 1,
      settings: { enable_seqscan: 'off' },
      params: ['7', null],
      status: 'done',
      total_cost: 50,
      planning_ms: 1,
      plan_hash: 'bbbbbbbbbbbbbbbb',
      plan: PLAN_B,
    }),
    variant({
      name: 'Hinted',
      position: 2,
      hints: 'SeqScan(t)',
      status: 'failed',
      error: 'hint_plan_inactive: pg_hint_plan is not loaded',
    }),
  ],
}

function renderView(workbook = WORKBOOK) {
  return renderWithProviders(<WorkbookView tier="T2" workbook={workbook} onDeleted={vi.fn()} />, {
    route: '/instances/x/queries/42/workbooks/w-1',
  })
}

function rowOf(name: string, tableName = 'Variants') {
  const table = screen.getByRole('table', { name: tableName })
  return within(table)
    .getByRole('rowheader', { name: new RegExp(name) })
    .closest('tr')!
}

describe('Tuning workbook', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-WBK-001 the variants table shows changed settings, parameters and ratios to Baseline', () => {
    renderView()
    const row = rowOf('No seqscan')
    expect(within(row).getByText('enable_seqscan=off')).toBeInTheDocument()
    expect(within(row).getByText('2 values')).toBeInTheDocument()
    expect(within(row).getByText('0.25×')).toBeInTheDocument()
    expect(within(row).getByText('0.5×')).toBeInTheDocument()
    expect(within(rowOf('Baseline')).getByText('none')).toBeInTheDocument()
    fireEvent.click(within(row).getByRole('button', { name: 'show' }))
    expect(within(row).getByText('$2 = NULL')).toBeInTheDocument()
    expect(
      screen.getByText(/Every run is an EXPLAIN in a read-only transaction/),
    ).toBeInTheDocument()
  })

  it('UI-WBK-002 the editor refuses a hint with */ and an out-of-range work_mem before sending', () => {
    const onSave = vi.fn()
    renderWithProviders(
      <VariantEditor
        canAnalyze
        editing={false}
        initial={{ name: 'Big memory', settings: {}, hints: '', params: [], analyze: false }}
        saving={false}
        onCancel={vi.fn()}
        onSave={onSave}
      />,
    )
    fireEvent.change(screen.getByRole('combobox', { name: 'Add a setting' }), {
      target: { value: 'work_mem' },
    })
    fireEvent.change(screen.getByRole('spinbutton', { name: 'work_mem' }), {
      target: { value: '999999999' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: /pg_hint_plan hints/ }), {
      target: { value: 'SeqScan(t) */' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save variant' }))
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('work_mem must be a number from 64 to 262144 kB.')
    expect(alert).toHaveTextContent('Hints may use only:')
    expect(onSave).not.toHaveBeenCalled()

    fireEvent.change(screen.getByRole('spinbutton', { name: 'work_mem' }), {
      target: { value: '65536' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: /pg_hint_plan hints/ }), {
      target: { value: 'SeqScan(t)' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save variant' }))
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({ settings: { work_mem: '65536' }, hints: 'SeqScan(t)' }),
    )
  })

  it('UI-WBK-003 Run all enqueues the variants one after the other', async () => {
    const order: string[] = []
    let inFlight = 0
    let most = 0
    server.use(
      http.post('*/api/v1/workbooks/:id/variants/:variantId/run', async ({ params }) => {
        inFlight += 1
        most = Math.max(most, inFlight)
        order.push(String(params.variantId))
        await delay(20)
        inFlight -= 1
        return HttpResponse.json({ command_id: `c-${String(params.variantId)}` }, { status: 202 })
      }),
      http.get('*/api/v1/workbooks', () => HttpResponse.json({ workbooks: [] })),
      http.get('*/api/v1/workbooks/:id', () => HttpResponse.json(WORKBOOK)),
    )
    renderView()
    fireEvent.click(screen.getByRole('button', { name: 'Run all' }))
    await waitFor(() => expect(order).toHaveLength(3))
    expect(order).toEqual(['v-Baseline', 'v-No seqscan', 'v-Hinted'])
    expect(most).toBe(1)
  })

  it('UI-WBK-004 a missing pg_hint_plan shows the hint_plan sentence', () => {
    renderView()
    expect(within(rowOf('Hinted')).getByRole('alert')).toHaveTextContent(
      'pg_hint_plan is not active on this server. It must be in shared_preload_libraries or session_preload_libraries; pglens never loads it.',
    )
  })

  it('UI-WBK-005 ticking two done variants opens the comparison', () => {
    renderView()
    expect(screen.getByRole('checkbox', { name: 'Compare Hinted' })).toBeDisabled()
    expect(screen.queryByRole('region', { name: 'Variant comparison' })).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Compare Baseline' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Compare No seqscan' }))
    const compare = screen.getByRole('region', { name: 'Variant comparison' })
    expect(within(compare).getByText('First')).toBeInTheDocument()
    expect(within(compare).getByText('Second')).toBeInTheDocument()
    expect(within(compare).getAllByText(/Index Scan/).length).toBeGreaterThan(0)
    fireEvent.click(within(compare).getByRole('button', { name: 'Close comparison' }))
    expect(screen.queryByRole('region', { name: 'Variant comparison' })).toBeNull()
  })

  it('UI-WBK-006 the workbook reads in Italian; setting names stay English', async () => {
    await setServerLanguage('it')
    renderView()
    const table = await screen.findByRole('table', { name: 'Varianti' })
    expect(within(table).getByRole('columnheader', { name: /Impostazioni/ })).toBeInTheDocument()
    expect(within(table).getByText('enable_seqscan=off')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Esegui tutte' })).toBeInTheDocument()
    expect(within(rowOf('Hinted', 'Varianti')).getByRole('alert')).toHaveTextContent(
      'pg_hint_plan non è attivo su questo server',
    )
  })
})
