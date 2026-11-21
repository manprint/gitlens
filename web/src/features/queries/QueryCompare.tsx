import { Link, useSearchParams } from 'react-router-dom'

import type { components } from '@/api/generated'
import { useStatementsCompare } from '@/api/queries'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { SqlText } from '@/components/layout/SqlText'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import { NativeSelect } from '@/components/ui/native-select'
import { ComparePeriodPicker, CompareRatio } from '@/components/layout/ComparePeriodPicker'
import { type ComparePeriods, useComparePeriods } from '@/hooks/useComparePeriods'
import type { MessageKey } from '@/i18n/messages/en'
import { type Translate, useT } from '@/i18n/react'
import {
  formatChange,
  formatCompareValue,
  periodParams,
  STATEMENT_COMPARE_METRIC,
  type StatementCompareMetric,
} from '@/lib/compare'
import { normaliseQueryText, queryPath } from '@/lib/statements'

type CompareRow = components['schemas']['StatementCompareRow']
type CompareOrder = keyof typeof STATEMENT_COMPARE_METRIC

const COMPARE_LIMIT = 50

const MEASURES: readonly { value: CompareOrder; labelKey: MessageKey }[] = [
  { value: 'total_exec_time', labelKey: 'queries.common.totalTime' },
  { value: 'mean_exec_time', labelKey: 'queries.common.meanTime' },
  { value: 'calls', labelKey: 'queries.common.calls' },
  { value: 'rows', labelKey: 'queries.common.rows' },
  { value: 'shared_blks_read', labelKey: 'queries.list.sortSharedRead' },
  { value: 'temp_blks_written', labelKey: 'queries.list.sortTempWritten' },
  { value: 'wal_bytes', labelKey: 'queries.list.sortWal' },
]

/** The list's `sort` picks the compared measure, so both modes rank alike. */
function readMeasure(value: string | null): CompareOrder {
  return MEASURES.find((measure) => measure.value === value)?.value ?? 'total_exec_time'
}

function columns(
  instanceId: string,
  metric: StatementCompareMetric,
  t: Translate,
): DataTableProps<CompareRow>['columns'] {
  const format = (size: number) => formatCompareValue(metric, size)
  return [
    {
      id: 'query',
      header: t('queries.common.query'),
      enableSorting: false,
      cell: ({ row }) => {
        const text = row.original.query_text ?? ''
        return (
          <div className="min-w-64 space-y-1">
            <Link
              className="text-primary text-xs font-semibold underline-offset-4 hover:underline"
              to={queryPath(instanceId, row.original.queryid, row.original.datname)}
            >
              {t('queries.list.queryLink', { queryid: row.original.queryid })}
            </Link>
            {text ? (
              <div className="max-w-xl truncate text-sm" title={normaliseQueryText(text)}>
                <SqlText sql={normaliseQueryText(text)} />
              </div>
            ) : null}
          </div>
        )
      },
    },
    {
      id: 'a',
      header: t('queries.compare.colA'),
      accessorFn: (row) => row.a?.[metric] ?? null,
      cell: ({ row }) => formatCompareValue(metric, row.original.a?.[metric]),
    },
    {
      id: 'b',
      header: t('queries.compare.colB'),
      accessorFn: (row) => row.b?.[metric] ?? null,
      cell: ({ row }) => formatCompareValue(metric, row.original.b?.[metric]),
    },
    {
      id: 'change',
      header: t('queries.compare.colChange'),
      accessorFn: (row) => row.delta[metric] ?? null,
      cell: ({ row }) => formatChange(row.original.delta[metric], format),
    },
    {
      id: 'ratio',
      header: t('queries.compare.colRatio'),
      accessorFn: (row) => row.ratio[metric] ?? null,
      cell: ({ row }) => (
        <CompareRatio
          area="queries"
          ratio={row.original.ratio[metric]}
          status={row.original.status}
        />
      ),
    },
  ]
}

export function QueryCompare({
  instanceId,
  periods: given,
}: {
  instanceId: string
  periods?: ComparePeriods
}) {
  const t = useT()
  const [searchParams, setSearchParams] = useSearchParams()
  const ownPeriods = useComparePeriods()
  const periods = given ?? ownPeriods
  const measure = readMeasure(searchParams.get('sort'))
  const database = searchParams.get('db') ?? undefined
  const params = periods.a
    ? {
        instance_id: instanceId,
        order_by: measure,
        limit: COMPARE_LIMIT,
        ...periodParams(periods.a, periods.b),
        ...(database === undefined ? {} : { database }),
      }
    : null
  const compare = useStatementsCompare(params)
  const metric = STATEMENT_COMPARE_METRIC[measure]

  const onMeasure = (value: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('sort', readMeasure(value))
    setSearchParams(next)
  }

  return (
    <Section
      id="statement-compare"
      title={t('queries.compare.tableTitle')}
      captionId="queries.compare-table"
      actions={
        <>
          <label className="text-muted-foreground text-sm font-medium" htmlFor="compare-measure">
            {t('queries.compare.metricLabel')}
          </label>
          <NativeSelect
            id="compare-measure"
            onChange={(event) => onMeasure(event.target.value)}
            value={measure}
          >
            {MEASURES.map((option) => (
              <option key={option.value} value={option.value}>
                {t(option.labelKey)}
              </option>
            ))}
          </NativeSelect>
        </>
      }
    >
      <div className="mb-3">
        <ComparePeriodPicker area="queries" periods={periods} />
      </div>
      {params === null ? null : compare.error && !compare.data ? (
        <ErrorState
          endpoint="statements/compare"
          failure={compare.error}
          onRetry={() => void compare.refetch()}
        />
      ) : compare.data === undefined ? (
        <p aria-busy="true" role="status">
          {t('queries.compare.loading')}
        </p>
      ) : (
        <DataTable
          ariaLabel={t('queries.compare.ariaLabel')}
          captionId="queries.compare-table"
          columns={columns(instanceId, metric, t)}
          data={compare.data.rows}
          emptyState={
            <EmptyState
              title={t('queries.compare.emptyTitle')}
              description={t('queries.compare.emptyText')}
            />
          }
          getRowId={(row) => `${row.datname}:${row.queryid}`}
          scope={t('queries.compare.statements')}
          tableId="statement-compare"
        />
      )}
    </Section>
  )
}
