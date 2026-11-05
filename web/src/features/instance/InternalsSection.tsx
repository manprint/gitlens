import { useMemo, type ReactNode } from 'react'

import { useInstanceInternals } from '@/api/queries'
import type { Schemas } from '@/api/types'
import {
  DataTable,
  type DataTableProps,
  type DataTableToolbar,
} from '@/components/layout/DataTable'
import { MetricTile } from '@/components/layout/MetricTile'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import { formatBytes, formatCount, formatPercent, formatRate } from '@/lib/format'

import { InstancePanel, InstanceViewTabs, ToolbarNote } from './InstancePanel'
import { INTERNALS_VIEW_IDS } from './instanceSections'
import { useInstanceView, type InstanceView } from './instanceViews'

type InternalsViewId = (typeof INTERNALS_VIEW_IDS)[number]

const WAL_NOT_LISTED =
  'pg_wal is not listed: the monitoring role needs EXECUTE on pg_ls_waldir(), which pg_monitor grants.'
const SLRU_HINT =
  "A page missing from a cache is read from disk under the cache's lock, which stalls every session that needs it: watch the subtransaction and multixact caches."

type DatabaseSize = Schemas['DatabaseSize']
type SLRUCache = Schemas['SLRUCache']
type TempFileRate = Schemas['TempFileRate']
type RecoveryConflictRate = Schemas['RecoveryConflictRate']

/** A WAL tile on the panel's heading line from 1600 px: one compact line. */
const TILE_IN_HEADING = 'min-[1600px]:py-1'

const perMinuteFormat = new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 })

function perMinute(perSecond: number): string {
  return `${perMinuteFormat.format(perSecond * 60)}/min`
}

function growth(bytes: number | null) {
  if (bytes === null) return <span className="text-muted-foreground">n/a</span>
  return bytes > 0 ? `+${formatBytes(bytes)}` : formatBytes(bytes)
}

// The database is the identity of the sizes and temporary-files rows: search only.
const DATABASE_SIZE_TOOLBAR: DataTableToolbar<DatabaseSize> = {
  searchFields: [(db) => db.database],
}
const SLRU_TOOLBAR: DataTableToolbar<SLRUCache> = { searchFields: [(cache) => cache.name] }
const TEMP_FILE_TOOLBAR: DataTableToolbar<TempFileRate> = {
  searchFields: [(rate) => rate.database],
}
const CONFLICT_TOOLBAR: DataTableToolbar<RecoveryConflictRate> = {
  searchFields: [(rate) => rate.database, (rate) => rate.reason],
  filters: [
    { id: 'database', label: 'Database', kind: 'select', accessor: (rate) => rate.database },
  ],
}

function databaseColumns(): DataTableProps<DatabaseSize>['columns'] {
  return [
    { accessorKey: 'database', header: 'Database', meta: { truncate: true } },
    {
      accessorKey: 'size_bytes',
      header: 'Size',
      cell: ({ row }) => formatBytes(row.original.size_bytes),
    },
    {
      id: 'growth_24h',
      header: 'Last 24 hours',
      accessorFn: (db) => db.growth_24h_bytes,
      cell: ({ row }) => growth(row.original.growth_24h_bytes),
    },
    {
      id: 'growth_7d',
      header: 'Last 7 days',
      accessorFn: (db) => db.growth_7d_bytes,
      cell: ({ row }) => growth(row.original.growth_7d_bytes),
    },
  ]
}

function slruColumns(): DataTableProps<SLRUCache>['columns'] {
  return [
    { accessorKey: 'name', header: 'Cache', meta: { truncate: true } },
    {
      id: 'hit_ratio',
      header: 'Hit ratio',
      accessorFn: (cache) => cache.hit_ratio,
      cell: ({ row }) =>
        formatPercent(
          row.original.hits_per_second,
          row.original.hits_per_second + row.original.reads_per_second,
        ) ?? <span className="text-muted-foreground">not read</span>,
    },
    {
      accessorKey: 'reads_per_second',
      header: 'Read from disk',
      cell: ({ row }) => formatRate(row.original.reads_per_second),
    },
    {
      accessorKey: 'writes_per_second',
      header: 'Written',
      cell: ({ row }) => formatRate(row.original.writes_per_second),
    },
    {
      accessorKey: 'zeroed_per_second',
      header: 'Zeroed',
      cell: ({ row }) => formatRate(row.original.zeroed_per_second),
    },
    {
      accessorKey: 'flushes_per_second',
      header: 'Flushes',
      cell: ({ row }) => formatRate(row.original.flushes_per_second),
    },
    {
      accessorKey: 'truncates_per_second',
      header: 'Truncations',
      cell: ({ row }) => formatRate(row.original.truncates_per_second),
    },
  ]
}

function tempFileColumns(): DataTableProps<TempFileRate>['columns'] {
  return [
    { accessorKey: 'database', header: 'Database', meta: { truncate: true } },
    {
      accessorKey: 'files_per_second',
      header: 'Files',
      cell: ({ row }) => perMinute(row.original.files_per_second),
    },
    {
      accessorKey: 'bytes_per_second',
      header: 'Written',
      cell: ({ row }) => `${formatBytes(row.original.bytes_per_second)}/s`,
    },
  ]
}

function conflictColumns(): DataTableProps<RecoveryConflictRate>['columns'] {
  return [
    { accessorKey: 'database', header: 'Database', meta: { truncate: true } },
    { accessorKey: 'reason', header: 'Reason', meta: { truncate: true } },
    {
      accessorKey: 'per_second',
      header: 'Queries cancelled',
      cell: ({ row }) => perMinute(row.original.per_second),
    },
  ]
}

/** The notes shown in a table's toolbar line, or above the view when it has no table. */
function notesOf(notes: readonly string[]) {
  return notes.length === 0 ? undefined : (
    <>
      {notes.map((note) => (
        <ToolbarNote key={note}>{note}</ToolbarNote>
      ))}
    </>
  )
}

function StandbyReplay({
  standby,
  walNotes,
}: {
  standby: Schemas['StandbyReplay']
  walNotes: readonly string[]
}) {
  const conflicts = useMemo(() => conflictColumns(), [])
  const prefetch = standby.prefetch_blocks_per_second
  const prefetched = prefetch.prefetch
  const cached = prefetch.hit
  const notes =
    prefetched === undefined || cached === undefined
      ? walNotes
      : [
          ...walNotes,
          `WAL prefetch: ${formatRate(prefetched)} blocks read ahead, ${formatRate(cached)} already in shared buffers.`,
        ]
  if (standby.conflicts.length === 0) {
    return (
      <div className="space-y-2">
        <p className="text-muted-foreground text-sm">
          Replay cancelled no standby query in the last hour.
        </p>
        {notes.map((note) => (
          <p key={note} className="text-muted-foreground text-sm">
            {note}
          </p>
        ))}
      </div>
    )
  }
  return (
    <DataTable
      ariaLabel="Recovery conflicts"
      captionId="internals.recovery-conflicts"
      tableId="recovery-conflicts"
      toolbar={CONFLICT_TOOLBAR}
      {...(notes.length === 0 ? {} : { toolbarExtra: notesOf(notes) })}
      columns={conflicts}
      data={standby.conflicts}
      emptyState={null}
      scope="conflicts"
      total={standby.conflicts.length}
    />
  )
}

export function InternalsSection({ instanceId }: { instanceId: string }) {
  const query = useInstanceInternals(instanceId)
  const databases = useMemo(() => databaseColumns(), [])
  const slru = useMemo(() => slruColumns(), [])
  const temp = useMemo(() => tempFileColumns(), [])
  // One table per view (15.3): only the lists the instance has are offered.
  const views: InstanceView<InternalsViewId>[] = []
  if (query.data) {
    if (query.data.databases.length > 0) views.push({ id: 'databases', label: 'Database sizes' })
    if (query.data.slru.length > 0) views.push({ id: 'slru', label: 'SLRU caches' })
    views.push({ id: 'temp', label: 'Temporary files' })
    if (query.data.standby !== null) views.push({ id: 'replay', label: 'Recovery conflicts' })
  }
  const [view, showView] = useInstanceView(views)

  let body: ReactNode
  // From 1600 px the WAL tiles share the heading line with the views (D26).
  let tiles: ReactNode = null
  if (query.error && !query.data) {
    body = (
      <ErrorState
        endpoint="instance internals"
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  } else if (query.isPending || query.data === undefined) {
    body = (
      <section aria-busy="true" aria-label="Loading server internals" role="status">
        Loading server internals…
      </section>
    )
  } else if (
    query.data.wal_directory === null &&
    query.data.databases.length === 0 &&
    query.data.slru.length === 0 &&
    query.data.temp_files.length === 0 &&
    query.data.standby === null
  ) {
    body = (
      <EmptyState
        title="No internals collected"
        description="The wal_directory, database_size, slru, database_stats and recovery checks have reported nothing in the last hour."
      />
    )
  } else {
    const { standby, temp_files: tempFiles, wal_directory: walDirectory } = query.data
    const spilling = tempFiles.filter((row) => row.files_per_second > 0 || row.bytes_per_second > 0)
    const walNotes = walDirectory === null ? [WAL_NOT_LISTED] : []
    tiles = (
      <>
        <MetricTile
          className={TILE_IN_HEADING}
          label="WAL directory"
          captionId="internals.wal-directory"
          value={walDirectory === null ? null : formatBytes(walDirectory.bytes)}
          {...(walDirectory === null
            ? {}
            : { detail: `Spread over ${formatCount(walDirectory.files)} files.` })}
        />
        {standby === null ? null : (
          <MetricTile
            className={TILE_IN_HEADING}
            label="WAL replay"
            captionId="internals.wal-replay"
            value={
              standby.replay_paused ? (
                <Badge variant="destructive">paused</Badge>
              ) : (
                <Badge variant="outline">running</Badge>
              )
            }
          />
        )}
      </>
    )
    body = (
      <>
        <TabsContent value="databases">
          <DataTable
            ariaLabel="Database sizes"
            captionId="internals.database-sizes"
            tableId="database-sizes"
            toolbar={DATABASE_SIZE_TOOLBAR}
            {...(walNotes.length === 0 ? {} : { toolbarExtra: notesOf(walNotes) })}
            columns={databases}
            data={query.data.databases}
            emptyState={null}
            scope="databases"
            total={query.data.databases.length}
          />
        </TabsContent>
        <TabsContent value="slru">
          <DataTable
            ariaLabel="SLRU caches"
            captionId="internals.slru"
            tableId="slru-caches"
            toolbar={SLRU_TOOLBAR}
            toolbarExtra={notesOf([...walNotes, SLRU_HINT])}
            columns={slru}
            data={query.data.slru}
            emptyState={null}
            scope="caches"
            total={query.data.slru.length}
          />
        </TabsContent>
        <TabsContent value="temp">
          {spilling.length > 0 ? (
            <DataTable
              ariaLabel="Temporary files"
              captionId="internals.temp-files"
              tableId="temporary-files"
              toolbar={TEMP_FILE_TOOLBAR}
              {...(walNotes.length === 0 ? {} : { toolbarExtra: notesOf(walNotes) })}
              columns={temp}
              data={spilling}
              emptyState={null}
              scope="databases"
              total={spilling.length}
            />
          ) : (
            <div className="space-y-2">
              <p className="text-muted-foreground text-sm">
                No query outgrew work_mem in the last hour.
              </p>
              {walNotes.map((note) => (
                <p key={note} className="text-muted-foreground text-sm">
                  {note}
                </p>
              ))}
            </div>
          )}
        </TabsContent>
        {standby === null ? null : (
          <TabsContent value="replay">
            <StandbyReplay standby={standby} walNotes={walNotes} />
          </TabsContent>
        )}
      </>
    )
  }

  return (
    <Tabs onValueChange={showView} value={view ?? ''}>
      <InstancePanel
        id="internals"
        title="Server internals"
        description="The WAL kept in pg_wal, database sizes, the SLRU caches, temporary files and, on a standby, WAL replay. Rates are averaged over the last hour."
        actions={
          tiles === null ? undefined : (
            <>
              {tiles}
              <InstanceViewTabs label="Server internals lists" views={views} />
            </>
          )
        }
      >
        {body}
      </InstancePanel>
    </Tabs>
  )
}
