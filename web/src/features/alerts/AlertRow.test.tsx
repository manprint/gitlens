import { screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { SimpleTable } from '@/components/layout/SimpleTable'
import { nameLookup } from '@/lib/names'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'

import { alertColumns } from './AlertRow'

type Alert = Schemas['Alert']

const instant = '2030-01-01T00:00:00Z'

describe('AlertRow', () => {
  it('UI-ALERT-060 shows an unknown instance, not a link to /instances/null, when the server sends null', () => {
    // The server sends null, not an absent field, for an alert with no instance.
    const alert = {
      alert_key: 'agent_down',
      rule_id: 'agent_down',
      severity: 'critical',
      state: 'firing',
      datname: '',
      labels: {},
      value: 1,
      summary: 'Agent is down',
      started_at: instant,
      last_eval_at: instant,
      resolved_at: null,
      suppressed: false,
      cluster_id: null,
      instance_id: null,
    } as unknown as Alert

    // The alert is a row of the alert list (15.3): its Cluster and Instance cells read Unknown.
    renderWithProviders(
      <SimpleTable
        ariaLabel="Alerts"
        columns={alertColumns({ onSelect: vi.fn(), silenceFor: () => undefined })}
        emptyState={null}
        rowKey={(row) => row.alert_key}
        rows={[alert]}
        tableId="alerts"
      />,
    )

    const cells = within(screen.getAllByRole('row')[1]!).getAllByRole('cell')
    const headers = screen.getAllByRole('columnheader').map((header) => header.textContent)
    expect(cells[headers.indexOf('Instance') - 1]).toHaveTextContent(/^Unknown$/)
    expect(cells[headers.indexOf('Cluster') - 1]).toHaveTextContent(/^Unknown$/)
    expect(screen.queryAllByRole('link').map((link) => link.getAttribute('href'))).toEqual([
      '/README.md#troubleshooting',
    ])
  })

  it('CLNAME-UI-012 names the cluster and instance, the id and address in the title, ids when unknown', () => {
    const base = {
      alert_key: 'replication-lag',
      rule_id: 'replication-lag',
      severity: 'warning',
      state: 'firing',
      datname: '',
      labels: {},
      value: 1,
      summary: 'Lag',
      started_at: instant,
      last_eval_at: instant,
      resolved_at: null,
      suppressed: false,
    }
    const known = { ...base, cluster_id: CLUSTER_ID, instance_id: INSTANCE_ID } as unknown as Alert
    const unknown = {
      ...base,
      alert_key: 'other',
      cluster_id: '77',
      instance_id: '00000000-0000-4000-8000-0000000000ff',
    } as unknown as Alert
    const names = nameLookup([
      {
        ...CLUSTER,
        name: 'odoo-prod',
        instances: [{ ...CLUSTER.instances[0], display_name: 'odoo-db-1' }],
      },
    ])
    renderWithProviders(
      <SimpleTable
        ariaLabel="Alerts"
        columns={alertColumns({ names, onSelect: vi.fn(), silenceFor: () => undefined })}
        emptyState={null}
        rowKey={(row) => row.alert_key}
        rows={[known, unknown]}
        tableId="alerts"
      />,
    )

    const cluster = screen.getByRole('link', { name: 'odoo-prod' })
    expect(cluster).toHaveAttribute('href', `/clusters/${CLUSTER_ID}`)
    // The one-line cell keeps the id in its tooltip (D26).
    expect(screen.getByTitle(CLUSTER_ID)).toContainElement(cluster)
    const instance = screen.getByRole('link', { name: 'odoo-db-1' })
    expect(instance).toHaveAttribute('href', `/instances/${INSTANCE_ID}`)
    expect(screen.getByTitle('postgres.example.test:5432')).toContainElement(instance)
    // Ids the cluster list does not hold stay ids.
    expect(screen.getByRole('link', { name: '77' })).toHaveAttribute('href', '/clusters/77')
    expect(
      screen.getByRole('link', { name: '00000000-0000-4000-8000-0000000000ff' }),
    ).toBeInTheDocument()
  })
})
