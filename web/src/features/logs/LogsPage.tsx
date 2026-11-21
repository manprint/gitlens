import { ChevronDown, ChevronRight, ScrollText } from 'lucide-react'
import { Fragment, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useLogClasses, useLogs } from '@/api/queries'
import type { components } from '@/api/generated'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { SearchBox } from '@/components/layout/SearchBox'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { useTimeRange } from '@/hooks/useTimeRange'
import { useT } from '@/i18n/react'
import { formatTimestamp } from '@/lib/format'
import {
  LOG_CLASSES,
  LOG_SEVERITIES,
  LOGS_PAGE_LIMIT,
  type LogFilters,
  logExtraEntries,
  logQueryHref,
  logSeverityVariant,
  logsRequest,
  readLogFilters,
  withLogFilter,
} from '@/lib/logs'
import { queryWindowOf } from '@/lib/timerange'

import { LogClassSummary } from './LogClassSummary'

type LogEvent = components['schemas']['LogEvent']

const NONE = '—'

function LogRow({ event, instanceId }: { event: LogEvent; instanceId: string }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const queryHref = logQueryHref(instanceId, event.queryid)
  const extra = logExtraEntries(event.extra)
  return (
    <Fragment>
      <tr className="border-b align-top" data-testid="log-row">
        <td className="py-1.5 pr-2">
          <Button
            aria-expanded={open}
            aria-label={open ? t('logs.row.collapse') : t('logs.row.expand')}
            onClick={() => setOpen(!open)}
            size="icon-sm"
            variant="ghost"
          >
            {open ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
          </Button>
        </td>
        <td className="py-1.5 pr-3 whitespace-nowrap tabular-nums">{formatTimestamp(event.ts)}</td>
        <td className="py-1.5 pr-3">
          <Badge variant={logSeverityVariant(event.severity)}>{event.severity}</Badge>
        </td>
        <td className="py-1.5 pr-3 font-mono text-xs whitespace-nowrap">{event.class}</td>
        <td className="py-1.5 pr-3">{event.datname || NONE}</td>
        <td className="py-1.5 pr-3">{event.usename || NONE}</td>
        <td className="max-w-[48rem] py-1.5 break-words">
          <span className={open ? '' : 'line-clamp-2'}>{event.message}</span>
          {queryHref ? (
            <Link
              className="text-primary ml-2 text-xs underline-offset-4 hover:underline"
              to={queryHref}
            >
              {t('logs.row.query')}
            </Link>
          ) : null}
        </td>
      </tr>
      {open ? (
        <tr className="bg-muted/40 border-b" data-testid="log-row-detail">
          <td />
          <td className="space-y-2 py-2" colSpan={6}>
            <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2 lg:grid-cols-4">
              <div>
                <dt className="text-muted-foreground text-xs">{t('logs.row.application')}</dt>
                <dd>{event.application_name || NONE}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">{t('logs.row.pid')}</dt>
                <dd>{event.pid ?? NONE}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">{t('logs.row.sqlstate')}</dt>
                <dd className="font-mono">{event.sqlstate || NONE}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">{t('logs.row.queryid')}</dt>
                <dd className="font-mono">{event.queryid ?? NONE}</dd>
              </div>
            </dl>
            {extra.length > 0 ? (
              <dl className="space-y-1 text-sm">
                {extra.map(([key, value]) => (
                  <div key={key}>
                    <dt className="text-muted-foreground font-mono text-xs">{key}</dt>
                    <dd>
                      <pre className="bg-muted max-h-48 overflow-auto rounded p-2 text-xs whitespace-pre-wrap">
                        {value}
                      </pre>
                    </dd>
                  </div>
                ))}
              </dl>
            ) : (
              <p className="text-muted-foreground text-sm">{t('logs.row.noExtra')}</p>
            )}
          </td>
        </tr>
      ) : null}
    </Fragment>
  )
}

function Filters({
  filters,
  onChange,
}: {
  filters: LogFilters
  onChange: (key: keyof LogFilters, value: string) => void
}) {
  const t = useT()
  return (
    <div
      aria-label={t('logs.filter.label')}
      className="flex flex-wrap items-end gap-3"
      role="group"
    >
      <label className="space-y-1 text-sm">
        <span className="text-muted-foreground block text-xs">{t('logs.filter.class')}</span>
        <NativeSelect value={filters.class} onChange={(e) => onChange('class', e.target.value)}>
          <option value="">{t('logs.classes.all')}</option>
          {LOG_CLASSES.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </NativeSelect>
      </label>
      <label className="space-y-1 text-sm">
        <span className="text-muted-foreground block text-xs">{t('logs.filter.severity')}</span>
        <NativeSelect
          value={filters.severity}
          onChange={(e) => onChange('severity', e.target.value)}
        >
          <option value="">{t('logs.filter.anySeverity')}</option>
          {LOG_SEVERITIES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </NativeSelect>
      </label>
      <div className="w-48">
        <SearchBox
          label={t('logs.filter.database')}
          placeholder={t('logs.filter.database')}
          value={filters.database}
          onChange={(value) => onChange('database', value)}
        />
      </div>
      <div className="w-72">
        <SearchBox
          label={t('logs.filter.text')}
          placeholder={t('logs.filter.text')}
          value={filters.q}
          onChange={(value) => onChange('q', value)}
        />
      </div>
    </div>
  )
}

export interface LogsPageProps {
  instanceId?: string
}

/** The PostgreSQL log lines the agent shipped for one instance (plan 007 6.6). */
export function LogsPage({ instanceId: explicitInstanceId }: LogsPageProps = {}) {
  const t = useT()
  const { instanceId: routeInstanceId } = useParams<{ instanceId: string }>()
  const instanceId = explicitInstanceId ?? routeInstanceId ?? ''
  const [searchParams, setSearchParams] = useSearchParams()
  const { range } = useTimeRange()
  const queryWindow = queryWindowOf(range)
  const filters = readLogFilters(searchParams)
  const logsQuery = useLogs(logsRequest(instanceId, filters), queryWindow)
  const classesQuery = useLogClasses(instanceId, queryWindow)

  if (!instanceId) {
    return <p role="status">{t('logs.page.selectInstance')}</p>
  }

  const setFilter = (key: keyof LogFilters, value: string) =>
    setSearchParams((current) => withLogFilter(current, key, value), { replace: true })

  const events = logsQuery.data
  const classes = classesQuery.data
  const nothingShipped = classes?.length === 0 && events?.length === 0

  let body
  if (logsQuery.isPending && events === undefined) {
    body = <p role="status">{t('logs.events.loading')}</p>
  } else if (logsQuery.isError && events === undefined) {
    body = (
      <ErrorState
        failure={logsQuery.error}
        onRetry={() => void logsQuery.refetch()}
        endpoint="logs"
      />
    )
  } else if (!events || events.length === 0) {
    body = <p className="text-muted-foreground text-sm">{t('logs.events.none')}</p>
  } else {
    const last = events[events.length - 1]
    body = (
      <div className="space-y-3">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-muted-foreground border-b text-left text-xs">
                <th className="w-8" />
                <th className="py-1.5 pr-3 font-medium">{t('logs.column.time')}</th>
                <th className="py-1.5 pr-3 font-medium">{t('logs.column.severity')}</th>
                <th className="py-1.5 pr-3 font-medium">{t('logs.column.class')}</th>
                <th className="py-1.5 pr-3 font-medium">{t('logs.column.database')}</th>
                <th className="py-1.5 pr-3 font-medium">{t('logs.column.user')}</th>
                <th className="py-1.5 font-medium">{t('logs.column.message')}</th>
              </tr>
            </thead>
            <tbody>
              {events.map((event, index) => (
                <LogRow
                  event={event}
                  instanceId={instanceId}
                  key={`${event.ts}-${event.pid ?? ''}-${index}`}
                />
              ))}
            </tbody>
          </table>
        </div>
        <div className="flex gap-2">
          {filters.before ? (
            <Button onClick={() => setFilter('before', '')} size="sm" variant="outline">
              {t('logs.events.newest')}
            </Button>
          ) : null}
          {events.length === LOGS_PAGE_LIMIT && last ? (
            <Button onClick={() => setFilter('before', last.ts)} size="sm" variant="outline">
              {t('logs.events.older')}
            </Button>
          ) : null}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        help={<HelpLink anchor="logs" label={t('logs.page.help')} />}
        icon={ScrollText}
        eyebrow={t('logs.page.eyebrow')}
        title={t('logs.page.title')}
        subtitle={t('logs.page.subtitle')}
      />
      {nothingShipped && !filters.class && !filters.severity && !filters.database && !filters.q ? (
        <EmptyState title={t('logs.empty.title')} description={t('logs.empty.description')} />
      ) : (
        <>
          <LogClassSummary
            classes={classes}
            failure={classesQuery.isError ? classesQuery.error : null}
            loading={classesQuery.isPending}
            onRetry={() => void classesQuery.refetch()}
            onSelect={(value) => setFilter('class', value)}
            selected={filters.class}
          />
          <Section captionId="logs.events" title={t('logs.events.title')}>
            <div className="space-y-4">
              <Filters filters={filters} onChange={setFilter} />
              {body}
            </div>
          </Section>
        </>
      )}
    </div>
  )
}

export default LogsPage
