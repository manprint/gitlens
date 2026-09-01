import type { Schemas } from '@/api/types'
import { useInstanceCommandAudit } from '@/api/queries'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { formatTimestamp } from '@/lib/format'

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
    <div>
      <span className="text-muted-foreground">{label}: </span>
      <time dateTime={value}>{formatTimestamp(value, 'UTC')}</time>
    </div>
  )
}

function timingFields(entry: CommandAuditEntry) {
  return [
    ['Requested', entry.requested_at ?? entry.created_at],
    ['Claimed', entry.claimed_at],
    ['Executed', entry.executed_at ?? entry.finished_at],
    ['Expires', entry.expires_at],
  ] as const
}

function InstanceCommandAudit({ instance }: { instance: InstanceSummary }) {
  const query = useInstanceCommandAudit(instance.instance_id)
  const label = `${instance.addr}:${instance.port}`

  if (query.isPending) {
    return (
      <section aria-busy="true" aria-label={`Loading command audit for ${label}`} role="status">
        Loading command audit…
      </section>
    )
  }

  if (query.error) {
    return (
      <ErrorState
        endpoint={`command audit for ${label}`}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }

  const entries = sortNewestFirst((query.data ?? []).filter(isCommandAuditEntry))
  if (entries.length === 0) {
    return (
      <EmptyState
        description="No command requests, results, expirations, or rejections have been recorded for this instance."
        title="No command activity"
      />
    )
  }

  return (
    <Section description={`Newest activity first for ${label}.`} title={label}>
      <div className="overflow-x-auto">
        <table aria-label={`Command audit for ${label}`}>
          <thead>
            <tr>
              <th scope="col">Time</th>
              <th scope="col">Command</th>
              <th scope="col">Arguments</th>
              <th scope="col">State</th>
              <th scope="col">Timing and detail</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry, index) => {
              const state = entryState(entry)
              const detail = entry.detail ?? entry.error
              return (
                <tr key={String(entry.audit_id ?? entry.command_id ?? index)}>
                  <td>
                    <Timestamp label="Executed" value={entryTime(entry)} />
                  </td>
                  <td>{entry.kind ?? 'unknown'}</td>
                  <td>
                    <code>{displayValue(entry.args)}</code>
                  </td>
                  <td>{state}</td>
                  <td>
                    {timingFields(entry).map(([timingLabel, timingValue]) => (
                      <Timestamp key={timingLabel} label={timingLabel} value={timingValue} />
                    ))}
                    {state.toLowerCase() === 'expired' && !entry.expires_at ? (
                      <div>Expired</div>
                    ) : null}
                    {detail ? <div>{detail}</div> : null}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </Section>
  )
}

export function CommandAudit({ instances }: CommandAuditProps) {
  return (
    <Section
      description="This audit records the action, not the person: pglens has no user identity, accounts, or roles."
      title="Command audit"
    >
      {instances.length === 0 ? (
        <EmptyState
          description="Add a monitored PostgreSQL instance to see command activity."
          title="No command audit"
        />
      ) : (
        <div className="space-y-6">
          {instances.map((instance) => (
            <InstanceCommandAudit instance={instance} key={instance.instance_id} />
          ))}
        </div>
      )}
    </Section>
  )
}
