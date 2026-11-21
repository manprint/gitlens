import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useInstance } from '@/api/queries'
import {
  useDeleteVariant,
  useDeleteWorkbook,
  useRunVariants,
  useSaveVariant,
  useUpdateWorkbook,
  useWorkbook,
  type Workbook,
  type WorkbookVariant,
} from '@/api/workbooks'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { ErrorState, NotPermitted } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { useFormat, useT, type Translate } from '@/i18n/react'
import { formatMilliseconds } from '@/lib/format'
import type { PermTier } from '@/api/types'
import {
  draftOf,
  runnableIds,
  variantRows,
  type VariantDraft,
  type VariantRow,
} from '@/lib/workbook'

import { requestErrorText, variantErrorText } from './errors'
import VariantCompare from './VariantCompare'
import VariantEditor from './VariantEditor'

function SettingsCell({ settings, t }: { settings: Record<string, string>; t: Translate }) {
  const entries = Object.entries(settings)
  if (entries.length === 0)
    return <span className="text-muted-foreground">{t('workbook.noSettings')}</span>
  return (
    <ul className="space-y-0.5 font-mono text-xs">
      {entries.map(([name, value]) => (
        <li key={name}>
          {name}={value}
        </li>
      ))}
    </ul>
  )
}

function ParamsCell({ params, t }: { params: readonly (string | null)[]; t: Translate }) {
  const [open, setOpen] = useState(false)
  if (params.length === 0) return <span className="text-muted-foreground">—</span>
  return (
    <div className="space-y-1">
      <span>{t('workbook.paramsCount', { count: params.length })}</span>{' '}
      <Button size="xs" variant="link" onClick={() => setOpen((value) => !value)}>
        {open ? t('workbook.hideValues') : t('workbook.showValues')}
      </Button>
      {open && (
        <ol className="font-mono text-xs">
          {params.map((param, index) => (
            <li key={index}>
              ${index + 1} = {param ?? 'NULL'}
            </li>
          ))}
        </ol>
      )}
    </div>
  )
}

interface WorkbookViewProps {
  workbook: Workbook
  tier: PermTier
  onDeleted: () => void
}

/** The workbook once loaded: header actions, variants table, editor and comparison. */
export function WorkbookView({ workbook, tier, onDeleted }: WorkbookViewProps) {
  const t = useT()
  const { number } = useFormat()
  const save = useSaveVariant(workbook.workbook_id)
  const remove = useDeleteVariant(workbook.workbook_id)
  const run = useRunVariants(workbook.workbook_id)
  const update = useUpdateWorkbook(workbook.workbook_id)
  const drop = useDeleteWorkbook(workbook.workbook_id)
  const [editing, setEditing] = useState<{ variant?: WorkbookVariant } | null>(null)
  const [ticked, setTicked] = useState<string[]>([])
  const [notes, setNotes] = useState(workbook.notes)
  const rows = useMemo(() => variantRows(workbook), [workbook])
  const canRun = tier !== 'T0'
  const canAnalyze = tier === 'T2'

  const done = rows.filter((row) => row.variant.status === 'done')
  const chosen = ticked
    .map((id) => done.find((row) => row.variant.variant_id === id)?.variant)
    .filter((variant): variant is WorkbookVariant => variant !== undefined)

  const ratioText = (value: number | null) =>
    value === null
      ? '—'
      : t('workbook.ratio', { ratio: number(value, { maximumFractionDigits: 2 }) })

  const runButton = (ids: readonly string[], label: string, disabled = false) => {
    const button = (
      <Button
        disabled={!canRun || disabled || run.isPending}
        size="xs"
        variant="outline"
        onClick={() => run.mutate(ids)}
      >
        {label}
      </Button>
    )
    return canRun ? (
      button
    ) : (
      <NotPermitted required="T1" current={tier}>
        {button}
      </NotPermitted>
    )
  }

  const columns: SimpleColumn<VariantRow>[] = [
    {
      id: 'name',
      header: t('workbook.col.name'),
      rowHeader: true,
      cell: (row) => (
        <span className="flex items-center gap-2">
          {row.variant.name}
          {row.isBaseline && <Badge variant="secondary">{t('workbook.baseline')}</Badge>}
          {row.variant.analyze && <Badge variant="outline">ANALYZE</Badge>}
        </span>
      ),
    },
    {
      id: 'settings',
      header: t('workbook.col.settings'),
      cell: (row) => <SettingsCell settings={row.variant.settings} t={t} />,
    },
    {
      id: 'hints',
      header: t('workbook.col.hints'),
      cell: (row) =>
        row.variant.hints ? <code className="text-xs">{row.variant.hints}</code> : '—',
    },
    {
      id: 'params',
      header: t('workbook.col.params'),
      cell: (row) => <ParamsCell params={row.variant.params ?? []} t={t} />,
    },
    {
      id: 'status',
      header: t('workbook.col.status'),
      cell: (row) => (
        <div className="space-y-1">
          <span>{t(`workbook.status.${row.variant.status}`)}</span>
          {row.variant.status === 'failed' && row.variant.error && (
            <p className="text-destructive text-xs" role="alert">
              {variantErrorText(row.variant, t)}
            </p>
          )}
        </div>
      ),
    },
    {
      id: 'cost',
      header: t('workbook.col.cost'),
      sortValue: (row) => row.variant.total_cost ?? null,
      cell: (row) =>
        row.variant.total_cost == null
          ? '—'
          : number(row.variant.total_cost, { maximumFractionDigits: 2 }),
    },
    {
      id: 'planning',
      header: t('workbook.col.planning'),
      cell: (row) =>
        row.variant.planning_ms == null ? '—' : formatMilliseconds(row.variant.planning_ms),
    },
    {
      id: 'execution',
      header: t('workbook.col.execution'),
      cell: (row) =>
        row.variant.execution_ms == null ? '—' : formatMilliseconds(row.variant.execution_ms),
    },
    {
      id: 'hash',
      header: t('workbook.col.hash'),
      cell: (row) =>
        row.variant.plan_hash ? (
          <code className="text-xs">{row.variant.plan_hash.slice(0, 12)}</code>
        ) : (
          '—'
        ),
    },
    {
      id: 'costRatio',
      header: t('workbook.col.costRatio'),
      cell: (row) => ratioText(row.costRatio),
    },
    {
      id: 'timeRatio',
      header: t('workbook.col.timeRatio'),
      cell: (row) => ratioText(row.timeRatio),
    },
    {
      id: 'compare',
      header: t('workbook.col.compare'),
      cell: (row) => (
        <input
          aria-label={t('workbook.selectVariant', { name: row.variant.name })}
          checked={ticked.includes(row.variant.variant_id)}
          disabled={row.variant.status !== 'done'}
          type="checkbox"
          onChange={(event) =>
            setTicked((current) =>
              event.target.checked
                ? [
                    ...current.filter((id) => id !== row.variant.variant_id),
                    row.variant.variant_id,
                  ].slice(-2)
                : current.filter((id) => id !== row.variant.variant_id),
            )
          }
        />
      ),
    },
    {
      id: 'actions',
      header: t('workbook.col.actions'),
      cell: (row) => (
        <div className="flex flex-wrap gap-1">
          {runButton([row.variant.variant_id], t('workbook.run'), row.variant.status === 'pending')}
          <Button
            disabled={row.variant.status === 'pending'}
            size="xs"
            variant="ghost"
            onClick={() => setEditing({ variant: row.variant })}
          >
            {t('workbook.edit')}
          </Button>
          <Button
            disabled={row.variant.status === 'pending' || remove.isPending}
            size="xs"
            variant="ghost"
            onClick={() => remove.mutate(row.variant.variant_id)}
          >
            {t('workbook.remove')}
          </Button>
        </div>
      ),
    },
  ]

  const onSave = (draft: VariantDraft) => {
    const body = {
      name: draft.name.trim(),
      settings: draft.settings,
      hints: draft.hints,
      params: draft.params,
      analyze: draft.analyze,
    }
    save.mutate(editing?.variant ? { variantId: editing.variant.variant_id, body } : { body }, {
      onSuccess: () => setEditing(null),
    })
  }

  const failure = run.error ?? remove.error ?? update.error ?? drop.error

  return (
    <div className="space-y-6">
      <PageHeader
        actions={
          <div className="flex flex-wrap gap-2">
            {runButton(runnableIds(workbook), t('workbook.runAll'))}
            <Button size="xs" variant="outline" onClick={() => setEditing({})}>
              {t('workbook.addVariant')}
            </Button>
            <Button
              disabled={drop.isPending}
              size="xs"
              variant="destructive"
              onClick={() => {
                if (window.confirm(t('workbook.deleteConfirm')))
                  drop.mutate(undefined, { onSuccess: onDeleted })
              }}
            >
              {t('workbook.delete')}
            </Button>
          </div>
        }
        eyebrow={t('workbook.title')}
        help={<HelpLink anchor="workbook" label={t('workbook.title')} />}
        subtitle={t('workbook.statement', {
          queryid: workbook.queryid,
          database: workbook.database || t('workbook.anyDatabase'),
        })}
        title={workbook.name}
      />
      <Callout>{t('workbook.intro')}</Callout>
      {failure && (
        <Callout role="alert" variant="destructive">
          {requestErrorText(failure, t)}
        </Callout>
      )}

      {editing && (
        <VariantEditor
          key={editing.variant?.variant_id ?? 'new'}
          canAnalyze={canAnalyze}
          editing={editing.variant !== undefined}
          initial={draftOf(editing.variant)}
          saving={save.isPending}
          serverError={save.error ? requestErrorText(save.error, t) : undefined}
          onCancel={() => {
            save.reset()
            setEditing(null)
          }}
          onSave={onSave}
        />
      )}

      <SimpleTable
        ariaLabel={t('workbook.variants')}
        captionId="queries.workbook-variants"
        columns={columns}
        emptyState={t('workbook.none')}
        rowKey={(row) => row.variant.variant_id}
        rows={rows}
        scope={t('workbook.variantsScope')}
        tableId="workbook-variants"
      />
      {chosen.length === 2 && chosen[0] && chosen[1] ? (
        <VariantCompare first={chosen[0]} second={chosen[1]} onClose={() => setTicked([])} />
      ) : (
        <p className="text-muted-foreground text-sm">{t('workbook.compareHint')}</p>
      )}

      <section aria-label={t('workbook.notes')} className="space-y-2">
        <label className="block space-y-1 text-sm">
          <span className="font-medium">{t('workbook.notes')}</span>
          <textarea
            className="bg-background w-full rounded-md border p-2 text-sm"
            rows={3}
            value={notes}
            onChange={(event) => setNotes(event.target.value)}
          />
        </label>
        <Button
          disabled={update.isPending || notes === workbook.notes}
          size="sm"
          variant="outline"
          onClick={() => update.mutate({ notes })}
        >
          {t('workbook.saveNotes')}
        </Button>
      </section>
    </div>
  )
}

export default function WorkbookPage() {
  const {
    instanceId = '',
    queryid = '',
    workbookId = '',
  } = useParams<{
    instanceId: string
    queryid: string
    workbookId: string
  }>()
  const t = useT()
  const navigate = useNavigate()
  const instance = useInstance(instanceId)
  const query = useWorkbook(workbookId)
  const back = `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(queryid)}`

  if (query.error) {
    return (
      <ErrorState
        endpoint="workbook"
        failure={query.error.failure}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!query.data) {
    return (
      <section aria-busy="true" aria-label={t('workbook.loading')} role="status">
        {t('workbook.loading')}
      </section>
    )
  }
  return (
    <div className="space-y-4">
      <Link className="text-sm underline" to={back}>
        {t('workbook.backToQuery')}
      </Link>
      <WorkbookView
        tier={instance.data?.perm_tier ?? 'T0'}
        workbook={query.data}
        onDeleted={() => void navigate(back)}
      />
    </div>
  )
}
