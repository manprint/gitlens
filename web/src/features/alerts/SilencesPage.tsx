import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { useDeleteSilence } from '@/api/silences'
import { useAlerts, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { formatDuration } from '@/lib/format'
import { silenceWindow, type Silence } from '@/lib/alerts'

import { SilenceEditor } from './SilenceEditor'

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

  function endSilence(silence: Silence) {
    if (
      !window.confirm(
        `End silence ${silence.silence_id}? Notifications will no longer be suppressed.`,
      )
    )
      return
    setStatus(null)
    deleteSilence.mutate(
      { silenceId: silence.silence_id },
      { onSuccess: () => setStatus(`Ended silence ${silence.silence_id}.`) },
    )
  }

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
        actions={
          <>
            <Link className="underline" to="/alerts">
              Alerts
            </Link>
            <Link className="underline" to="/alerts/rules">
              Rules
            </Link>
            <button onClick={() => setEditorOpen((open) => !open)} type="button">
              {editorOpen ? 'Close editor' : 'Create silence'}
            </button>
          </>
        }
        freshness={<FreshnessBadge dataUpdatedAt={silencesQuery.dataUpdatedAt} policy="findings" />}
        subtitle="Suppress notifications during planned work while keeping alert evaluation and visibility intact."
        title="Alert silences"
      />

      {alertsQuery.error ? (
        <div className="border-warning/40 bg-warning/10 p-3 text-sm" role="alert">
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
      {deleteSilence.error ? <p role="alert">{deleteSilence.error.message}</p> : null}

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
          <div className="overflow-x-auto">
            <table aria-label="Alert silences" className="w-full text-left text-sm">
              <thead>
                <tr>
                  <th scope="col">Matchers</th>
                  <th scope="col">Reason</th>
                  <th scope="col">Start</th>
                  <th scope="col">End</th>
                  <th scope="col">State</th>
                  <th scope="col">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {sortedSilences.map((silence) => (
                  <tr key={silence.silence_id}>
                    <td>{matcherLabel(silence)}</td>
                    <td>{silence.reason}</td>
                    <td>{silence.starts_at}</td>
                    <td>{silence.ends_at}</td>
                    <td>{windowLabel(silence)}</td>
                    <td>
                      <button
                        aria-label={`End silence ${silence.silence_id}`}
                        disabled={deleteSilence.isPending}
                        onClick={() => endSilence(silence)}
                        type="button"
                      >
                        End silence
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Section>
    </div>
  )
}
