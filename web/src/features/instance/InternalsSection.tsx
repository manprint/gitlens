import { useMemo } from 'react'

import { useInstanceInternals } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { MetricTile } from '@/components/layout/MetricTile'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { formatBytes, formatCount, formatPercent, formatRate } from '@/lib/format'

type DatabaseSize = Schemas['DatabaseSize']
type SLRUCache = Schemas['SLRUCache']
type TempFileRate = Schemas['TempFileRate']
type RecoveryConflictRate = Schemas['RecoveryConflictRate']

const perMinuteFormat = new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 })

function perMinute(perSecond: number): string {
  return `${perMinuteFormat.format(perSecond * 60)}/min`
}

function growth(bytes: number | null) {
  if (bytes === null) return <span className="text-muted-foreground">n/a</span>
  return bytes > 0 ? `+${formatBytes(bytes)}` : formatBytes(bytes)
}

function databaseColumns(): DataTableProps<DatabaseSize>['columns'] {
  return [
    { accessorKey: 'database', header: 'Database' },
    {
      accessorKey: 'size_bytes',
      header: 'Size',
      cell: ({ row }) => formatBytes(row.original.size_bytes),
    },
    {
      id: 'growth_24h',
      header: 'Last 24 hours',
      accessorFn: (db) => db.growth_24h_bytes ?? Number.NEGATIVE_INFINITY,
      cell: ({ row }) => growth(row.original.growth_24h_bytes),
    },
    {
      id: 'growth_7d',
      header: 'Last 7 days',
      accessorFn: (db) => db.growth_7d_bytes ?? Number.NEGATIVE_INFINITY,
      cell: ({ row }) => growth(row.original.growth_7d_bytes),
    },
  ]
}

function slruColumns(): DataTableProps<SLRUCache>['columns'] {
  return [
    { accessorKey: 'name', header: 'Cache' },
    {
      id: 'hit_ratio',
      header: 'Hit ratio',
      accessorFn: (cache) => cache.hit_ratio ?? -1,
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
    { accessorKey: 'database', header: 'Database' },
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
    { accessorKey: 'database', header: 'Database' },
    { accessorKey: 'reason', header: 'Reason' },
    {
      accessorKey: 'per_second',
      header: 'Queries cancelled',
      cell: ({ row }) => perMinute(row.original.per_second),
    },
  ]
}

function StandbyReplay({ standby }: { standby: Schemas['StandbyReplay'] }) {
  const conflicts = useMemo(() => conflictColumns(), [])
  const prefetch = standby.prefetch_blocks_per_second
  const prefetched = prefetch.prefetch
  const cached = prefetch.hit
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-medium">Recovery conflicts</h3>
      {standby.conflicts.length > 0 ? (
        <DataTable
          ariaLabel="Recovery conflicts"
          columns={conflicts}
          data={standby.conflicts}
          emptyState={null}
          scope="conflicts"
          total={standby.conflicts.length}
        />
      ) : (
        <p className="text-muted-foreground text-sm">
          Replay cancelled no standby query in the last hour.
        </p>
      )}
      {prefetched === undefined || cached === undefined ? null : (
        <p className="text-muted-foreground text-sm">
          WAL prefetch: {formatRate(prefetched)} blocks read ahead, {formatRate(cached)} already in
          shared buffers.
        </p>
      )}
    </div>
  )
}

export function InternalsSection({ instanceId }: { instanceId: string }) {
  const query = useInstanceInternals(instanceId)
  const databases = useMemo(() => databaseColumns(), [])
  const slru = useMemo(() => slruColumns(), [])
  const temp = useMemo(() => tempFileColumns(), [])

  let body
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
    body = (
      <div className="space-y-6">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <MetricTile
            label="WAL directory"
            value={walDirectory === null ? null : formatBytes(walDirectory.bytes)}
            unit={walDirectory === null ? null : `${formatCount(walDirectory.files)} files`}
          />
          {standby === null ? null : (
            <MetricTile
              label="WAL replay"
              value={
                standby.replay_paused ? (
                  <Badge variant="destructive">paused</Badge>
                ) : (
                  <Badge variant="outline">running</Badge>
                )
              }
            />
          )}
        </div>
        {walDirectory === null ? (
          <p className="text-muted-foreground text-sm">
            pg_wal is not listed: the monitoring role needs EXECUTE on pg_ls_waldir(), which
            pg_monitor grants.
          </p>
        ) : null}
        {query.data.databases.length > 0 ? (
          <div className="space-y-2">
            <h3 className="text-sm font-medium">Database sizes</h3>
            <DataTable
              ariaLabel="Database sizes"
              columns={databases}
              data={query.data.databases}
              emptyState={null}
              scope="databases"
              total={query.data.databases.length}
            />
          </div>
        ) : null}
        {query.data.slru.length > 0 ? (
          <div className="space-y-2">
            <h3 className="text-sm font-medium">SLRU caches</h3>
            <p className="text-muted-foreground text-sm">
              A page missing from a cache is read from disk under the cache&apos;s lock, which
              stalls every session that needs it: watch the subtransaction and multixact caches.
            </p>
            <DataTable
              ariaLabel="SLRU caches"
              columns={slru}
              data={query.data.slru}
              emptyState={null}
              scope="caches"
              total={query.data.slru.length}
            />
          </div>
        ) : null}
        <div className="space-y-2">
          <h3 className="text-sm font-medium">Temporary files</h3>
          {spilling.length > 0 ? (
            <DataTable
              ariaLabel="Temporary files"
              columns={temp}
              data={spilling}
              emptyState={null}
              scope="databases"
              total={spilling.length}
            />
          ) : (
            <p className="text-muted-foreground text-sm">
              No query outgrew work_mem in the last hour.
            </p>
          )}
        </div>
        {standby === null ? null : <StandbyReplay standby={standby} />}
      </div>
    )
  }

  return (
    <Section
      id="internals"
      title="Server internals"
      description="The WAL kept in pg_wal, database sizes, the SLRU caches, temporary files and, on a standby, WAL replay. Rates are averaged over the last hour."
    >
      {body}
    </Section>
  )
}
