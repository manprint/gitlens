import { useMemo } from 'react'

import type { PermTier } from '@/api/types'
import { Caption } from '@/components/layout/Caption'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
import type { CaptionId, ColumnHelpId } from '@/lib/captions'
import {
  connectionGroup,
  countOf,
  SESSION_SEARCH_FIELDS,
  sessionFilters,
  SESSION_SORT,
  firstLine,
  formatAge,
  queryAge,
  rowKey,
  sessionApplication,
  sessionClient,
  sessionDatabase,
  sessionPid,
  sessionQueryText,
  sessionState,
  sessionUser,
  sessionWait,
  transactionAge,
  type ConnectionsSummary,
  type Session,
  type SessionOrigin,
} from '@/lib/sessions'
import { SessionDetail } from './SessionDetail'

const NONE = '—'

interface OriginTable {
  title: MessageKey
  captionId: CaptionId
  tableId: string
  empty: MessageKey
}

const ORIGIN_TABLES: Record<SessionOrigin, OriginTable> = {
  application: {
    title: 'locks.sessions.applicationTitle',
    captionId: 'locks.sessions-application',
    tableId: 'app-sessions',
    empty: 'locks.sessions.applicationEmpty',
  },
  pglens: {
    title: 'locks.sessions.pglensTitle',
    captionId: 'locks.sessions-pglens',
    tableId: 'pglens-sessions',
    empty: 'locks.sessions.pglensEmpty',
  },
}

function sessionToolbar(): TableToolbarSpec<Session> {
  return { searchFields: SESSION_SEARCH_FIELDS, filters: sessionFilters() }
}

function textColumn(
  id: 'user' | 'application' | 'database' | 'client' | 'state' | 'wait',
  header: string,
  read: (session: Session) => string | null,
  width: string,
  help?: ColumnHelpId,
): SimpleColumn<Session> {
  return {
    id,
    header,
    width,
    ...(help === undefined ? {} : { help }),
    sortValue: SESSION_SORT[id],
    truncate: (session) => read(session) ?? '',
    cell: (session) => read(session) ?? NONE,
  }
}

function sessionColumns(t: Translate): readonly SimpleColumn<Session>[] {
  return [
    {
      id: 'pid',
      header: t('locks.sessions.pid'),
      help: 'sessions.pid',
      width: '7rem',
      sortValue: SESSION_SORT.pid,
      cell: sessionPid,
    },
    textColumn('user', t('locks.sessions.user'), sessionUser, '8rem'),
    textColumn('application', t('locks.sessions.application'), sessionApplication, '10rem'),
    textColumn('database', t('locks.sessions.database'), sessionDatabase, '8rem'),
    textColumn('client', t('locks.sessions.client'), sessionClient, '8rem'),
    textColumn('state', t('locks.sessions.state'), sessionState, '10rem', 'sessions.state'),
    textColumn('wait', t('locks.sessions.wait'), sessionWait, '10rem', 'sessions.wait'),
    {
      id: 'xact_age',
      header: t('locks.sessions.transactionAge'),
      help: 'sessions.transaction-age',
      width: '11rem',
      firstDirection: 'desc',
      sortValue: SESSION_SORT.xact_age,
      cell: (session) => formatAge(transactionAge(session)),
    },
    {
      id: 'query_age',
      header: t('locks.sessions.queryAge'),
      help: 'sessions.query-age',
      width: '10rem',
      firstDirection: 'desc',
      sortValue: SESSION_SORT.query_age,
      cell: (session) => formatAge(queryAge(session)),
    },
    {
      id: 'query',
      header: t('locks.sessions.query'),
      sortValue: SESSION_SORT.query,
      className: 'max-w-md truncate min-[1600px]:max-w-none',
      truncate: (session) => sessionQueryText(session)?.text ?? '',
      cell: (session) => {
        const query = sessionQueryText(session)
        if (query === null) return NONE
        return (
          <>
            {query.source === 'raw' ? (
              <span className="text-muted-foreground mr-1 text-xs">
                {t('locks.sessions.rawSource')}
              </span>
            ) : null}
            {firstLine(query.text) ?? NONE}
          </>
        )
      },
    },
  ]
}

/**
 * One origin's sessions as a table with the shared toolbar, sort buttons and
 * 20-row pages, newest-first by transaction age; a row expands to the session
 * detail. `sessions` holds every session of the sample (the detail names the
 * sessions one blocks), the table keeps the ones of its origin.
 */
export function SessionsTable({
  origin,
  rows,
  sessions,
  currentTier,
  instanceId,
  sampleStale,
  connections,
}: {
  origin: SessionOrigin
  rows: readonly Session[]
  sessions: readonly Session[]
  currentTier: PermTier
  instanceId: string | undefined
  sampleStale: boolean
  /** Names the connections of this origin on the instance when the sample was cut. */
  connections?: ConnectionsSummary | undefined
}) {
  const t = useT()
  const columns = useMemo(() => sessionColumns(t), [t])
  const toolbar = useMemo(() => sessionToolbar(), [])
  const meta = ORIGIN_TABLES[origin]
  const sampleTotal = connections?.truncated
    ? countOf(connectionGroup(connections, 'by_origin'), origin)
    : undefined
  const detail = useMemo(
    () => (row: Session) => (
      <SessionDetail
        currentTier={currentTier}
        instanceId={instanceId}
        sampleStale={sampleStale}
        session={row}
        sessions={sessions}
      />
    ),
    [currentTier, instanceId, sampleStale, sessions],
  )
  return (
    <div className="space-y-2" data-origin-table={origin}>
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-medium">{t(meta.title)}</h3>
        <HelpLink anchor="locks#sessions" label={t('locks.sessions.tableHelp')} />
      </div>
      {sampleTotal !== undefined && sampleTotal > rows.length ? (
        <p className="text-muted-foreground text-xs">
          {t('locks.sessions.sampleHolds', { shown: rows.length, total: sampleTotal })}
        </p>
      ) : null}
      {rows.length === 0 ? (
        <div className="space-y-2" data-caption-id={meta.captionId} data-card="">
          <div className="flex w-full min-w-0 items-center gap-1">
            <Caption className="text-xs" id={meta.captionId} marked={false} oneLine />
            <InfoHint className="shrink-0" id={meta.captionId} label={t(meta.title)} />
          </div>
          <p className="text-muted-foreground text-sm">{t(meta.empty)}</p>
        </div>
      ) : (
        <SimpleTable
          ariaLabel={t(meta.title)}
          captionId={meta.captionId}
          columns={columns}
          defaultSort={{ id: 'xact_age', desc: true }}
          detailLabel={(row, expanded) =>
            expanded
              ? t('locks.sessions.hideDetails', { pid: String(sessionPid(row)) })
              : t('locks.sessions.showDetails', { pid: String(sessionPid(row)) })
          }
          emptyState={null}
          renderDetail={detail}
          rowKey={rowKey}
          rows={rows}
          scope="sessions"
          tableId={meta.tableId}
          toolbar={toolbar}
        />
      )}
    </div>
  )
}
