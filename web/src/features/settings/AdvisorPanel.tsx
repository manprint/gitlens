import { createContext, useContext, useId, useMemo, useState, type FormEvent } from 'react'

import {
  useAdvisorRules,
  useAdvisorWindows,
  useSetAdvisorRuleOverride,
  useSetAdvisorWindows,
} from '@/api/queries'
import { DataTable, type DataTableToolbar } from '@/components/layout/DataTable'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
import { ruleText } from '@/lib/ruleText'
import { InstanceViewTabs } from '@/features/instance/InstancePanel'
import { useInstanceView } from '@/features/instance/instanceViews'
import {
  advisorErrorMessage,
  isShortWindow,
  ruleCategory,
  validateWindow,
  windowDraft,
  windowHelp,
  windowLabel,
  windowText,
  type AdvisorRule,
  type AdvisorWindow,
  type AdvisorWindowKey,
  type WindowUnit,
} from '@/lib/advisorSettings'
import { cn } from '@/lib/utils'

import { ADVISOR_VIEWS } from './settingsTabs'

/** What each control of the tab changes (I-16), referenced by its `aria-describedby`. */
const ADVISOR_HELP = {
  enabled: 'settings.advisor.help.enabled',
  filters: 'settings.advisor.help.filters',
  window: 'settings.advisor.help.window',
  reset: 'settings.advisor.help.reset',
} as const satisfies Record<string, MessageKey>

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

function rulesToolbar(t: Translate): DataTableToolbar<RuleRow> {
  return {
    searchFields: [(row) => row.id, (row) => ruleText(row.id) ?? '', (row) => row.category],
    filters: [
      {
        id: 'category',
        label: t('settings.advisor.colCategory'),
        kind: 'select',
        accessor: (row) => row.category,
      },
      {
        id: 'severity',
        label: t('settings.advisor.colSeverity'),
        kind: 'select',
        accessor: (row) => row.severity,
      },
      {
        id: 'status',
        label: t('settings.advisor.filterRuns'),
        kind: 'select',
        options: [
          { value: 'enabled', label: t('settings.advisor.enabled') },
          { value: 'disabled', label: t('settings.advisor.disabled') },
        ],
        accessor: (row) => row.status,
      },
    ],
  }
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
  const t = useT()
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
        aria-label={t('settings.advisor.runLabel', { rule: rule.id })}
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
        {pending
          ? t('settings.advisor.saving')
          : rule.enabled
            ? t('settings.advisor.enabled')
            : t('settings.advisor.disabled')}
      </span>
      {rule.overridden ? (
        <Badge title={t('settings.advisor.overridden')} variant="outline">
          {t('settings.advisor.custom')}
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

function ruleColumns(t: Translate) {
  return [
    {
      accessorKey: 'id',
      header: t('settings.advisor.colRule'),
      cell: ({ row }: { row: { original: RuleRow } }) => (
        <>
          {ruleText(row.original.id)}
          {ruleText(row.original.id) === undefined ? null : ' '}
          <span
            className={`font-mono text-xs ${ruleText(row.original.id) === undefined ? '' : 'text-muted-foreground'}`}
          >
            {row.original.id}
          </span>
        </>
      ),
      meta: { width: '45%', truncate: true, help: 'rules.rule' as const },
    },
    {
      accessorKey: 'category',
      header: t('settings.advisor.colCategory'),
      meta: { width: '9rem' },
    },
    {
      accessorKey: 'severity',
      header: t('settings.advisor.colSeverity'),
      meta: { width: '8rem' },
    },
    { accessorKey: 'scope', header: t('settings.advisor.colScope'), meta: { width: '8rem' } },
    {
      accessorKey: 'minTier',
      header: t('settings.advisor.colMinTier'),
      meta: { width: '9.5rem' },
    },
    {
      accessorKey: 'status',
      header: t('settings.advisor.enabled'),
      enableHiding: false,
      meta: { help: 'advisor.enabled' as const },
      cell: ({ row }: { row: { original: RuleRow } }) => <EnabledCell row={row.original} />,
    },
  ]
}

/** The rules view: every rule with a switch that disables or enables it. */
function AdvisorRules() {
  const t = useT()
  const rulesQuery = useAdvisorRules()
  const setOverride = useSetAdvisorRuleOverride()
  const [pending, setPending] = useState<Record<string, boolean>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const baseId = useId().replaceAll(':', '')
  const helpIds = { enabled: `${baseId}-enabled-help` }
  const rows = useMemo(() => (rulesQuery.data ?? []).map(ruleRow), [rulesQuery.data])
  const disabled = rows.filter((row) => row.status === 'disabled').length
  const columns = useMemo(() => ruleColumns(t), [t])
  const toolbar = useMemo(() => rulesToolbar(t), [t])

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
        {t(ADVISOR_HELP.enabled)} {t(ADVISOR_HELP.filters)}
      </p>
      {rulesQuery.error ? (
        <ErrorState
          endpoint={t('settings.advisor.endpointRules')}
          failure={rulesQuery.error}
          onRetry={() => void rulesQuery.refetch()}
        />
      ) : rulesQuery.isPending ? (
        <p aria-busy="true" role="status">
          {t('settings.advisor.loadingRules')}
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
            ariaLabel={t('settings.advisor.tableLabel')}
            captionId="settings.advisor-rules-table"
            columns={columns}
            data={rows}
            emptyState={<p>{t('settings.advisor.emptyRules')}</p>}
            getRowId={(row) => row.id}
            scope="rules"
            tableId="advisor-rules"
            toolbar={toolbar}
            toolbarExtra={
              <>
                <span
                  className="text-muted-foreground text-sm whitespace-nowrap"
                  data-testid="advisor-rules-disabled-count"
                >
                  {disabled === 0
                    ? t('settings.advisor.allRun')
                    : t('settings.advisor.countDisabled', { disabled, total: rows.length })}
                </span>
                <HelpLink anchor="settings#rules" label={t('settings.tabs.advisor')} />
              </>
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
  const t = useT()
  const id = useId().replaceAll(':', '')
  const ids = { input: `${id}-input`, unit: `${id}-unit`, help: `${id}-help`, error: `${id}-error` }
  // An untouched field shows the stored value and is never refused: a stored
  // value that is not a whole number of hours (5400 s) is only read.
  const validation = edited
    ? validateWindow(draft.value, draft.unit, window)
    : ({ ok: true, seconds: window.seconds } as const)
  const label = windowLabel(window.key)

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
            aria-label={t('settings.advisor.unitLabel', { window: label })}
            className="h-8 w-28 px-2 text-sm"
            disabled={disabled}
            id={ids.unit}
            onChange={(event) => onDraft({ ...draft, unit: event.target.value as WindowUnit })}
            value={draft.unit}
          >
            <option value="hours">{t('settings.advisor.unitHours')}</option>
            <option value="days">{t('settings.advisor.unitDays')}</option>
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
            {t('settings.advisor.shortWindow')}
          </span>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-2 pt-1 text-sm">
        <span>
          {t('settings.advisor.now')}{' '}
          <span className="tabular-nums">{windowText(window.seconds)}</span>
        </span>
        <Badge variant={window.source === 'default' ? 'secondary' : 'outline'}>
          {t(
            window.source === 'default'
              ? 'settings.retention.sourceDefault'
              : 'settings.retention.sourceCustom',
          )}
        </Badge>
        <Button
          aria-describedby={resetHelpId}
          aria-label={t('settings.advisor.resetLabel', {
            action: resetting
              ? t('settings.advisor.resetting')
              : t('settings.advisor.resetDefault'),
            window: label,
          })}
          disabled={disabled || window.source === 'default'}
          onClick={onReset}
          size="xs"
          type="button"
          variant="outline"
        >
          {resetting ? t('settings.advisor.resetting') : t('settings.advisor.resetDefault')}
        </Button>
      </div>
      <p className="text-muted-foreground text-sm leading-relaxed min-[1600px]:pt-1" id={ids.help}>
        {windowHelp(window.key)}{' '}
        {t('settings.advisor.range', {
          min: windowText(window.min_seconds),
          max: windowText(window.max_seconds),
          fallback: windowText(window.default_seconds),
        })}
      </p>
    </div>
  )
}

/** The observation windows view: two windows, saved together, each reset alone. */
function AdvisorWindows() {
  const t = useT()
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
        t('settings.advisor.savedList', {
          what,
          windows: result
            .map((window) =>
              t('settings.advisor.windowPair', {
                label: windowLabel(window.key),
                window: windowText(window.seconds),
              }),
            )
            .join(', '),
        }),
      )
    } catch (failure) {
      // The typed values and the stored ones stay on screen (I-11).
      setError(advisorErrorMessage(failure, t('settings.advisor.windowsName')))
    } finally {
      setPending(null)
    }
  }

  function save(event: FormEvent) {
    event.preventDefault()
    if (!valid || !changed || pending !== null) return
    setPending('save')
    void write(changes, t('settings.advisor.savedWhat'))
  }

  function reset(key: AdvisorWindowKey) {
    if (pending !== null) return
    setPending(key)
    setDrafts((current) => without(current, key))
    void write({ [key]: null }, t('settings.advisor.resetWhat'))
  }

  return (
    <Section
      actions={
        <HelpLink anchor="settings#observation-windows" label={t('settings.views.windows')} />
      }
      captionId="settings.advisor-windows"
      id="settings-advisor-windows"
      title={t('settings.views.windows')}
    >
      <form className="space-y-4 px-5 py-4" noValidate onSubmit={save}>
        <Callout role="note" variant="warning">
          <strong>{t('settings.advisor.warningTitle')}</strong> {t('settings.advisor.warningBody')}
        </Callout>
        <ul className="text-muted-foreground grid gap-1 text-sm leading-relaxed min-[1600px]:grid-cols-2 min-[1600px]:gap-4">
          <li id={helpIds.window}>{ADVISOR_HELP.window}</li>
          <li id={helpIds.reset}>{ADVISOR_HELP.reset}</li>
        </ul>
        {windowsQuery.error ? (
          <ErrorState
            endpoint={t('settings.advisor.endpointWindows')}
            failure={windowsQuery.error}
            onRetry={() => void windowsQuery.refetch()}
          />
        ) : windowsQuery.isPending ? (
          <p aria-busy="true" role="status">
            {t('settings.advisor.loadingWindows')}
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
                {pending === 'save'
                  ? t('settings.advisor.saving')
                  : t('settings.advisor.saveWindows')}
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
  const t = useT()
  const [view = 'rules', showView] = useInstanceView(ADVISOR_VIEWS)

  return (
    <Tabs className="gap-4" onValueChange={showView} value={view}>
      <InstanceViewTabs
        label={t('settings.page.advisorViews')}
        views={ADVISOR_VIEWS.map((entry) => ({ id: entry.id, label: t(entry.labelKey) }))}
      />
      <TabsContent value="rules">
        <AdvisorRules />
      </TabsContent>
      <TabsContent value="windows">
        <AdvisorWindows />
      </TabsContent>
    </Tabs>
  )
}
