import { Lightbulb } from 'lucide-react'
import { useEffect, useMemo, useRef, type ReactNode } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAdvisorRules, useClusters, useFindings } from '@/api/queries'
import type { Cluster } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { SearchBox } from '@/components/layout/SearchBox'
import { Section } from '@/components/layout/Section'
import { InfoHint } from '@/components/layout/InfoHint'
import { EmptyState, ErrorState } from '@/components/state'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useSearchDraft, useShownTerm } from '@/hooks/useSearch'
import { useT } from '@/i18n/react'
import type { CaptionId } from '@/lib/captions'
import { searchTermOf } from '@/lib/highlight'
import { clusterLabel, instanceLabel, nameLookup } from '@/lib/names'
import {
  joinCatalogue,
  rankFindings,
  summariseFindings,
  type FindingRecord,
  type JoinedFinding,
} from '@/lib/findings'

import { FindingsTable } from './FindingsTable'
import {
  DEFAULT_FINDINGS_VIEW,
  FINDINGS_VIEWS,
  readFindingsView,
  type FindingsViewId,
} from './findingsViews'
import { RuleCatalogue } from './RuleCatalogue'

type StateFilter = 'active' | 'all' | 'open' | 'degraded' | 'muted' | 'resolved'
type SeverityFilter = 'all' | 'critical' | 'warning' | 'info'
type ScopeFilter = 'all' | 'cluster' | 'instance'

const stateFilters: readonly StateFilter[] = [
  'active',
  'all',
  'open',
  'degraded',
  'muted',
  'resolved',
]
const severityFilters: readonly SeverityFilter[] = ['all', 'critical', 'warning', 'info']
const scopeFilters: readonly ScopeFilter[] = ['all', 'cluster', 'instance']

// The server's maximum. It returns open and degraded findings first, so a
// full page is cut in the muted and resolved ones.
const FINDINGS_LIMIT = 1000

const FILTER_HINT: Record<'state' | 'severity' | 'scope', CaptionId> = {
  state: 'findings.filter-state',
  severity: 'findings.filter-severity',
  scope: 'findings.filter-scope',
}

function readFilter<T extends string>(value: string | null, allowed: readonly T[], fallback: T): T {
  return allowed.includes(value as T) ? (value as T) : fallback
}

function updateSearchParams(
  searchParams: URLSearchParams,
  setSearchParams: (next: URLSearchParams) => void,
  updates: Record<string, string>,
) {
  const next = new URLSearchParams(searchParams)
  for (const [key, value] of Object.entries(updates)) {
    if (value) next.set(key, value)
    else next.delete(key)
  }
  // A new filter shows its first page of findings.
  next.delete('findings.page')
  setSearchParams(next)
}

function matchesFilter(
  finding: JoinedFinding,
  state: StateFilter,
  severity: SeverityFilter,
  scope: ScopeFilter,
  clusterId: string,
  instanceId: string,
): boolean {
  const stateMatches =
    state === 'active'
      ? finding.state === 'open' || finding.state === 'degraded'
      : state === 'all'
        ? true
        : finding.state === state
  if (!stateMatches) return false
  if (severity !== 'all' && finding.severity !== severity) return false
  if (scope !== 'all' && finding.scope !== scope) return false
  if (clusterId && String(finding.cluster_id ?? '') !== clusterId) return false
  if (instanceId && String(finding.instance_id ?? '') !== instanceId) return false
  return true
}

function SummaryButton({
  label,
  count,
  active,
  onClick,
}: {
  label: string
  count: number
  active: boolean
  onClick: () => void
}) {
  // From 1600 px a one-line chip in the section header (D26).
  return (
    <button
      aria-label={`${label} ${count}`}
      aria-pressed={active}
      className={`bg-card shadow-card min-w-28 rounded-xl border px-4 py-2.5 text-left text-sm transition-colors min-[1600px]:flex min-[1600px]:h-6 min-[1600px]:min-w-0 min-[1600px]:items-center min-[1600px]:gap-1.5 min-[1600px]:rounded-md min-[1600px]:px-2 min-[1600px]:py-0 min-[1600px]:shadow-none ${active ? 'border-primary ring-primary/20 ring-2' : 'hover:border-primary/40'}`}
      onClick={onClick}
      type="button"
    >
      <span className="text-muted-foreground block text-xs font-medium min-[1600px]:inline">
        {label}
      </span>{' '}
      <span className="text-foreground text-xl font-semibold tabular-nums min-[1600px]:text-sm">
        {count}
      </span>
    </button>
  )
}

interface FilterOption {
  value: string
  label: string
}

const byLabel = (left: FilterOption, right: FilterOption) => left.label.localeCompare(right.label)

/** An id the URL names but the cluster list does not hold stays selectable, as itself. */
function withSelected(options: FilterOption[], selected: string): FilterOption[] {
  return selected && !options.some((option) => option.value === selected)
    ? [...options, { value: selected, label: selected }]
    : options
}

/** The Cluster filter's options: every cluster by its name (D14), value the id. */
function clusterOptions(clusters: readonly Cluster[], selected: string): FilterOption[] {
  const options = clusters.map((cluster) => ({
    value: cluster.cluster_id,
    label: clusterLabel(cluster),
  }))
  return withSelected(options.sort(byLabel), selected)
}

/** The Instance filter's options: every instance by its name or address (D15), value the id. */
function instanceOptions(clusters: readonly Cluster[], selected: string): FilterOption[] {
  const seen = new Map<string, FilterOption>()
  for (const cluster of clusters) {
    for (const instance of cluster.instances) {
      seen.set(instance.instance_id, {
        value: instance.instance_id,
        label: instanceLabel(instance),
      })
    }
  }
  return withSelected([...seen.values()].sort(byLabel), selected)
}

/**
 * One control of the findings filter line. From 1600 px the line is one row
 * (D26): the name above the control is hidden and the control keeps it as
 * its accessible name.
 */
function FilterControl({
  label,
  hint,
  children,
}: {
  label: string
  hint?: CaptionId
  children: ReactNode
}) {
  const t = useT()
  return (
    <div className="text-muted-foreground flex flex-col gap-1 text-xs">
      <span aria-hidden="true" className="font-medium min-[1600px]:hidden">
        {label}
      </span>
      {hint === undefined ? (
        children
      ) : (
        <div className="flex items-center gap-1">
          {children}
          <InfoHint id={hint} label={t('findings.page.filterHint', { label })} />
        </div>
      )}
    </div>
  )
}

/** A warning kept on the filter line: one truncated line from 1600 px, the full text in `title`. */
function FilterLineNote({ children }: { children: string }) {
  return (
    <p
      className="text-warning-foreground basis-full text-sm min-[1600px]:min-w-0 min-[1600px]:flex-1 min-[1600px]:basis-auto min-[1600px]:self-center min-[1600px]:truncate"
      role="status"
      title={children}
    >
      {children}
    </p>
  )
}

export function FindingsPage() {
  const t = useT()
  const [searchParams, setSearchParams] = useSearchParams()
  // The search runs on the server over every finding, whatever its state (D20).
  const q = searchParams.get('q') ?? ''
  const term = searchTermOf(q)
  const findingsQuery = useFindings(
    { state: 'all', limit: FINDINGS_LIMIT, ...(term === '' ? {} : { q: term }) },
    { keepPrevious: true },
  )
  // The term the listed findings answer: the previous one while a new search loads.
  const shownTerm = useShownTerm(term, findingsQuery.isPlaceholderData, findingsQuery.isSuccess)
  // The box is drawn again when a failed search replaces the page: its text lives here.
  const searchDraft = useSearchDraft(q)
  const rulesQuery = useAdvisorRules()
  // The Cluster and Instance filters list what the fleet reports, by name.
  const clustersQuery = useClusters()
  const location = useLocation()
  const navigate = useNavigate()
  const redirectedForUnauthorized = useRef(false)
  const view = readFindingsView(searchParams.get('view'))

  const state = readFilter(searchParams.get('state'), stateFilters, 'active')
  const severity = readFilter(searchParams.get('severity'), severityFilters, 'all')
  const scope = readFilter(searchParams.get('scope'), scopeFilters, 'all')
  const clusterId = searchParams.get('cluster_id') ?? ''
  const instanceId = searchParams.get('instance_id') ?? ''
  const fleet = clustersQuery.data
  const names = useMemo(() => nameLookup(fleet), [fleet])
  const clusterFilterOptions = useMemo(
    () => clusterOptions(fleet ?? [], clusterId),
    [clusterId, fleet],
  )
  const instanceFilterOptions = useMemo(
    () => instanceOptions(fleet ?? [], instanceId),
    [fleet, instanceId],
  )

  const findings = useMemo(
    () => (findingsQuery.data ?? []) as FindingRecord[],
    [findingsQuery.data],
  )
  const rules = useMemo(() => rulesQuery.data ?? [], [rulesQuery.data])
  const catalogueLoaded = rulesQuery.data !== undefined
  const joinedFindings = useMemo(
    () => joinCatalogue(findings, rules, catalogueLoaded),
    [catalogueLoaded, findings, rules],
  )
  const summary = useMemo(() => summariseFindings(findings), [findings])
  const allFindingsDegraded =
    findings.length > 0 && findings.every((finding) => finding.state === 'degraded')
  const visibleFindings = useMemo(
    () =>
      rankFindings(
        joinedFindings.filter((finding) =>
          matchesFilter(finding, state, severity, scope, clusterId, instanceId),
        ),
      ),
    [clusterId, instanceId, joinedFindings, scope, severity, state],
  )

  function updateFilter(key: string, value: string) {
    updateSearchParams(searchParams, setSearchParams, { [key]: value })
  }

  function selectSeverity(value: SeverityFilter) {
    updateSearchParams(searchParams, setSearchParams, { severity: value === 'all' ? '' : value })
  }

  function setSearch(value: string) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (value.trim() === '') next.delete('q')
        else next.set('q', value)
        // Another search shows its first page of findings.
        next.delete('findings.page')
        return next
      },
      { replace: true },
    )
  }

  // A no-match action removes its own button: the search box keeps the keyboard.
  function afterNoMatch(action: () => void) {
    action()
    document.getElementById('findings-search')?.focus()
  }

  function selectDegraded() {
    updateSearchParams(searchParams, setSearchParams, { state: 'degraded', severity: '' })
  }

  function showView(next: FindingsViewId) {
    setSearchParams(
      (current) => {
        const params = new URLSearchParams(current)
        // The default tab is never written to the URL (D12).
        if (next === DEFAULT_FINDINGS_VIEW) params.delete('view')
        else params.set('view', next)
        return params
      },
      { replace: true },
    )
  }

  const unauthorized =
    findingsQuery.error?.kind === 'unauthorized' || rulesQuery.error?.kind === 'unauthorized'

  useEffect(() => {
    if (!unauthorized || redirectedForUnauthorized.current) return
    redirectedForUnauthorized.current = true
    const next = `${location.pathname}${location.search}`
    void navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate, unauthorized])

  const searchBox = (
    <SearchBox
      busy={findingsQuery.isFetching && term !== ''}
      draft={searchDraft}
      fullSearch
      id="findings-search"
      label={t('findings.page.searchLabel')}
      onChange={setSearch}
      placeholder={t('findings.page.searchPlaceholder')}
      value={q}
    />
  )

  if (findingsQuery.error && !findingsQuery.data) {
    // The box stays, so a search the server refused can be changed or cleared.
    return (
      <div className="space-y-4">
        {term === '' ? null : <div className="flex">{searchBox}</div>}
        <ErrorState
          endpoint="findings"
          failure={findingsQuery.error}
          onRetry={() => void findingsQuery.refetch()}
        />
      </div>
    )
  }
  // Loading and failing draw the box in the same place, so it keeps its text
  // and focus while a corrected search loads after a failed one.
  if (findingsQuery.isPending || (rulesQuery.isPending && !rulesQuery.data)) {
    return (
      <div className="space-y-4">
        {term === '' ? null : <div className="flex">{searchBox}</div>}
        <section aria-busy="true" aria-label={t('findings.page.loadingLabel')} role="status">
          {t('findings.page.loading')}
        </section>
      </div>
    )
  }

  const defaultView = state === 'active' && !searchParams.has('state')
  const hiddenMuted = defaultView
    ? findings.filter((finding) => finding.state === 'muted').length
    : 0
  const hiddenResolved = defaultView
    ? findings.filter((finding) => finding.state === 'resolved').length
    : 0

  const shownText =
    shownTerm === ''
      ? t('findings.page.shownAll', { shown: visibleFindings.length, total: findings.length })
      : t('findings.page.shownMatching', {
          shown: visibleFindings.length,
          total: findings.length,
          term: shownTerm,
        })
  // What hides the matches of a search (D20): the state filter alone, or the other filters too.
  const matchesInOtherStates =
    shownTerm !== '' &&
    visibleFindings.length === 0 &&
    joinedFindings.some((finding) =>
      matchesFilter(finding, 'all', severity, scope, clusterId, instanceId),
    )
  const description =
    defaultView && (hiddenMuted > 0 || hiddenResolved > 0)
      ? t('findings.page.hiddenStates', {
          shown: shownText,
          muted: hiddenMuted,
          resolved: hiddenResolved,
        })
      : shownText

  // One table per tab (15.3, D26); the tab list sits in the page header line.
  return (
    <Tabs
      className="gap-8 min-[1600px]:gap-4"
      onValueChange={(value) => showView(readFindingsView(value))}
      value={view}
    >
      <PageHeader
        help={<HelpLink anchor="findings" label={t('findings.page.helpLabel')} />}
        icon={Lightbulb}
        eyebrow={t('findings.page.eyebrow')}
        title={t('findings.advisorFindings')}
        subtitle={t('findings.page.subtitle')}
        freshness={<FreshnessBadge dataUpdatedAt={findingsQuery.dataUpdatedAt} policy="findings" />}
        actions={
          <TabsList aria-label={t('findings.page.viewsLabel')}>
            {FINDINGS_VIEWS.map((entry) => (
              <TabsTrigger key={entry.id} value={entry.id}>
                {t(entry.labelKey)}
              </TabsTrigger>
            ))}
          </TabsList>
        }
      />

      {findingsQuery.error ? (
        <ErrorState
          endpoint="findings"
          failure={findingsQuery.error}
          onRetry={() => void findingsQuery.refetch()}
        />
      ) : null}

      {rulesQuery.error ? (
        <div className="space-y-2">
          <ErrorState
            endpoint="advisor rules"
            failure={rulesQuery.error}
            onRetry={() => void rulesQuery.refetch()}
          />
          <p role="status">{t('findings.page.rulesUnavailable')}</p>
        </div>
      ) : null}

      <TabsContent value="findings">
        <Section
          title={t('findings.view.findings')}
          captionId="findings.list"
          actions={
            <>
              <div
                aria-label={t('findings.page.summaryLabel')}
                className="flex flex-wrap gap-2"
                role="group"
              >
                <SummaryButton
                  active={severity === 'critical'}
                  count={summary.critical}
                  label={t('findings.page.severityCritical')}
                  onClick={() => selectSeverity('critical')}
                />
                <SummaryButton
                  active={severity === 'warning'}
                  count={summary.warning}
                  label={t('findings.page.severityWarning')}
                  onClick={() => selectSeverity('warning')}
                />
                <SummaryButton
                  active={severity === 'info'}
                  count={summary.info}
                  label={t('findings.page.severityInfo')}
                  onClick={() => selectSeverity('info')}
                />
                <SummaryButton
                  active={state === 'degraded'}
                  count={summary.degraded}
                  label={t('findings.page.notEvaluated')}
                  onClick={selectDegraded}
                />
              </div>
              <InfoHint id="findings.summary" label={t('findings.page.summaryLabel')} />
              <HelpLink anchor="findings#findings-table" label={t('findings.page.tableHelp')} />
            </>
          }
        >
          <div className="space-y-3">
            {/* The filters are kept in the URL so a findings view can be shared. */}
            <div
              aria-label={t('findings.page.filtersLabel')}
              className="flex flex-wrap items-end gap-3"
              role="group"
            >
              {searchBox}
              {term !== '' ? (
                // The search reaches muted and resolved findings too; one press shows them (D20).
                <Button
                  aria-pressed={state === 'all'}
                  className="self-end min-[1600px]:self-center"
                  onClick={() => updateFilter('state', state === 'all' ? '' : 'all')}
                  size="sm"
                  type="button"
                  variant={state === 'all' ? 'secondary' : 'outline'}
                >
                  {t('findings.page.allStates')}
                </Button>
              ) : null}
              <FilterControl hint={FILTER_HINT.state} label={t('findings.page.state')}>
                <NativeSelect
                  aria-label={t('findings.page.state')}
                  className="h-8 px-2 text-sm min-[1600px]:h-7"
                  id="findings-state"
                  onChange={(event) =>
                    updateFilter('state', event.target.value === 'active' ? '' : event.target.value)
                  }
                  value={state}
                >
                  <option value="active">{t('findings.page.stateActive')}</option>
                  <option value="all">{t('findings.page.allStates')}</option>
                  <option value="open">{t('findings.page.stateOpen')}</option>
                  <option value="degraded">{t('findings.page.stateDegraded')}</option>
                  <option value="muted">{t('findings.page.stateMuted')}</option>
                  <option value="resolved">{t('findings.page.stateResolved')}</option>
                </NativeSelect>
              </FilterControl>
              <FilterControl hint={FILTER_HINT.severity} label={t('findings.table.severity')}>
                <NativeSelect
                  aria-label={t('findings.table.severity')}
                  className="h-8 px-2 text-sm min-[1600px]:h-7"
                  id="findings-severity"
                  onChange={(event) =>
                    updateFilter('severity', event.target.value === 'all' ? '' : event.target.value)
                  }
                  value={severity}
                >
                  <option value="all">{t('findings.ruleCatalogue.allSeverities')}</option>
                  <option value="critical">{t('findings.page.severityCritical')}</option>
                  <option value="warning">{t('findings.page.severityWarning')}</option>
                  <option value="info">{t('findings.page.severityInfo')}</option>
                </NativeSelect>
              </FilterControl>
              <FilterControl hint={FILTER_HINT.scope} label={t('findings.page.scope')}>
                <NativeSelect
                  aria-label={t('findings.page.scope')}
                  className="h-8 px-2 text-sm min-[1600px]:h-7"
                  id="findings-scope"
                  onChange={(event) =>
                    updateFilter('scope', event.target.value === 'all' ? '' : event.target.value)
                  }
                  value={scope}
                >
                  <option value="all">{t('findings.ruleCatalogue.allScopes')}</option>
                  <option value="cluster">{t('findings.page.cluster')}</option>
                  <option value="instance">{t('findings.page.instance')}</option>
                </NativeSelect>
              </FilterControl>
              <FilterControl label={t('findings.page.cluster')}>
                <NativeSelect
                  aria-label={t('findings.page.cluster')}
                  className="h-8 w-44 px-2 text-sm min-[1600px]:h-7"
                  id="findings-cluster"
                  onChange={(event) => updateFilter('cluster_id', event.target.value)}
                  value={clusterId}
                >
                  <option value="">{t('findings.page.allClusters')}</option>
                  {clusterFilterOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </NativeSelect>
              </FilterControl>
              <FilterControl label={t('findings.page.instance')}>
                <NativeSelect
                  aria-label={t('findings.page.instance')}
                  className="h-8 w-56 px-2 text-sm min-[1600px]:h-7"
                  id="findings-instance"
                  onChange={(event) => updateFilter('instance_id', event.target.value)}
                  value={instanceId}
                >
                  <option value="">{t('findings.page.allInstances')}</option>
                  {instanceFilterOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </NativeSelect>
              </FilterControl>
              {/* Polite: a new search's match count is announced. */}
              <p
                aria-live="polite"
                className="text-muted-foreground basis-full text-sm min-[1600px]:line-clamp-2 min-[1600px]:min-w-[14rem] min-[1600px]:flex-1 min-[1600px]:basis-0 min-[1600px]:self-center min-[1600px]:text-xs"
                title={description}
              >
                {description}
              </p>
              {findings.length >= FINDINGS_LIMIT ? (
                <FilterLineNote>
                  {t('findings.page.limitReached', { limit: FINDINGS_LIMIT })}
                </FilterLineNote>
              ) : null}
              {allFindingsDegraded ? (
                <FilterLineNote>{t('findings.page.allDegraded')}</FilterLineNote>
              ) : null}
            </div>

            {visibleFindings.length > 0 ? (
              <FindingsTable findings={visibleFindings} names={names} q={shownTerm} />
            ) : shownTerm !== '' ? (
              <EmptyState
                action={
                  findings.length === 0 ? (
                    <Button
                      onClick={() => afterNoMatch(() => setSearch(''))}
                      size="sm"
                      type="button"
                      variant="outline"
                    >
                      {t('findings.page.showAll')}
                    </Button>
                  ) : matchesInOtherStates ? (
                    <Button
                      onClick={() => afterNoMatch(() => updateFilter('state', 'all'))}
                      size="sm"
                      type="button"
                      variant="outline"
                    >
                      {t('findings.page.searchAllStates')}
                    </Button>
                  ) : (
                    <Button
                      onClick={() =>
                        afterNoMatch(() =>
                          updateSearchParams(searchParams, setSearchParams, {
                            state: 'all',
                            severity: '',
                            scope: '',
                            cluster_id: '',
                            instance_id: '',
                          }),
                        )
                      }
                      size="sm"
                      type="button"
                      variant="outline"
                    >
                      {t('findings.page.clearFilters')}
                    </Button>
                  )
                }
                description={
                  findings.length === 0
                    ? t('findings.page.noMatchAny')
                    : matchesInOtherStates
                      ? t('findings.page.matchesOtherStates', { count: findings.length })
                      : t('findings.page.matchesHidden', { count: findings.length })
                }
                title={t('findings.page.noMatchTitle', { term: shownTerm })}
              />
            ) : (
              <EmptyState
                description={
                  findings.length === 0
                    ? t('findings.page.noFindingsFiring', { count: rules.length })
                    : t('findings.page.tryFilters')
                }
                title={
                  findings.length === 0
                    ? t('findings.page.noActive')
                    : t('findings.page.noMatchFilters')
                }
              />
            )}
          </div>
        </Section>
      </TabsContent>

      <TabsContent value="rules">
        <RuleCatalogue findings={findings} rules={rules} />
      </TabsContent>
    </Tabs>
  )
}
