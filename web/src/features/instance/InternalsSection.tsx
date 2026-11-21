import { useMemo, type ReactNode } from 'react'

import { useInstanceInternals } from '@/api/queries'
import type { Schemas } from '@/api/types'
import {
  DataTable,
  type DataTableProps,
  type DataTableToolbar,
} from '@/components/layout/DataTable'
import { HelpLink } from '@/components/layout/HelpLink'
import { MetricTile } from '@/components/layout/MetricTile'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
import { formatBytes, formatCount, formatPercent, formatRate } from '@/lib/format'

import { CaptionedBlock, InstancePanel, InstanceViewTabs, ToolbarNote } from './InstancePanel'
import { INTERNALS_VIEW_IDS } from './instanceSections'
import { useInstanceView, type InstanceView } from './instanceViews'

type InternalsViewId = (typeof INTERNALS_VIEW_IDS)[number]

const WAL_NOT_LISTED: MessageKey = 'instance.internals.walNotListed'
const SLRU_HINT: MessageKey = 'instance.internals.slruHint'

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

function growth(bytes: number | null, t: Translate) {
  if (bytes === null)
    return <span className="text-muted-foreground">{t('instance.internals.notAvailable')}</span>
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
function conflictToolbar(t: Translate): DataTableToolbar<RecoveryConflictRate> {
  return {
    searchFields: [(rate) => rate.database, (rate) => rate.reason],
    filters: [
      {
        id: 'database',
        label: t('instance.internals.col.database'),
        kind: 'select',
        accessor: (rate) => rate.database,
      },
    ],
  }
}

function databaseColumns(t: Translate): DataTableProps<DatabaseSize>['columns'] {
  return [
    {
      accessorKey: 'database',
      header: t('instance.internals.col.database'),
      meta: { truncate: true },
    },
    {
      accessorKey: 'size_bytes',
      header: t('instance.internals.col.size'),
      cell: ({ row }) => formatBytes(row.original.size_bytes),
    },
    {
      id: 'growth_24h',
      header: t('instance.internals.col.growth24h'),
      accessorFn: (db) => db.growth_24h_bytes,
      cell: ({ row }) => growth(row.original.growth_24h_bytes, t),
    },
    {
      id: 'growth_7d',
      header: t('instance.internals.col.growth7d'),
      accessorFn: (db) => db.growth_7d_bytes,
      cell: ({ row }) => growth(row.original.growth_7d_bytes, t),
    },
  ]
}

function slruColumns(t: Translate): DataTableProps<SLRUCache>['columns'] {
  return [
    { accessorKey: 'name', header: t('instance.internals.col.cache'), meta: { truncate: true } },
    {
      id: 'hit_ratio',
      header: t('instance.internals.col.hitRatio'),
      accessorFn: (cache) => cache.hit_ratio,
      cell: ({ row }) =>
        formatPercent(
          row.original.hits_per_second,
          row.original.hits_per_second + row.original.reads_per_second,
        ) ?? <span className="text-muted-foreground">{t('instance.internals.noReads')}</span>,
    },
    {
      accessorKey: 'reads_per_second',
      header: t('instance.internals.col.readFromDisk'),
      cell: ({ row }) => formatRate(row.original.reads_per_second),
    },
    {
      accessorKey: 'writes_per_second',
      header: t('instance.internals.col.written'),
      cell: ({ row }) => formatRate(row.original.writes_per_second),
    },
    {
      accessorKey: 'zeroed_per_second',
      header: t('instance.internals.col.zeroed'),
      meta: { help: 'internals.slru-zeroed' },
      cell: ({ row }) => formatRate(row.original.zeroed_per_second),
    },
    {
      accessorKey: 'flushes_per_second',
      header: t('instance.internals.col.flushes'),
      meta: { help: 'internals.slru-flushes' },
      cell: ({ row }) => formatRate(row.original.flushes_per_second),
    },
    {
      accessorKey: 'truncates_per_second',
      header: t('instance.internals.col.truncations'),
      meta: { help: 'internals.slru-truncations' },
      cell: ({ row }) => formatRate(row.original.truncates_per_second),
    },
  ]
}

function tempFileColumns(t: Translate): DataTableProps<TempFileRate>['columns'] {
  return [
    {
      accessorKey: 'database',
      header: t('instance.internals.col.database'),
      meta: { truncate: true },
    },
    {
      accessorKey: 'files_per_second',
      header: t('instance.internals.col.files'),
      meta: { help: 'internals.temp-files-rate' },
      cell: ({ row }) => perMinute(row.original.files_per_second),
    },
    {
      accessorKey: 'bytes_per_second',
      header: t('instance.internals.col.written'),
      cell: ({ row }) => `${formatBytes(row.original.bytes_per_second)}/s`,
    },
  ]
}

function conflictColumns(t: Translate): DataTableProps<RecoveryConflictRate>['columns'] {
  return [
    {
      accessorKey: 'database',
      header: t('instance.internals.col.database'),
      meta: { truncate: true },
    },
    { accessorKey: 'reason', header: t('instance.internals.col.reason'), meta: { truncate: true } },
    {
      accessorKey: 'per_second',
      header: t('instance.internals.col.queriesCancelled'),
      meta: { help: 'internals.recovery-cancelled' },
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
  const t = useT()
  const conflicts = useMemo(() => conflictColumns(t), [t])
  const toolbar = useMemo(() => conflictToolbar(t), [t])
  const prefetch = standby.prefetch_blocks_per_second
  const prefetched = prefetch.prefetch
  const cached = prefetch.hit
  const notes =
    prefetched === undefined || cached === undefined
      ? walNotes
      : [
          ...walNotes,
          t('instance.internals.walPrefetch', {
            read: formatRate(prefetched),
            cached: formatRate(cached),
          }),
        ]
  if (standby.conflicts.length === 0) {
    return (
      <CaptionedBlock
        ariaLabel={t('instance.internals.recoveryConflicts')}
        captionId="internals.recovery-conflicts"
      >
        <div className="space-y-2">
          <p className="text-muted-foreground text-sm">{t('instance.internals.replayCancelled')}</p>
          {notes.map((note) => (
            <p key={note} className="text-muted-foreground text-sm">
              {note}
            </p>
          ))}
        </div>
      </CaptionedBlock>
    )
  }
  return (
    <DataTable
      ariaLabel={t('instance.internals.recoveryConflicts')}
      captionId="internals.recovery-conflicts"
      tableId="recovery-conflicts"
      toolbar={toolbar}
      {...(notes.length === 0 ? {} : { toolbarExtra: notesOf(notes) })}
      columns={conflicts}
      data={standby.conflicts}
      emptyState={null}
      scope={t('instance.internals.noun.conflicts')}
      total={standby.conflicts.length}
    />
  )
}

export function InternalsSection({ instanceId }: { instanceId: string }) {
  const t = useT()
  const query = useInstanceInternals(instanceId)
  const databases = useMemo(() => databaseColumns(t), [t])
  const slru = useMemo(() => slruColumns(t), [t])
  const temp = useMemo(() => tempFileColumns(t), [t])
  // One table per view (15.3): only the lists the instance has are offered.
  const views: InstanceView<InternalsViewId>[] = []
  if (query.data) {
    if (query.data.databases.length > 0)
      views.push({ id: 'databases', label: t('instance.internals.databaseSizes') })
    if (query.data.slru.length > 0) views.push({ id: 'slru', label: t('instance.internals.slru') })
    views.push({ id: 'temp', label: t('instance.internals.tempFiles') })
    if (query.data.standby !== null)
      views.push({ id: 'replay', label: t('instance.internals.recoveryConflicts') })
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
      <section aria-busy="true" aria-label={t('instance.internals.loadingAria')} role="status">
        {t('instance.internals.loading')}
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
        title={t('instance.internals.noCollected')}
        description={t('instance.internals.noCollectedDescription')}
      />
    )
  } else {
    const { standby, temp_files: tempFiles, wal_directory: walDirectory } = query.data
    const spilling = tempFiles.filter((row) => row.files_per_second > 0 || row.bytes_per_second > 0)
    const walNotes = walDirectory === null ? [t(WAL_NOT_LISTED)] : []
    tiles = (
      <>
        <MetricTile
          className={TILE_IN_HEADING}
          label={t('instance.internals.walDirectory')}
          captionId="internals.wal-directory"
          value={walDirectory === null ? null : formatBytes(walDirectory.bytes)}
          {...(walDirectory === null
            ? {}
            : {
                detail: t('instance.internals.spreadOverFiles', {
                  count: formatCount(walDirectory.files),
                }),
              })}
        />
        {standby === null ? null : (
          <MetricTile
            className={TILE_IN_HEADING}
            label={t('instance.internals.walReplay')}
            captionId="internals.wal-replay"
            value={
              standby.replay_paused ? (
                <Badge variant="destructive">{t('instance.internals.paused')}</Badge>
              ) : (
                <Badge variant="outline">{t('instance.internals.running')}</Badge>
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
            ariaLabel={t('instance.internals.databaseSizes')}
            captionId="internals.database-sizes"
            tableId="database-sizes"
            toolbar={DATABASE_SIZE_TOOLBAR}
            {...(walNotes.length === 0 ? {} : { toolbarExtra: notesOf(walNotes) })}
            columns={databases}
            data={query.data.databases}
            emptyState={null}
            scope={t('instance.internals.noun.databases')}
            total={query.data.databases.length}
          />
        </TabsContent>
        <TabsContent value="slru">
          <DataTable
            ariaLabel={t('instance.internals.slru')}
            captionId="internals.slru"
            tableId="slru-caches"
            toolbar={SLRU_TOOLBAR}
            toolbarExtra={notesOf([...walNotes, t(SLRU_HINT)])}
            columns={slru}
            data={query.data.slru}
            emptyState={null}
            scope={t('instance.internals.noun.caches')}
            total={query.data.slru.length}
          />
        </TabsContent>
        <TabsContent value="temp">
          {spilling.length > 0 ? (
            <DataTable
              ariaLabel={t('instance.internals.tempFiles')}
              captionId="internals.temp-files"
              tableId="temporary-files"
              toolbar={TEMP_FILE_TOOLBAR}
              {...(walNotes.length === 0 ? {} : { toolbarExtra: notesOf(walNotes) })}
              columns={temp}
              data={spilling}
              emptyState={null}
              scope={t('instance.internals.noun.databases')}
              total={spilling.length}
            />
          ) : (
            <CaptionedBlock
              ariaLabel={t('instance.internals.tempFiles')}
              captionId="internals.temp-files"
            >
              <div className="space-y-2">
                <p className="text-muted-foreground text-sm">{t('instance.internals.noOutgrew')}</p>
                {walNotes.map((note) => (
                  <p key={note} className="text-muted-foreground text-sm">
                    {note}
                  </p>
                ))}
              </div>
            </CaptionedBlock>
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
        title={t('instance.internals.section')}
        captionId="internals.section"
        actions={
          <>
            {tiles === null ? null : (
              <>
                {tiles}
                <InstanceViewTabs label={t('instance.internals.viewsLabel')} views={views} />
              </>
            )}
            <HelpLink
              anchor="instance-overview#internals"
              label={t('instance.internals.section')}
            />
          </>
        }
      >
        {body}
      </InstancePanel>
    </Tabs>
  )
}
