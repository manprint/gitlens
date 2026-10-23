import { act, fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { CatalogSection } from './CatalogSection'

const SEEN = '2026-08-28T12:00:00Z'

function trigger(overrides: Partial<Schemas['Trigger']> = {}): Schemas['Trigger'] {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    name: 'audit',
    kind: 'user',
    state: 'enabled',
    function: 'public.audit_orders',
    timing: 'AFTER',
    events: 'INSERT OR UPDATE',
    level: 'ROW',
    constraint: '',
    constraint_type: '',
    first_seen: SEEN,
    last_seen: SEEN,
    changed_at: SEEN,
    ...overrides,
  }
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function renderCatalog(triggers: Schemas['Trigger'][], truncated: string[] = []) {
  server.use(
    ok('getInstanceTriggers', {
      instance_id: INSTANCE_ID,
      triggers,
      truncated_databases: truncated,
    }),
  )
  return renderWithProviders(<CatalogSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

function tableRows() {
  return within(screen.getByRole('table')).getAllByRole('row').slice(1)
}

describe('CatalogSection', () => {
  it('UI-CAT-001 lists triggers with what they enforce and how they fire', async () => {
    renderCatalog([
      trigger(),
      trigger({
        name: 'RI_ConstraintTrigger_c_1',
        kind: 'internal',
        state: 'disabled',
        timing: 'AFTER',
        events: 'INSERT',
        constraint: 'orders_customer_fkey',
        constraint_type: 'foreign key',
      }),
    ])
    await settle()

    expect(screen.getByRole('tab', { name: /Triggers/ })).toHaveAccessibleName(
      'Triggers 1 disabled',
    )
    expect(tableRows()).toHaveLength(2)
    expect(screen.getByText('foreign key orders_customer_fkey')).toBeInTheDocument()
    expect(screen.getByText('AFTER INSERT OR UPDATE FOR EACH ROW')).toBeInTheDocument()
    expect(screen.getByText('disabled')).toHaveAttribute('data-variant', 'destructive')
  })

  it('UI-CAT-002 filters by database and to disabled triggers', async () => {
    renderCatalog([
      trigger(),
      trigger({ name: 'paused', state: 'disabled' }),
      trigger({ datname: 'crm', relname: 'leads', name: 'stamp' }),
    ])
    await settle()
    expect(tableRows()).toHaveLength(3)

    fireEvent.change(screen.getByRole('combobox', { name: 'Database' }), {
      target: { value: 'crm' },
    })
    expect(tableRows()).toHaveLength(1)
    expect(screen.getByText('stamp')).toBeInTheDocument()

    fireEvent.change(screen.getByRole('combobox', { name: 'Database' }), {
      target: { value: 'all' },
    })
    fireEvent.click(screen.getByLabelText('Disabled only'))
    expect(tableRows()).toHaveLength(1)
    expect(screen.getByText('paused')).toBeInTheDocument()
    expect(screen.getByText('disabled')).toHaveAttribute('data-variant', 'secondary')
  })

  it('UI-CAT-003 says which databases were truncated, and when none were reported', async () => {
    renderCatalog([trigger()], ['app'])
    await settle()
    expect(screen.getByText(/app has more triggers than the agent reports/)).toBeInTheDocument()

    renderCatalog([])
    await settle()
    expect(screen.getByText('No triggers reported')).toBeInTheDocument()
  })
})
