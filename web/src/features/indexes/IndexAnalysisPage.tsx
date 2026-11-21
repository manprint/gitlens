import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Database, ListTree, Server } from 'lucide-react'

import { useClusters, useInstanceIndexes } from '@/api/queries'
import { useFindings } from '@/api/queries'
import type { Cluster } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { StandbyStatsNote } from '@/components/StandbyStatsNote'
import { HelpLink } from '@/components/layout/HelpLink'
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
import { MissingIndexesPanel } from './MissingIndexesPanel'
import { INDEX_TABLE_IDS, INDEX_VIEW_PARAM } from './indexViews'
import { useT } from '@/i18n/react'

/** The most indexes the API returns for one server. */
export const INDEX_LIMIT = 500

export const TABS = [
  { value: 'indexes', labelKey: 'indexes.page.tabIndexes' },
  { value: 'missing', labelKey: 'indexes.page.tabMissing' },
  { value: 'cleanup', labelKey: 'indexes.page.tabCleanup' },
  { value: 'dead-rows', labelKey: 'indexes.page.tabDeadRows' },
  { value: 'tax', labelKey: 'indexes.page.tabTax' },
] as const

type TabValue = (typeof TABS)[number]['value']

const DEFAULT_TAB: TabValue = 'indexes'

/** Old tab names still opened by saved links. */
const TAB_ALIASES: Readonly<Record<string, TabValue>> = { suggestions: 'missing' }

function readTab(value: string | null): TabValue {
  const name = value === null ? null : (TAB_ALIASES[value] ?? value)
  return TABS.find((tab) => tab.value === name)?.value ?? DEFAULT_TAB
}

/** Every table of the page: a database change sends each back to its first page (D23). */
const TABLE_IDS = [...INDEX_TABLE_IDS, 'cleanup', 'dead-rows', 'missing', 'tax']

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

function instanceRole(clusters: readonly Cluster[], instanceId: string): string | undefined {
  return clusters
    .flatMap((cluster) => cluster.instances)
    .find((instance) => instance.instance_id === instanceId)?.role
}

/**
 * The page title. With a server picked, its actions hold the tab list, the
 * server and database selects and the freshness, on the title line from
 * 1600 px (D26), so each tab's table has the height of the screen.
 */
function IndexPageHeader({ actions }: { actions?: ReactNode }) {
  const t = useT()

  return (
    <PageHeader
      help={<HelpLink anchor="indexes" label={t('indexes.page.helpLabel')} />}
      eyebrow={t('indexes.page.eyebrow')}
      icon={ListTree}
      subtitleHint="indexes.page"
      subtitle={t('indexes.page.subtitle')}
      title={t('indexes.page.title')}
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
  /** The chosen server's role: a standby says its statistics are its own (V-10). */
  role: string | null | undefined
  /** The chosen server's name, for generated scripts. */
  serverLabel: string
}

function InstanceIndexes({
  serverControl,
  instanceId,
  siblings,
  role,
  serverLabel,
}: AnalysisProps) {
  const t = useT()

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
        t,
      }),
    [allRows.length, database, findings, t],
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
      <div aria-busy="true" aria-label={t('indexes.page.loadingIndexes')} role="status">
        {t('indexes.page.loadingIndexesEllipsis')}
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
      <TabsList aria-label={t('indexes.page.sectionsAria')} variant="line">
        {TABS.map((option) => (
          <TabsTrigger key={option.value} value={option.value}>
            {t(option.labelKey)}
          </TabsTrigger>
        ))}
      </TabsList>
      {serverControl}
      <div className="space-y-1.5 min-[1600px]:space-y-0">
        <label
          className="text-muted-foreground text-xs font-medium min-[1600px]:sr-only"
          htmlFor="index-db"
        >
          {t('indexes.page.database')}
        </label>
        <div className="relative">
          <Database
            aria-hidden="true"
            className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
          />
          <NativeSelect
            className="w-56 max-w-full pr-3 pl-8 min-[1600px]:h-8 min-[1600px]:w-52"
            id="index-db"
            onChange={(event) => setParam('db', event.target.value)}
            value={database}
          >
            <option value="">{t('indexes.page.allDatabases')}</option>
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
    <Tabs className="gap-6 min-[1600px]:gap-2" onValueChange={setTab} value={tab}>
      <IndexPageHeader actions={controls} />
      <SearchEverythingResults database={database} instanceId={instanceId} />
      <StandbyStatsNote role={role} />
      {missing !== null ? (
        <div
          className="border-warning/40 bg-warning/10 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border px-4 py-2 text-sm"
          role="status"
        >
          <span className="min-w-0">
            {missing.tab === 'dead-rows'
              ? t('indexes.page.missingTable')
              : t('indexes.page.missingIndex')}{' '}
            <code className="font-mono text-xs">{missing.key}</code> {t('indexes.page.notInList')}
          </span>
          <Button onClick={() => setMissing(null)} size="xs" type="button" variant="ghost">
            {t('indexes.page.dismiss')}
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
      <TabsContent value="missing">
        <MissingIndexesPanel
          database={database}
          findings={findings}
          findingsPending={findingsQuery.isPending}
          instanceId={instanceId}
          serverLabel={serverLabel}
        />
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
  const t = useT()

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
              {t('indexes.page.server')}
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
                    {option.up ? '' : t('indexes.page.down')}
                  </option>
                ))}
              </NativeSelect>
            </div>
          </div>
        }
        siblings={siblingServers(clustersQuery.data, instanceId)}
        role={instanceRole(clustersQuery.data, instanceId)}
        serverLabel={options.find((option) => option.id === instanceId)?.label ?? instanceId}
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
        <div aria-busy="true" aria-label={t('indexes.page.loadingServers')} role="status">
          {t('indexes.page.loadingServersEllipsis')}
        </div>
      ) : (
        <EmptyState
          description={t('indexes.page.setUpAgent')}
          title={t('indexes.page.noServers')}
        />
      )}
    </div>
  )
}

export default IndexAnalysisPage
