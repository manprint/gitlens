import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Cluster, Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID } from '@/test/fixture-helpers'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { makeGetInstanceDatabases } from '@/test/fixtures/getInstanceDatabases'
import { renderWithProviders } from '@/test/render'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

import { InstancePage } from './InstancePage'

vi.mock('echarts-for-react', () => ({
  default: () => null,
}))

const cluster = {
  ...(CLUSTER as unknown as Cluster),
  max_replay_lag_seconds: 1.25,
}
const baseInstance = makeGetInstance() as unknown as Schemas['Instance']

function makeInstance(overrides: Partial<Schemas['Instance']> = {}): Schemas['Instance'] {
  return { ...baseInstance, ...overrides }
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function renderPage(
  instance: Schemas['Instance'] = baseInstance,
  databases: Schemas['DatabasesResponse'] = makeGetInstanceDatabases(),
) {
  server.use(
    ok('getInstance', instance),
    ok('getClusters', [cluster]),
    ok('getInstanceDatabases', databases),
    ok('getInstanceHost', {
      instance_id: INSTANCE_ID,
      available: false,
      reason: 'agent unreachable',
    }),
    ok('queryMetrics', { series: [] }),
  )
  return renderWithProviders(<InstancePage instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('InstancePage', () => {
  it('UI-INST-002 renders a standby read-only banner with its replay lag', async () => {
    renderPage(makeInstance({ pg_version: 170011, role: 'standby' }))
    await settle()

    expect(screen.getByText('standby (read-only)')).toBeInTheDocument()
    expect(screen.getByText('Replay lag: 1.25 s')).toBeInTheDocument()
    expect(screen.getByText('17.11')).toBeInTheDocument()
    vi.useRealTimers()
    await expectNoA11yViolations(document.body)
  })

  it('UI-INST-003 renders no read-only banner for a primary', async () => {
    renderPage(makeInstance({ pg_version: 160002, role: 'primary' }))
    await settle()

    expect(screen.queryByText('standby (read-only)')).not.toBeInTheDocument()
    expect(screen.getByText('16.2')).toBeInTheDocument()
  })

  it('UI-INST-004 marks an instance with up=false as down', async () => {
    renderPage(makeInstance({ up: false }))
    await settle()

    expect(screen.getByText('down')).toBeInTheDocument()
    expect(
      screen.getByText('Instance is down and its latest values may be stale.'),
    ).toBeInTheDocument()
  })

  it('UI-INST-005 links the owning cluster with the exact cluster id string', async () => {
    renderPage()
    await settle()

    expect(screen.getByRole('link', { name: CLUSTER_ID })).toHaveAttribute(
      'href',
      `/clusters/${CLUSTER_ID}`,
    )
  })

  it('UI-INST-013 writes the selected database to the URL', async () => {
    const view = renderPage(baseInstance, {
      instance_id: INSTANCE_ID,
      databases: [
        { datname: 'app', monitored: true, skip_reason: null },
        { datname: 'warehouse', monitored: true, skip_reason: null },
      ],
      not_monitored_count: 0,
    })
    await settle()

    const selector = screen.getByRole('combobox', { name: 'Database' })
    expect(selector).toHaveValue('app')
    fireEvent.change(selector, { target: { value: 'warehouse' } })

    expect(view.router.state.location.search).toBe('?db=warehouse')
  })

  it('UI-INST-014 displays the unmonitored count and grouped reasons', async () => {
    renderPage(baseInstance, {
      instance_id: INSTANCE_ID,
      databases: [
        { datname: 'app', monitored: true, skip_reason: null },
        { datname: 'audit', monitored: false, skip_reason: 'excluded' },
        { datname: 'legacy', monitored: false, skip_reason: 'db_budget' },
      ],
      not_monitored_count: 2,
    })
    await settle()

    expect(screen.getByText('2 databases not monitored.')).toBeInTheDocument()
    expect(screen.getByText('excluded')).toBeInTheDocument()
    expect(screen.getByText('db_budget')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'databases.max' })).toHaveAttribute(
      'href',
      '/README.md#agent-configuration',
    )
  })

  it('UI-INST-015 renders an explicit state when no database is monitored', async () => {
    renderPage(baseInstance, {
      instance_id: INSTANCE_ID,
      databases: [{ datname: 'audit', monitored: false, skip_reason: 'excluded' }],
      not_monitored_count: 1,
    })
    await settle()

    expect(screen.getByText('No monitored databases')).toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: 'Database' })).not.toBeInTheDocument()
    expect(screen.getByText('1 database not monitored.')).toBeInTheDocument()
  })
})
