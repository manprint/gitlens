import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { renderWithProviders } from '@/test/render'

import { AlertRow } from './AlertRow'

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

    renderWithProviders(<AlertRow alert={alert} onSelect={vi.fn()} />)

    expect(screen.getByText('Instance: Unknown')).toBeInTheDocument()
    expect(screen.getByText('Cluster: Unknown')).toBeInTheDocument()
    expect(screen.queryAllByRole('link').map((link) => link.getAttribute('href'))).toEqual([
      '/README.md#troubleshooting',
    ])
  })
})
