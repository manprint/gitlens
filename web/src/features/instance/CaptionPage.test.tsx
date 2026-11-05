import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { captionOf, type CaptionId } from '@/lib/captions'
import { expectCaptionedCards, expectClearCard } from '@/test/clearCard'
import { INSTANCE_ID, NOW } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { CatalogSection } from './CatalogSection'
import { DatabaseSelector } from './DatabaseSelector'
import { HostSection } from './HostSection'
import { InternalsSection } from './InternalsSection'
import { LogicalReplicationSection } from './LogicalReplicationSection'
import { OverviewSection } from './OverviewSection'
import { RelationsSection } from './RelationsSection'
import { SettingsSection } from './SettingsSection'
import { INSTANCE_SECTIONS } from './instanceSections'

vi.mock('echarts-for-react/esm/core', () => ({
  default: () => null,
}))

const SEEN = '2026-08-28T12:00:00Z'

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

async function openTab(name: RegExp) {
  const tab = screen.getByRole('tab', { name })
  fireEvent.mouseDown(tab)
  fireEvent.click(tab)
  await settle()
}

/** The caption sentence of an id is on the page, in at least one element. */
function expectCaptionText(id: CaptionId) {
  expect(screen.getAllByText(captionOf(id).what).length, id).toBeGreaterThan(0)
}

function tiles(): HTMLElement[] {
  return screen.queryAllByRole('article')
}

function renderOverview() {
  const values: Record<string, number> = {
    pg_connections_used: 7,
    pg_connections_limit: 100,
    pg_xact_commit_total: 4,
    pg_xact_rollback_total: 2,
    pg_blks_hit_total: 99,
    pg_blks_read_total: 1,
  }
  server.use(
    http.get('http://localhost/api/v1/metrics/query', ({ request }) => {
      const metric = new URL(request.url).searchParams.get('metric') ?? ''
      return HttpResponse.json({
        series: [
          { ts: '2026-08-27T00:58:00Z', value: values[metric] ?? 1 },
          { ts: '2026-08-27T00:59:00Z', value: values[metric] ?? 1 },
          { ts: '2026-08-27T01:00:00Z', value: null },
        ],
      })
    }),
  )
  return renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
  })
}

type HostFixture = Schemas['HostResponse'] & Record<string, unknown>

function renderHost(data: HostFixture) {
  server.use(ok('getInstanceHost', data), ok('queryMetrics', { series: [] }))
  return renderWithProviders(<HostSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

const HOST: HostFixture = {
  instance_id: INSTANCE_ID,
  available: true,
  source: 'host',
  sampled_at: NOW,
  stale: false,
  host_cpu_used_ratio: 0.25,
  host_cpu_count: 4,
  host_mem_total_bytes: 8 * 1024 ** 3,
  host_mem_available_bytes: 3 * 1024 ** 3,
  host_swap_total_bytes: 2 * 1024 ** 3,
  host_swap_used_bytes: 1024 ** 2,
  host_load1: 0.42,
  host_load5: 0.4,
  host_load15: 0.3,
  host_disk_total_bytes: 100 * 1024 ** 3,
  host_disk_free_bytes: 40 * 1024 ** 3,
  host_disk_free_ratio: 0.4,
}

function renderInternals(data: Partial<Schemas['InternalsResponse']>) {
  server.use(
    ok('getInstanceInternals', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      wal_directory: null,
      databases: [],
      slru: [],
      temp_files: [],
      standby: null,
      ...data,
    }),
  )
  return renderWithProviders(<InternalsSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

const INTERNALS: Partial<Schemas['InternalsResponse']> = {
  wal_directory: { bytes: 2 * 1024 ** 3, files: 128, observed_at: NOW },
  databases: [
    {
      database: 'app',
      size_bytes: 5 * 1024 ** 3,
      observed_at: NOW,
      growth_24h_bytes: 1024 ** 3,
      growth_7d_bytes: null,
    },
  ],
  slru: [
    {
      name: 'subtransaction',
      hit_ratio: 0.9,
      hits_per_second: 90,
      reads_per_second: 10,
      writes_per_second: 0.5,
      zeroed_per_second: 0,
      flushes_per_second: 0.02,
      truncates_per_second: 0,
      exists_per_second: 0,
      last_seen: NOW,
    },
  ],
  temp_files: [{ database: 'app', files_per_second: 0.05, bytes_per_second: 1024 * 1024 }],
  standby: {
    replay_paused: true,
    observed_at: NOW,
    conflicts: [{ database: 'app', reason: 'snapshot', per_second: 0.1 }],
    prefetch_blocks_per_second: { prefetch: 4, hit: 200 },
  },
}

function renderLogical() {
  server.use(
    ok('getInstanceLogicalReplication', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      subscriptions: [
        {
          name: 'orders_sub',
          database: 'app',
          owner: 'postgres',
          enabled: true,
          slot_name: 'orders_sub',
          publications: ['orders'],
          apply_worker: true,
          sync_workers: 0,
          last_message_at: new Date(new Date(NOW).getTime() - 60_000).toISOString(),
          apply_errors: 0,
          sync_errors: 0,
          conflicts: null,
          first_seen: NOW,
          last_seen: NOW,
        },
      ],
      slots: [
        {
          slot_name: 'orders_sub',
          bytes_per_second: 20480,
          txns_per_second: 12.5,
          spill_bytes_per_second: 0,
          spill_txns_per_second: 0,
          stream_bytes_per_second: 0,
          last_seen: NOW,
        },
      ],
    }),
  )
  return renderWithProviders(<LogicalReplicationSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

function renderCatalog({ noExtensions = false } = {}) {
  server.use(
    ok('getInstanceTriggers', {
      instance_id: INSTANCE_ID,
      triggers: [
        {
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
        },
      ],
      truncated_databases: [],
    }),
    ok('getInstanceUnindexedForeignKeys', {
      instance_id: INSTANCE_ID,
      foreign_keys: [
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
      truncated_databases: [],
    }),
    ok('getInstanceExtensions', {
      instance_id: INSTANCE_ID,
      extensions: [
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
      ],
      truncated_databases: [],
    }),
    ok('getInstanceSpatialColumns', {
      instance_id: INSTANCE_ID,
      columns: [
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
      ],
      truncated_databases: [],
    }),
    ok('getInstanceSequences', {
      instance_id: INSTANCE_ID,
      sequences: [
        {
          datname: 'app',
          schemaname: 'public',
          name: 'orders_id_seq',
          data_type: 'integer',
          state: 'used',
          start: '1',
          increment: '1',
          min: '1',
          max: '2147483647',
          cycle: false,
          last_value: '1000',
          limit: '2147483647',
          limit_source: 'sequence',
          exhausts: true,
          used_ratio: 0.000001,
          remaining: '2147482647',
          table_schema: 'public',
          table: 'orders',
          column: 'id',
          column_type: 'integer',
          link: 'owned',
          columns: 1,
          first_seen: SEEN,
          last_seen: SEEN,
          changed_at: SEEN,
        },
      ],
      truncated_databases: [],
    }),
    ok('getInstancePartitionedTables', {
      instance_id: INSTANCE_ID,
      tables: [
        {
          datname: 'app',
          schemaname: 'public',
          relname: 'events',
          strategy: 'range',
          partition_key: 'RANGE (at)',
          partitions: 12,
          direct_partitions: 12,
          depth: 1,
          bytes: 1048576,
          rows: 50000,
          default_partition: null,
          default_rows: null,
          invalid_indexes: [],
          analyzed_at: SEEN,
          covered_until: null,
          first_seen: SEEN,
          last_seen: SEEN,
        },
      ],
      truncated_databases: [],
    }),
    ok('getInstanceFunctions', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      databases: [],
      functions: [
        {
          datname: 'app',
          schemaname: 'public',
          name: 'audit_row',
          arguments: '',
          language: 'plpgsql',
          calls_per_second: 40.5,
          total_ms_per_second: 12.2,
          self_ms_per_second: 11.9,
          mean_ms: 0.3,
          last_seen: SEEN,
        },
      ],
    }),
    ok('getInstanceSecurityDefinerFunctions', {
      instance_id: INSTANCE_ID,
      functions: [
        {
          datname: 'app',
          schemaname: 'public',
          name: 'grant_role',
          arguments: 'r text',
          signature: 'public.grant_role(r text)',
          kind: 'function',
          language: 'plpgsql',
          owner: 'postgres',
          owner_superuser: true,
          public_execute: true,
          search_path: null,
          pg_temp_last: false,
          first_seen: SEEN,
          last_seen: SEEN,
        },
      ],
      truncated_databases: [],
    }),
  )
  if (noExtensions) {
    server.use(
      ok('getInstanceExtensions', {
        instance_id: INSTANCE_ID,
        extensions: [],
        truncated_databases: [],
      }),
    )
  }
  return renderWithProviders(<CatalogSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

const RELATION_ROW = { schemaname: 'public', relname: 'orders', ts: NOW }

function renderRelations(tables: Record<string, unknown>[], indexes: Record<string, unknown>[]) {
  const list = (items: Record<string, unknown>[], extra = {}) => ({
    instance_id: INSTANCE_ID,
    items,
    truncated: false,
    relations_not_reported: null,
    ...extra,
  })
  server.use(
    ok('getInstanceTables', list(tables)),
    ok('getInstanceIndexes', list(indexes)),
    http.get('*/api/v1/instances/:id/bloat', () =>
      HttpResponse.json({ ...list([]), skipped_tables: 0, skipped_relations: [] }),
    ),
  )
  return renderWithProviders(<RelationsSection instanceId={INSTANCE_ID} currentTier="T1" />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

function renderSettings(settings: Schemas['Setting'][]) {
  server.use(ok('getInstanceSettings', { instance_id: INSTANCE_ID, settings }))
  return renderWithProviders(<SettingsSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}?range=1h`,
  })
}

describe('instance captions', () => {
  it('CAPTION-PAGE-001 the Overview section, tiles and charts are captioned', async () => {
    const { container } = renderOverview()
    await settle()

    const ids = expectCaptionedCards(container)
    expect(ids).toContain('overview.section')
    expect(ids).toContain('overview.tx-rate')
    expect(ids).toContain('activity.connections-used')
    expect(ids).toContain('overview.chart-transactions')
    expect(ids).toContain('overview.chart-index-size')
    expectCaptionText('overview.section')
    expectCaptionText('overview.chart-transactions')
  })

  it('CAPTION-PAGE-002 the Transactions tile explains that it counts commits and rollbacks', async () => {
    renderOverview()
    await settle()

    const tile = screen.getByRole('article', { name: 'Transactions' })
    expect(tile).toHaveAttribute('data-caption-id', 'overview.tx-rate')
    expect(captionOf('overview.tx-rate').what).toContain('commits and rollbacks')
    expect(within(tile).getByRole('button', { name: 'About Transactions' })).toBeInTheDocument()
  })

  it('CAPTION-PAGE-003 the Host section, tiles and charts are captioned', async () => {
    const { container } = renderHost(HOST)
    await settle()

    const ids = expectCaptionedCards(container)
    expect(ids).toContain('host.section')
    expect(ids).toContain('host.cpu-used')
    expect(ids).toContain('host.disk-free-ratio')
    expect(ids).toContain('host.chart-cpu')
    expect(ids).toContain('host.chart-filesystem')
    expectCaptionText('host.section')
    expectCaptionText('host.chart-memory')
  })

  it('CAPTION-PAGE-004 the Internals tiles and every list are captioned', async () => {
    const { container } = renderInternals(INTERNALS)
    await settle()

    expect(expectCaptionedCards(container)).toEqual(
      expect.arrayContaining(['internals.wal-directory', 'internals.wal-replay']),
    )
    expect(expectCaptionedCards(container)).toContain('internals.database-sizes')
    expectCaptionText('internals.database-sizes')

    await openTab(/SLRU caches/)
    expect(expectCaptionedCards(container)).toContain('internals.slru')
    expectCaptionText('internals.slru')

    await openTab(/Temporary files/)
    expect(expectCaptionedCards(container)).toContain('internals.temp-files')

    await openTab(/Recovery conflicts/)
    expect(expectCaptionedCards(container)).toContain('internals.recovery-conflicts')
    expectCaptionText('internals.recovery-conflicts')
  })

  it('CAPTION-PAGE-005 the Logical replication lists are captioned', async () => {
    const { container } = renderLogical()
    await settle()

    expect(expectCaptionedCards(container)).toContain('logical.subscriptions')
    expectCaptionText('logical.subscriptions')

    await openTab(/Logical slots/)
    expect(expectCaptionedCards(container)).toContain('logical.slots')
    expectCaptionText('logical.slots')
  })

  it('CAPTION-PAGE-006 every Schema objects list is captioned', async () => {
    const { container } = renderCatalog()
    await settle()

    const lists: [RegExp, CaptionId][] = [
      [/^Triggers/, 'catalog.triggers'],
      [/^Sequences/, 'catalog.sequences'],
      [/^Partitions/, 'catalog.partitions'],
      [/^Functions/, 'catalog.functions'],
      [/^Security definer/, 'catalog.security-definer'],
      [/^Unindexed foreign keys/, 'catalog.foreign-keys'],
      [/^Extensions/, 'catalog.extensions'],
      [/^PostGIS/, 'catalog.spatial-columns'],
    ]
    for (const [tab, id] of lists) {
      await openTab(tab)
      expect(expectCaptionedCards(container), id).toContain(id)
      expectCaptionText(id)
    }
  })

  it('CAPTION-PAGE-007 the database scope section is captioned', () => {
    const { container } = renderWithProviders(
      <DatabaseSelector databases={[]} notMonitoredCount={0} />,
      { route: `/instances/${INSTANCE_ID}` },
    )

    expect(expectCaptionedCards(container)).toContain('instance.database-scope')
    expectCaptionText('instance.database-scope')
  })

  it('CAPTION-PAGE-008 every section of the page registry names a caption that exists', () => {
    for (const section of INSTANCE_SECTIONS) {
      if (section.captionId !== null) {
        expect(captionOf(section.captionId as CaptionId).what, section.id).not.toBe('')
      }
    }
  })

  it('CAPTION-PAGE-009 the Internals and Logical replication panels carry their section caption', async () => {
    const internals = renderInternals(INTERNALS)
    await settle()
    expect(expectCaptionedCards(internals.container)).toContain('internals.section')
    expectCaptionText('internals.section')
    internals.unmount()

    const logical = renderLogical()
    await settle()
    expect(expectCaptionedCards(logical.container)).toContain('logical.section')
    expectCaptionText('logical.section')
  })

  it('CAPTION-PAGE-010 the Internals lists keep their caption when they have nothing to list', async () => {
    const { container } = renderInternals({
      ...INTERNALS,
      temp_files: [],
      standby: { ...INTERNALS.standby!, conflicts: [] },
    })
    await settle()

    await openTab(/Temporary files/)
    expect(expectCaptionedCards(container)).toContain('internals.temp-files')
    expect(screen.getByText('No query outgrew work_mem in the last hour.')).toBeInTheDocument()

    await openTab(/Recovery conflicts/)
    expect(expectCaptionedCards(container)).toContain('internals.recovery-conflicts')
  })

  it('CAPTION-PAGE-011 the Schema objects panel is captioned', async () => {
    const { container } = renderCatalog()
    await settle()
    expect(expectCaptionedCards(container)).toContain('catalog.section')
    expectCaptionText('catalog.section')
  })

  it('CAPTION-PAGE-016 an empty Schema objects list keeps its caption', async () => {
    const { container } = renderCatalog({ noExtensions: true })
    await settle()

    await openTab(/^Extensions/)
    expect(expectCaptionedCards(container)).toContain('catalog.extensions')
    expectCaptionText('catalog.extensions')
  })

  it('CAPTION-PAGE-012 every Relations list and the panel are captioned', async () => {
    const { container } = renderRelations(
      [{ ...RELATION_ROW, n_live_tup: 10 }],
      [{ ...RELATION_ROW, indexrelname: 'orders_pkey', object_kind: 'index' }],
    )
    await settle()

    expect(expectCaptionedCards(container)).toEqual(
      expect.arrayContaining(['relations.section', 'relations.tables']),
    )
    expectCaptionText('relations.section')
    expectCaptionText('relations.tables')

    await openTab(/^Indexes/)
    expect(expectCaptionedCards(container)).toContain('relations.indexes')
    expectCaptionText('relations.indexes')

    await openTab(/^Table bloat/)
    expect(expectCaptionedCards(container)).toContain('relations.table-bloat')
    expectCaptionText('relations.table-bloat')

    await openTab(/^Index bloat/)
    expect(expectCaptionedCards(container)).toContain('relations.index-bloat')
    expectCaptionText('relations.index-bloat')
  })

  it('CAPTION-PAGE-013 the Relations lists keep their caption when empty and name the check window', async () => {
    const { container } = renderRelations([], [])
    await settle()

    expect(expectCaptionedCards(container)).toContain('relations.tables')
    await openTab(/^Indexes/)
    expect(expectCaptionedCards(container)).toContain('relations.indexes')
    expect(screen.getAllByText(/index_stats check/).length).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-014 the Settings panel and its change table are captioned', async () => {
    const changed = {
      name: 'work_mem',
      value: '4096',
      unit: 'kB',
      source: 'configuration file',
      context: 'user',
      pending_restart: 'false',
      first_seen: NOW,
      last_seen: NOW,
      changed_at: NOW,
    }
    const { container } = renderSettings([changed])
    await settle()

    const ids = expectCaptionedCards(container)
    expect(ids).toContain('instance.settings')
    expect(ids).toContain('instance.settings-changes')
    expectCaptionText('instance.settings')
    expect(screen.getByText('Changed since')).toBeVisible()
    for (const name of ['fsync', 'archive_mode', 'wal_level']) {
      expect(screen.getByRole('button', { name: `About ${name}` })).toBeInTheDocument()
    }
  })

  it('CAPTION-PAGE-015 the Settings change table keeps its caption when nothing changed', async () => {
    const { container } = renderSettings([])
    await settle()

    expect(expectCaptionedCards(container)).toContain('instance.settings-changes')
    expect(
      screen.getByText('No setting changed since the date in Changed since.'),
    ).toBeInTheDocument()
  })

  it('CARD-ALL-001 every Overview tile shows one figure and no bare second number', async () => {
    renderOverview()
    await settle()

    expect(tiles()).toHaveLength(8)
    for (const tile of tiles()) expectClearCard(tile)
    expect(screen.getByRole('article', { name: 'Connections in use' })).toHaveTextContent(
      'Out of a configured maximum of 100 connections.',
    )
  })

  it('CARD-ALL-001 every Host tile shows one figure and no bare second number', async () => {
    renderHost(HOST)
    await settle()

    expect(tiles()).toHaveLength(12)
    for (const tile of tiles()) expectClearCard(tile)
  })

  it('CARD-ALL-001 the Internals tiles show one figure and no bare second number', async () => {
    renderInternals(INTERNALS)
    await settle()

    expect(tiles()).toHaveLength(2)
    for (const tile of tiles()) expectClearCard(tile)
    expect(screen.getByRole('article', { name: 'WAL directory' })).toHaveTextContent(
      'Spread over 128 files.',
    )
  })
})
