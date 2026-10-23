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
  sequences: Schemas['Sequence'][] = [],
  partitioned: Schemas['PartitionedTable'][] = [],
  more: {
    functions?: Partial<Schemas['FunctionsResponse']>
    securityDefiner?: Schemas['SecurityDefinerFunction'][]
  } = {},
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
    ok('getInstanceSequences', {
      instance_id: INSTANCE_ID,
      sequences,
      truncated_databases: [],
    }),
    ok('getInstancePartitionedTables', {
      instance_id: INSTANCE_ID,
      tables: partitioned,
      truncated_databases: [],
    }),
    ok('getInstanceFunctions', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      databases: [],
      functions: [],
      ...more.functions,
    }),
    ok('getInstanceSecurityDefinerFunctions', {
      instance_id: INSTANCE_ID,
      functions: more.securityDefiner ?? [],
      truncated_databases: [],
    }),
  )
  return renderWithProviders(<CatalogSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

function sequence(overrides: Partial<Schemas['Sequence']> = {}): Schemas['Sequence'] {
  return {
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
    ...overrides,
  }
}

function partitionedTable(
  overrides: Partial<Schemas['PartitionedTable']> = {},
): Schemas['PartitionedTable'] {
  return {
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
    ...overrides,
  }
}

function functionStat(overrides: Partial<Schemas['FunctionStat']> = {}): Schemas['FunctionStat'] {
  return {
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
    ...overrides,
  }
}

function securityDefiner(
  overrides: Partial<Schemas['SecurityDefinerFunction']> = {},
): Schemas['SecurityDefinerFunction'] {
  return {
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
    ...overrides,
  }
}

function daysFromNow(days: number): string {
  return new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString()
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

  // UI-CAT-006: sequences are listed closest to their limit first, bigint
  // bounds are shown exactly, a column narrower than its sequence is marked,
  // and hidden values say how to make them readable.
  it('lists sequences by how much of their range they used', async () => {
    renderCatalog(
      [],
      [],
      [],
      [],
      [],
      [
        sequence(),
        sequence({
          name: 'wide',
          data_type: 'bigint',
          max: '9223372036854775807',
          last_value: '2000000000',
          limit: '2147483647',
          limit_source: 'column',
          used_ratio: 0.931323,
          remaining: '147483647',
          table: 'narrow',
          link: 'default',
        }),
        sequence({
          name: 'ring',
          cycle: true,
          exhausts: false,
          used_ratio: 0.99,
          last_value: '99',
          max: '100',
          limit: '100',
          remaining: '1',
          column: '',
          table: '',
          table_schema: '',
          column_type: '',
          link: '',
          columns: 0,
        }),
        sequence({
          name: 'secret',
          state: 'hidden',
          last_value: null,
          used_ratio: null,
          remaining: null,
        }),
        sequence({ name: 'fresh', state: 'unused', last_value: null, used_ratio: 0 }),
      ],
    )
    await settle()

    const tab = screen.getByRole('tab', { name: /Sequences/ })
    expect(tab).toHaveAccessibleName('Sequences 1 near their limit')
    fireEvent.mouseDown(tab)
    fireEvent.click(tab)
    await settle()
    const rows = tableRows()
    expect(rows.map((row) => within(row).getAllByRole('cell')[1]?.textContent)).toEqual([
      'public.ringcycles',
      'public.wide',
      'public.orders_id_seq',
      'public.fresh',
      'public.secret',
    ])
    expect(within(rows[1]!).getByText('93.1%')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('column type')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('2,147,483,647')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('99.0%')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('no column')).toBeInTheDocument()
    expect(within(rows[3]!).getByText('never called')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent(
      'The values of 1 sequence are not readable by the monitoring role',
    )
  })

  // UI-CAT-007: partition trees are listed whole; a default partition
  // holding rows, an invalid partitioned index and partitions about to run
  // out are marked, the last as ended once past, as the advisor does.
  it('lists partitioned tables with what needs attention', async () => {
    renderCatalog(
      [],
      [],
      [],
      [],
      [],
      [],
      [
        partitionedTable({
          default_partition: 'public.events_default',
          default_rows: 5,
          covered_until: daysFromNow(2),
        }),
        partitionedTable({
          relname: 'logs',
          strategy: 'list',
          partition_key: 'LIST (kind)',
          partitions: 2,
          depth: 2,
          invalid_indexes: ['logs_kind_idx'],
          analyzed_at: null,
        }),
        partitionedTable({ relname: 'forever', covered_until: 'unbounded' }),
        partitionedTable({ relname: 'old', covered_until: daysFromNow(-3) }),
        partitionedTable({ relname: 'planned', covered_until: daysFromNow(90) }),
        partitionedTable({ relname: 'archive', covered_until: daysFromNow(-400) }),
      ],
    )
    await settle()

    const tab = screen.getByRole('tab', { name: /Partitions/ })
    expect(tab).toHaveAccessibleName('Partitions 3 need attention')
    fireEvent.mouseDown(tab)
    fireEvent.click(tab)
    await settle()
    const rows = tableRows()
    expect(rows.map((row) => within(row).getAllByRole('cell')[1]?.textContent)).toEqual([
      'public.events',
      'public.logs',
      'public.forever',
      'public.old',
      'public.planned',
      'public.archive',
    ])
    expect(within(rows[0]!).getByText('5 rows')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('ends soon')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('2 in 2 levels')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('logs_kind_idx')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('never')).toBeInTheDocument()
    expect(within(rows[2]!).getByText('no end (MAXVALUE)')).toBeInTheDocument()
    expect(within(rows[3]!).getByText('ended')).toBeInTheDocument()
    expect(within(rows[4]!).queryByText(/ends soon|ended/)).not.toBeInTheDocument()
    // Long past, a table is taken as retired: shown, not counted.
    expect(within(rows[5]!).getByText('(ended)')).toBeInTheDocument()
  })

  // UI-CAT-008: function rates are listed with their mean per call, and a
  // database without tracking says so; SECURITY DEFINER functions show the
  // role they run as and whether they pin search_path, the exposed case
  // marked as the advisor's critical one.
  it('lists function rates and SECURITY DEFINER functions', async () => {
    renderCatalog([], [], [], [], [], [], [], {
      functions: {
        databases: [
          {
            datname: 'app',
            track_functions: 'pl',
            tracked_functions: 2,
            pl_functions: 2,
            last_seen: SEEN,
          },
          {
            datname: 'crm',
            track_functions: 'none',
            tracked_functions: 0,
            pl_functions: 4,
            last_seen: SEEN,
          },
        ],
        functions: [
          functionStat(),
          functionStat({
            name: 'f',
            arguments: 'x text',
            calls_per_second: 0,
            total_ms_per_second: 0,
            self_ms_per_second: 0,
            mean_ms: null,
          }),
        ],
      },
      securityDefiner: [
        securityDefiner(),
        securityDefiner({
          name: 'job',
          arguments: '',
          signature: 'public.job()',
          kind: 'procedure',
          owner: 'app',
          owner_superuser: false,
          public_execute: false,
        }),
        securityDefiner({
          name: 'safe',
          arguments: '',
          signature: 'public.safe()',
          search_path: 'public, pg_temp',
          pg_temp_last: true,
        }),
        securityDefiner({
          name: 'half',
          arguments: '',
          signature: 'public.half()',
          search_path: 'public',
        }),
      ],
    })
    await settle()

    const functionsTab = screen.getByRole('tab', { name: /^Functions/ })
    fireEvent.mouseDown(functionsTab)
    fireEvent.click(functionsTab)
    await settle()
    let rows = tableRows()
    expect(rows.map((row) => within(row).getAllByRole('cell')[1]?.textContent)).toEqual([
      'public.audit_row()',
      'public.f(x text)',
    ])
    expect(within(rows[0]!).getByText('41/s')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('11.9 ms/s')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('0.3 ms')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('not called')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent(
      'Function calls are not tracked in crm: track_functions is none.',
    )

    const secdefTab = screen.getByRole('tab', { name: /Security definer/ })
    expect(secdefTab).toHaveAccessibleName('Security definer 2 without a pinned search_path')
    fireEvent.mouseDown(secdefTab)
    fireEvent.click(secdefTab)
    await settle()
    rows = tableRows()
    expect(within(rows[0]!).getByText("caller's")).toBeInTheDocument()
    expect(within(rows[0]!).getByText('superuser')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('every role (PUBLIC)')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('procedure')).toBeInTheDocument()
    expect(within(rows[1]!).getByText('granted roles')).toBeInTheDocument()
    expect(within(rows[2]!).getByText('public, pg_temp')).toBeInTheDocument()
    expect(within(rows[2]!).queryByText('(pg_temp first)')).not.toBeInTheDocument()
    expect(within(rows[3]!).getByText('(pg_temp first)')).toBeInTheDocument()
  })
})
