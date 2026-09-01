import { useEffect, useMemo, useRef } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAdvisorRules, useFindings } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import {
  joinCatalogue,
  rankFindings,
  summariseFindings,
  type FindingRecord,
  type JoinedFinding,
} from '@/lib/findings'

import { FindingCard } from './FindingCard'
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
    state === 'active' ? finding.state === 'open' || finding.state === 'degraded' :
    state === 'all' ? true : finding.state === state
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
  return (
    <button
      aria-label={`${label} ${count}`}
      aria-pressed={active}
      className="rounded-md border px-3 py-2 text-left text-sm"
      onClick={onClick}
      type="button"
    >
      <span className="block font-medium">{label}</span>
      <span className="text-muted-foreground">{count}</span>
    </button>
  )
}

export function FindingsPage() {
  const findingsQuery = useFindings({ state: 'all', limit: 1000 })
  const rulesQuery = useAdvisorRules()
  const location = useLocation()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const redirectedForUnauthorized = useRef(false)

  const state = readFilter(searchParams.get('state'), stateFilters, 'active')
  const severity = readFilter(searchParams.get('severity'), severityFilters, 'all')
  const scope = readFilter(searchParams.get('scope'), scopeFilters, 'all')
  const clusterId = searchParams.get('cluster_id') ?? ''
  const instanceId = searchParams.get('instance_id') ?? ''

  const findings = useMemo(
    () => (findingsQuery.data ?? []) as FindingRecord[],
    [findingsQuery.data],
  )
  const rules = useMemo(() => rulesQuery.data ?? [], [rulesQuery.data])
  const joinedFindings = useMemo(() => joinCatalogue(findings, rules), [findings, rules])
  const summary = useMemo(() => summariseFindings(findings), [findings])
  const allFindingsDegraded = findings.length > 0 && findings.every((finding) => finding.state === 'degraded')
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

  function selectDegraded() {
    updateSearchParams(searchParams, setSearchParams, { state: 'degraded', severity: '' })
  }

  const unauthorized =
    findingsQuery.error?.kind === 'unauthorized' || rulesQuery.error?.kind === 'unauthorized'

  useEffect(() => {
    if (!unauthorized || redirectedForUnauthorized.current) return
    redirectedForUnauthorized.current = true
    const next = `${location.pathname}${location.search}`
    void navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate, unauthorized])

  if (findingsQuery.error && !findingsQuery.data) {
    return (
      <ErrorState
        endpoint="findings"
        failure={findingsQuery.error}
        onRetry={() => void findingsQuery.refetch()}
      />
    )
  }
  if (findingsQuery.isPending || (rulesQuery.isPending && !rulesQuery.data)) {
    return (
      <section aria-busy="true" aria-label="Loading advisor findings" role="status">
        Loading advisor findings…
      </section>
    )
  }

  const defaultView = state === 'active' && !searchParams.has('state')
  const hiddenMuted = defaultView ? findings.filter((finding) => finding.state === 'muted').length : 0
  const hiddenResolved = defaultView
    ? findings.filter((finding) => finding.state === 'resolved').length
    : 0

  return (
    <div className="space-y-8">
      <PageHeader
        title="Advisor findings"
        subtitle="Rules evaluated from collected PostgreSQL statistics, with the evidence and inputs behind each result."
        freshness={<FreshnessBadge dataUpdatedAt={findingsQuery.dataUpdatedAt} policy="findings" />}
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
            Advisor rule explanations are unavailable; findings remain visible without catalogue context.
          </p>
        </div>
      ) : null}

      <Section
        title="Summary"
        description="Open findings are grouped by severity; degraded rules could not be evaluated."
      >
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
        {defaultView && (hiddenMuted > 0 || hiddenResolved > 0) ? (
          <p className="text-muted-foreground mt-3 text-sm">
            By default, {hiddenMuted} muted and {hiddenResolved} resolved findings are hidden. Use the
            state filter to show them.
          </p>
        ) : null}
        {allFindingsDegraded ? (
          <p className="text-warning mt-3 text-sm" role="status">
            Rules could not be evaluated; this is not a healthy “no findings” result.
          </p>
        ) : null}
      </Section>

      <Section title="Filters" description="Filters are kept in the URL so a findings view can be shared.">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <label className="space-y-1 text-sm" htmlFor="findings-state">
            <span className="font-medium">State</span>
            <select
              className="border-input bg-background h-9 w-full rounded-md border px-3"
              id="findings-state"
              onChange={(event) => updateFilter('state', event.target.value === 'active' ? '' : event.target.value)}
              value={state}
            >
              <option value="active">Open and degraded</option>
              <option value="all">All states</option>
              <option value="open">Open</option>
              <option value="degraded">Degraded</option>
              <option value="muted">Muted</option>
              <option value="resolved">Resolved</option>
            </select>
          </label>
          <label className="space-y-1 text-sm" htmlFor="findings-severity">
            <span className="font-medium">Severity</span>
            <select
              className="border-input bg-background h-9 w-full rounded-md border px-3"
              id="findings-severity"
              onChange={(event) => updateFilter('severity', event.target.value === 'all' ? '' : event.target.value)}
              value={severity}
            >
              <option value="all">All severities</option>
              <option value="critical">Critical</option>
              <option value="warning">Warning</option>
              <option value="info">Info</option>
            </select>
          </label>
          <label className="space-y-1 text-sm" htmlFor="findings-scope">
            <span className="font-medium">Scope</span>
            <select
              className="border-input bg-background h-9 w-full rounded-md border px-3"
              id="findings-scope"
              onChange={(event) => updateFilter('scope', event.target.value === 'all' ? '' : event.target.value)}
              value={scope}
            >
              <option value="all">All scopes</option>
              <option value="cluster">Cluster</option>
              <option value="instance">Instance</option>
            </select>
          </label>
          <label className="space-y-1 text-sm" htmlFor="findings-cluster">
            <span className="font-medium">Cluster</span>
            <Input
              id="findings-cluster"
              onChange={(event) => updateFilter('cluster_id', event.target.value)}
              placeholder="Cluster ID"
              value={clusterId}
            />
          </label>
          <label className="space-y-1 text-sm" htmlFor="findings-instance">
            <span className="font-medium">Instance</span>
            <Input
              id="findings-instance"
              onChange={(event) => updateFilter('instance_id', event.target.value)}
              placeholder="Instance ID"
              value={instanceId}
            />
          </label>
        </div>
      </Section>

      <aside className="border-muted bg-muted/30 rounded-md border p-3 text-sm" role="note">
        Findings come from collected statistics, not query plans. Index recommendations are candidates
        for review, not automatic changes.
      </aside>

      <RuleCatalogue findings={findings} rules={rules} />

      <Section
        title="Findings"
        description={`${visibleFindings.length} of ${findings.length} findings shown`}
      >
        {visibleFindings.length > 0 ? (
          <div aria-label="Advisor findings" className="grid gap-4" role="list">
            {visibleFindings.map((finding) => (
              <div key={finding.finding_id} role="listitem">
                <FindingCard finding={finding} />
              </div>
            ))}
          </div>
        ) : (
          <EmptyState
            description={
              findings.length === 0
                ? `No findings are currently firing. ${rules.length} advisor rule${rules.length === 1 ? '' : 's'} evaluated successfully.`
                : 'Try clearing one or more filters or choose a different state.'
            }
            title={findings.length === 0 ? 'No active findings' : 'No findings match these filters'}
          />
        )}
      </Section>
    </div>
  )
}
