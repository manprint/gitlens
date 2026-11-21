import { Fragment, useMemo } from 'react'
import { Link } from 'react-router-dom'

import { useAdvisorWindows, useInstanceTables } from '@/api/queries'
import { CopyButton } from '@/components/layout/CopyButton'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useT, type Translate } from '@/i18n/react'
import { formatBytes, formatCount, formatPercent, formatRate } from '@/lib/format'
import type { IndexRow } from '@/lib/indexAnalysis'
import {
  buildCleanup,
  cleanupScript,
  indexWriteRates,
  summariseCleanup,
  UNUSED_INDEX_MIN_BYTES,
  unusedWindowPhrase,
  type CleanupCandidate,
  type CleanupReason,
} from '@/lib/indexCleanup'
import { summariseColumns } from '@/lib/indexDefinition'
import { FLAG_GUIDE, flagTitle, guideText, type FlagLabel } from '@/lib/indexGuide'

import { DEAD_ROWS_LIMIT } from './DeadRowsPanel'
import { noIndexStats } from './emptyText'
import { CELL_BADGE, IndexPanel, PanelNote } from './IndexPanel'

type MessageKey = Parameters<Translate>[0]

const REASONS: Record<
  CleanupReason,
  { labelKey: MessageKey; variant: 'destructive' | 'warning' | 'outline'; flag: FlagLabel }
> = {
  invalid: { labelKey: 'indexes.cleanup.reasonInvalid', variant: 'destructive', flag: 'invalid' },
  duplicate: { labelKey: 'indexes.cleanup.reasonDuplicate', variant: 'warning', flag: 'duplicate' },
  redundant: {
    labelKey: 'indexes.cleanup.reasonRedundant',
    variant: 'warning',
    flag: 'redundant prefix',
  },
  unused: { labelKey: 'indexes.cleanup.reasonUnused', variant: 'warning', flag: 'unused' },
  'never-scanned': {
    labelKey: 'indexes.cleanup.reasonNeverScanned',
    variant: 'outline',
    flag: 'never scanned',
  },
}

function cleanupToolbar(t: Translate): TableToolbarSpec<CleanupCandidate> {
  return {
    searchFields: [
      (candidate) => candidate.row.name,
      (candidate) => `${candidate.row.schema}.${candidate.row.table}`,
    ],
    filters: [
      {
        id: 'reason',
        label: t('indexes.cleanup.reason'),
        kind: 'select',
        options: Object.entries(REASONS).map(([value, { labelKey }]) => ({
          value,
          label: t(labelKey),
        })),
        accessor: (candidate) => candidate.reason,
      },
      {
        id: 'basis',
        label: t('indexes.cleanup.basis'),
        kind: 'select',
        options: [
          { value: 'advisor', label: t('indexes.cleanup.basisAdvisor') },
          { value: 'statistics', label: t('indexes.cleanup.basisStatistics') },
        ],
        accessor: (candidate) => candidate.basis,
      },
    ],
  }
}

function cleanupColumns(t: Translate): readonly SimpleColumn<CleanupCandidate>[] {
  return [
    {
      id: 'index',
      header: t('indexes.cleanup.index'),
      sortValue: (candidate) => candidate.row.name,
      className: 'font-medium break-all max-[1599px]:min-w-48',
      width: '22rem',
      truncate: (candidate) => candidate.row.name,
      cell: (candidate) => candidate.row.name,
    },
    {
      id: 'table',
      header: t('indexes.cleanup.table'),
      sortValue: (candidate) => `${candidate.row.schema}.${candidate.row.table}`,
      className: 'break-all max-[1599px]:min-w-48',
      truncate: ({ row }) => `${row.datname} · ${row.schema}.${row.table}`,
      cell: ({ row }) => (
        <>
          <span className="text-muted-foreground">
            {row.datname} · {row.schema}.
          </span>
          {row.table}
        </>
      ),
    },
    {
      id: 'reason',
      header: t('indexes.cleanup.reason'),
      help: 'indexes.cleanup-reason-column',
      width: '10rem',
      cell: (candidate) => {
        const reason = REASONS[candidate.reason]
        return (
          <Badge className={CELL_BADGE} title={flagTitle(reason.flag)} variant={reason.variant}>
            {t(reason.labelKey)}
          </Badge>
        )
      },
    },
    {
      id: 'size',
      header: t('indexes.cleanup.size'),
      sortValue: (candidate) => candidate.row.bytes,
      className: 'whitespace-nowrap',
      width: '7rem',
      cell: ({ row }) =>
        row.bytes === null ? t('indexes.cleanup.sizeUnknown') : formatBytes(row.bytes),
    },
    {
      id: 'writes',
      header: t('indexes.cleanup.indexWrites'),
      help: 'indexes.cleanup-writes',
      sortValue: (candidate) => candidate.writeRate,
      className: 'whitespace-nowrap',
      width: '11.5rem',
      cell: (candidate) =>
        candidate.writeRate === null
          ? t('indexes.cleanup.notMeasured')
          : formatRate(candidate.writeRate),
    },
  ]
}

/** The servers that share a cluster with this one: they may use an index this one never scans. */
export interface SiblingServer {
  id: string
  label: string
}

function Statement({ label, subject, sql }: { label: string; subject: string; sql: string }) {
  return (
    <div className="space-y-1">
      <h4 className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
        {label}
      </h4>
      <div className="bg-muted/40 rounded-lg border">
        <pre className="overflow-x-auto px-3 py-2 font-mono text-xs break-words whitespace-pre-wrap">
          {sql}
        </pre>
        <div className="flex justify-end border-t px-1.5 py-0.5">
          <CopyButton subject={subject} text={sql} />
        </div>
      </div>
    </div>
  )
}

function Detail({
  candidate,
  siblings,
}: {
  candidate: CleanupCandidate
  siblings: readonly SiblingServer[]
}) {
  const t = useT()

  const { row } = candidate
  const rarelyUsed = candidate.reason === 'unused' || candidate.reason === 'never-scanned'
  return (
    <div className="space-y-4 px-4 py-4">
      <p className="text-sm">{candidate.why}</p>
      <p className="text-sm">
        <span className="font-medium">{t('indexes.cleanup.whatToDo')}</span>{' '}
        {guideText(FLAG_GUIDE[REASONS[candidate.reason].flag].action)}
      </p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
        <dt className="text-muted-foreground">{t('indexes.cleanup.columns')}</dt>
        <dd>
          <code className="font-mono">{summariseColumns(row.parsed) || '—'}</code>
        </dd>
        {candidate.coveredBy === null ? null : (
          <>
            <dt className="text-muted-foreground">{t('indexes.cleanup.stays')}</dt>
            <dd>
              <code className="font-mono">{candidate.coveredBy}</code>
            </dd>
          </>
        )}
      </dl>

      <Statement
        label={t('indexes.cleanup.dropLabel')}
        sql={candidate.dropStatement}
        subject={t('indexes.cleanup.dropStatementOf', { name: row.name })}
      />
      {candidate.recreateStatement === null ? (
        <p className="text-muted-foreground text-sm">{t('indexes.cleanup.noDefinition')}</p>
      ) : (
        <Statement
          label={t('indexes.cleanup.rebuildLabel')}
          sql={candidate.recreateStatement}
          subject={t('indexes.cleanup.rebuildStatementOf', { name: row.name })}
        />
      )}

      <div className="space-y-1 text-sm">
        <h4 className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          {t('indexes.cleanup.beforeDrop')}
        </h4>
        <ul className="list-disc space-y-1 pl-5">
          {rarelyUsed ? (
            <>
              <li>{t('indexes.cleanup.foreignKeyCheck')}</li>
              <li>{t('indexes.cleanup.reportCheck')}</li>
            </>
          ) : null}
          {candidate.reason === 'invalid' ? (
            <li>
              {t('indexes.cleanup.invalidBefore')} <code>CREATE INDEX CONCURRENTLY</code>
              {t('indexes.cleanup.invalidAfter')}
            </li>
          ) : null}
          {siblings.length > 0 ? (
            <li>
              {t('indexes.cleanup.standbyBefore')}{' '}
              {siblings.map((sibling, position) => (
                <Fragment key={sibling.id}>
                  {position > 0 ? ', ' : ''}
                  <Link
                    className="text-primary font-medium hover:underline"
                    to={`/indexes?instance=${encodeURIComponent(sibling.id)}&tab=cleanup`}
                  >
                    {sibling.label}
                  </Link>
                </Fragment>
              ))}
              {t('indexes.cleanup.standbyAfter')}
            </li>
          ) : null}
          <li>{t('indexes.cleanup.runDrop')}</li>
        </ul>
      </div>
    </div>
  )
}

interface CleanupPanelProps {
  instanceId: string
  /** The indexes in scope: the page's database filter already applied. */
  rows: readonly IndexRow[]
  siblings: readonly SiblingServer[]
  /** Why the list is empty when no index was reported. */
  emptyDescription?: string
}

/**
 * The indexes that look removable, with what a drop gives back. pglens never
 * runs a drop: it writes the statement, and the operator decides.
 */
export function CleanupPanel({
  instanceId,
  rows,
  siblings,
  emptyDescription = noIndexStats(),
}: CleanupPanelProps) {
  const t = useT()

  const tablesQuery = useInstanceTables(instanceId, {
    limit: DEAD_ROWS_LIMIT,
    order_by: 'dead_tup',
  })
  const writeRates = useMemo(
    () => indexWriteRates(tablesQuery.data?.items ?? []),
    [tablesQuery.data],
  )
  // The configured window names the length when no finding carries it; any
  // console session may read it. Until it loads, the texts name no length.
  const windowsQuery = useAdvisorWindows()
  const configuredWindow =
    windowsQuery.data?.find((window) => window.key === 'window.unused_index')?.seconds ?? null
  const candidates = useMemo(
    () => buildCleanup(rows, writeRates, configuredWindow),
    [rows, writeRates, configuredWindow],
  )
  const totalBytes = useMemo(() => rows.reduce((sum, row) => sum + (row.bytes ?? 0), 0), [rows])
  const summary = useMemo(() => summariseCleanup(candidates, totalBytes), [candidates, totalBytes])
  const script = useMemo(
    () => cleanupScript(candidates, configuredWindow),
    [candidates, configuredWindow],
  )
  const statistical = candidates.filter((candidate) => candidate.basis === 'statistics').length
  const columns = useMemo(() => cleanupColumns(t), [t])
  const toolbar = useMemo(() => cleanupToolbar(t), [t])

  const clusterText = t('indexes.cleanup.clusterTitle', {
    names: siblings.map((sibling) => sibling.label).join(', '),
  })
  const statisticalHeadline = t('indexes.cleanup.statisticalHeadline', {
    count: statistical,
    shown: formatCount(statistical),
  })
  const statisticalText = t('indexes.cleanup.statisticalText', {
    neverScanned: t('indexes.cleanup.reasonNeverScanned'),
    window: unusedWindowPhrase(rows, configuredWindow),
    bytes: formatBytes(UNUSED_INDEX_MIN_BYTES),
  })
  const notes =
    siblings.length > 0 || statistical > 0 ? (
      <>
        {siblings.length > 0 ? (
          <PanelNote title={clusterText}>
            <strong>{t('indexes.cleanup.clusterHeadline')}</strong>{' '}
            {t('indexes.cleanup.clusterCheck')}{' '}
            {siblings.map((sibling, position) => (
              <Fragment key={sibling.id}>
                {position > 0 ? ', ' : ''}
                <Link
                  className="text-primary font-medium hover:underline"
                  to={`/indexes?instance=${encodeURIComponent(sibling.id)}&tab=cleanup`}
                >
                  {sibling.label}
                </Link>
              </Fragment>
            ))}{' '}
            {t('indexes.cleanup.clusterAfter')}
          </PanelNote>
        ) : null}
        {statistical > 0 ? (
          <PanelNote title={`${statisticalHeadline} ${statisticalText}`}>
            <strong>{statisticalHeadline}</strong> {statisticalText}
          </PanelNote>
        ) : null}
      </>
    ) : null

  const writesUnmeasured =
    summary.confirmed.count > 0 && summary.confirmed.writeRateUnknown >= summary.confirmed.count

  return (
    <IndexPanel
      actions={
        <>
          <InfoHint id="indexes.cleanup-reason" label={t('indexes.cleanup.reasonHint')} />
          <HelpLink anchor="indexes#cleanup" label={t('indexes.cleanup.cleanupTab')} />
          {candidates.length > 0 ? (
            <CopyButton subject={t('indexes.cleanup.scriptSubject')} text={script} />
          ) : null}
        </>
      }
      captionId="indexes.cleanup"
      description={t('indexes.cleanup.description')}
      notes={notes}
      stats={[
        {
          label: t('indexes.cleanup.candidates'),
          value: formatCount(summary.count),
          unit: 'indexes',
          detail: t('indexes.cleanup.candidatesDetail', {
            confirmed: formatCount(summary.confirmed.count),
            provisional: formatCount(summary.provisional.count),
          }),
        },
        {
          label: t('indexes.cleanup.spaceBack'),
          value: formatBytes(summary.confirmed.reclaimBytes),
          detail: [
            summary.confirmedShare === null
              ? null
              : t('indexes.cleanup.percentOfIndexes', {
                  percent: formatPercent(summary.confirmed.reclaimBytes, totalBytes) ?? '',
                }),
            summary.provisional.count > 0
              ? t('indexes.cleanup.rawBytes', {
                  bytes: formatBytes(summary.provisional.reclaimBytes),
                })
              : null,
          ]
            .filter((part) => part !== null)
            .join(' · '),
        },
        {
          label: t('indexes.cleanup.writesSaved'),
          // No candidate has a measured write rate: a 0 would be a made-up figure.
          value: writesUnmeasured
            ? t('indexes.cleanup.notMeasured')
            : formatRate(summary.confirmed.writeRate).replace(/\/s$/, ''),
          ...(writesUnmeasured ? {} : { unit: 'per second' as const }),
          detail: [
            summary.confirmed.writeRateUnknown > 0 && !writesUnmeasured
              ? t('indexes.cleanup.countNotMeasured', {
                  n: formatCount(summary.confirmed.writeRateUnknown),
                })
              : null,
            summary.provisional.count > summary.provisional.writeRateUnknown
              ? t('indexes.cleanup.rawRate', {
                  rate: formatRate(summary.provisional.writeRate).replace(
                    '/s',
                    ' ' + t('indexes.cleanup.perSecond'),
                  ),
                })
              : null,
          ]
            .filter((part) => part !== null)
            .join(' · '),
        },
        {
          label: t('indexes.cleanup.unusedIndexes'),
          value: formatCount(summary.byReason.unused),
          unit: 'indexes',
          detail: t('indexes.cleanup.unusedDetail'),
        },
        {
          label: t('indexes.cleanup.duplicateIndexes'),
          value: formatCount(summary.byReason.duplicate + summary.byReason.redundant),
          unit: 'indexes',
        },
        {
          label: t('indexes.cleanup.invalidIndexes'),
          value: formatCount(summary.byReason.invalid),
          unit: 'indexes',
        },
      ]}
      title={t('indexes.cleanup.title')}
      titleId="cleanup-title"
    >
      {candidates.length === 0 ? (
        <EmptyState
          description={rows.length === 0 ? emptyDescription : t('indexes.cleanup.allClean')}
          title={t('indexes.cleanup.nothingToClean')}
        />
      ) : (
        <SimpleTable
          ariaLabel={t('indexes.cleanup.candidatesAria')}
          captionId="indexes.cleanup-table"
          columns={columns}
          detailLabel={(candidate, expanded) =>
            expanded
              ? t('indexes.cleanup.hideDrop', { name: candidate.row.name })
              : t('indexes.cleanup.showDrop', { name: candidate.row.name })
          }
          emptyState={null}
          renderDetail={(candidate) => <Detail candidate={candidate} siblings={siblings} />}
          rowKey={(candidate) => candidate.row.key}
          rows={candidates}
          scope="candidates"
          tableId="cleanup"
          toolbar={toolbar}
        />
      )}
    </IndexPanel>
  )
}
