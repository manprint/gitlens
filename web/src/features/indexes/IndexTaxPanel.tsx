import { useMemo } from 'react'
import { Link } from 'react-router-dom'

import { useInstanceTables } from '@/api/queries'
import { HelpLink } from '@/components/layout/HelpLink'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState } from '@/components/state'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useT, type Translate } from '@/i18n/react'
import { formatBytes, formatCount, formatCountOf, formatPercent, formatRate } from '@/lib/format'
import type { IndexRow } from '@/lib/indexAnalysis'
import { buildIndexTax, summariseIndexTax, type IndexTax } from '@/lib/indexTax'

import { noIndexStats } from './emptyText'
import { IndexPanel, PanelNote } from './IndexPanel'

const TAX_TOOLBAR: TableToolbarSpec<IndexTax> = {
  searchFields: [(tax) => `${tax.schema}.${tax.table}`, (tax) => tax.datname],
  filters: [],
}

/** The most tables the API returns for one server. */
const TABLE_LIMIT = 500

/** The Cleanup tab narrowed to one table: its search matches `schema.table`. */
function cleanupLink(instanceId: string, database: string, tax: IndexTax): string {
  const params = new URLSearchParams({ instance: instanceId, tab: 'cleanup' })
  if (database !== '') params.set('db', database)
  params.set('cleanup.q', `${tax.schema}.${tax.table}`)
  return `/indexes?${params.toString()}`
}

function columns(
  instanceId: string,
  database: string,
  t: Translate,
): readonly SimpleColumn<IndexTax>[] {
  const unknown = t('indexes.tax.notMeasured')
  return [
    {
      id: 'table',
      header: t('indexes.tax.table'),
      sortValue: (tax) => `${tax.schema}.${tax.table}`,
      className: 'font-medium break-all max-[1599px]:min-w-48',
      truncate: (tax) => `${tax.datname} · ${tax.schema}.${tax.table}`,
      cell: (tax) => (
        <>
          <span className="text-muted-foreground">
            {tax.datname} · {tax.schema}.
          </span>
          {tax.table}
        </>
      ),
    },
    {
      id: 'indexes',
      header: t('indexes.tax.indexes'),
      sortValue: (tax) => tax.indexCount,
      className: 'whitespace-nowrap',
      width: '7rem',
      cell: (tax) => formatCount(tax.indexCount),
    },
    {
      id: 'index-size',
      header: t('indexes.tax.indexSize'),
      help: 'indexes.tax-index-size',
      sortValue: (tax) => tax.indexBytes,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => formatBytes(tax.indexBytes),
    },
    {
      id: 'table-size',
      header: t('indexes.tax.tableSize'),
      help: 'indexes.tax-table-size',
      sortValue: (tax) => tax.tableBytes,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => (tax.tableBytes === null ? unknown : formatBytes(tax.tableBytes)),
    },
    {
      id: 'ratio',
      header: t('indexes.tax.ratio'),
      help: 'indexes.tax-ratio',
      sortValue: (tax) => tax.indexToTableRatio,
      className: 'whitespace-nowrap',
      width: '10rem',
      cell: (tax) =>
        tax.tableBytes === null ? '—' : (formatPercent(tax.indexBytes, tax.tableBytes) ?? '—'),
    },
    {
      id: 'unused',
      header: t('indexes.tax.unusedSize'),
      help: 'indexes.tax-unused',
      sortValue: (tax) => tax.unusedBytes,
      className: 'whitespace-nowrap',
      width: '13rem',
      truncate: (tax) =>
        tax.unusedCount === 0
          ? t('indexes.tax.unusedNone')
          : t('indexes.tax.unusedDetail', {
              bytes: formatBytes(tax.unusedBytes),
              count: tax.unusedCount,
              basis:
                tax.unusedBasis === 'advisor'
                  ? t('indexes.tax.basisAdvisor')
                  : t('indexes.tax.basisRaw'),
            }),
      cell: (tax) =>
        tax.unusedCount === 0 ? (
          '0 B'
        ) : (
          <>
            {formatBytes(tax.unusedBytes)}{' '}
            <span className="text-muted-foreground text-xs">
              {tax.unusedBasis === 'advisor'
                ? t('indexes.tax.advisor')
                : t('indexes.tax.unusedRaw')}
            </span>
          </>
        ),
    },
    {
      id: 'writes',
      header: t('indexes.tax.writes'),
      help: 'indexes.tax-writes',
      sortValue: (tax) => tax.writeRate,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => (tax.writeRate === null ? unknown : formatRate(tax.writeRate)),
    },
    {
      id: 'amplification',
      header: t('indexes.tax.amplification'),
      help: 'indexes.tax-amplification',
      sortValue: (tax) => tax.writeAmplification,
      className: 'whitespace-nowrap',
      width: '10rem',
      cell: (tax) =>
        tax.writeAmplification === null ? unknown : `${formatRate(tax.writeAmplification)}`,
    },
    {
      id: 'cleanup',
      header: t('indexes.tax.cleanup'),
      srOnlyHeader: true,
      className: 'whitespace-nowrap',
      width: '7rem',
      cell: (tax) => (
        <Link
          aria-label={t('indexes.tax.cleanupLabel', { table: `${tax.schema}.${tax.table}` })}
          className="text-primary font-medium hover:underline"
          to={cleanupLink(instanceId, database, tax)}
        >
          {t('indexes.tax.cleanup')}
        </Link>
      ),
    },
  ]
}

interface IndexTaxPanelProps {
  instanceId: string
  /** The indexes in scope: the page's database filter already applied. */
  rows: readonly IndexRow[]
  database: string
  /** The agent or the API cut the index list: every figure covers the reported indexes only. */
  incomplete?: boolean
  /** Why the list is empty, when it is. */
  emptyDescription?: string
}

/**
 * What the indexes of each table cost, computed here from the index and table
 * statistics already loaded: pglens runs no statement.
 */
export function IndexTaxPanel({
  instanceId,
  rows,
  database,
  incomplete = false,
  emptyDescription = noIndexStats(),
}: IndexTaxPanelProps) {
  const t = useT()
  // The largest tables first: the ones whose indexes cost the most are never cut.
  const tablesQuery = useInstanceTables(instanceId, { limit: TABLE_LIMIT, order_by: 'size' })
  const taxes = useMemo(
    () => buildIndexTax(rows, tablesQuery.data?.items ?? []),
    [rows, tablesQuery.data],
  )
  const summary = useMemo(() => summariseIndexTax(taxes), [taxes])
  const tableColumns = useMemo(() => columns(instanceId, database, t), [instanceId, database, t])
  const unmeasured = taxes.filter((tax) => tax.tableBytes === null).length

  return (
    <IndexPanel
      actions={<HelpLink anchor="indexes#tax" label={t('indexes.tax.helpLabel')} />}
      captionId="indexes.tax"
      description={t('indexes.tax.description')}
      notes={
        <>
          {incomplete ? (
            <PanelNote
              title={t('indexes.tax.incompleteTitle', {
                detail: t('indexes.tax.incompleteDetail'),
              })}
              tone="warning"
            >
              <strong>{t('indexes.tax.incompleteLead')}</strong> {t('indexes.tax.incompleteDetail')}
            </PanelNote>
          ) : null}
          <PanelNote title={t('indexes.tax.estimate')}>
            <strong>{t('indexes.tax.estimatesLead')}</strong> {t('indexes.tax.estimate')}
          </PanelNote>
          {unmeasured > 0 ? (
            <PanelNote title={t('indexes.tax.unmeasuredTitle', { count: unmeasured })}>
              {t('indexes.tax.unmeasuredNote', {
                shown: formatCount(unmeasured),
                unmeasured,
              })}
            </PanelNote>
          ) : null}
        </>
      }
      stats={[
        { label: t('indexes.tax.tables'), value: formatCount(summary.tables), unit: 'tables' },
        {
          label: t('indexes.tax.indexSize'),
          value: formatBytes(summary.indexBytes),
          detail: t('indexes.tax.reportedDetail', {
            shown: formatCountOf(
              summary.indexCount,
              t('indexes.tax.wordIndex'),
              t('indexes.tax.wordIndexes'),
            ),
          }),
        },
        {
          label: t('indexes.tax.unusedSize'),
          value: formatBytes(summary.unusedBytes),
          detail: t('indexes.tax.confirmedDetail', {
            bytes: formatBytes(summary.unusedConfirmedBytes),
          }),
        },
        {
          label: t('indexes.tax.amplification'),
          value:
            summary.writeAmplification === null
              ? t('indexes.tax.notMeasured')
              : formatRate(summary.writeAmplification).replace(/\/s$/, ''),
          ...(summary.writeAmplification === null ? {} : { unit: 'per second' as const }),
          detail: t('indexes.tax.amplificationDetail', {
            measured: formatCount(summary.measuredTables),
            tables: formatCount(summary.tables),
          }),
        },
      ]}
      title={t('indexes.tax.title')}
      titleId="tax-title"
    >
      {taxes.length === 0 ? (
        <EmptyState description={emptyDescription} title={t('indexes.tax.emptyTitle')} />
      ) : (
        <SimpleTable
          ariaLabel={t('indexes.tax.tableAria')}
          captionId="indexes.tax-table"
          columns={tableColumns}
          defaultSort={{ id: 'index-size', desc: true }}
          emptyState={null}
          rowKey={(tax) => tax.key}
          rows={taxes}
          scope="tables"
          tableId="tax"
          toolbar={TAX_TOOLBAR}
        />
      )}
    </IndexPanel>
  )
}
