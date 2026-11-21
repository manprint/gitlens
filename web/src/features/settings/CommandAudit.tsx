import { useMemo } from 'react'

import type { Schemas } from '@/api/types'
import { REFRESH } from '@/api/policy'
import { useInstanceCommandAudit } from '@/api/queries'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useT, type Translate } from '@/i18n/react'
import { TIMESTAMP_COLUMN_WIDTH, formatTimestamp } from '@/lib/format'
import { instanceLabel } from '@/lib/names'

import { InstancePicker } from './InstancePicker'
import { usePickedInstance } from './usePickedInstance'

type InstanceSummary = Schemas['InstanceSummary']

interface CommandAuditEntry {
  args?: unknown
  audit_id?: number | string
  claimed_at?: string
  command_id?: string
  created_at?: string
  detail?: string
  error?: string
  executed_at?: string
  expires_at?: string
  finished_at?: string
  instance_id?: string
  kind?: string
  outcome?: string
  requested_at?: string
  requested_by?: unknown
  state?: string
  status?: string
}

interface CommandAuditProps {
  instances: InstanceSummary[]
}

function isCommandAuditEntry(value: unknown): value is CommandAuditEntry {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function displayValue(value: unknown): string {
  if (value === undefined || value === null) return '—'
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value) ?? '—'
  } catch {
    return '—'
  }
}

function entryState(entry: CommandAuditEntry): string {
  return entry.state ?? entry.status ?? entry.outcome ?? 'unknown'
}

function entryTime(entry: CommandAuditEntry): string | undefined {
  return entry.executed_at ?? entry.finished_at ?? entry.claimed_at ?? entry.created_at
}

function sortNewestFirst(entries: CommandAuditEntry[]): CommandAuditEntry[] {
  return entries
    .map((entry, index) => ({ entry, index }))
    .sort((left, right) => {
      const rightTime = Date.parse(entryTime(right.entry) ?? '') || 0
      const leftTime = Date.parse(entryTime(left.entry) ?? '') || 0
      if (rightTime !== leftTime) return rightTime - leftTime

      const rightId = String(right.entry.audit_id ?? '')
      const leftId = String(left.entry.audit_id ?? '')
      if (rightId && leftId && rightId !== leftId) {
        return rightId.localeCompare(leftId, undefined, { numeric: true }) * -1
      }
      return left.index - right.index
    })
    .map(({ entry }) => entry)
}

function Timestamp({ label, value }: { label: string; value: string | undefined }) {
  if (!value) return null
  return (
    <span className="mr-3">
      <span className="text-muted-foreground">{label}: </span>
      <time dateTime={value}>{formatTimestamp(value)}</time>
    </span>
  )
}

// One line per entry (GAP-9): the full text is the cell's title.
function timingText(entry: CommandAuditEntry, t: Translate): string {
  const parts: string[] = timingFields(entry, t)
    .filter(([, value]) => value)
    .map(([label, value]) => `${label}: ${formatTimestamp(value ?? '')}`)
  if (entryState(entry).toLowerCase() === 'expired' && !entry.expires_at) {
    parts.push(t('settings.audit.expired'))
  }
  const detail = entry.detail ?? entry.error
  if (detail) parts.push(detail)
  return parts.join('; ')
}

// The time of the entry itself is in the Time column: an audit entry is one
// event (the request, the result or the expiry), recorded when it happened.
function timingFields(entry: CommandAuditEntry, t: Translate): [string, string | undefined][] {
  return [
    [t('settings.audit.requested'), entry.requested_at ?? entry.created_at],
    [t('settings.audit.claimed'), entry.claimed_at],
    [t('settings.audit.expires'), entry.expires_at],
  ]
}

function auditToolbar(t: Translate): TableToolbarSpec<CommandAuditEntry> {
  return {
    searchFields: [
      (entry) => entry.kind,
      (entry) => displayValue(entry.args),
      (entry) => displayValue(entry.requested_by),
      (entry) => entry.detail ?? entry.error,
    ],
    filters: [
      { id: 'state', label: t('settings.audit.colState'), kind: 'select', accessor: entryState },
      {
        id: 'kind',
        label: t('settings.audit.colCommand'),
        kind: 'select',
        accessor: (entry) => entry.kind ?? 'unknown',
      },
    ],
  }
}

function auditColumns(t: Translate): readonly SimpleColumn<CommandAuditEntry>[] {
  return [
    {
      id: 'time',
      header: t('settings.audit.colTime'),
      width: TIMESTAMP_COLUMN_WIDTH,
      className: 'whitespace-nowrap',
      sortValue: (entry) => Date.parse(entryTime(entry) ?? '') || null,
      // The column says Time: the entry's own moment, without a label that would clip it.
      cell: (entry) => {
        const value = entryTime(entry)
        return value ? <time dateTime={value}>{formatTimestamp(value)}</time> : null
      },
    },
    {
      id: 'command',
      header: t('settings.audit.colCommand'),
      width: '7rem',
      sortValue: (entry) => entry.kind ?? 'unknown',
      cell: (entry) => entry.kind ?? 'unknown',
    },
    {
      id: 'args',
      header: t('settings.audit.colArgs'),
      truncate: (entry) => displayValue(entry.args),
      cell: (entry) => <code>{displayValue(entry.args)}</code>,
    },
    {
      id: 'requested-by',
      header: t('settings.audit.colRequestedBy'),
      truncate: (entry) => displayValue(entry.requested_by),
      cell: (entry) => displayValue(entry.requested_by),
      help: 'settings.audit-requested-by',
    },
    {
      id: 'state',
      header: t('settings.audit.colState'),
      width: '7rem',
      sortValue: entryState,
      cell: entryState,
      help: 'settings.audit-state',
    },
    {
      id: 'timing',
      header: t('settings.audit.colTiming'),
      truncate: (entry) => timingText(entry, t),
      cell: (entry) => {
        const state = entryState(entry)
        const detail = entry.detail ?? entry.error
        return (
          <>
            {timingFields(entry, t).map(([timingLabel, timingValue]) => (
              <Timestamp key={timingLabel} label={timingLabel} value={timingValue} />
            ))}
            {state.toLowerCase() === 'expired' && !entry.expires_at ? (
              <span className="mr-3">{t('settings.audit.expired')}</span>
            ) : null}
            {detail ? <span>{detail}</span> : null}
          </>
        )
      },
    },
  ]
}

function auditKey(entry: CommandAuditEntry): string {
  return String(entry.audit_id ?? entry.command_id ?? JSON.stringify(entry))
}

function InstanceCommandAudit({ instance }: { instance: InstanceSummary }) {
  const t = useT()
  const query = useInstanceCommandAudit(instance.instance_id, REFRESH.inventory)
  const label = instanceLabel(instance)
  const entries = useMemo(
    () => sortNewestFirst((query.data ?? []).filter(isCommandAuditEntry)),
    [query.data],
  )
  const columns = useMemo(() => auditColumns(t), [t])
  const toolbar = useMemo(() => auditToolbar(t), [t])

  if (query.isPending) {
    return (
      <section
        aria-busy="true"
        aria-label={t('settings.audit.loadingLabel', { instance: label })}
        role="status"
      >
        {t('settings.audit.loading')}
      </section>
    )
  }

  if (query.error) {
    return (
      <ErrorState
        endpoint={t('settings.audit.endpoint', { instance: label })}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }

  if (entries.length === 0) {
    return (
      <EmptyState
        description={t('settings.audit.emptyDescription')}
        title={t('settings.audit.emptyTitle')}
      />
    )
  }

  return (
    <Section captionId="settings.command-audit-instance" title={label}>
      <SimpleTable
        ariaLabel={t('settings.audit.tableLabel', { instance: label })}
        captionId="settings.command-audit-table"
        columns={columns}
        emptyState={null}
        rowKey={auditKey}
        rows={entries}
        scope="entries"
        tableId={`command-audit-${instance.instance_id}`}
        toolbar={toolbar}
      />
    </Section>
  )
}

export function CommandAudit({ instances }: CommandAuditProps) {
  const t = useT()
  const [picked, pick] = usePickedInstance('auditInstance', instances)
  return (
    <Section
      actions={
        <>
          <InstancePicker
            instances={instances}
            label={t('settings.audit.pickerLabel')}
            onPick={pick}
            picked={picked}
          />
          <HelpLink anchor="settings#command-audit" label={t('settings.audit.title')} />
        </>
      }
      captionId="settings.command-audit"
      title={t('settings.audit.title')}
    >
      {picked ? (
        <InstanceCommandAudit instance={picked} key={picked.instance_id} />
      ) : (
        <EmptyState
          description={t('settings.audit.emptyPickDescription')}
          title={t('settings.audit.emptyPickTitle')}
        />
      )}
    </Section>
  )
}
