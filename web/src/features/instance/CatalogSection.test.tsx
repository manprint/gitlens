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

function renderCatalog(
  triggers: Schemas['Trigger'][],
  truncated: string[] = [],
  foreignKeys: Schemas['UnindexedForeignKey'][] = [],
  extensions: Schemas['Extension'][] = [],
  spatial: Schemas['SpatialColumn'][] = [],
) {
  server.use(
    ok('getInstanceTriggers', {
      instance_id: INSTANCE_ID,
      triggers,
      truncated_databases: truncated,
    }),
    ok('getInstanceUnindexedForeignKeys', {
      instance_id: INSTANCE_ID,
      foreign_keys: foreignKeys,
      truncated_databases: [],
    }),
    ok('getInstanceExtensions', {
      instance_id: INSTANCE_ID,
      extensions,
      truncated_databases: [],
    }),
    ok('getInstanceSpatialColumns', {
      instance_id: INSTANCE_ID,
      columns: spatial,
      truncated_databases: [],
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

  it('UI-CAT-004 lists unindexed foreign keys with the referencing table size', async () => {
    renderCatalog(
      [],
      [],
      [
        {
          datname: 'app',
          schemaname: 'public',
          relname: 'orders',
          constraint: 'orders_customer_fkey',
          columns: 'customer_id',
          referenced: 'public.customers',
          table_bytes: 52428800,
          first_seen: SEEN,
          last_seen: SEEN,
        },
      ],
    )
    await settle()

    const tab = screen.getByRole('tab', { name: /Unindexed foreign keys/ })
    expect(tab).toHaveAccessibleName('Unindexed foreign keys 1 reported')
    fireEvent.mouseDown(tab)
    fireEvent.click(tab)
    await settle()
    expect(tableRows()).toHaveLength(1)
    expect(screen.getByText('orders_customer_fkey')).toBeInTheDocument()
    expect(screen.getByText('public.customers')).toBeInTheDocument()
    expect(screen.getByText('50.0 MiB')).toBeInTheDocument()
  })

  it('UI-CAT-005 shows extension updates and PostGIS columns with their SRID and index', async () => {
    renderCatalog(
      [],
      [],
      [],
      [
        {
          datname: 'gis',
          name: 'postgis',
          schema: 'public',
          version: '3.5.2',
          default_version: '3.6.4',
          upgrade: 'available',
          first_seen: SEEN,
          last_seen: SEEN,
          changed_at: SEEN,
        },
        {
          datname: 'gis',
          name: 'plpgsql',
          schema: 'pg_catalog',
          version: '1.0',
          default_version: '1.0',
          upgrade: 'none',
          first_seen: SEEN,
          last_seen: SEEN,
          changed_at: SEEN,
        },
      ],
      [
        {
          datname: 'gis',
          schemaname: 'public',
          relname: 'parcels',
          column: 'geom',
          type: 'geometry',
          subtype: 'MultiPolygon',
          srid: 3857,
          srid_known: true,
          index_method: '',
          rows: 250000,
          first_seen: SEEN,
          last_seen: SEEN,
        },
        {
          datname: 'gis',
          schemaname: 'public',
          relname: 'legacy',
          column: 'shape',
          type: 'geometry',
          subtype: 'Polygon',
          srid: 0,
          srid_known: false,
          index_method: 'gist',
          rows: 12,
          first_seen: SEEN,
          last_seen: SEEN,
        },
      ],
    )
    await settle()

    const extensionsTab = screen.getByRole('tab', { name: /Extensions/ })
    expect(extensionsTab).toHaveAccessibleName('Extensions 1 with an update')
    fireEvent.mouseDown(extensionsTab)
    fireEvent.click(extensionsTab)
    await settle()
    expect(tableRows()).toHaveLength(2)
    expect(screen.getByText('available')).toBeInTheDocument()
    expect(screen.getByText('up to date')).toBeInTheDocument()

    const postgisTab = screen.getByRole('tab', { name: /PostGIS/ })
    expect(postgisTab).toHaveAccessibleName('PostGIS 1 unindexed')
    fireEvent.mouseDown(postgisTab)
    fireEvent.click(postgisTab)
    await settle()
    expect(screen.getByText('gis: PostGIS 3.5.2 (packages carry 3.6.4)')).toBeInTheDocument()
    expect(tableRows()).toHaveLength(2)
    expect(screen.getByText('geometry(MultiPolygon)')).toBeInTheDocument()
    expect(screen.getByText('250,000')).toBeInTheDocument()
    expect(screen.getByText('missing')).toBeInTheDocument()
    expect(screen.getByText('unknown')).toBeInTheDocument()
  })
})
