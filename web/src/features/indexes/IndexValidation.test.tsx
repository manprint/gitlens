import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import type { IndexSuggestion } from '@/lib/indexSuggestions'
import { resetPreferences } from '@/lib/preferences'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { base } from '@/test/fixtures/getIndexSuggestions'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { IndexValidationSection } from './IndexValidation'

const COMMAND_ID = '22222222-2222-4222-8222-222222222222'
const ROW: IndexSuggestion = {
  ...base.suggestions[0]!,
  database: 'app',
  table: 'orders',
  columns: [
    { name: 'customer_id', desc: false },
    { name: 'created_at', desc: false },
  ],
  statements: [
    { queryid: '11', share_pct: 30, query_text: 'SELECT 1' },
    { queryid: '-42', share_pct: 70, query_text: 'SELECT 2' },
  ],
}

function renderSection(row: IndexSuggestion = ROW, tier: 'T0' | 'T1' | 'T2' = 'T1') {
  server.use(ok('getInstance', { ...makeGetInstance(), perm_tier: tier }))
  return renderWithProviders(<IndexValidationSection instanceId={INSTANCE_ID} row={row} />)
}

function commandAnswers(response: object) {
  let body: unknown
  server.use(
    http.post('*/api/v1/instances/:id/commands', async ({ request }) => {
      body = await request.json()
      return HttpResponse.json({ command_id: COMMAND_ID })
    }),
    http.get('*/api/v1/commands/:id', () =>
      HttpResponse.json({ command_id: COMMAND_ID, ...response }),
    ),
  )
  return () => body
}

async function validate() {
  const button = await screen.findByRole('button', { name: 'Validate with hypopg' })
  await waitFor(() => expect(button).toBeEnabled())
  fireEvent.click(button)
}

describe('IndexValidationSection', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-IDXV-002 enqueues index_check with the key and the heaviest statement', async () => {
    const sent = commandAnswers({ state: 'pending' })
    renderSection()
    expect(screen.getByText(/Nothing is created on the database/)).toBeInTheDocument()
    await validate()
    await waitFor(() => expect(sent()).toBeDefined())
    expect(sent()).toEqual({
      kind: 'index_check',
      args: {
        queryid: '-42',
        datname: 'app',
        index_schema: 'public',
        index_relation: 'orders',
        index_columns: ['customer_id', 'created_at'],
        index_include: [],
      },
    })
  })

  it('UI-IDXV-003 shows whether the planner would use the index, costs and size', async () => {
    commandAnswers({
      state: 'done',
      result: { cost_before: 1693, cost_after: 8.5, index_used: true, index_size_bytes: 8192 },
    })
    renderSection()
    await validate()
    expect(await screen.findByText('Planner would use it')).toBeInTheDocument()
    expect(screen.getByText('1,693.0 → 8.5 (×0.01)')).toBeInTheDocument()
    expect(screen.getByText(/^8(\.0)? KiB$/)).toBeInTheDocument()

    const stored = {
      queryid: '11',
      index_used: false,
      cost_before: 100,
      cost_after: 100,
      cost_ratio: 1,
      index_size_bytes: null,
      validated_at: '2026-11-21T10:00:00Z',
      approximate: true,
    }
    renderSection({ ...ROW, validation: stored })
    expect(await screen.findByText('Planner would not use it')).toBeInTheDocument()
    expect(screen.getByText(/hypopg costed the key in ascending order/)).toBeInTheDocument()
  })

  it('UI-IDXV-004 tells how to install hypopg in the database', async () => {
    commandAnswers({
      state: 'failed',
      error:
        'hypopg_missing: the hypopg extension is not installed in database "app"; pglens does not install it',
    })
    renderSection()
    await validate()
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Install the hypopg extension in database app to validate suggestions (CREATE EXTENSION hypopg).',
    )
  })

  it('UI-IDXV-005 keeps the button disabled when commands are off', async () => {
    renderSection(ROW, 'T0')
    expect(
      await screen.findByText('Requires T1 permission; current tier is T0.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Validate with hypopg/ })).toBeDisabled()
  })

  it('UI-IDXV-006 reads in Italian', async () => {
    await setServerLanguage('it')
    commandAnswers({
      state: 'done',
      result: { cost_before: 10, cost_after: 20, index_used: false },
    })
    renderSection()
    const button = await screen.findByRole('button', { name: 'Valida con hypopg' })
    expect(screen.getByText(/Sul database non viene creato nulla/)).toBeInTheDocument()
    await waitFor(() => expect(button).toBeEnabled())
    fireEvent.click(button)
    expect(await screen.findByText('Il planner non lo userebbe')).toBeInTheDocument()
  })
})
