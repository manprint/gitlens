import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { TabsList, TabsTrigger, Tabs } from '@/components/ui/tabs'
import { INSTANCE_ID, NOW } from '@/test/fixture-helpers'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { CatalogSection } from './CatalogSection'
import { InstanceHeader } from './InstanceHeader'
import { InstancePanel } from './InstancePanel'
import { InternalsSection } from './InternalsSection'
import { LogicalReplicationSection } from './LogicalReplicationSection'
import { RelationsSection } from './RelationsSection'
import { SettingsSection } from './SettingsSection'
import { readInstanceView } from './instanceViews'

// The instance tabs at Full HD (15.3, D26): a two-line header, one table per
// view with the view in the URL, and controls and notes on existing lines.

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

async function openView(name: string | RegExp) {
  const tab = screen.getByRole('tab', { name })
  fireEvent.mouseDown(tab)
  fireEvent.click(tab)
  await settle()
}

function panelActions(): HTMLElement {
  return screen.getByTestId('instance-panel-actions')
}

/** True when `later` comes after `earlier` in the document. */
function follows(earlier: Node, later: Node): boolean {
  return (earlier.compareDocumentPosition(later) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
}

function relation(overrides: Record<string, unknown> = {}) {
  return { datname: 'app', schemaname: 'public', relname: 'orders', ts: NOW, ...overrides }
}

function renderRelations(search = '') {
  const page = (items: Record<string, unknown>[], truncated = false) => ({
    instance_id: INSTANCE_ID,
    items,
    truncated,
    relations_not_reported: null,
  })
  server.use(
    ok('getInstanceTables', page([relation({ n_live_tup: 10 })], true)),
    ok('getInstanceIndexes', page([relation({ indexrelname: 'orders_pkey' })])),
    ok('getInstanceBloat', {
      ...page([relation({ object_kind: 'table', bloat_bytes: 2048, method: 'estimate' })]),
      skipped_tables: 2,
      skipped_relations: [{ datname: 'app', schemaname: 'public', relname: 'secrets' }],
    }),
  )
  return renderWithProviders(<RelationsSection instanceId={INSTANCE_ID} currentTier="T1" />, {
    route: `/instances/${INSTANCE_ID}${search}`,
  })
}

describe('Instance tabs at Full HD', () => {
  it('FHD-APPLY-040 the header holds the title and views, then the section tabs beside the metrics', () => {
    const instance = makeGetInstance() as unknown as Schemas['Instance']
    renderWithProviders(
      <Tabs value="overview">
        <InstanceHeader
          dataUpdatedAt={Date.parse(NOW)}
          instance={instance}
          replayLag={{ state: 'loading' }}
          sectionTabs={
            <TabsList aria-label="Instance sections">
              <TabsTrigger value="overview">Overview</TabsTrigger>
            </TabsList>
          }
        />
      </Tabs>,
      { route: `/instances/${INSTANCE_ID}` },
    )

    const header = screen.getByTestId('instance-header')
    expect(header).toHaveClass('space-y-5', 'min-[1600px]:grid', 'min-[1600px]:space-y-0')
    const sectionTabs = screen.getByTestId('instance-section-tabs')
    expect(sectionTabs).toHaveClass('mt-8', 'min-[1600px]:mt-0', 'min-[1600px]:row-start-2')
    expect(
      within(sectionTabs).getByRole('tablist', { name: 'Instance sections' }),
    ).toBeInTheDocument()
    expect(within(header).getByRole('navigation', { name: 'Instance views' })).toHaveClass(
      'min-[1600px]:col-start-3',
      'min-[1600px]:row-start-1',
    )
    // The metrics are one line: each label beside its value, the dt and dd kept.
    expect(within(header).getAllByRole('term')).toHaveLength(5)
    expect(within(header).getAllByRole('definition')).toHaveLength(5)
    expect(within(header).getByText('Role')).toHaveClass(
      'min-[1600px]:shrink-0',
      'min-[1600px]:text-[11px]',
    )
    expect(screen.getByRole('link', { name: instance.cluster_id })).toHaveAttribute(
      'title',
      instance.cluster_id,
    )
  })

  it('FHD-APPLY-041 the relation lists are views of one table each, the view in the URL', async () => {
    const view = renderRelations()
    await settle()

    expect(screen.getByRole('tablist', { name: 'Relation lists' })).toBeInTheDocument()
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table', { name: 'Tables' })).toBeInTheDocument()
    expect(view.router.state.location.search).toBe('')

    await openView('Indexes')
    expect(view.router.state.location.search).toBe('?view=indexes')
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table', { name: 'Indexes' })).toBeInTheDocument()

    // The default view is never written to the URL.
    await openView('Tables')
    expect(view.router.state.location.search).toBe('')
  })

  it('FHD-APPLY-042 a relation view opens from the URL; an unknown view falls back to Tables', async () => {
    renderRelations('?view=table-bloat')
    await settle()
    expect(screen.getByRole('tab', { name: 'Table bloat' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('table', { name: 'Table bloat estimates' })).toBeInTheDocument()
    // Both notes share one line above the table, each truncated with its full text.
    const skipped = screen.getByTitle(/Largest: app: public\.secrets/)
    expect(skipped).toHaveClass('min-[1600px]:truncate')
    expect(skipped).toHaveTextContent(/2 tables have no bloat estimate/)
    const exact = screen.getByText(/pglens never installs extensions/)
    expect(exact).toHaveAttribute('title', expect.stringMatching(/^An exact measurement runs/))
    expect(exact).toHaveClass('min-[1600px]:truncate')
    // The notes come before the table.
    expect(follows(exact, screen.getByRole('table', { name: 'Table bloat estimates' }))).toBe(true)
  })

  it('FHD-APPLY-043 the relation tables keep one line per row and the budget sits in the heading', async () => {
    renderRelations('?view=nonsense')
    await settle()

    expect(screen.getByRole('tab', { name: 'Tables' })).toHaveAttribute('aria-selected', 'true')
    const table = screen.getByRole('table', { name: 'Tables' })
    expect(table).toHaveClass('min-[1600px]:table-fixed')
    expect(within(table).getByTitle('public.orders')).toHaveClass('min-[1600px]:truncate')
    expect(screen.getByTestId('data-table-scroll')).toHaveClass(
      'min-[1600px]:[&_td]:px-2',
      'min-[1600px]:[&_thead_.size-6]:size-5',
    )
    expect(
      within(panelActions()).getByText(
        'Showing 1 of 50 tables shared per instance across databases (truncated; budget 50).',
      ),
    ).toBeInTheDocument()
    expect(within(panelActions()).getByText('checks.table_stats.top_n')).toBeInTheDocument()
  })

  it('FHD-APPLY-044 the settings heading holds the durability summary and the changed-since controls', async () => {
    server.use(
      ok('getInstanceSettings', {
        instance_id: INSTANCE_ID,
        settings: [
          {
            name: 'work_mem',
            value: '4096',
            unit: 'kB',
            source: 'configuration file',
            context: 'user',
            pending_restart: 'false',
            first_seen: NOW,
            last_seen: NOW,
            changed_at: NOW,
          },
        ],
      }),
    )
    renderWithProviders(<SettingsSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?range=1h`,
    })
    await settle()

    const actions = panelActions()
    expect(within(actions).getByRole('heading', { name: 'Durability summary' })).toHaveClass(
      'min-[1600px]:sr-only',
    )
    expect(within(actions).getByLabelText('Changed since')).toHaveClass('min-[1600px]:h-8')
    expect(within(actions).getByText('Changed since')).toHaveClass('min-[1600px]:sr-only')
    expect(within(actions).getByRole('button', { name: 'Use selected range' })).toBeInTheDocument()
    const table = screen.getByRole('table', { name: 'Observed PostgreSQL settings' })
    expect(table).toHaveClass('min-[1600px]:table-fixed')
    expect(within(table).getByTitle('work_mem')).toHaveClass('min-[1600px]:truncate')
  })

  it('FHD-APPLY-045 logical replication offers only the lists the instance has', async () => {
    const slot = {
      slot_name: 'orders_sub',
      bytes_per_second: 1024,
      txns_per_second: 1,
      spill_bytes_per_second: 0,
      spill_txns_per_second: 0,
      stream_bytes_per_second: 0,
      last_seen: NOW,
    }
    server.use(
      ok('getInstanceLogicalReplication', {
        instance_id: INSTANCE_ID,
        window_seconds: 3600,
        subscriptions: [],
        slots: [slot],
      }),
    )
    renderWithProviders(<LogicalReplicationSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}`,
    })
    await settle()

    expect(screen.queryByRole('tablist')).not.toBeInTheDocument()
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table', { name: 'Logical slots' })).toBeInTheDocument()
    // The decoding window is a note of the toolbar line, truncated with its full text.
    expect(
      screen.getByTitle('Decoding of each logical slot, averaged over the last 60 minutes.'),
    ).toHaveClass('min-[1600px]:truncate', 'min-[1600px]:basis-0')
  })

  it('FHD-APPLY-046 internals: the WAL tile sits in the heading and the SLRU hint in the toolbar line', async () => {
    server.use(
      ok('getInstanceInternals', {
        instance_id: INSTANCE_ID,
        window_seconds: 3600,
        wal_directory: { bytes: 1024 ** 3, files: 64, observed_at: NOW },
        databases: [
          {
            database: 'app',
            size_bytes: 1024 ** 3,
            observed_at: NOW,
            growth_24h_bytes: null,
            growth_7d_bytes: null,
          },
        ],
        slru: [
          {
            name: 'subtransaction',
            hit_ratio: 0.9,
            hits_per_second: 90,
            reads_per_second: 10,
            writes_per_second: 0,
            zeroed_per_second: 0,
            flushes_per_second: 0,
            truncates_per_second: 0,
            exists_per_second: 0,
            last_seen: NOW,
          },
        ],
        temp_files: [],
        standby: null,
      }),
    )
    const view = renderWithProviders(<InternalsSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?view=slru`,
    })
    await settle()

    const actions = panelActions()
    expect(within(actions).getByRole('article', { name: 'WAL directory' })).toHaveClass(
      'min-[1600px]:py-1',
    )
    expect(within(actions).getByRole('tablist', { name: 'Server internals lists' })).toBeVisible()
    expect(screen.getAllByRole('table')).toHaveLength(1)
    // The hint follows the search box on the toolbar line, before the table.
    const hint = screen.getByText(/A page missing from a cache/)
    expect(hint).toHaveClass('min-[1600px]:truncate')
    expect(follows(screen.getByRole('search', { name: 'SLRU caches search' }), hint)).toBe(true)
    expect(follows(hint, screen.getByRole('table', { name: 'SLRU caches' }))).toBe(true)

    await openView('Database sizes')
    expect(view.router.state.location.search).toBe('')
    expect(screen.getByRole('table', { name: 'Database sizes' })).toBeInTheDocument()
  })

  it('FHD-APPLY-048 schema objects: the kind is in the URL and the filters are on the toolbar line', async () => {
    const empty = { instance_id: INSTANCE_ID, truncated_databases: [] }
    server.use(
      ok('getInstanceTriggers', {
        ...empty,
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
            first_seen: NOW,
            last_seen: NOW,
            changed_at: NOW,
          },
        ],
      }),
      ok('getInstanceUnindexedForeignKeys', { ...empty, foreign_keys: [] }),
      ok('getInstanceExtensions', { ...empty, extensions: [] }),
      ok('getInstanceSpatialColumns', { ...empty, columns: [] }),
      ok('getInstanceSequences', { ...empty, sequences: [] }),
      ok('getInstancePartitionedTables', { ...empty, tables: [] }),
      ok('getInstanceFunctions', {
        instance_id: INSTANCE_ID,
        window_seconds: 3600,
        databases: [],
        functions: [],
      }),
      ok('getInstanceSecurityDefinerFunctions', { ...empty, functions: [] }),
    )
    const view = renderWithProviders(<CatalogSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?view=foreign-keys`,
    })
    await settle()

    expect(screen.getByRole('tab', { name: /^Unindexed foreign keys/ })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    // No row, no toolbar: the filter and the hint stay above the empty state.
    expect(screen.getByRole('combobox', { name: 'Database' })).toBeInTheDocument()
    expect(screen.getByText(/scans the whole referencing table/)).toHaveAttribute(
      'title',
      expect.stringMatching(/^Deleting a referenced row/),
    )

    await openView(/^Triggers/)
    expect(view.router.state.location.search).toBe('')
    // With rows the filters move onto the toolbar line, after the search box.
    const search = screen.getByRole('search', { name: 'Triggers search' })
    const database = screen.getByRole('combobox', { name: 'Database' })
    const disabledOnly = screen.getByRole('checkbox', { name: 'Disabled only' })
    expect(follows(search, database)).toBe(true)
    expect(follows(search, disabledOnly)).toBe(true)
    expect(follows(disabledOnly, screen.getByRole('table', { name: 'Triggers' }))).toBe(true)
    expect(database).toHaveClass('h-8')
    expect(screen.getByText('Database', { selector: 'label > span' })).toHaveClass(
      'min-[1600px]:sr-only',
    )
  })

  it('FHD-APPLY-049 the panel is flat from 1600 px and a view value falls back to the first view', () => {
    render(
      <InstancePanel actions={<button type="button">Act</button>} description="Long hint" title="T">
        <p>Body</p>
      </InstancePanel>,
    )
    const panel = screen.getByRole('region', { name: 'T' })
    expect(panel).toHaveClass('rounded-xl', 'border', 'min-[1600px]:border-0')
    expect(screen.getByText('Long hint')).toHaveAttribute('title', 'Long hint')
    expect(screen.getByText('Long hint')).toHaveClass('min-[1600px]:truncate')

    const views = [{ id: 'a' }, { id: 'b' }] as const
    expect(readInstanceView(views, null)).toBe('a')
    expect(readInstanceView(views, 'b')).toBe('b')
    expect(readInstanceView(views, 'nonsense')).toBe('a')
    expect(readInstanceView([], 'b')).toBeUndefined()
  })
})
