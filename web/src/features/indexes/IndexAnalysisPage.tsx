import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Database, ListTree, Server } from 'lucide-react'

import { useClusters, useInstanceIndexes } from '@/api/queries'
import { useFindings } from '@/api/queries'
import type { Cluster } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { EmptyState, ErrorState } from '@/components/state'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { buildIndexRows, foreignKeyGaps } from '@/lib/indexAnalysis'
import { clusterLabel, instanceLabel } from '@/lib/names'
import { chooseInstance, readLastInstance } from '@/lib/lastInstance'

import { CleanupPanel, type SiblingServer } from './CleanupPanel'
import { DeadRowsPanel } from './DeadRowsPanel'
import { IndexesPanel } from './IndexesPanel'
import { noIndexStatsText } from './emptyText'
import { IndexTaxPanel } from './IndexTaxPanel'
import { SearchEverythingBox, SearchEverythingResults } from './SearchEverything'
import { SuggestionsPanel } from './SuggestionsPanel'
import { INDEX_TABLE_IDS, INDEX_VIEW_PARAM } from './indexViews'

/** The most indexes the API returns for one server. */
export const INDEX_LIMIT = 500

export const TABS = [
  { value: 'indexes', label: 'Indexes' },
  { value: 'cleanup', label: 'Cleanup' },
  { value: 'dead-rows', label: 'Dead rows' },
  { value: 'suggestions', label: 'Suggestions' },
  { value: 'tax', label: 'Index tax' },
] as const

type TabValue = (typeof TABS)[number]['value']

const DEFAULT_TAB: TabValue = 'indexes'

function readTab(value: string | null): TabValue {
  return TABS.find((tab) => tab.value === value)?.value ?? DEFAULT_TAB
}

/** Every table of the page: a database change sends each back to its first page (D23). */
const TABLE_IDS = [...INDEX_TABLE_IDS, 'cleanup', 'dead-rows', 'suggestions', 'tax']

interface InstanceOption {
  id: string
  label: string
  up: boolean
}

function instanceOptions(clusters: readonly Cluster[]): InstanceOption[] {
  return clusters.flatMap((cluster) =>
    cluster.instances.map((instance) => ({
      id: instance.instance_id,
      label: `${instanceLabel(instance)}${cluster.name ? ` · ${clusterLabel(cluster)}` : ''}`,
      up: instance.up,
    })),
  )
}

function siblingServers(clusters: readonly Cluster[], instanceId: string): SiblingServer[] {
  const cluster = clusters.find((candidate) =>
    candidate.instances.some((instance) => instance.instance_id === instanceId),
  )
  return (cluster?.instances ?? [])
    .filter((instance) => instance.instance_id !== instanceId)
    .map((instance) => ({ id: instance.instance_id, label: instanceLabel(instance) }))
}

/**
 * The page title. With a server picked, its actions hold the tab list, the
 * server and database selects and the freshness, on the title line from
 * 1600 px (D26), so each tab's table has the height of the screen.
 */
function IndexPageHeader({ actions }: { actions?: ReactNode }) {
  return (
    <PageHeader
      eyebrow="Index management"
      icon={ListTree}
      subtitle="Every index the agent reports for a server: how many there are, which columns they cover, how big they are and how they are used, with the advisor's findings on unused, duplicate, redundant and invalid indexes and on foreign keys that lack one."
      title="Index analysis"
      wrapActionsBelowFullHd
      {...(actions === undefined ? {} : { actions })}
    />
  )
}

interface AnalysisProps {
  /** The server select. */
  serverControl: ReactNode
  instanceId: string
  /** The other servers of the same cluster. */
  siblings: readonly SiblingServer[]
}

function InstanceIndexes({ serverControl, instanceId, siblings }: AnalysisProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const database = searchParams.get('db') ?? ''
  const tab = readTab(searchParams.get('tab'))

  // A search hit opens its row (D20): `focus` names the row key. The open
  // tab's table turns to the row and expands it, then the key leaves the URL.
  // The page shows current metrics only, so a row may be gone: say so.
  const focusKey = searchParams.get('focus') ?? undefined
  const [missing, setMissing] = useState<{ key: string; tab: TabValue } | null>(null)
  if (focusKey !== undefined && missing !== null && missing.key !== focusKey) setMissing(null)
  const focusDone = useCallback(
    (found: boolean) => {
      setSearchParams(
        (current) => {
          const next = new URLSearchParams(current)
          next.delete('focus')
          return next
        },
        { replace: true },
      )
      setMissing(found || focusKey === undefined ? null : { key: focusKey, tab })
    },
    [focusKey, setSearchParams, tab],
  )

  const indexesQuery = useInstanceIndexes(instanceId, { limit: INDEX_LIMIT })
  const findingsQuery = useFindings({ instance_id: instanceId, limit: 1000 })
  const response = indexesQuery.data
  const findings = useMemo(() => findingsQuery.data ?? [], [findingsQuery.data])

  const allRows = useMemo(
    () => buildIndexRows(response?.items ?? [], findings),
    [response?.items, findings],
  )
  const databases = useMemo(() => [...new Set(allRows.map((row) => row.datname))].sort(), [allRows])
  const scoped = useMemo(
    () => (database === '' ? allRows : allRows.filter((row) => row.datname === database)),
    [allRows, database],
  )

  // Why an empty index list is empty (the Indexes, Cleanup and Index tax tabs).
  const emptyIndexes = useMemo(
    () =>
      noIndexStatsText({
        database,
        reported: allRows.length,
        foreignKeys: foreignKeyGaps(findings).filter(
          (gap) => database === '' || gap.datname === database,
        ).length,
      }),
    [allRows.length, database, findings],
  )

  function setParam(key: 'db', value: string) {
    const next = new URLSearchParams(searchParams)
    if (value && value !== 'all') next.set(key, value)
    else next.delete(key)
    // Another database is a new row set: every table starts on its first page (D23).
    for (const table of TABLE_IDS) next.delete(`${table}.page`)
    setSearchParams(next, { replace: true })
  }

  function setTab(value: string) {
    const next = new URLSearchParams(searchParams)
    if (value === DEFAULT_TAB) next.delete('tab')
    else next.set('tab', value)
    // The view belongs to the Indexes tab: a tab change starts on its first view.
    next.delete(INDEX_VIEW_PARAM)
    setMissing(null)
    setSearchParams(next, { replace: true })
  }

  // The Indexes and Cleanup tabs wait for the indexes; the Dead rows tab loads its own tables.
  const indexesState =
    indexesQuery.isPending && response === undefined ? (
      <div aria-busy="true" aria-label="Loading indexes" role="status">
        Loading indexes…
      </div>
    ) : indexesQuery.error && response === undefined ? (
      <ErrorState
        endpoint="indexes"
        failure={indexesQuery.error}
        onRetry={() => void indexesQuery.refetch()}
      />
    ) : null

  const controls = (
    <div
      className="flex flex-wrap items-end gap-x-6 gap-y-4 min-[1600px]:flex-nowrap min-[1600px]:items-center min-[1600px]:gap-3"
      data-testid="index-page-controls"
    >
      <SearchEverythingBox instanceId={instanceId} />
      <TabsList aria-label="Index management sections" variant="line">
        {TABS.map((option) => (
          <TabsTrigger key={option.value} value={option.value}>
            {option.label}
          </TabsTrigger>
        ))}
      </TabsList>
      {serverControl}
      <div className="space-y-1.5 min-[1600px]:space-y-0">
        <label
          className="text-muted-foreground text-xs font-medium min-[1600px]:sr-only"
          htmlFor="index-db"
        >
          Database
        </label>
        <div className="relative">
          <Database
            aria-hidden="true"
            className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
          />
          <NativeSelect
            className="w-56 max-w-full pr-3 pl-8 min-[1600px]:h-8 min-[1600px]:w-44"
            id="index-db"
            onChange={(event) => setParam('db', event.target.value)}
            value={database}
          >
            <option value="">All databases</option>
            {databases.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
            {database !== '' && !databases.includes(database) ? (
              <option value={database}>{database}</option>
            ) : null}
          </NativeSelect>
        </div>
      </div>
      <div className="self-center">
        <FreshnessBadge dataUpdatedAt={indexesQuery.dataUpdatedAt} policy="instance" />
      </div>
    </div>
  )

  return (
    <Tabs className="gap-6 min-[1600px]:gap-4" onValueChange={setTab} value={tab}>
      <IndexPageHeader actions={controls} />
      <SearchEverythingResults database={database} instanceId={instanceId} />
      {missing !== null ? (
        <div
          className="border-warning/40 bg-warning/10 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border px-4 py-2 text-sm"
          role="status"
        >
          <span className="min-w-0">
            {missing.tab === 'dead-rows' ? 'The table' : 'The index'}{' '}
            <code className="font-mono text-xs">{missing.key}</code> is not in the current list:
            Index analysis shows only what the agent reported in its latest collection.
          </span>
          <Button onClick={() => setMissing(null)} size="xs" type="button" variant="ghost">
            Dismiss
          </Button>
        </div>
      ) : null}
      <TabsContent value="indexes">
        {indexesState ?? (
          <IndexesPanel
            instanceId={instanceId}
            dataUpdatedAt={indexesQuery.dataUpdatedAt}
            database={database}
            findings={findings}
            emptyDescription={emptyIndexes}
            focusKey={tab === 'indexes' ? focusKey : undefined}
            onFocusDone={focusDone}
            findingsPending={findingsQuery.isPending}
            incomplete={response?.truncated === true || allRows.length >= INDEX_LIMIT}
            limit={INDEX_LIMIT}
            reported={allRows.length}
            rows={scoped}
          />
        )}
      </TabsContent>
      <TabsContent value="cleanup">
        {indexesState ?? (
          <CleanupPanel
            emptyDescription={emptyIndexes}
            instanceId={instanceId}
            rows={scoped}
            siblings={siblings}
          />
        )}
      </TabsContent>
      <TabsContent value="dead-rows">
        <DeadRowsPanel
          database={database}
          findings={findings}
          focusKey={tab === 'dead-rows' ? focusKey : undefined}
          instanceId={instanceId}
          onFocusDone={focusDone}
        />
      </TabsContent>
      <TabsContent value="suggestions">
        <SuggestionsPanel database={database} instanceId={instanceId} />
      </TabsContent>
      <TabsContent value="tax">
        {indexesState ?? (
          <IndexTaxPanel
            database={database}
            emptyDescription={emptyIndexes}
            incomplete={response?.truncated === true || allRows.length >= INDEX_LIMIT}
            instanceId={instanceId}
            rows={scoped}
          />
        )}
      </TabsContent>
    </Tabs>
  )
}

export function IndexAnalysisPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const clustersQuery = useClusters()
  const options = useMemo(() => instanceOptions(clustersQuery.data ?? []), [clustersQuery.data])
  const instanceId = chooseInstance(
    clustersQuery.data ?? [],
    searchParams.get('instance'),
    readLastInstance(),
  )

  function selectInstance(value: string) {
    const next = new URLSearchParams(searchParams)
    next.set('instance', value)
    // A database belongs to one instance, so switching server clears it.
    next.delete('db')
    setSearchParams(next, { replace: true })
  }

  if (clustersQuery.data !== undefined && instanceId !== null) {
    return (
      <InstanceIndexes
        key={instanceId}
        instanceId={instanceId}
        serverControl={
          <div className="space-y-1.5 min-[1600px]:space-y-0">
            <label
              className="text-muted-foreground text-xs font-medium min-[1600px]:sr-only"
              htmlFor="index-instance"
            >
              Server
            </label>
            <div className="relative">
              <Server
                aria-hidden="true"
                className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
              />
              <NativeSelect
                className="w-72 max-w-full pr-3 pl-8 min-[1600px]:h-8 min-[1600px]:w-56"
                id="index-instance"
                onChange={(event) => selectInstance(event.target.value)}
                value={instanceId}
              >
                {options.map((option) => (
                  <option key={option.id} value={option.id}>
                    {option.label}
                    {option.up ? '' : ' (down)'}
                  </option>
                ))}
              </NativeSelect>
            </div>
          </div>
        }
        siblings={siblingServers(clustersQuery.data, instanceId)}
      />
    )
  }

  return (
    <div className="space-y-6 min-[1600px]:space-y-4">
      <IndexPageHeader />
      {clustersQuery.error && clustersQuery.data === undefined ? (
        <ErrorState
          endpoint="clusters"
          failure={clustersQuery.error}
          onRetry={() => void clustersQuery.refetch()}
        />
      ) : clustersQuery.isPending ? (
        <div aria-busy="true" aria-label="Loading servers" role="status">
          Loading servers…
        </div>
      ) : (
        <EmptyState
          description="Set up the pglens agent on a PostgreSQL host to start collecting index statistics."
          title="No monitored servers yet"
        />
      )}
    </div>
  )
}

export default IndexAnalysisPage
