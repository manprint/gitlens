import { BellOff } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { useDeleteSilence } from '@/api/silences'
import { useAlerts, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { Button } from '@/components/ui/button'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { TIMESTAMP_COLUMN_WIDTH, formatDuration, formatTimestamp } from '@/lib/format'
import { useT, type Translate } from '@/i18n/react'
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

function matcherLabel(silence: Silence, t: Translate): string {
  const matchers = readMatchers(silence)
  if (matchers.length === 0) return t('alerts.silences.noMatchers')
  return matchers
    .map(
      (matcher) =>
        `${matcher.negate ? `${t('alerts.silences.not')} ` : ''}${matcher.name}${matcher.is_regex ? '~' : '='}${matcher.value}`,
    )
    .join(', ')
}

function windowLabel(silence: Silence, t: Translate): string {
  const window = silenceWindow(silence, new Date())
  if (window.state === 'pending')
    return t('alerts.silences.pendingIn', {
      duration: formatDuration(Math.ceil(window.remainingSeconds)),
    })
  if (window.state === 'active')
    return t('alerts.silences.activeEndsIn', {
      duration: formatDuration(Math.ceil(window.remainingSeconds)),
    })
  return t('alerts.silences.expired')
}

function silenceToolbar(t: Translate): TableToolbarSpec<Silence> {
  return {
    searchFields: [(silence) => matcherLabel(silence, t), (silence) => silence.reason],
    filters: [
      {
        id: 'state',
        label: t('alerts.column.state'),
        kind: 'select',
        options: [
          { value: 'pending', label: t('alerts.filter.pending') },
          { value: 'active', label: t('alerts.silences.active') },
          { value: 'expired', label: t('alerts.silences.expired') },
        ],
        accessor: (silence) => silenceWindow(silence, new Date()).state,
      },
    ],
  }
}

function silenceColumns(
  t: Translate,
  onEnd: (silence: Silence) => void,
  ending: boolean,
): SimpleColumn<Silence>[] {
  return [
    {
      id: 'matchers',
      header: t('alerts.silences.matchers'),
      help: 'silences.matchers',
      sortValue: (silence) => matcherLabel(silence, t),
      className: 'font-mono text-xs',
      width: '30%',
      truncate: (silence) => matcherLabel(silence, t),
      cell: (silence) => matcherLabel(silence, t),
    },
    {
      id: 'reason',
      header: t('alerts.silences.reason'),
      truncate: (silence) => silence.reason,
      cell: (silence) => silence.reason,
    },
    {
      id: 'start',
      header: t('alerts.silences.start'),
      width: TIMESTAMP_COLUMN_WIDTH,
      sortValue: (silence) => Date.parse(silence.starts_at),
      className: 'whitespace-nowrap',
      cell: (silence) => (
        <time dateTime={silence.starts_at}>{formatTimestamp(silence.starts_at)}</time>
      ),
    },
    {
      id: 'end',
      header: t('alerts.silences.end'),
      width: TIMESTAMP_COLUMN_WIDTH,
      sortValue: (silence) => Date.parse(silence.ends_at),
      className: 'whitespace-nowrap',
      cell: (silence) => <time dateTime={silence.ends_at}>{formatTimestamp(silence.ends_at)}</time>,
    },
    {
      id: 'state',
      header: t('alerts.column.state'),
      width: '13rem',
      className: 'whitespace-nowrap',
      truncate: (silence) => windowLabel(silence, t),
      cell: (silence) => windowLabel(silence, t),
    },
    {
      id: 'actions',
      header: t('alerts.silences.actions'),
      srOnlyHeader: true,
      width: '8rem',
      className: 'text-right',
      cell: (silence) => (
        <Button
          aria-label={t('alerts.silences.endAria', { id: silence.silence_id })}
          // From 1600 px the button fits the one-line row (D26).
          className="min-[1600px]:h-6 min-[1600px]:text-xs"
          disabled={ending}
          onClick={() => onEnd(silence)}
          size="sm"
          type="button"
          variant="outline"
        >
          {t('alerts.silences.endButton')}
        </Button>
      ),
    },
  ]
}

export function SilencesPage() {
  const t = useT()
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
      if (!window.confirm(t('alerts.silences.confirmEnd', { id: silence.silence_id }))) return
      setStatus(null)
      deleteMutate(
        { silenceId: silence.silence_id },
        { onSuccess: () => setStatus(t('alerts.silences.ended', { id: silence.silence_id })) },
      )
    },
    [deleteMutate, t],
  )
  const columns = useMemo(
    () => silenceColumns(t, endSilence, deletePending),
    [t, endSilence, deletePending],
  )
  const toolbar = useMemo(() => silenceToolbar(t), [t])

  if (silencesQuery.isPending || alertsQuery.isPending) {
    return (
      <section aria-busy="true" aria-label={t('alerts.silences.loadingLabel')} role="status">
        {t('alerts.silences.loading')}
      </section>
    )
  }
  if (silencesQuery.error) {
    return (
      <ErrorState
        endpoint={t('alerts.endpoint.silences')}
        failure={silencesQuery.error}
        onRetry={() => void silencesQuery.refetch()}
      />
    )
  }

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        help={<HelpLink anchor="silences" label={t('alerts.silences.helpLabel')} />}
        icon={BellOff}
        eyebrow={t('alerts.page.eyebrow')}
        actions={
          <>
            <Link
              className="hover:bg-muted inline-flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition-colors"
              to="/alerts"
            >
              {t('alerts.section.alerts')}
            </Link>
            <Link
              className="hover:bg-muted inline-flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition-colors"
              to="/alerts/rules"
            >
              {t('alerts.silences.rules')}
            </Link>
            <Button onClick={() => setEditorOpen((open) => !open)} type="button">
              {editorOpen ? t('alerts.silences.closeEditor') : t('alerts.silences.create')}
            </Button>
          </>
        }
        freshness={<FreshnessBadge dataUpdatedAt={silencesQuery.dataUpdatedAt} policy="findings" />}
        subtitle={t('alerts.silences.subtitle')}
        title={t('alerts.silences.title')}
      />

      <AlertListTruncated truncated={alertsQuery.truncated} />
      {alertsQuery.error ? (
        <div className="border-warning/40 bg-warning/10 rounded-lg border p-3 text-sm" role="alert">
          {t('alerts.silences.firingUnavailable')}
          <button
            className="ml-2 underline"
            onClick={() => void alertsQuery.refetch()}
            type="button"
          >
            {t('alerts.silences.retryFiring')}
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
            setStatus(t('alerts.silences.created'))
          }}
          previewUnavailable={Boolean(alertsQuery.error)}
        />
      ) : null}

      <Section
        actions={<HelpLink anchor="silences#list" label={t('alerts.silences.stored')} />}
        captionId="silences.list"
        title={t('alerts.silences.stored')}
      >
        {sortedSilences.length === 0 ? (
          <EmptyState
            description={t('alerts.silences.emptyDescription')}
            title={t('alerts.silences.emptyTitle')}
          />
        ) : (
          <SimpleTable
            ariaLabel={t('alerts.silences.tableLabel')}
            captionId="silences.table"
            columns={columns}
            emptyState={null}
            rowKey={(silence) => silence.silence_id}
            rows={sortedSilences}
            scope="silences"
            tableId="silences"
            toolbar={toolbar}
          />
        )}
      </Section>
    </div>
  )
}
