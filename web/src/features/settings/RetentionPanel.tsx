import { createContext, useContext, useId, useMemo, useState } from 'react'

import { useRetention, useRetentionPreview, useSetRetention } from '@/api/queries'
import { DataTable } from '@/components/layout/DataTable'
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
import {
  confirmationText,
  isShortening,
  previewSummary,
  RETENTION_FAMILY_LABELS,
  retentionErrorMessage,
  validateRetentionDays,
  type RetentionFamily,
  type RetentionFamilyName,
  type RetentionPreview,
} from '@/lib/retention'

/** What each control of a row changes (I-16), referenced by its `aria-describedby`. */
const RETENTION_HELP = {
  days: 'How many days of history each metrics family keeps. History older than this is deleted by the policy; a longer value keeps more.',
  preview:
    'Preview counts the chunks and bytes this value would delete now. It changes nothing; Apply is enabled once the preview matches the value typed.',
  apply:
    'Apply replaces the retention policy. A longer retention applies at once; a shorter one asks for confirmation. Deletion is not immediate: each policy runs once a day and drops whole chunks older than its retention.',
} as const

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
      {RETENTION_FAMILY_LABELS[row.family]}
    </span>
  )
}

function CurrentCell({ row }: { row: RetentionFamily }) {
  if (row.unsupported_policy !== undefined) {
    return (
      <span className="flex flex-wrap items-center gap-2" title={row.unsupported_policy}>
        <span>Set outside pglens</span>
        <Badge variant="outline">custom</Badge>
      </span>
    )
  }
  if (row.days === null) {
    return (
      <span className="flex flex-wrap items-center gap-2">
        <span>No policy: keeps all history</span>
        <Badge variant="outline">custom</Badge>
      </span>
    )
  }
  return (
    <span className="flex flex-wrap items-center gap-2">
      <span className="tabular-nums">{row.days} days</span>
      <Badge variant={row.source === 'default' ? 'secondary' : 'outline'}>{row.source}</Badge>
    </span>
  )
}

function DaysCell({ row }: { row: RetentionFamily }) {
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
          aria-label={`New retention of ${row.family} in days`}
          autoComplete="off"
          className="h-8 w-24"
          disabled={state.pending === 'apply' || row.unsupported_policy !== undefined}
          inputMode="numeric"
          onChange={(event) => actions.edit(row, event.target.value)}
          value={state.draft}
        />
        <span className="text-muted-foreground text-xs whitespace-nowrap" id={rangeId}>
          {row.min_days}–{row.max_days} days
        </span>
      </div>
    </div>
  )
}

function PreviewCell({ row }: { row: RetentionFamily }) {
  const actions = useRetentionActions()
  const state = actions.state(row)
  const validation = validateRetentionDays(state.draft, row.min_days, row.max_days)
  const unsupported = row.unsupported_policy !== undefined

  return (
    <div className="flex min-w-0 items-center gap-3">
      <Button
        aria-describedby={actions.helpIds.preview}
        aria-label={`Preview ${row.family}`}
        disabled={!validation.ok || state.pending !== null || unsupported}
        onClick={() => actions.preview(row)}
        size="xs"
        type="button"
        variant="outline"
      >
        {state.pending === 'preview' ? 'Previewing…' : 'Preview'}
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
            {row.unsupported_policy}: change it with TimescaleDB.
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
      aria-label={`Apply ${row.family}`}
      disabled={!ready || state.pending !== null}
      onClick={() => actions.apply(row)}
      size="xs"
      type="button"
    >
      {state.pending === 'apply' ? 'Applying…' : 'Apply'}
    </Button>
  )
}

const RETENTION_COLUMNS = [
  {
    accessorKey: 'family',
    header: 'Family',
    meta: { width: '17rem' },
    cell: ({ row }: { row: { original: RetentionFamily } }) => <FamilyCell row={row.original} />,
  },
  {
    accessorKey: 'days',
    header: 'Retention now',
    meta: { width: '13rem' },
    cell: ({ row }: { row: { original: RetentionFamily } }) => <CurrentCell row={row.original} />,
  },
  {
    id: 'new-days',
    header: 'New retention',
    enableSorting: false,
    enableHiding: false,
    meta: { width: '10rem' },
    cell: ({ row }: { row: { original: RetentionFamily } }) => <DaysCell row={row.original} />,
  },
  {
    id: 'preview',
    header: 'Preview',
    enableSorting: false,
    enableHiding: false,
    cell: ({ row }: { row: { original: RetentionFamily } }) => <PreviewCell row={row.original} />,
  },
  {
    id: 'apply',
    header: 'Apply',
    enableSorting: false,
    enableHiding: false,
    meta: { width: '7rem' },
    cell: ({ row }: { row: { original: RetentionFamily } }) => <ApplyCell row={row.original} />,
  },
]

interface Confirming {
  row: RetentionFamily
  preview: RetentionPreview
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
        saved: `Applied: ${saved.days ?? days} days.`,
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
    <Section captionId="settings.retention" id="settings-retention" title="Data retention">
      <div className="space-y-4 min-[1600px]:space-y-3">
        <ul className="text-muted-foreground grid gap-1 text-sm leading-relaxed min-[1600px]:grid-cols-3 min-[1600px]:gap-4">
          <li id={helpIds.days}>{RETENTION_HELP.days}</li>
          <li id={helpIds.preview}>{RETENTION_HELP.preview}</li>
          <li id={helpIds.apply}>{RETENTION_HELP.apply}</li>
        </ul>
        {retentionQuery.error ? (
          <ErrorState
            endpoint="data retention"
            failure={retentionQuery.error}
            onRetry={() => void retentionQuery.refetch()}
          />
        ) : retentionQuery.isPending ? (
          <p aria-busy="true" role="status">
            Loading data retention…
          </p>
        ) : (
          <RetentionContext.Provider value={actions}>
            <DataTable
              ariaLabel="Data retention by metrics family"
              captionId="settings.retention-table"
              columns={RETENTION_COLUMNS}
              data={retentionQuery.data}
              emptyState={<p>No metrics family was reported.</p>}
              getRowId={(row) => row.family}
              scope="families"
            />
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
              Shorten the retention of {confirming?.row.family ?? 'this family'}?
            </DialogTitle>
            <DialogDescription>
              {confirming ? confirmationText(confirming.preview) : ''}
            </DialogDescription>
          </DialogHeader>
          <Callout variant="destructive">
            Deleted history cannot be restored from pglens. Retention{' '}
            {confirming?.row.days === null || confirming === null
              ? 'is set for the first time'
              : `goes from ${confirming.row.days} to ${confirming.preview.days} days`}
            .
          </Callout>
          <label className="flex items-start gap-2 text-sm" htmlFor={confirmId}>
            <input
              checked={acknowledged}
              className="mt-0.5"
              id={confirmId}
              onChange={(event) => setAcknowledged(event.target.checked)}
              type="checkbox"
            />
            <span>
              I understand that this history is deleted permanently at the next policy run.
            </span>
          </label>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button
              disabled={!acknowledged}
              onClick={confirmShortening}
              type="button"
              variant="destructive"
            >
              Delete history and apply
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Section>
  )
}
