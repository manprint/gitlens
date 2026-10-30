import { createContext, useContext, useId, useMemo, useState, type FormEvent } from 'react'

import {
  useAdvisorRules,
  useAdvisorWindows,
  useSetAdvisorRuleOverride,
  useSetAdvisorWindows,
} from '@/api/queries'
import { DataTable, type DataTableToolbar } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import { InstanceViewTabs } from '@/features/instance/InstancePanel'
import { useInstanceView } from '@/features/instance/instanceViews'
import {
  advisorErrorMessage,
  formatWindow,
  isShortWindow,
  ruleCategory,
  validateWindow,
  WINDOW_HELP,
  WINDOW_LABELS,
  windowDraft,
  type AdvisorRule,
  type AdvisorWindow,
  type AdvisorWindowKey,
  type WindowUnit,
} from '@/lib/advisorSettings'
import { cn } from '@/lib/utils'

import { ADVISOR_VIEWS } from './settingsTabs'

/** What each control of the tab changes (I-16), referenced by its `aria-describedby`. */
const ADVISOR_HELP = {
  enabled:
    'The switch decides whether a rule runs. Off: the rule is not evaluated and its open, muted and non-evaluable findings are resolved at the next evaluation. On again: its findings reopen at the following evaluation if the condition still holds.',
  filters:
    'Search and filters narrow the list only; they change no rule. Thresholds are fixed by the advisor and cannot be edited.',
  window:
    'Saving stores the window for the advisor. A shorter window reports sooner and can raise false positives.',
  reset: 'Reset to default deletes the stored value, so the window is 7 days again.',
} as const

interface RuleRow {
  id: string
  category: string
  severity: string
  scope: string
  minTier: string
  status: 'enabled' | 'disabled'
  rule: AdvisorRule
}

function ruleRow(rule: AdvisorRule): RuleRow {
  return {
    id: rule.id,
    category: ruleCategory(rule.id),
    severity: rule.severity,
    scope: rule.scope,
    minTier: rule.min_tier,
    status: rule.enabled ? 'enabled' : 'disabled',
    rule,
  }
}

const RULES_TOOLBAR: DataTableToolbar<RuleRow> = {
  searchFields: [(row) => row.id, (row) => row.category],
  filters: [
    { id: 'category', label: 'Category', kind: 'select', accessor: (row) => row.category },
    { id: 'severity', label: 'Severity', kind: 'select', accessor: (row) => row.severity },
    {
      id: 'status',
      label: 'Runs',
      kind: 'select',
      options: [
        { value: 'enabled', label: 'Enabled' },
        { value: 'disabled', label: 'Disabled' },
      ],
      accessor: (row) => row.status,
    },
  ],
}

/** The record without `key`. */
function without<T>(record: Record<string, T>, key: string): Record<string, T> {
  return Object.fromEntries(Object.entries(record).filter(([entry]) => entry !== key))
}

/** The write in flight and the last failure, per rule. */
interface RuleWriteState {
  pending: Record<string, boolean>
  errors: Record<string, string>
  toggle: (rule: AdvisorRule) => void
  helpId: string
}

// The table's cells read the panel's state through the context, so a sort, a
// filter or a page change never loses a row's pending write or error.
const RuleWriteContext = createContext<RuleWriteState | null>(null)

function useRuleWrites(): RuleWriteState {
  const state = useContext(RuleWriteContext)
  if (state === null) throw new Error('Advisor rule cells need the rule write context')
  return state
}

function EnabledCell({ row }: { row: RuleRow }) {
  const writes = useRuleWrites()
  const { rule } = row
  const pending = writes.pending[rule.id] === true
  const error = writes.errors[rule.id]
  const errorId = `${writes.helpId}-${rule.id.replaceAll('.', '-')}-error`

  return (
    <div className="flex min-w-0 items-center gap-2">
      <button
        aria-checked={rule.enabled}
        aria-describedby={[writes.helpId, error === undefined ? null : errorId]
          .filter(Boolean)
          .join(' ')}
        aria-label={`Run ${rule.id}`}
        className={cn(
          'focus-visible:ring-ring/50 relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-60',
          rule.enabled ? 'bg-primary border-primary' : 'bg-muted border-border',
        )}
        disabled={pending}
        onClick={() => writes.toggle(rule)}
        role="switch"
        type="button"
      >
        <span
          aria-hidden="true"
          className={cn(
            'bg-background size-4 rounded-full shadow-sm transition-transform',
            rule.enabled ? 'translate-x-4' : 'translate-x-0.5',
          )}
        />
      </button>
      <span className="text-sm whitespace-nowrap">
        {pending ? 'Saving…' : rule.enabled ? 'Enabled' : 'Disabled'}
      </span>
      {rule.overridden ? (
        <Badge title="Changed from the console" variant="outline">
          custom
        </Badge>
      ) : null}
      {error === undefined ? null : (
        <span
          className="text-destructive min-w-0 truncate text-xs"
          id={errorId}
          role="alert"
          title={error}
        >
          {error}
        </span>
      )}
    </div>
  )
}

const RULE_COLUMNS = [
  { accessorKey: 'id', header: 'Rule ID', meta: { width: '24%', truncate: true } },
  { accessorKey: 'category', header: 'Category', meta: { width: '9rem' } },
  { accessorKey: 'severity', header: 'Severity', meta: { width: '8rem' } },
  { accessorKey: 'scope', header: 'Scope', meta: { width: '8rem' } },
  { accessorKey: 'minTier', header: 'Minimum tier', meta: { width: '9.5rem' } },
  {
    accessorKey: 'status',
    header: 'Enabled',
    enableHiding: false,
    meta: { help: 'advisor.enabled' as const },
    cell: ({ row }: { row: { original: RuleRow } }) => <EnabledCell row={row.original} />,
  },
]

/** The rules view: every rule with a switch that disables or enables it. */
function AdvisorRules() {
  const rulesQuery = useAdvisorRules()
  const setOverride = useSetAdvisorRuleOverride()
  const [pending, setPending] = useState<Record<string, boolean>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const baseId = useId().replaceAll(':', '')
  const helpIds = { enabled: `${baseId}-enabled-help` }
  const rows = useMemo(() => (rulesQuery.data ?? []).map(ruleRow), [rulesQuery.data])
  const disabled = rows.filter((row) => row.status === 'disabled').length

  async function toggle(rule: AdvisorRule) {
    setPending((current) => ({ ...current, [rule.id]: true }))
    setErrors((current) => without(current, rule.id))
    try {
      await setOverride.mutateAsync({ ruleId: rule.id, enabled: !rule.enabled })
    } catch (error) {
      // The switch keeps showing the stored value (I-11); the row says why.
      setErrors((current) => ({ ...current, [rule.id]: advisorErrorMessage(error, rule.id) }))
    } finally {
      setPending((current) => without(current, rule.id))
    }
  }

  // No section around the table: with 20 rows per page the rules view fits a
  // 1920×950 screen only with the table right under the view tabs (D26). The
  // help is the table caption, the Enabled column's ⓘ and each switch's
  // description.
  return (
    <div className="space-y-2" id="settings-advisor-rules">
      <p className="sr-only" id={helpIds.enabled}>
        {ADVISOR_HELP.enabled} {ADVISOR_HELP.filters}
      </p>
      {rulesQuery.error ? (
        <ErrorState
          endpoint="advisor rules"
          failure={rulesQuery.error}
          onRetry={() => void rulesQuery.refetch()}
        />
      ) : rulesQuery.isPending ? (
        <p aria-busy="true" role="status">
          Loading advisor rules…
        </p>
      ) : (
        <RuleWriteContext.Provider
          value={{
            pending,
            errors,
            toggle: (rule) => void toggle(rule),
            helpId: helpIds.enabled,
          }}
        >
          <DataTable
            ariaLabel="Advisor rules"
            captionId="settings.advisor-rules-table"
            columns={RULE_COLUMNS}
            data={rows}
            emptyState={<p>The advisor reported no rule.</p>}
            getRowId={(row) => row.id}
            scope="rules"
            tableId="advisor-rules"
            toolbar={RULES_TOOLBAR}
            toolbarExtra={
              <span
                className="text-muted-foreground text-sm whitespace-nowrap"
                data-testid="advisor-rules-disabled-count"
              >
                {disabled === 0 ? 'All rules run' : `${disabled} of ${rows.length} disabled`}
              </span>
            }
          />
        </RuleWriteContext.Provider>
      )}
    </div>
  )
}

/** The typed value of one window; absent until the window is edited. */
interface WindowDraft {
  value: string
  unit: WindowUnit
}

function WindowField({
  window,
  draft,
  edited,
  onDraft,
  onReset,
  resetting,
  disabled,
  helpId,
  resetHelpId,
}: {
  window: AdvisorWindow
  draft: WindowDraft
  onDraft: (draft: WindowDraft) => void
  onReset: () => void
  resetting: boolean
  disabled: boolean
  helpId: string
  resetHelpId: string
  /** Whether the field holds a typed draft; only a draft is validated and saved. */
  edited: boolean
}) {
  const id = useId().replaceAll(':', '')
  const ids = { input: `${id}-input`, unit: `${id}-unit`, help: `${id}-help`, error: `${id}-error` }
  // An untouched field shows the stored value and is never refused: a stored
  // value that is not a whole number of hours (5400 s) is only read.
  const validation = edited
    ? validateWindow(draft.value, draft.unit, window)
    : ({ ok: true, seconds: window.seconds } as const)
  const label = WINDOW_LABELS[window.key]

  return (
    <div className="grid gap-2 border-b pb-4 last:border-b-0 last:pb-0 min-[1600px]:grid-cols-[13rem_15rem_14rem_1fr] min-[1600px]:items-start min-[1600px]:gap-4">
      <label className="text-foreground pt-1.5 text-sm font-medium" htmlFor={ids.input}>
        {label}
      </label>
      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <Input
            aria-describedby={[validation.ok ? null : ids.error, ids.help, helpId]
              .filter(Boolean)
              .join(' ')}
            aria-invalid={validation.ok ? undefined : true}
            autoComplete="off"
            className="h-8 w-24"
            disabled={disabled}
            id={ids.input}
            inputMode="numeric"
            onChange={(event) => onDraft({ ...draft, value: event.target.value })}
            value={draft.value}
          />
          <NativeSelect
            aria-describedby={ids.help}
            aria-label={`${label} unit`}
            className="h-8 w-28 px-2 text-sm"
            disabled={disabled}
            id={ids.unit}
            onChange={(event) => onDraft({ ...draft, unit: event.target.value as WindowUnit })}
            value={draft.unit}
          >
            <option value="hours">hours</option>
            <option value="days">days</option>
          </NativeSelect>
        </div>
        <span
          aria-live="polite"
          className="text-destructive block text-xs empty:hidden"
          id={ids.error}
        >
          {validation.ok ? '' : validation.error}
        </span>
        {validation.ok && isShortWindow(validation.seconds, window) ? (
          <span className="text-warning-foreground block text-xs">
            Shorter than the 7-day default: more findings, some of them false positives.
          </span>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-2 pt-1 text-sm">
        <span>
          Now <span className="tabular-nums">{formatWindow(window.seconds)}</span>
        </span>
        <Badge variant={window.source === 'default' ? 'secondary' : 'outline'}>
          {window.source}
        </Badge>
        <Button
          aria-describedby={resetHelpId}
          aria-label={`Reset ${label} to default`}
          disabled={disabled || window.source === 'default'}
          onClick={onReset}
          size="xs"
          type="button"
          variant="outline"
        >
          {resetting ? 'Resetting…' : 'Reset to default'}
        </Button>
      </div>
      <p className="text-muted-foreground text-sm leading-relaxed min-[1600px]:pt-1" id={ids.help}>
        {WINDOW_HELP[window.key]} Between {formatWindow(window.min_seconds)} and{' '}
        {formatWindow(window.max_seconds)}; default {formatWindow(window.default_seconds)}.
      </p>
    </div>
  )
}

/** The observation windows view: two windows, saved together, each reset alone. */
function AdvisorWindows() {
  const windowsQuery = useAdvisorWindows()
  const setWindows = useSetAdvisorWindows()
  const [drafts, setDrafts] = useState<Partial<Record<AdvisorWindowKey, WindowDraft>>>({})
  const [pending, setPending] = useState<'save' | AdvisorWindowKey | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState<string | null>(null)
  const baseId = useId().replaceAll(':', '')
  const helpIds = { window: `${baseId}-window-help`, reset: `${baseId}-reset-help` }
  const windows = windowsQuery.data ?? []

  const draftOf = (window: AdvisorWindow): WindowDraft =>
    drafts[window.key] ?? windowDraft(window.seconds)
  // Only the windows typed into are validated and sent: an untouched window,
  // even one stored as a value the form cannot show exactly, is never rewritten.
  const changes: Partial<Record<AdvisorWindowKey, number>> = {}
  let valid = true
  for (const window of windows) {
    const draft = drafts[window.key]
    if (draft === undefined) continue
    const validation = validateWindow(draft.value, draft.unit, window)
    if (!validation.ok) valid = false
    else if (validation.seconds !== window.seconds) changes[window.key] = validation.seconds
  }
  const changed = Object.keys(changes).length > 0

  async function write(body: Partial<Record<AdvisorWindowKey, number | null>>, what: string) {
    setError(null)
    setSaved(null)
    try {
      const result = await setWindows.mutateAsync(body)
      // The form shows the stored values again.
      setDrafts({})
      setSaved(
        `${what}: ${result.map((window) => `${WINDOW_LABELS[window.key]} ${formatWindow(window.seconds)}`).join(', ')}.`,
      )
    } catch (failure) {
      // The typed values and the stored ones stay on screen (I-11).
      setError(advisorErrorMessage(failure, 'The observation windows'))
    } finally {
      setPending(null)
    }
  }

  function save(event: FormEvent) {
    event.preventDefault()
    if (!valid || !changed || pending !== null) return
    setPending('save')
    void write(changes, 'Saved')
  }

  function reset(key: AdvisorWindowKey) {
    if (pending !== null) return
    setPending(key)
    setDrafts((current) => without(current, key))
    void write({ [key]: null }, 'Reset')
  }

  return (
    <Section
      captionId="settings.advisor-windows"
      id="settings-advisor-windows"
      title="Observation windows"
    >
      <form className="space-y-4 px-5 py-4" noValidate onSubmit={save}>
        <Callout role="note" variant="warning">
          <strong>Short windows can raise false positives.</strong> Saved windows apply at the next
          advisor evaluation. On PostgreSQL 16 and later the statistics age counts only for an index
          never scanned since the reset; a base backup stays recent for 7 days (30 days for a backup
          strategy) whatever the window.
        </Callout>
        <ul className="text-muted-foreground grid gap-1 text-sm leading-relaxed min-[1600px]:grid-cols-2 min-[1600px]:gap-4">
          <li id={helpIds.window}>{ADVISOR_HELP.window}</li>
          <li id={helpIds.reset}>{ADVISOR_HELP.reset}</li>
        </ul>
        {windowsQuery.error ? (
          <ErrorState
            endpoint="advisor windows"
            failure={windowsQuery.error}
            onRetry={() => void windowsQuery.refetch()}
          />
        ) : windowsQuery.isPending ? (
          <p aria-busy="true" role="status">
            Loading observation windows…
          </p>
        ) : (
          <>
            <div className="space-y-4">
              {windows.map((window) => (
                <WindowField
                  disabled={pending !== null}
                  draft={draftOf(window)}
                  edited={drafts[window.key] !== undefined}
                  helpId={helpIds.window}
                  key={window.key}
                  onDraft={(draft) => {
                    setSaved(null)
                    setError(null)
                    setDrafts((current) => ({ ...current, [window.key]: draft }))
                  }}
                  onReset={() => reset(window.key)}
                  resetHelpId={helpIds.reset}
                  resetting={pending === window.key}
                  window={window}
                />
              ))}
            </div>
            <div className="flex flex-wrap items-center gap-3">
              <Button
                aria-describedby={helpIds.window}
                disabled={!valid || !changed || pending !== null}
                size="sm"
                type="submit"
              >
                {pending === 'save' ? 'Saving…' : 'Save windows'}
              </Button>
              {saved ? <span role="status">{saved}</span> : null}
              {error ? (
                <span className="text-destructive" role="alert">
                  {error}
                </span>
              ) : null}
            </div>
          </>
        )}
      </form>
    </Section>
  )
}

/**
 * The Advisor rules tab (11.4, D18, D22c): which rules run, and the two
 * observation windows. Rules and windows are separate views (`?view=`) so each
 * fits a Full HD screen; Rules is the default and is never written to the URL.
 */
export function AdvisorPanel() {
  const [view = 'rules', showView] = useInstanceView(ADVISOR_VIEWS)

  return (
    <Tabs className="gap-4" onValueChange={showView} value={view}>
      <InstanceViewTabs label="Advisor views" views={ADVISOR_VIEWS} />
      <TabsContent value="rules">
        <AdvisorRules />
      </TabsContent>
      <TabsContent value="windows">
        <AdvisorWindows />
      </TabsContent>
    </Tabs>
  )
}
