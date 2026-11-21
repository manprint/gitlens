import { Link, useSearchParams } from 'react-router-dom'

import type { components } from '@/api/generated'
import { useAshCompare } from '@/api/queries'
import { ComparePeriodPicker, CompareRatio } from '@/components/layout/ComparePeriodPicker'
import { type ComparePeriods, useComparePeriods } from '@/hooks/useComparePeriods'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import { NativeSelect } from '@/components/ui/native-select'
import { type Translate, useT } from '@/i18n/react'
import type { AshDimensionFilters } from '@/lib/ash'
import { ASH_GROUP_LABELS, type AshChartGroup } from '@/lib/ashGroups'
import { formatChange, periodParams } from '@/lib/compare'
import { formatDecimal } from '@/lib/format'
import { queryPath } from '@/lib/statements'

type CompareRow = components['schemas']['AshCompareRow']

const COMPARE_LIMIT = 50
const GROUPS = Object.keys(ASH_GROUP_LABELS) as AshChartGroup[]

export interface AshCompareProps {
  instanceId: string
  groupBy: AshChartGroup
  database?: string | undefined
  waitEventType?: string | undefined
  waitEvent?: string | undefined
  dimensions: AshDimensionFilters
  periods?: ComparePeriods
}

function aas(value: number | null | undefined): string {
  return value === null || value === undefined ? '—' : formatDecimal(value, 2)
}

function columns(
  instanceId: string,
  groupBy: AshChartGroup,
  t: Translate,
): DataTableProps<CompareRow>['columns'] {
  return [
    {
      id: 'key',
      header: t(ASH_GROUP_LABELS[groupBy]),
      accessorFn: (row) => row.key,
      cell: ({ row }) => {
        const key = row.original.key
        if (key === '') return t('ash.dimension.unknown')
        return groupBy === 'queryid' ? (
          <Link
            className="text-primary font-semibold underline-offset-4 hover:underline"
            to={queryPath(instanceId, key)}
          >
            {key}
          </Link>
        ) : (
          key
        )
      },
    },
    {
      id: 'a',
      header: t('ash.compare.colA'),
      accessorFn: (row) => row.a?.aas ?? null,
      cell: ({ row }) => aas(row.original.a?.aas),
    },
    {
      id: 'b',
      header: t('ash.compare.colB'),
      accessorFn: (row) => row.b?.aas ?? null,
      cell: ({ row }) => aas(row.original.b?.aas),
    },
    {
      id: 'change',
      header: t('ash.compare.colChange'),
      accessorFn: (row) => row.delta.aas ?? null,
      cell: ({ row }) => formatChange(row.original.delta.aas, aas),
    },
    {
      id: 'ratio',
      header: t('ash.compare.colRatio'),
      accessorFn: (row) => row.ratio.aas ?? null,
      cell: ({ row }) => (
        <CompareRatio area="ash" ratio={row.original.ratio.aas} status={row.original.status} />
      ),
    },
  ]
}

/** Average active sessions per group in period A and B, under the page's filters. */
export function AshCompare({
  instanceId,
  groupBy,
  database,
  waitEventType,
  waitEvent,
  dimensions,
  periods: given,
}: AshCompareProps) {
  const t = useT()
  const [searchParams, setSearchParams] = useSearchParams()
  const ownPeriods = useComparePeriods()
  const periods = given ?? ownPeriods
  const params = periods.a
    ? {
        instance_id: instanceId,
        group_by: groupBy,
        limit: COMPARE_LIMIT,
        ...periodParams(periods.a, periods.b),
        ...(database === undefined ? {} : { database }),
        ...(waitEventType === undefined ? {} : { wait_event_type: waitEventType }),
        ...(waitEvent === undefined ? {} : { wait_event: waitEvent }),
        ...dimensions,
      }
    : null
  const compare = useAshCompare(params)

  const onGroup = (value: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('group', value)
    next.delete('wait_event_type')
    next.delete('wait_event')
    setSearchParams(next)
  }

  return (
    <Section
      id="ash-compare"
      title={t('ash.compare.tableTitle')}
      captionId="ash.compare-table"
      actions={
        <>
          <label className="text-muted-foreground text-sm font-medium" htmlFor="ash-compare-group">
            {t('ash.compare.groupLabel')}
          </label>
          <NativeSelect
            id="ash-compare-group"
            onChange={(event) => onGroup(event.target.value)}
            value={groupBy}
          >
            {GROUPS.map((group) => (
              <option key={group} value={group}>
                {t(ASH_GROUP_LABELS[group])}
              </option>
            ))}
          </NativeSelect>
        </>
      }
    >
      <div className="mb-3">
        <ComparePeriodPicker area="ash" periods={periods} />
      </div>
      {params === null ? null : compare.error && !compare.data ? (
        <ErrorState
          endpoint="ash/compare"
          failure={compare.error}
          onRetry={() => void compare.refetch()}
        />
      ) : compare.data === undefined ? (
        <p aria-busy="true" role="status">
          {t('ash.compare.loading')}
        </p>
      ) : (
        <DataTable
          ariaLabel={t('ash.compare.ariaLabel')}
          captionId="ash.compare-table"
          columns={columns(instanceId, groupBy, t)}
          data={compare.data.rows}
          emptyState={
            <EmptyState
              title={t('ash.compare.emptyTitle')}
              description={t('ash.compare.emptyText')}
            />
          }
          getRowId={(row) => row.key}
          scope={t('ash.compare.groups')}
          tableId="ash-compare"
        />
      )}
    </Section>
  )
}
