import { fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import { useClusters, useInstances } from '@/api/queries'
import type { Cluster } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { assertMatchesContract } from '@/test/contract'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { base as renamedCluster, cleared as clearedCluster } from '@/test/fixtures/renameCluster'
import { base as renamedInstance } from '@/test/fixtures/renameInstance'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts } from '@/test/table'

import { nameRows } from './nameRows'
import { NamesPanel } from './NamesPanel'

const UNASSIGNED_ID = '00000000-0000-4000-8000-000000000009'
const cluster = CLUSTER as unknown as Cluster

/** The panel fed by the live queries, as Settings mounts it. */
function Harness() {
  const clusters = useClusters()
  const instances = useInstances()
  if (!clusters.data || !instances.data) return null
  return <NamesPanel clusters={clusters.data} instances={instances.data} />
}

async function renderPanel() {
  const view = renderWithProviders(<Harness />, { route: '/settings' })
  await flushNetwork()
  return view
}

const TABLE = 'Cluster and instance names'
const COLUMN = { clusterName: 0, instanceName: 2, address: 3 } as const

function namesTable() {
  return screen.getByRole('table', { name: TABLE })
}

async function openRename(name: string) {
  fireEvent.click(within(namesTable()).getByRole('button', { name }))
  await flushNetwork(2)
  return screen.getByRole('dialog')
}

describe('NamesPanel', () => {
  it('CLNAME-UI-010 lists each cluster followed by its instances, and loose instances last', () => {
    const loose = { ...INSTANCE_SUMMARY, instance_id: UNASSIGNED_ID, addr: 'loose.example.test' }
    const rows = nameRows([cluster], [INSTANCE_SUMMARY, loose])

    expect(rows.map((row) => [row.kind, row.cluster_id, row.instance_id])).toEqual([
      ['cluster', CLUSTER_ID, null],
      ['instance', CLUSTER_ID, INSTANCE_ID],
      ['instance', null, UNASSIGNED_ID],
    ])
    expect(rows[1]?.address).toBe('postgres.example.test:5432')
    // The instance list is the source of the instance fields (display_name).
    const named = nameRows([cluster], [{ ...INSTANCE_SUMMARY, display_name: 'odoo-db-1' }])
    expect(named[1]?.instance_name).toBe('odoo-db-1')
    expect(named[1]?.instance_label).toBe('odoo-db-1')
  })

  it('CLNAME-UI-007 shows names, and default labels marked as such', async () => {
    server.use(
      ok('getClusters', [{ ...CLUSTER, name: null }]),
      ok('getInstances', [{ ...INSTANCE_SUMMARY, display_name: 'odoo-db-1' }]),
    )
    await renderPanel()

    const table = namesTable()
    expect(columnTexts(TABLE, COLUMN.clusterName)).toEqual([`Cluster ${CLUSTER_ID} (default)`, ''])
    expect(columnTexts(TABLE, COLUMN.instanceName)).toEqual(['', 'odoo-db-1'])
    expect(columnTexts(TABLE, COLUMN.address)).toEqual(['', 'postgres.example.test:5432'])
    expect(
      within(table).getByRole('button', { name: `Rename cluster Cluster ${CLUSTER_ID}` }),
    ).toBeInTheDocument()
    expect(within(table).getByRole('button', { name: 'Rename instance odoo-db-1' })).toBeEnabled()
  })

  it('CLNAME-UI-004 renaming a cluster sends the name and shows it after the refetch', async () => {
    let body: unknown
    server.use(
      sequence('getClusters', [CLUSTER], [renamedCluster]),
      ok('getInstances', [INSTANCE_SUMMARY]),
      http.patch('*/api/v1/clusters/:id', async ({ params, request }) => {
        body = await request.json()
        expect(params.id).toBe(CLUSTER_ID)
        assertMatchesContract('renameCluster', 200, renamedCluster)
        return HttpResponse.json(renamedCluster)
      }),
    )
    await renderPanel()

    const dialog = await openRename('Rename cluster production')
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Cluster name' }), {
      target: { value: 'prod-eu' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await flushNetwork()

    expect(body).toEqual({ name: 'prod-eu' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(columnTexts(TABLE, COLUMN.clusterName)).toEqual(['prod-eu', ''])
  })

  it('CLNAME-UI-005 clearing a name sends null and shows the default label', async () => {
    let body: unknown
    server.use(
      sequence('getClusters', [CLUSTER], [clearedCluster]),
      ok('getInstances', [INSTANCE_SUMMARY]),
      http.patch('*/api/v1/clusters/:id', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json(clearedCluster)
      }),
    )
    await renderPanel()

    const dialog = await openRename('Rename cluster production')
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Cluster name' }), {
      target: { value: '' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await flushNetwork()

    expect(body).toEqual({ name: null })
    expect(columnTexts(TABLE, COLUMN.clusterName)).toEqual([`Cluster ${CLUSTER_ID} (default)`, ''])
  })

  it('CLNAME-UI-004 renaming an instance sends display_name', async () => {
    let body: unknown
    server.use(
      ok('getClusters', [CLUSTER]),
      sequence('getInstances', [INSTANCE_SUMMARY], [renamedInstance]),
      http.patch('*/api/v1/instances/:id', async ({ params, request }) => {
        body = await request.json()
        expect(params.id).toBe(INSTANCE_ID)
        return HttpResponse.json(renamedInstance)
      }),
    )
    await renderPanel()

    const dialog = await openRename('Rename instance postgres.example.test:5432')
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Instance name' }), {
      target: { value: 'odoo-db-1' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await flushNetwork()

    expect(body).toEqual({ display_name: 'odoo-db-1' })
    expect(columnTexts(TABLE, COLUMN.instanceName)).toEqual(['', 'odoo-db-1'])
  })

  it('CLNAME-UI-006 a duplicate name keeps the dialog open with the typed value', async () => {
    server.use(
      ok('getClusters', [CLUSTER]),
      ok('getInstances', [INSTANCE_SUMMARY]),
      status('renameCluster', 409, {
        error: 'name already in use',
        detail: 'another cluster has this name',
      }),
    )
    await renderPanel()

    const dialog = await openRename('Rename cluster production')
    const input = within(dialog).getByRole('textbox', { name: 'Cluster name' })
    fireEvent.change(input, { target: { value: 'Staging' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await flushNetwork()

    expect(within(dialog).getByRole('alert')).toHaveTextContent('That name is already used')
    expect(input).toHaveValue('Staging')
    expect(within(dialog).getByRole('button', { name: 'Save' })).toBeEnabled()
  })

  it('CLNAME-UI-006 a refused name shows the server message', async () => {
    server.use(
      ok('getClusters', [CLUSTER]),
      ok('getInstances', [INSTANCE_SUMMARY]),
      status('renameInstance', 400, {
        error: 'invalid name',
        detail: 'the name may not contain control characters',
      }),
    )
    await renderPanel()

    const dialog = await openRename('Rename instance postgres.example.test:5432')
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Instance name' }), {
      target: { value: 'db one' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await flushNetwork()

    expect(within(dialog).getByRole('alert')).toHaveTextContent(
      'invalid name: the name may not contain control characters',
    )
  })

  it('CLNAME-UI-009 the panel has no serious accessibility violations', async () => {
    server.use(ok('getClusters', [CLUSTER]), ok('getInstances', [INSTANCE_SUMMARY]))
    const view = await renderPanel()
    expect(namesTable()).toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  }, 15000)
})
