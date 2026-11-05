import { useMemo } from 'react'

import type { PermTier } from '@/api/types'
import { Caption } from '@/components/layout/Caption'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import type { CaptionId, ColumnHelpId } from '@/lib/captions'
import {
  SESSION_FILTERS,
  connectionGroup,
  countOf,
  SESSION_SEARCH_FIELDS,
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
  title: string
  captionId: CaptionId
  tableId: string
  empty: string
}

const ORIGIN_TABLES: Record<SessionOrigin, OriginTable> = {
  application: {
    title: 'Application sessions',
    captionId: 'locks.sessions-application',
    tableId: 'app-sessions',
    empty: 'No application sessions at this moment',
  },
  pglens: {
    title: 'pglens monitoring sessions',
    captionId: 'locks.sessions-pglens',
    tableId: 'pglens-sessions',
    empty: 'No pglens sessions',
  },
}

const TOOLBAR: TableToolbarSpec<Session> = {
  searchFields: SESSION_SEARCH_FIELDS,
  filters: SESSION_FILTERS,
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

const COLUMNS: readonly SimpleColumn<Session>[] = [
  {
    id: 'pid',
    header: 'PID',
    help: 'sessions.pid',
    width: '7rem',
    sortValue: SESSION_SORT.pid,
    cell: sessionPid,
  },
  textColumn('user', 'User', sessionUser, '8rem'),
  textColumn('application', 'Application', sessionApplication, '10rem'),
  textColumn('database', 'Database', sessionDatabase, '8rem'),
  textColumn('client', 'Client', sessionClient, '8rem'),
  textColumn('state', 'State', sessionState, '10rem', 'sessions.state'),
  textColumn('wait', 'Wait', sessionWait, '10rem', 'sessions.wait'),
  {
    id: 'xact_age',
    header: 'Transaction age',
    help: 'sessions.transaction-age',
    width: '11rem',
    firstDirection: 'desc',
    sortValue: SESSION_SORT.xact_age,
    cell: (session) => formatAge(transactionAge(session)),
  },
  {
    id: 'query_age',
    header: 'Query age',
    help: 'sessions.query-age',
    width: '10rem',
    firstDirection: 'desc',
    sortValue: SESSION_SORT.query_age,
    cell: (session) => formatAge(queryAge(session)),
  },
  {
    id: 'query',
    header: 'Query',
    sortValue: SESSION_SORT.query,
    className: 'max-w-md truncate min-[1600px]:max-w-none',
    truncate: (session) => sessionQueryText(session)?.text ?? '',
    cell: (session) => {
      const query = sessionQueryText(session)
      if (query === null) return NONE
      return (
        <>
          {query.source === 'raw' ? (
            <span className="text-muted-foreground mr-1 text-xs">raw</span>
          ) : null}
          {firstLine(query.text) ?? NONE}
        </>
      )
    },
  },
]

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
      <h3 className="font-medium">{meta.title}</h3>
      {sampleTotal !== undefined && sampleTotal > rows.length ? (
        <p className="text-muted-foreground text-xs">
          The sample holds {rows.length} of the {sampleTotal} connections of this origin: the
          sessions with the oldest transactions.
        </p>
      ) : null}
      {rows.length === 0 ? (
        <div className="space-y-2" data-caption-id={meta.captionId} data-card="">
          <div className="flex w-full min-w-0 items-center gap-1">
            <Caption className="text-xs" id={meta.captionId} marked={false} oneLine />
            <InfoHint className="shrink-0" id={meta.captionId} label={meta.title} />
          </div>
          <p className="text-muted-foreground text-sm">{meta.empty}</p>
        </div>
      ) : (
        <SimpleTable
          ariaLabel={meta.title}
          captionId={meta.captionId}
          columns={COLUMNS}
          defaultSort={{ id: 'xact_age', desc: true }}
          detailLabel={(row, expanded) =>
            `${expanded ? 'Hide' : 'Show'} details of session ${sessionPid(row)}`
          }
          emptyState={null}
          renderDetail={detail}
          rowKey={rowKey}
          rows={rows}
          scope="sessions"
          tableId={meta.tableId}
          toolbar={TOOLBAR}
        />
      )}
    </div>
  )
}
