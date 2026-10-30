import { Lightbulb } from 'lucide-react'
import { useEffect, useMemo, useRef, type ReactNode } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAdvisorRules, useClusters, useFindings } from '@/api/queries'
import type { Cluster } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { FULL_SEARCH_HINT, SearchBox } from '@/components/layout/SearchBox'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useSearchDraft, useShownTerm } from '@/hooks/useSearch'
import { searchTermOf } from '@/lib/highlight'
import { clusterLabel, instanceLabel } from '@/lib/names'
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
function FilterControl({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="text-muted-foreground flex flex-col gap-1 text-xs">
      <span aria-hidden="true" className="font-medium min-[1600px]:hidden">
        {label}
      </span>
      {children}
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
      hint={FULL_SEARCH_HINT}
      id="findings-search"
      label="Search findings"
      onChange={setSearch}
      placeholder="Rule, title, object or evidence"
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
        <section aria-busy="true" aria-label="Loading advisor findings" role="status">
          Loading advisor findings…
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
      ? `${visibleFindings.length} of ${findings.length} findings shown`
      : `${visibleFindings.length} of ${findings.length} findings matching “${shownTerm}” shown`
  // What hides the matches of a search (D20): the state filter alone, or the other filters too.
  const matchesInOtherStates =
    shownTerm !== '' &&
    visibleFindings.length === 0 &&
    joinedFindings.some((finding) =>
      matchesFilter(finding, 'all', severity, scope, clusterId, instanceId),
    )
  const description =
    defaultView && (hiddenMuted > 0 || hiddenResolved > 0)
      ? `${shownText}. By default, ${hiddenMuted} muted and ${hiddenResolved} resolved findings are hidden. Use the state filter to show them.`
      : shownText

  // One table per tab (15.3, D26); the tab list sits in the page header line.
  return (
    <Tabs
      className="gap-8 min-[1600px]:gap-4"
      onValueChange={(value) => showView(readFindingsView(value))}
      value={view}
    >
      <PageHeader
        icon={Lightbulb}
        eyebrow="Checkup"
        title="Advisor findings"
        subtitle="Automatic health checks on the collected PostgreSQL statistics: missing or unused indexes, bloat, vacuum, configuration and more. Each finding shows the evidence behind it and what to do. Findings come from collected statistics, not query plans; index recommendations are candidates for review, not automatic changes."
        freshness={<FreshnessBadge dataUpdatedAt={findingsQuery.dataUpdatedAt} policy="findings" />}
        actions={
          <TabsList aria-label="Findings views">
            {FINDINGS_VIEWS.map((entry) => (
              <TabsTrigger key={entry.id} value={entry.id}>
                {entry.label}
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
          <p role="status">
            Advisor rule explanations are unavailable; findings remain visible without catalogue
            context.
          </p>
        </div>
      ) : null}

      <TabsContent value="findings">
        <Section
          title="Findings"
          description={
            // Polite: a new search's match count is announced.
            <span aria-live="polite" title={description}>
              {description}
            </span>
          }
          actions={
            <div aria-label="Finding summary" className="flex flex-wrap gap-2" role="group">
              <SummaryButton
                active={severity === 'critical'}
                count={summary.critical}
                label="Critical"
                onClick={() => selectSeverity('critical')}
              />
              <SummaryButton
                active={severity === 'warning'}
                count={summary.warning}
                label="Warning"
                onClick={() => selectSeverity('warning')}
              />
              <SummaryButton
                active={severity === 'info'}
                count={summary.info}
                label="Info"
                onClick={() => selectSeverity('info')}
              />
              <SummaryButton
                active={state === 'degraded'}
                count={summary.degraded}
                label="Rules not evaluated"
                onClick={selectDegraded}
              />
            </div>
          }
        >
          <div className="space-y-3">
            {/* The filters are kept in the URL so a findings view can be shared. */}
            <div
              aria-label="Finding filters"
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
                  All states
                </Button>
              ) : null}
              <FilterControl label="State">
                <NativeSelect
                  aria-label="State"
                  className="h-8 px-2 text-sm"
                  id="findings-state"
                  onChange={(event) =>
                    updateFilter('state', event.target.value === 'active' ? '' : event.target.value)
                  }
                  value={state}
                >
                  <option value="active">Open and degraded</option>
                  <option value="all">All states</option>
                  <option value="open">Open</option>
                  <option value="degraded">Degraded</option>
                  <option value="muted">Muted</option>
                  <option value="resolved">Resolved</option>
                </NativeSelect>
              </FilterControl>
              <FilterControl label="Severity">
                <NativeSelect
                  aria-label="Severity"
                  className="h-8 px-2 text-sm"
                  id="findings-severity"
                  onChange={(event) =>
                    updateFilter('severity', event.target.value === 'all' ? '' : event.target.value)
                  }
                  value={severity}
                >
                  <option value="all">All severities</option>
                  <option value="critical">Critical</option>
                  <option value="warning">Warning</option>
                  <option value="info">Info</option>
                </NativeSelect>
              </FilterControl>
              <FilterControl label="Scope">
                <NativeSelect
                  aria-label="Scope"
                  className="h-8 px-2 text-sm"
                  id="findings-scope"
                  onChange={(event) =>
                    updateFilter('scope', event.target.value === 'all' ? '' : event.target.value)
                  }
                  value={scope}
                >
                  <option value="all">All scopes</option>
                  <option value="cluster">Cluster</option>
                  <option value="instance">Instance</option>
                </NativeSelect>
              </FilterControl>
              <FilterControl label="Cluster">
                <NativeSelect
                  aria-label="Cluster"
                  className="h-8 w-44 px-2 text-sm"
                  id="findings-cluster"
                  onChange={(event) => updateFilter('cluster_id', event.target.value)}
                  value={clusterId}
                >
                  <option value="">All clusters</option>
                  {clusterFilterOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </NativeSelect>
              </FilterControl>
              <FilterControl label="Instance">
                <NativeSelect
                  aria-label="Instance"
                  className="h-8 w-56 px-2 text-sm"
                  id="findings-instance"
                  onChange={(event) => updateFilter('instance_id', event.target.value)}
                  value={instanceId}
                >
                  <option value="">All instances</option>
                  {instanceFilterOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </NativeSelect>
              </FilterControl>
              {findings.length >= FINDINGS_LIMIT ? (
                <FilterLineNote>
                  {`Only the first ${FINDINGS_LIMIT} findings were returned. Open and degraded findings come first, so muted and resolved findings may be missing from the counts and the list.`}
                </FilterLineNote>
              ) : null}
              {allFindingsDegraded ? (
                <FilterLineNote>
                  Rules could not be evaluated; this is not a healthy “no findings” result.
                </FilterLineNote>
              ) : null}
            </div>

            {visibleFindings.length > 0 ? (
              <FindingsTable findings={visibleFindings} q={shownTerm} />
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
                      Show all findings
                    </Button>
                  ) : matchesInOtherStates ? (
                    <Button
                      onClick={() => afterNoMatch(() => updateFilter('state', 'all'))}
                      size="sm"
                      type="button"
                      variant="outline"
                    >
                      Search all states
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
                      Clear filters
                    </Button>
                  )
                }
                description={
                  findings.length === 0
                    ? 'No finding of any state matches the search: rule, title, detail, remediation, object, database, mute reason and evidence were searched.'
                    : matchesInOtherStates
                      ? `${findings.length} ${findings.length === 1 ? 'finding matches' : 'findings match'} in other states.`
                      : `${findings.length} ${findings.length === 1 ? 'finding matches' : 'findings match'}, hidden by the severity, scope, cluster or instance filter.`
                }
                title={`No finding matches “${shownTerm}”`}
              />
            ) : (
              <EmptyState
                description={
                  findings.length === 0
                    ? `No findings are currently firing. ${rules.length} advisor rule${rules.length === 1 ? '' : 's'} evaluated successfully.`
                    : 'Try clearing one or more filters or choose a different state.'
                }
                title={
                  findings.length === 0 ? 'No active findings' : 'No findings match these filters'
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
