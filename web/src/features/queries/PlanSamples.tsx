import { useState } from 'react'

import { usePlanSamples } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { TraceId } from '@/components/layout/TraceId'
import { ErrorState } from '@/components/state/ErrorState'
import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/react'
import { AUTO_PLAN_SETTINGS } from '@/lib/autoPlans'
import { formatMilliseconds, formatTimestamp } from '@/lib/format'

type Sample = Schemas['PlanSample']

export function AutoPlanSetup() {
  const t = useT()
  return (
    <div className="text-muted-foreground space-y-1 text-sm" data-testid="auto-plan-setup">
      <p>{t('queries.planSamples.empty')}</p>
      <ul className="list-disc pl-5">
        {AUTO_PLAN_SETTINGS.map((setting) => (
          <li key={setting}>
            <code className="font-mono text-xs">{setting}</code>
          </li>
        ))}
      </ul>
      <p>{t('queries.planSamples.setupAgent')}</p>
    </div>
  )
}

export interface PlanSamplesProps {
  instanceId: string
  queryid: string
  planHash: string
  datname?: string
}

export function PlanSamples({ instanceId, queryid, planHash, datname }: PlanSamplesProps) {
  const t = useT()
  const [showParameters, setShowParameters] = useState(false)
  const params = {
    instance_id: instanceId,
    queryid,
    plan_hash: planHash,
    ...(datname === undefined ? {} : { database: datname }),
    ...(showParameters ? { include_parameters: true } : {}),
  }
  const query = usePlanSamples(params)
  const response = query.data as Schemas['PlanSamplesResponse'] | undefined
  const samples = response?.samples ?? []
  const anyParameters = samples.some((sample) => sample.has_parameters)

  const columns: SimpleColumn<Sample>[] = [
    {
      id: 'time',
      header: t('queries.planSamples.time'),
      rowHeader: true,
      sortValue: (row) => Date.parse(row.ts),
      cell: (row) => formatTimestamp(row.ts),
    },
    {
      id: 'duration',
      header: t('queries.planSamples.duration'),
      sortValue: (row) => row.duration_ms,
      cell: (row) => formatMilliseconds(row.duration_ms),
    },
    {
      id: 'analyzed',
      header: t('queries.planSamples.analyzed'),
      sortValue: (row) => (row.analyzed ? 1 : 0),
      cell: (row) =>
        row.analyzed ? t('queries.planSamples.analyzedYes') : t('queries.planSamples.analyzedNo'),
    },
    {
      id: 'parameters',
      header: t('queries.planSamples.parameters'),
      cell: (row) =>
        !row.has_parameters ? (
          t('queries.planSamples.parametersNone')
        ) : row.parameters === undefined ? (
          t('queries.planSamples.parametersHidden')
        ) : (
          <code className="font-mono text-xs break-all">{row.parameters}</code>
        ),
    },
    {
      id: 'trace',
      header: t('queries.planSamples.trace'),
      cell: (row) => (row.trace_id ? <TraceId id={row.trace_id} /> : null),
    },
  ]

  let body
  if (query.isPending && response === undefined) {
    body = <div role="status">{t('queries.planSamples.loading')}</div>
  } else if (query.error && response === undefined) {
    body = (
      <ErrorState
        endpoint={t('queries.planSamples.endpoint')}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  } else if (samples.length === 0) {
    body = <AutoPlanSetup />
  } else {
    body = (
      <>
        {anyParameters && !showParameters && (
          <Button size="sm" variant="outline" onClick={() => setShowParameters(true)}>
            {t('queries.planSamples.showParameters')}
          </Button>
        )}
        <SimpleTable
          ariaLabel={t('queries.planSamples.entriesLabel')}
          captionId="queries.plan-sample-rows"
          caption={t('queries.planSamples.caption')}
          columns={columns}
          emptyState={null}
          rowKey={(row) => `${row.datname}:${row.ts}`}
          rows={samples}
          scope={t('queries.planSamples.scope')}
          tableId="plan-samples"
        />
      </>
    )
  }

  return (
    <section
      aria-labelledby="plan-samples-title"
      className="space-y-3"
      data-caption-id="queries.plan-samples"
    >
      <div>
        <div className="flex items-center gap-1">
          <h3 id="plan-samples-title" className="text-sm font-semibold">
            {t('queries.planSamples.title')}
          </h3>
          <InfoHint id="queries.plan-samples" label={t('queries.planSamples.title')} />
        </div>
        <p className="text-muted-foreground mt-1 text-sm">{t('queries.planSamples.intro')}</p>
      </div>
      {body}
    </section>
  )
}

export default PlanSamples
