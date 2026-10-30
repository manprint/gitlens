import { screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { SimpleTable } from '@/components/layout/SimpleTable'
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
})
