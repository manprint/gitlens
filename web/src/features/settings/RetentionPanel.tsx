import { createContext, useContext, useId, useMemo, useState } from 'react'

import { useRetention, useRetentionPreview, useSetRetention } from '@/api/queries'
import { DataTable } from '@/components/layout/DataTable'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
import {
  confirmationText,
  isShortening,
  previewSummary,
  retentionErrorMessage,
  retentionFamilyLabel,
  splitRetentionGroups,
  validateRetentionDays,
  type RetentionFamily,
  type RetentionFamilyName,
  type RetentionPreview,
} from '@/lib/retention'

/** What each control of a row changes (I-16), referenced by its `aria-describedby`. */
const RETENTION_HELP = {
  days: 'settings.retention.help.days',
  preview: 'settings.retention.help.preview',
  apply: 'settings.retention.help.apply',
} as const satisfies Record<string, MessageKey>

/** A preview, with the live retention it was computed against. */
interface RowPreview {
  result: RetentionPreview
  /** `days` of the row when the preview was asked; Apply waits for a new preview if it moved. */
  liveDays: number | null
}

/** The editing state of one family; absent until the row is edited. */
interface RowState {
  draft: string
  preview: RowPreview | null
  error: string | null
  saved: string | null
  pending: 'preview' | 'apply' | null
}

interface RetentionActions {
  state: (row: RetentionFamily) => RowState
  edit: (row: RetentionFamily, draft: string) => void
  preview: (row: RetentionFamily) => void
  apply: (row: RetentionFamily) => void
  helpIds: Record<keyof typeof RETENTION_HELP, string>
  /** The id of a row's validation message, shown in its Preview cell. */
  errorIdOf: (row: RetentionFamily) => string
}

// The table's cells read the panel's state through the context, so a sort or a
// re-render of the table never loses what a row is editing.
const RetentionContext = createContext<RetentionActions | null>(null)

function useRetentionActions(): RetentionActions {
  const actions = useContext(RetentionContext)
  if (actions === null) throw new Error('RetentionPanel cells need the retention context')
  return actions
}

function initialState(row: RetentionFamily): RowState {
  return {
    draft: row.days === null ? '' : String(row.days),
    preview: null,
    error: null,
    saved: null,
    pending: null,
  }
}

// One line per row (Full HD fit): the hypertable, which bears the family's
// name, is in the title; the inputs and buttons name the family.
function FamilyCell({ row }: { row: RetentionFamily }) {
  return (
    <span className="block truncate font-medium" title={row.hypertable}>
      {retentionFamilyLabel(row.family)}
    </span>
  )
}

function CurrentCell({ row }: { row: RetentionFamily }) {
  const t = useT()
  if (row.unsupported_policy !== undefined) {
    return (
      <span className="flex flex-wrap items-center gap-2" title={row.unsupported_policy}>
        <span>{t('settings.retention.setOutside')}</span>
        <Badge variant="outline">{t('settings.retention.sourceCustom')}</Badge>
      </span>
    )
  }
  if (row.days === null) {
    return (
      <span className="flex flex-wrap items-center gap-2">
        <span>{t('settings.retention.noPolicy')}</span>
        <Badge variant="outline">{t('settings.retention.sourceCustom')}</Badge>
      </span>
    )
  }
  return (
    <span className="flex flex-wrap items-center gap-2">
      <span className="tabular-nums">{t('settings.retention.days', { days: row.days })}</span>
      <Badge variant={row.source === 'default' ? 'secondary' : 'outline'}>
        {t(
          row.source === 'default'
            ? 'settings.retention.sourceDefault'
            : 'settings.retention.sourceCustom',
        )}
      </Badge>
    </span>
  )
}

function DaysCell({ row }: { row: RetentionFamily }) {
  const t = useT()
  const actions = useRetentionActions()
  const state = actions.state(row)
  const id = useId().replaceAll(':', '')
  const validation = validateRetentionDays(state.draft, row.min_days, row.max_days)
  const errorId = actions.errorIdOf(row)
  const rangeId = `${id}-range`

  return (
    <div className="space-y-1">
      {/* The range sits beside the input so a row stays one control high (Full HD fit). */}
      <div className="flex items-center gap-2">
        <Input
          aria-describedby={[validation.ok ? null : errorId, rangeId, actions.helpIds.days]
            .filter(Boolean)
            .join(' ')}
          aria-invalid={validation.ok ? undefined : true}
          aria-label={t('settings.retention.newDaysLabel', { family: row.family })}
          autoComplete="off"
          className="h-8 w-24"
          disabled={state.pending === 'apply' || row.unsupported_policy !== undefined}
          inputMode="numeric"
          onChange={(event) => actions.edit(row, event.target.value)}
          value={state.draft}
        />
        <span className="text-muted-foreground text-xs whitespace-nowrap" id={rangeId}>
          {t('settings.retention.range', { min: row.min_days, max: row.max_days })}
        </span>
      </div>
    </div>
  )
}

function PreviewCell({ row }: { row: RetentionFamily }) {
  const t = useT()
  const actions = useRetentionActions()
  const state = actions.state(row)
  const validation = validateRetentionDays(state.draft, row.min_days, row.max_days)
  const unsupported = row.unsupported_policy !== undefined

  return (
    <div className="flex min-w-0 items-center gap-3">
      <Button
        aria-describedby={actions.helpIds.preview}
        aria-label={t('settings.retention.previewLabel', { family: row.family })}
        disabled={!validation.ok || state.pending !== null || unsupported}
        onClick={() => actions.preview(row)}
        size="xs"
        type="button"
        variant="outline"
      >
        {state.pending === 'preview'
          ? t('settings.retention.previewing')
          : t('settings.retention.preview')}
      </Button>
      <div className="min-w-0 text-sm">
        {/* The days' validation message sits here, not under the input, so a
            row never grows; always mounted, so a screen reader hears it. */}
        <span
          aria-live="polite"
          className="text-destructive block empty:hidden"
          id={actions.errorIdOf(row)}
        >
          {validation.ok || unsupported ? '' : validation.error}
        </span>
        {unsupported ? (
          <span className="text-muted-foreground">
            {t('settings.retention.unsupportedChange', { policy: row.unsupported_policy ?? '' })}
          </span>
        ) : null}
        {state.preview ? (
          <span data-testid={`retention-preview-${row.family}`} role="status">
            {previewSummary(state.preview.result, state.preview.liveDays)}
          </span>
        ) : null}
        {state.saved ? <span role="status">{state.saved}</span> : null}
        {state.error ? (
          <span className="text-destructive block" role="alert">
            {state.error}
          </span>
        ) : null}
      </div>
    </div>
  )
}

function ApplyCell({ row }: { row: RetentionFamily }) {
  const t = useT()
  const actions = useRetentionActions()
  const state = actions.state(row)
  const validation = validateRetentionDays(state.draft, row.min_days, row.max_days)
  const ready =
    validation.ok &&
    state.preview !== null &&
    state.preview.result.days === validation.value &&
    state.preview.liveDays === row.days &&
    validation.value !== row.days

  return (
    <Button
      aria-describedby={actions.helpIds.apply}
      aria-label={t('settings.retention.applyLabel', { family: row.family })}
      disabled={!ready || state.pending !== null}
      onClick={() => actions.apply(row)}
      size="xs"
      type="button"
    >
      {state.pending === 'apply' ? t('settings.retention.applying') : t('settings.retention.apply')}
    </Button>
  )
}

function retentionColumns(t: Translate) {
  return [
    {
      accessorKey: 'family',
      header: t('settings.retention.colFamily'),
      meta: { width: '17rem' },
      cell: ({ row }: { row: { original: RetentionFamily } }) => <FamilyCell row={row.original} />,
    },
    {
      accessorKey: 'days',
      header: t('settings.retention.colNow'),
      meta: { width: '13rem' },
      cell: ({ row }: { row: { original: RetentionFamily } }) => <CurrentCell row={row.original} />,
    },
    {
      id: 'new-days',
      header: t('settings.retention.colNew'),
      enableSorting: false,
      enableHiding: false,
      meta: { width: '10rem' },
      cell: ({ row }: { row: { original: RetentionFamily } }) => <DaysCell row={row.original} />,
    },
    {
      id: 'preview',
      header: t('settings.retention.colPreview'),
      enableSorting: false,
      enableHiding: false,
      cell: ({ row }: { row: { original: RetentionFamily } }) => <PreviewCell row={row.original} />,
    },
    {
      id: 'apply',
      header: t('settings.retention.colApply'),
      enableSorting: false,
      enableHiding: false,
      meta: { width: '7rem' },
      cell: ({ row }: { row: { original: RetentionFamily } }) => <ApplyCell row={row.original} />,
    },
  ]
}

interface Confirming {
  row: RetentionFamily
  preview: RetentionPreview
}

/** The change the confirmation names: a first setting, or a move from one value to another. */
function changeText(confirming: Confirming | null, t: Translate): string {
  if (confirming?.row.days === null || confirming === null) {
    return t('settings.retention.changeFirst')
  }
  return t('settings.retention.changeFromTo', {
    from: confirming.row.days,
    to: confirming.preview.days,
  })
}

/** Whether the row's typed value still reads as `days`. */
function draftIs(row: RetentionFamily, state: RowState, days: number): boolean {
  const validation = validateRetentionDays(state.draft, row.min_days, row.max_days)
  return validation.ok && validation.value === days
}

/**
 * The Data retention tab (11.3, D17): the retention of each metrics family,
 * with a preview of what a value would delete before it is applied. A shorter
 * retention needs an explicit confirmation; only then does the request carry
 * `confirm: true`. A failure shows inline and the live value stays as it was
 * (I-11).
 */
export function RetentionPanel() {
  const t = useT()
  const retentionQuery = useRetention()
  const previewMutation = useRetentionPreview()
  const setMutation = useSetRetention()
  const [rows, setRows] = useState<Partial<Record<RetentionFamilyName, RowState>>>({})
  const [confirming, setConfirming] = useState<Confirming | null>(null)
  const [acknowledged, setAcknowledged] = useState(false)
  const baseId = useId().replaceAll(':', '')
  const helpIds = useMemo(
    () => ({
      days: `${baseId}-days-help`,
      preview: `${baseId}-preview-help`,
      apply: `${baseId}-apply-help`,
    }),
    [baseId],
  )
  const confirmId = `${baseId}-confirm`
  const columns = useMemo(() => retentionColumns(t), [t])
  const [rawFamilies, rollupFamilies] = useMemo(
    () => splitRetentionGroups(retentionQuery.data ?? []),
    [retentionQuery.data],
  )

  const stateOf = (row: RetentionFamily) => rows[row.family] ?? initialState(row)
  const update = (row: RetentionFamily, patch: Partial<RowState>) =>
    setRows((current) => ({
      ...current,
      [row.family]: { ...(current[row.family] ?? initialState(row)), ...patch },
    }))

  // A preview answers the value it was asked for: if the typed value moved
  // on meanwhile, the late answer (or error) is dropped and only the pending
  // flag is cleared.
  const settlePreview = (row: RetentionFamily, days: number, patch: Partial<RowState>) =>
    setRows((current) => {
      const state = current[row.family] ?? initialState(row)
      const next = draftIs(row, state, days) ? { ...state, ...patch } : state
      return { ...current, [row.family]: { ...next, pending: null } }
    })

  async function save(row: RetentionFamily, days: number, confirm: boolean) {
    update(row, { pending: 'apply', error: null, saved: null })
    try {
      const saved = await setMutation.mutateAsync({
        family: row.family,
        days,
        ...(confirm ? { confirm: true as const } : {}),
      })
      update(row, {
        ...initialState(saved),
        saved: t('settings.retention.applied', { days: saved.days ?? days }),
      })
    } catch (error) {
      // The row keeps showing the live value and the typed one (I-11); the
      // listing is read again (useSetRetention), so a new preview is needed.
      update(row, { pending: null, preview: null, error: retentionErrorMessage(error) })
    }
  }

  const actions: RetentionActions = {
    state: stateOf,
    helpIds,
    errorIdOf: (row) => `${baseId}-${row.family}-days-error`,
    edit: (row, draft) => update(row, { draft, preview: null, error: null, saved: null }),
    preview: (row) => {
      const state = stateOf(row)
      const validation = validateRetentionDays(state.draft, row.min_days, row.max_days)
      if (!validation.ok) return
      const days = validation.value
      const liveDays = row.days
      update(row, { pending: 'preview', error: null, saved: null, preview: null })
      previewMutation
        .mutateAsync({ family: row.family, days })
        .then((result) => settlePreview(row, days, { preview: { result, liveDays } }))
        .catch((error: unknown) =>
          settlePreview(row, days, { error: retentionErrorMessage(error) }),
        )
    },
    apply: (row) => {
      const { preview } = stateOf(row)
      if (preview?.liveDays !== row.days) return
      if (preview === null) return
      if (isShortening(row.days, preview.result.days)) {
        setAcknowledged(false)
        setConfirming({ row, preview: preview.result })
        return
      }
      void save(row, preview.result.days, false)
    },
  }

  function confirmShortening() {
    if (confirming === null || !acknowledged) return
    const { row, preview } = confirming
    setConfirming(null)
    void save(row, preview.days, true)
  }

  return (
    <Section
      actions={<HelpLink anchor="settings#retention" label={t('settings.retention.title')} />}
      captionId="settings.retention"
      id="settings-retention"
      title={t('settings.retention.title')}
    >
      <div className="space-y-4 min-[1600px]:space-y-3">
        <ul className="text-muted-foreground grid gap-1 text-sm leading-relaxed min-[1600px]:grid-cols-3 min-[1600px]:gap-4">
          <li id={helpIds.days}>{t(RETENTION_HELP.days)}</li>
          <li id={helpIds.preview}>{t(RETENTION_HELP.preview)}</li>
          <li id={helpIds.apply}>{t(RETENTION_HELP.apply)}</li>
        </ul>
        {retentionQuery.error ? (
          <ErrorState
            endpoint={t('settings.retention.endpoint')}
            failure={retentionQuery.error}
            onRetry={() => void retentionQuery.refetch()}
          />
        ) : retentionQuery.isPending ? (
          <p aria-busy="true" role="status">
            {t('settings.retention.loading')}
          </p>
        ) : (
          <RetentionContext.Provider value={actions}>
            <div className="space-y-2">
              <h3 className="text-sm font-semibold">{t('settings.retention.groupRaw')}</h3>
              <DataTable
                ariaLabel={t('settings.retention.tableLabel')}
                captionId="settings.retention-table"
                columns={columns}
                data={rawFamilies}
                emptyState={<p>{t('settings.retention.empty')}</p>}
                getRowId={(row) => row.family}
                scope="families"
              />
            </div>
            {rollupFamilies.length > 0 ? (
              <div className="space-y-2">
                <h3 className="text-sm font-semibold">{t('settings.retention.groupRollup')}</h3>
                <DataTable
                  ariaLabel={t('settings.retention.rollupTableLabel')}
                  captionId="settings.retention-rollup-table"
                  columns={columns}
                  data={rollupFamilies}
                  emptyState={<p>{t('settings.retention.empty')}</p>}
                  getRowId={(row) => row.family}
                  scope="families"
                />
              </div>
            ) : null}
          </RetentionContext.Provider>
        )}
      </div>

      <Dialog
        onOpenChange={(open) => {
          if (!open) setConfirming(null)
        }}
        open={confirming !== null}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('settings.retention.shortenTitle', {
                family: confirming?.row.family ?? t('settings.retention.thisFamily'),
              })}
            </DialogTitle>
            <DialogDescription>
              {confirming ? confirmationText(confirming.preview) : ''}
            </DialogDescription>
          </DialogHeader>
          <Callout variant="destructive">
            {t('settings.retention.deleteWarning', { change: changeText(confirming, t) })}
          </Callout>
          <label className="flex items-start gap-2 text-sm" htmlFor={confirmId}>
            <input
              checked={acknowledged}
              className="mt-0.5"
              id={confirmId}
              onChange={(event) => setAcknowledged(event.target.checked)}
              type="checkbox"
            />
            <span>{t('settings.retention.acknowledge')}</span>
          </label>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                {t('settings.retention.cancel')}
              </Button>
            </DialogClose>
            <Button
              disabled={!acknowledged}
              onClick={confirmShortening}
              type="button"
              variant="destructive"
            >
              {t('settings.retention.confirmDelete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Section>
  )
}
