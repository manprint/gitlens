import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { delay } from 'msw/utils/delay'
import { describe, expect, it, vi } from 'vitest'

import type { Cluster, Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID } from '@/test/fixture-helpers'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { makeGetInstanceDatabases } from '@/test/fixtures/getInstanceDatabases'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

import { InstancePage } from './InstancePage'

vi.mock('echarts-for-react/esm/core', () => ({
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

const PRIMARY_ID = '00000000-0000-4000-8000-000000000002'
const OTHER_STANDBY_ID = '00000000-0000-4000-8000-000000000003'

function replayEdge(from: string, series: { ts: string; value: number | null }[]) {
  return {
    from,
    to: PRIMARY_ID,
    metric: 'replay_lag_sec',
    series,
  } satisfies Schemas['ReplicationEdge']
}

function renderPage(
  instance: Schemas['Instance'] = baseInstance,
  databases: Schemas['DatabasesResponse'] = makeGetInstanceDatabases(),
  replicationEdges: Schemas['ReplicationEdge'][] = [],
  replicationHandler: ReturnType<typeof ok> = ok('getClusterReplication', {
    cluster_id: CLUSTER_ID,
    edges: replicationEdges,
  }),
  route = `/instances/${INSTANCE_ID}`,
) {
  server.use(
    ok('getInstance', instance),
    ok('getClusters', [cluster]),
    replicationHandler,
    ok('getInstanceDatabases', databases),
    ok('getInstanceHost', {
      instance_id: INSTANCE_ID,
      available: false,
      reason: 'agent unreachable',
    }),
    ok('getInstanceSettings', { instance_id: INSTANCE_ID, settings: [] }),
    ok('getInstanceTriggers', { instance_id: INSTANCE_ID, triggers: [], truncated_databases: [] }),
    ok('getInstanceExtensions', {
      instance_id: INSTANCE_ID,
      extensions: [],
      truncated_databases: [],
    }),
    ok('getInstanceSpatialColumns', {
      instance_id: INSTANCE_ID,
      columns: [],
      truncated_databases: [],
    }),
    ok('getInstanceSequences', {
      instance_id: INSTANCE_ID,
      sequences: [],
      truncated_databases: [],
    }),
    ok('getInstancePartitionedTables', {
      instance_id: INSTANCE_ID,
      tables: [],
      truncated_databases: [],
    }),
    ok('getInstanceFunctions', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      databases: [],
      functions: [],
    }),
    ok('getInstanceSecurityDefinerFunctions', {
      instance_id: INSTANCE_ID,
      functions: [],
      truncated_databases: [],
    }),
    ok('getInstanceLogicalReplication', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      subscriptions: [],
      slots: [],
    }),
    ok('getInstanceInternals', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      wal_directory: null,
      databases: [],
      slru: [],
      temp_files: [],
      standby: null,
    }),
    ok('getInstanceUnindexedForeignKeys', {
      instance_id: INSTANCE_ID,
      foreign_keys: [],
      truncated_databases: [],
    }),
    ok('getInstanceTables', {
      instance_id: INSTANCE_ID,
      items: [],
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items: [],
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getInstanceBloat', {
      instance_id: INSTANCE_ID,
      items: [],
      truncated: false,
      relations_not_reported: null,
      skipped_tables: 0,
      skipped_relations: [],
    }),
    ok('queryMetrics', { series: [] }),
  )
  return renderWithProviders(<InstancePage instanceId={INSTANCE_ID} />, {
    route,
  })
}

describe('InstancePage', () => {
  it('UI-INST-002 renders a standby read-only banner with its own replay lag', async () => {
    renderPage(makeInstance({ pg_version: 170011, role: 'standby' }), undefined, [
      replayEdge(INSTANCE_ID, [
        { ts: '2026-08-27T01:59:30Z', value: 0.2 },
        { ts: '2026-08-27T02:00:00Z', value: 0.12 },
      ]),
      replayEdge(OTHER_STANDBY_ID, [{ ts: '2026-08-27T02:00:00Z', value: 1.25 }]),
    ])
    await flushNetwork()

    expect(screen.getByText('standby (read-only)')).toBeInTheDocument()
    expect(screen.getByText('Replay lag: 0.12 s')).toBeInTheDocument()
    expect(screen.queryByText('Replay lag: 1.25 s')).not.toBeInTheDocument()
    expect(screen.getByText('17.11')).toBeInTheDocument()
    vi.useRealTimers()
    await expectNoA11yViolations(document.body)
  })

  it('HA-V-10-2 the relations section of a standby carries the standby statistics note', async () => {
    renderPage(
      makeInstance({ role: 'standby' }),
      undefined,
      [],
      undefined,
      `/instances/${INSTANCE_ID}?section=relations`,
    )
    await flushNetwork()
    expect(
      screen
        .getAllByRole('note')
        .filter((note) => /statistics are this standby's own/i.test(note.textContent ?? '')),
    ).toHaveLength(1)
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

  it('UI-INST-060 shows a standby lag as not measured when only the cluster maximum is known', async () => {
    renderPage(makeInstance({ role: 'standby' }), undefined, [
      replayEdge(OTHER_STANDBY_ID, [{ ts: '2026-08-27T02:00:00Z', value: 1.25 }]),
    ])
    await flushNetwork()

    expect(screen.getByText('standby (read-only)')).toBeInTheDocument()
    expect(screen.queryByText('Replay lag: 1.25 s')).not.toBeInTheDocument()
    expect(screen.getByTitle('Replay lag was not measured.')).toBeInTheDocument()
  })

  it('UI-INST-061 shows not measured when the newest standby reading is null', async () => {
    renderPage(makeInstance({ role: 'standby' }), undefined, [
      replayEdge(INSTANCE_ID, [
        { ts: '2026-08-27T01:59:30Z', value: 0.5 },
        { ts: '2026-08-27T02:00:00Z', value: null },
      ]),
    ])
    await flushNetwork()

    expect(screen.queryByText('Replay lag: 0.5 s')).not.toBeInTheDocument()
    expect(screen.queryByText('Replay lag: 1.25 s')).not.toBeInTheDocument()
    expect(screen.getByTitle('Replay lag was not measured.')).toBeInTheDocument()
  })

  it('UI-INST-062 does not show a standby reading older than the server freshness bound', async () => {
    renderPage(makeInstance({ role: 'standby' }), undefined, [
      replayEdge(INSTANCE_ID, [{ ts: '2026-08-27T01:56:00Z', value: 0.5 }]),
    ])
    await flushNetwork()

    expect(screen.queryByText('Replay lag: 0.5 s')).not.toBeInTheDocument()
    expect(screen.getByTitle('Replay lag was not measured.')).toBeInTheDocument()
  })

  it('UI-INST-063 shows the replay lag as loading, not as not measured, while its series is pending', async () => {
    renderPage(
      makeInstance({ role: 'standby' }),
      undefined,
      [],
      http.get('/api/v1/clusters/:clusterId/replication', async () => {
        await delay('infinite')
        return HttpResponse.json({})
      }),
    )
    await flushNetwork()

    expect(screen.getByText('Replay lag: loading')).toBeInTheDocument()
    expect(screen.queryByTitle('Replay lag was not measured.')).not.toBeInTheDocument()
  })

  it('UI-INST-064 shows the replay lag as unavailable, not as not measured, when its series fails', async () => {
    renderPage(
      makeInstance({ role: 'standby' }),
      undefined,
      [],
      http.get('/api/v1/clusters/:clusterId/replication', () =>
        HttpResponse.json({ detail: 'replication unavailable' }, { status: 500 }),
      ),
    )
    await flushNetwork()

    expect(screen.getByText('Replay lag: unavailable')).toBeInTheDocument()
    expect(screen.queryByTitle('Replay lag was not measured.')).not.toBeInTheDocument()
  })
})

describe('InstancePage sections', () => {
  const at = (query: string) =>
    renderPage(
      baseInstance,
      makeGetInstanceDatabases(),
      [],
      undefined,
      `/instances/${INSTANCE_ID}${query}`,
    )
  const tab = (name: string) => screen.getByRole('tab', { name })
  const requested: string[] = []
  const record = () => {
    requested.length = 0
    server.events.on('request:start', ({ request }) => {
      requested.push(new URL(request.url).pathname)
    })
  }

  it('INSTSEC-001 opens Overview by default and writes nothing to the URL (INSTSEC-009)', async () => {
    const view = at('')
    await settle()

    expect(tab('Overview')).toHaveAttribute('aria-selected', 'true')
    expect(view.router.state.location.search).toBe('')
    fireEvent.mouseDown(tab('Host'))
    await settle()
    fireEvent.mouseDown(tab('Overview'))
    await settle()
    expect(tab('Overview')).toHaveAttribute('aria-selected', 'true')
    expect(view.router.state.location.search).toBe('')
  })

  it('INSTSEC-002 ?section=host selects Host and a click writes the section', async () => {
    const view = at('?section=host')
    await settle()
    expect(tab('Host')).toHaveAttribute('aria-selected', 'true')

    fireEvent.mouseDown(tab('Schema objects'))
    await settle()
    expect(view.router.state.location.search).toBe('?section=catalog')
    expect(tab('Schema objects')).toHaveAttribute('aria-selected', 'true')
  })

  it('INSTSEC-003 an unknown section falls back to Overview', async () => {
    at('?section=nonsense')
    await settle()
    expect(tab('Overview')).toHaveAttribute('aria-selected', 'true')
  })

  it('INSTSEC-004 mounts and fetches only the active section', async () => {
    record()
    try {
      at('?section=internals')
      await settle()

      expect(requested.some((path) => path.endsWith('/internals'))).toBe(true)
      for (const other of [
        '/settings',
        '/triggers',
        '/logical-replication',
        '/tables',
        '/host',
        '/metrics/query',
      ]) {
        expect(
          requested.some((path) => path.endsWith(other)),
          other,
        ).toBe(false)
      }
      expect(screen.queryByRole('table', { name: 'Observed PostgreSQL settings' })).toBeNull()
    } finally {
      server.events.removeAllListeners()
    }
  })

  it('INSTSEC-005 a legacy in-page anchor opens its section', async () => {
    for (const [hash, label] of [
      ['#instance-overview', 'Overview'],
      ['#host-metrics', 'Host'],
      ['#host', 'Host'],
      ['#settings', 'Settings'],
      ['#instance-relations', 'Tables and indexes'],
      ['#catalog', 'Schema objects'],
      ['#logical-replication', 'Logical replication'],
      ['#internals', 'Internals'],
    ] as const) {
      const view = at(hash)
      await settle()
      expect(tab(label), hash).toHaveAttribute('aria-selected', 'true')
      view.unmount()
    }
  })

  it('INSTSEC-008 the Overview Host link opens the Host tab; group links stay anchors', async () => {
    const view = renderPage(baseInstance, {
      ...makeGetInstanceDatabases(),
      databases: [{ datname: 'app', monitored: true, skip_reason: null }],
    })
    await settle()
    const nav = screen.getByRole('navigation', { name: 'Chart groups' })

    expect(within(nav).getByRole('link', { name: 'Workload' })).toHaveAttribute('href', '#workload')
    fireEvent.click(within(nav).getByRole('link', { name: 'Host' }))
    await settle()
    expect(view.router.state.location.search).toContain('section=host')
    expect(tab('Host')).toHaveAttribute('aria-selected', 'true')
    // The link unmounted with the Overview: focus moves to the Host tab.
    expect(tab('Host')).toHaveFocus()
  })

  it('INSTSEC-007 the section tabs have no serious accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = at('?section=settings')
    await screen.findByRole('tablist', { name: 'Instance sections' })
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('FHD-APPLY-047 the section tabs sit in the header and a section change drops the view of the previous one', async () => {
    const view = at('?section=catalog&view=sequences')
    await settle()

    // The tab list is part of the header grid, beside the metrics from 1600 px (15.3).
    const sectionTabs = screen.getByTestId('instance-section-tabs')
    expect(
      within(sectionTabs).getByRole('tablist', { name: 'Instance sections' }),
    ).toBeInTheDocument()
    expect(sectionTabs).toHaveClass('min-[1600px]:col-start-1', 'min-[1600px]:row-start-2')
    expect(within(screen.getByTestId('instance-header')).getByTestId('instance-section-tabs')).toBe(
      sectionTabs,
    )
    expect(screen.getByRole('tab', { name: /^Sequences/ })).toHaveAttribute('aria-selected', 'true')

    fireEvent.mouseDown(tab('Settings'))
    await settle()
    expect(view.router.state.location.search).toBe('?section=settings')
  })
})
