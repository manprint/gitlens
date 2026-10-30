import { BellOff } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { useDeleteSilence } from '@/api/silences'
import { useAlerts, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { Button } from '@/components/ui/button'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { formatDuration, formatTimestamp } from '@/lib/format'
import { silenceWindow, type Silence } from '@/lib/alerts'

import { SilenceEditor } from './SilenceEditor'
import { Callout } from '@/components/ui/callout'

interface SilenceMatcher extends Record<string, unknown> {
  name: string
  value: string
  is_regex?: boolean
  negate?: boolean
}

const EMPTY_SILENCES: Silence[] = []
const EMPTY_ALERTS = [] as NonNullable<ReturnType<typeof useAlerts>['data']>

function readMatchers(silence: Silence): SilenceMatcher[] {
  return silence.matchers.filter(
    (matcher): matcher is SilenceMatcher =>
      typeof matcher === 'object' &&
      matcher !== null &&
      typeof matcher.name === 'string' &&
      typeof matcher.value === 'string',
  )
}

function matcherLabel(silence: Silence): string {
  const matchers = readMatchers(silence)
  if (matchers.length === 0) return 'No matchers'
  return matchers
    .map(
      (matcher) =>
        `${matcher.negate ? 'not ' : ''}${matcher.name}${matcher.is_regex ? '~' : '='}${matcher.value}`,
    )
    .join(', ')
}

function windowLabel(silence: Silence): string {
  const window = silenceWindow(silence, new Date())
  if (window.state === 'pending')
    return `Pending · starts in ${formatDuration(Math.ceil(window.remainingSeconds))}`
  if (window.state === 'active')
    return `Active · ends in ${formatDuration(Math.ceil(window.remainingSeconds))}`
  return 'Expired'
}

const SILENCE_STATES = [
  { value: 'pending', label: 'Pending' },
  { value: 'active', label: 'Active' },
  { value: 'expired', label: 'Expired' },
]

const SILENCE_TOOLBAR: TableToolbarSpec<Silence> = {
  searchFields: [matcherLabel, (silence) => silence.reason],
  filters: [
    {
      id: 'state',
      label: 'State',
      kind: 'select',
      options: SILENCE_STATES,
      accessor: (silence) => silenceWindow(silence, new Date()).state,
    },
  ],
}

function silenceColumns(
  onEnd: (silence: Silence) => void,
  ending: boolean,
): SimpleColumn<Silence>[] {
  return [
    {
      id: 'matchers',
      header: 'Matchers',
      sortValue: matcherLabel,
      className: 'font-mono text-xs',
      cell: matcherLabel,
    },
    { id: 'reason', header: 'Reason', cell: (silence) => silence.reason },
    {
      id: 'start',
      header: 'Start',
      sortValue: (silence) => Date.parse(silence.starts_at),
      className: 'whitespace-nowrap',
      cell: (silence) => (
        <time dateTime={silence.starts_at}>{formatTimestamp(silence.starts_at, 'UTC')}</time>
      ),
    },
    {
      id: 'end',
      header: 'End',
      sortValue: (silence) => Date.parse(silence.ends_at),
      className: 'whitespace-nowrap',
      cell: (silence) => (
        <time dateTime={silence.ends_at}>{formatTimestamp(silence.ends_at, 'UTC')}</time>
      ),
    },
    { id: 'state', header: 'State', className: 'whitespace-nowrap', cell: windowLabel },
    {
      id: 'actions',
      header: 'Actions',
      srOnlyHeader: true,
      className: 'text-right',
      cell: (silence) => (
        <Button
          aria-label={`End silence ${silence.silence_id}`}
          disabled={ending}
          onClick={() => onEnd(silence)}
          size="sm"
          type="button"
          variant="outline"
        >
          End silence
        </Button>
      ),
    },
  ]
}

export function SilencesPage() {
  const silencesQuery = useSilences({ all: true })
  const alertsQuery = useAlerts({ state: 'firing' })
  const deleteSilence = useDeleteSilence()
  const [editorOpen, setEditorOpen] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const silences = silencesQuery.data ?? EMPTY_SILENCES
  const firingAlerts = alertsQuery.data ?? EMPTY_ALERTS
  const sortedSilences = useMemo(
    () => [...silences].sort((left, right) => Date.parse(left.ends_at) - Date.parse(right.ends_at)),
    [silences],
  )

  const { mutate: deleteMutate, isPending: deletePending } = deleteSilence
  const endSilence = useCallback(
    (silence: Silence) => {
      if (
        !window.confirm(
          `End silence ${silence.silence_id}? Notifications will no longer be suppressed.`,
        )
      )
        return
      setStatus(null)
      deleteMutate(
        { silenceId: silence.silence_id },
        { onSuccess: () => setStatus(`Ended silence ${silence.silence_id}.`) },
      )
    },
    [deleteMutate],
  )
  const columns = useMemo(
    () => silenceColumns(endSilence, deletePending),
    [endSilence, deletePending],
  )

  if (silencesQuery.isPending || alertsQuery.isPending) {
    return (
      <section aria-busy="true" aria-label="Loading silences" role="status">
        Loading silences…
      </section>
    )
  }
  if (silencesQuery.error) {
    return (
      <ErrorState
        endpoint="silences"
        failure={silencesQuery.error}
        onRetry={() => void silencesQuery.refetch()}
      />
    )
  }

  return (
    <div className="space-y-8">
      <PageHeader
        icon={BellOff}
        eyebrow="Alerting"
        actions={
          <>
            <Link
              className="hover:bg-muted inline-flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition-colors"
              to="/alerts"
            >
              Alerts
            </Link>
            <Link
              className="hover:bg-muted inline-flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition-colors"
              to="/alerts/rules"
            >
              Rules
            </Link>
            <Button onClick={() => setEditorOpen((open) => !open)} type="button">
              {editorOpen ? 'Close editor' : 'Create silence'}
            </Button>
          </>
        }
        freshness={<FreshnessBadge dataUpdatedAt={silencesQuery.dataUpdatedAt} policy="findings" />}
        subtitle="Suppress notifications during planned work while keeping alert evaluation and visibility intact."
        title="Alert silences"
      />

      <AlertListTruncated truncated={alertsQuery.truncated} />
      {alertsQuery.error ? (
        <div className="border-warning/40 bg-warning/10 rounded-lg border p-3 text-sm" role="alert">
          Current firing alerts are unavailable; the silence list remains available but creation
          preview is disabled.
          <button
            className="ml-2 underline"
            onClick={() => void alertsQuery.refetch()}
            type="button"
          >
            Retry firing alerts
          </button>
        </div>
      ) : null}
      {status ? (
        <p aria-live="polite" role="status">
          {status}
        </p>
      ) : null}
      {deleteSilence.error ? (
        <Callout variant="destructive">{deleteSilence.error.message}</Callout>
      ) : null}

      {editorOpen ? (
        <SilenceEditor
          firingAlerts={firingAlerts}
          onCancel={() => setEditorOpen(false)}
          onCreated={() => {
            setEditorOpen(false)
            setStatus('Silence created.')
          }}
          previewUnavailable={Boolean(alertsQuery.error)}
        />
      ) : null}

      <Section
        title="Stored silences"
        description="Pending, active, and expired silences are listed for auditability."
      >
        {sortedSilences.length === 0 ? (
          <EmptyState description="No silences have been created." title="No stored silences" />
        ) : (
          <SimpleTable
            ariaLabel="Alert silences"
            columns={columns}
            emptyState={null}
            rowKey={(silence) => silence.silence_id}
            rows={sortedSilences}
            scope="silences"
            tableId="silences"
            toolbar={SILENCE_TOOLBAR}
          />
        )}
      </Section>
    </div>
  )
}
