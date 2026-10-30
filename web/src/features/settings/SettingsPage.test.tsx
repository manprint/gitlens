import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import type { HttpHandler } from 'msw/http'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import { qk } from '@/api/keys'
import { REFRESH } from '@/api/policy'
import type { Schemas } from '@/api/types'
import { formatTimestamp } from '@/lib/format'
import { expectNoA11yViolations } from '@/test/a11y'
import { base as hostBase } from '@/test/fixtures/getInstanceHost'
import { CLUSTER, INSTANCE_ID, INSTANCE_SUMMARY, NOW } from '@/test/fixture-helpers'
import { preloadLazyRoutes, renderRoute, renderWithProviders } from '@/test/render'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { columnTexts, dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'

import { SettingsPage } from './SettingsPage'

type Alert = Schemas['Alert']
type AdvisorRule = Schemas['AdvisorRule']

const SECOND_INSTANCE_ID = '00000000-0000-4000-8000-000000000002'

const baseAlert: Alert = {
  alert_key: `agent_down/${SECOND_INSTANCE_ID}`,
  rule_id: 'agent_down',
  severity: 'critical',
  state: 'firing',
  instance_id: SECOND_INSTANCE_ID,
  datname: 'postgres',
  labels: {},
  value: 1,
  summary: 'Agent has stopped reporting',
  started_at: NOW,
  last_eval_at: NOW,
  resolved_at: null,
  suppressed: false,
}

const rules: AdvisorRule[] = [
  {
    id: 'connections',
    severity: 'warning',
    scope: 'instance',
    needs: ['activity'],
    min_tier: 'T0',
  },
  { id: 'bloat', severity: 'warning', scope: 'database', needs: ['stats'], min_tier: 'T1' },
  { id: 'locks', severity: 'critical', scope: 'instance', needs: ['locks'], min_tier: 'T2' },
]

async function settleInitialQueries() {
  vi.useRealTimers()
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
}

async function settleFakeTimers() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

interface RenderSettingsOptions {
  alerts?: Alert[]
  clusters?: Schemas['Cluster'][]
  databases?: { datname: string; monitored: boolean; skip_reason?: string | null }[]
  instances?: Schemas['InstanceSummary'][]
  instancesHandler?: HttpHandler
  route?: string
  advisorRules?: AdvisorRule[]
  databaseHandler?: HttpHandler
}

function renderSettings(options: RenderSettingsOptions = {}) {
  const instances = options.instances ?? [INSTANCE_SUMMARY]
  server.use(
    options.instancesHandler ?? ok('getInstances', instances),
    ok('getClusters', options.clusters ?? [CLUSTER]),
    ok('getAlerts', options.alerts ?? []),
    ok('getAdvisorRules', options.advisorRules ?? rules),
    ok('getInstanceCommandAudit', []),
    ok('getSession', { authenticated: true, configured: true, expires_at: NOW }),
    options.databaseHandler ??
      ok('getInstanceDatabases', {
        instance_id: INSTANCE_ID,
        databases: options.databases ?? [],
        not_monitored_count:
          options.databases?.filter((database) => !database.monitored).length ?? 0,
      }),
    ok('getInstanceHost', hostBase),
  )
  return renderWithProviders(<SettingsPage />, options.route ? { route: options.route } : {})
}

describe('SettingsPage', () => {
  beforeAll(preloadLazyRoutes)

  it('UI-SET-030 an empty fleet renders setup guidance', async () => {
    renderSettings({ clusters: [], instances: [] })
    await settleInitialQueries()

    // Real timers here: wait for the guidance instead of a fixed 25 ms, which
    // was too short when the suite runs under coverage.
    expect(
      await screen.findByText(
        'Install and configure the pglens agent on a PostgreSQL host to populate the fleet inventory.',
      ),
    ).toBeInTheDocument()
  })

  it('UI-SET-031 keeps the instance inventory when a database endpoint fails', async () => {
    const secondInstance = {
      ...INSTANCE_SUMMARY,
      addr: 'standby.example.test',
      instance_id: SECOND_INSTANCE_ID,
    }
    renderSettings({
      clusters: [],
      databaseHandler: sequence(
        'getInstanceDatabases',
        {
          status: 500,
          body: { error: 'internal_error', detail: 'database inventory unavailable' },
        },
        {
          body: {
            instance_id: SECOND_INSTANCE_ID,
            databases: [{ datname: 'app', monitored: true }],
            not_monitored_count: 0,
          },
        },
      ),
      instances: [INSTANCE_SUMMARY, secondInstance],
    })
    await settleInitialQueries()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Could not load databases for postgres',
    )
    const secondTable = await waitFor(() =>
      screen.getByRole('table', { name: 'Databases for standby.example.test:5432' }),
    )
    expect(within(secondTable).getByText('app')).toBeInTheDocument()
    expect(
      within(screen.getByRole('table', { name: 'Monitored instances' })).getAllByRole('row'),
    ).toHaveLength(3)
  })

  it('UI-SET-032 marks stale instance data as Stale', async () => {
    const view = renderSettings()
    await settleInitialQueries()
    view.queryClient.setQueryData(qk.instances(), [INSTANCE_SUMMARY], {
      updatedAt: Date.parse(NOW) - REFRESH.fleet.staleAfter,
    })

    expect(await screen.findByRole('status', { name: /Stale data:/i })).toBeInTheDocument()
  })

  it('UI-SET-033 navigates a 401 to login exactly once', async () => {
    vi.useRealTimers()
    server.use(
      status('getInstances', 401),
      ok('getClusters', []),
      ok('getAlerts', []),
      ok('getAdvisorRules', rules),
      ok('getSession', { authenticated: true, configured: true, expires_at: NOW }),
    )
    const view = renderRoute('/settings')

    await waitFor(() => expect(view.router.state.location.pathname).toBe('/login'))
    expect(view.router.state.location.search).toBe('?next=%2F')
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 25))
    })
    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe('?next=%2F')
  })

  it('UI-SET-034 retries a server error and renders Settings after recovery', async () => {
    renderSettings({
      instancesHandler: sequence(
        'getInstances',
        { status: 500, body: { error: 'internal_error', detail: 'instances unavailable' } },
        [INSTANCE_SUMMARY],
      ),
    })
    await settleInitialQueries()

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load instances')
    fireEvent.click(screen.getByRole('button', { name: 'Retry instances' }))
    await settleInitialQueries()

    expect(screen.getByRole('heading', { name: 'Settings and inventory' })).toBeInTheDocument()
  })

  it('UI-SET-001 unmonitored databases sort first with their skip reason', async () => {
    renderSettings({
      databases: [
        { datname: 'app', monitored: true },
        { datname: 'legacy', monitored: false, skip_reason: 'extension unavailable' },
      ],
    })
    await settleInitialQueries()

    const table = await waitFor(() =>
      screen.getByRole('table', { name: 'Databases for postgres.example.test:5432' }),
    )
    const rows = within(table).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('legacy')
    expect(rows[1]).toHaveTextContent('No')
    expect(rows[1]).toHaveTextContent('extension unavailable')
    expect(screen.getByText(/Databases not monitored: 1/)).toBeInTheDocument()
  })

  it('UI-SET-002 the tier summary counts instances per tier', async () => {
    renderSettings({
      instances: [
        { ...INSTANCE_SUMMARY, perm_tier: 'T0' },
        { ...INSTANCE_SUMMARY, instance_id: SECOND_INSTANCE_ID, perm_tier: 'T1' },
        {
          ...INSTANCE_SUMMARY,
          instance_id: '00000000-0000-4000-8000-000000000003',
          perm_tier: 'T2',
        },
      ],
      clusters: [],
    })
    await settleInitialQueries()

    const tierCards = screen.getAllByRole('article')
    expect(tierCards).toHaveLength(3)
    for (const card of tierCards) {
      expect(card).toHaveTextContent(/Instances:\s*1/)
    }
  })

  it('UI-SET-003 the tier summary lists what each tier unlocks', async () => {
    renderSettings()
    await settleInitialQueries()

    expect(screen.getAllByText('connections (T0)')).toHaveLength(3)
    expect(screen.getAllByText('bloat (T1)')).toHaveLength(2)
    expect(screen.getByText('locks (T2)')).toBeInTheDocument()
    expect(screen.getByText('Run plan-only EXPLAIN on eligible targets.')).toBeInTheDocument()
    expect(
      screen.getByText('Cancel or terminate client backends on eligible targets.'),
    ).toBeInTheDocument()
  })

  it('UI-SET-036 the T1 card says the interface offers EXPLAIN ANALYZE only at T2', async () => {
    renderSettings()
    await settleInitialQueries()

    expect(
      screen.getByText(
        'EXPLAIN ANALYZE is accepted when the target sets allow_explain_analyze; the interface offers it at T2 only.',
      ),
    ).toBeInTheDocument()
  })

  it('UI-SET-004 the agent view is derived from last_seen and alerts', async () => {
    renderSettings({
      alerts: [baseAlert],
      instances: [
        INSTANCE_SUMMARY,
        { ...INSTANCE_SUMMARY, instance_id: SECOND_INSTANCE_ID, addr: 'standby.example.test' },
      ],
      clusters: [],
    })
    await settleInitialQueries()

    const table = screen.getByRole('table', { name: 'Derived agent inventory' })
    const rows = within(table).getAllByRole('row')
    expect(rows[2]).toHaveTextContent('Down')
    expect(rows[2]).toHaveTextContent('Agent has stopped reporting')
    expect(within(table).getAllByText(formatTimestamp(NOW, 'UTC'))).toHaveLength(2)
    expect(screen.getByText(/no separate agent listing endpoint/i)).toBeInTheDocument()
  })

  it('UI-SET-005 filters round-trip through the URL', async () => {
    const view = renderSettings({
      instances: [
        INSTANCE_SUMMARY,
        { ...INSTANCE_SUMMARY, instance_id: SECOND_INSTANCE_ID, addr: 'standby.example.test' },
      ],
      clusters: [],
      route: '/?monitored-instances.q=standby',
    })
    await settleInitialQueries()

    expect(screen.getByLabelText('Search Monitored instances')).toHaveValue('standby')
    const instanceTable = screen.getByRole('table', { name: 'Monitored instances' })
    expect(within(instanceTable).getByText('standby.example.test:5432')).toBeInTheDocument()
    expect(within(instanceTable).queryByText('postgres.example.test:5432')).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Search Monitored instances'), {
      target: { value: 'postgres' },
    })
    expect(view.router.state.location.search).toBe('?monitored-instances.q=postgres')
    expect(within(instanceTable).getByText('postgres.example.test:5432')).toBeInTheDocument()
  })

  it('has no serious accessibility violations on a populated page', async () => {
    const view = renderSettings({ databases: [{ datname: 'app', monitored: true }] })
    await settleInitialQueries()
    await waitFor(() =>
      expect(screen.getByRole('table', { name: 'Monitored instances' })).toBeInTheDocument(),
    )

    await expectNoA11yViolations(view.container)
  }, 15000)

  it('UI-SET-035 polls the per-instance inventory less often than the instance policy', async () => {
    const inventoryPaths: string[] = []
    const countInventory = ({ request }: { request: Request }) => {
      const { pathname } = new URL(request.url)
      if (/\/api\/v1\/instances\/[^/]+\/(host|databases|command-audit)$/.test(pathname)) {
        inventoryPaths.push(pathname)
      }
    }
    server.events.on('request:start', countInventory)
    try {
      renderSettings({ clusters: [] })
      await settleFakeTimers()
      expect(inventoryPaths.length).toBeGreaterThan(0)

      inventoryPaths.length = 0
      await act(async () => {
        await vi.advanceTimersByTimeAsync(REFRESH.instance.interval + 1_000)
      })
      await settleFakeTimers()

      expect(inventoryPaths).toEqual([])
    } finally {
      server.events.removeListener('request:start', countInventory)
    }
  })
})

describe('SettingsPage tables: pages, search, filters, sort', () => {
  beforeAll(preloadLazyRoutes)

  it('TBL-UI-028 instance-databases pages, searches, filters and sorts one instance', async () => {
    renderSettings({
      databases: seq(25).map((n, index) => ({
        datname: `db_${n}`,
        monitored: index >= 3,
        skip_reason: index < 3 ? 'no CONNECT privilege' : null,
      })),
    })
    await settleInitialQueries()
    const name = `Databases for ${INSTANCE_SUMMARY.addr}:${INSTANCE_SUMMARY.port}`
    await waitFor(() => expect(dataRows(name)).toHaveLength(20))

    searchTable(name, 'CONNECT')
    expect(columnTexts(name)).toEqual(['db_01', 'db_02', 'db_03'])
    searchTable(name, '')
    filterTable(name, 'Monitored', 'no')
    expect(dataRows(name)).toHaveLength(3)
    filterTable(name, 'Monitored', '')
    sortTable(name, 'Database', 'descending')
    expect(columnTexts(name)[0]).toBe('db_25')
  })

  it('TBL-UI-030 monitored-instances', async () => {
    const start = new Date(NOW).getTime()
    const view = renderSettings({
      clusters: [],
      instances: seq(25).map((n, index) => ({
        ...INSTANCE_SUMMARY,
        instance_id: `00000000-0000-4000-8000-0000000000${n}`,
        addr: `host-${n}.example.test`,
        role: index < 5 ? 'standby' : 'primary',
        up: index !== 0,
        last_seen: new Date(start - index * 60_000).toISOString(),
      })),
    })
    await settleInitialQueries()

    const instances = 'Monitored instances'
    expect(dataRows(instances)).toHaveLength(20)
    searchTable(instances, 'host-2')
    expect(dataRows(instances)).toHaveLength(6)
    expect(view.router.state.location.search).toBe('?monitored-instances.q=host-2')
    // A search with no match keeps the toolbar; the database sections say so.
    searchTable(instances, 'nothing-like-this')
    expect(screen.getByText('No rows match')).toBeInTheDocument()
    expect(screen.getByText('No instances match this filter')).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Search Monitored instances' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeVisible()
    expect(screen.getByText(/\(filtered from 25\)/)).toBeInTheDocument()
    searchTable(instances, '')
    filterTable(instances, 'Role', 'standby')
    expect(dataRows(instances)).toHaveLength(5)
    filterTable(instances, 'Role', '')
    filterTable(instances, 'State', 'down')
    expect(columnTexts(instances, 1)).toEqual(['host-01.example.test:5432'])
    filterTable(instances, 'State', '')
    sortTable(instances, 'Last seen', 'ascending')
    expect(columnTexts(instances, 1)[0]).toBe('host-25.example.test:5432')
  })

  it('TBL-UI-029 derived-agent-inventory', async () => {
    const start = new Date(NOW).getTime()
    renderSettings({
      clusters: [],
      instances: seq(25).map((n, index) => ({
        ...INSTANCE_SUMMARY,
        instance_id: `00000000-0000-4000-8000-0000000000${n}`,
        addr: `host-${n}.example.test`,
        role: index < 5 ? 'standby' : 'primary',
        up: index !== 0,
        last_seen: new Date(start - index * 60_000).toISOString(),
      })),
    })
    await settleInitialQueries()
    const agents = 'Derived agent inventory'
    expect(dataRows(agents)).toHaveLength(20)
    searchTable(agents, 'host-0')
    expect(dataRows(agents)).toHaveLength(9)
    searchTable(agents, '')
    filterTable(agents, 'State', 'Down')
    expect(columnTexts(agents, 1)).toEqual(['host-01.example.test:5432'])
    filterTable(agents, 'State', '')
    sortTable(agents, 'Last seen', 'ascending')
    expect(columnTexts(agents, 1)[0]).toBe('host-25.example.test:5432')
  })
})
