import { Degraded } from '@/components/state'
import { formatTimestamp } from '@/lib/format/timestamp'
import type { JoinedFinding } from '@/lib/findings'

interface FindingCardProps {
  finding: JoinedFinding
}

function formatNeed(need: string): string {
  const normalized = need.toLowerCase()
  if (normalized === 'history_7d' || normalized === 'history7d') {
    return 'seven days of history'
  }
  if (normalized.includes('metric')) return `metric ${need.replace(/^metric[:.]/i, '')}`
  if (normalized.includes('check')) return `check ${need.replace(/^check[:.]/i, '')}`
  if (normalized.includes('host')) return 'host view'
  return need.replaceAll('_', ' ')
}

function formatEvidence(value: unknown): string {
  if (typeof value === 'string') return value
  try {
    const serialized = JSON.stringify(value)
    return serialized ?? String(value)
  } catch {
    return String(value)
  }
}

function requirementText(finding: JoinedFinding): string | undefined {
  const requirements = finding.needs.map(formatNeed)
  if (finding.min_tier && finding.min_tier !== 'T0') {
    requirements.push(`permission tier ${finding.min_tier}`)
  }
  return requirements.length > 0 ? requirements.join(', ') : undefined
}

function remedyText(finding: JoinedFinding): string | undefined {
  if (finding.needs.some((need) => /history[_-]?7d/i.test(need))) {
    return 'Remedy: waiting for seven days of history.'
  }
  if (finding.min_tier && finding.min_tier !== 'T0') {
    return `Remedy: grant ${finding.min_tier} access or request an extension.`
  }
  if (finding.needs.length > 0 || finding.catalogueMissing) {
    return 'Remedy: collect the missing inputs before evaluating this rule again.'
  }
  return undefined
}

function timestamp(value: string | undefined): React.ReactNode {
  if (!value) return '—'
  return (
    <time dateTime={value}>
      {formatTimestamp(value, 'UTC')}
    </time>
  )
}

export function FindingCard({ finding }: FindingCardProps) {
  const requirements = requirementText(finding)
  const target = [
    finding.object_name,
    finding.datname ? `database ${finding.datname}` : undefined,
    finding.instance_id ? `instance ${finding.instance_id}` : undefined,
    finding.cluster_id ? `cluster ${finding.cluster_id}` : undefined,
  ].filter((value): value is string => Boolean(value))
  const evidence = Object.entries(finding.evidence ?? {})

  return (
    <article aria-labelledby={`finding-${finding.finding_id}`} className="space-y-4 rounded-lg border p-4">
      <header className="space-y-2">
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="rounded border px-2 py-1 uppercase">{finding.severity}</span>
          <span className="rounded border px-2 py-1">{finding.state}</span>
          <span className="rounded border px-2 py-1">{finding.scope}</span>
        </div>
        <h3 className="text-base font-semibold" id={`finding-${finding.finding_id}`}>
          {finding.title}
        </h3>
        <p className="text-muted-foreground text-sm">Rule {finding.rule_id}</p>
      </header>

      {finding.state === 'degraded' ? (
        <div className="space-y-2">
          <Degraded
            reason={finding.degraded_reason ?? 'The rule could not be evaluated with the collected inputs.'}
            {...(requirements ? { requires: requirements } : {})}
          />
          {remedyText(finding) ? <p className="text-sm">{remedyText(finding)}</p> : null}
        </div>
      ) : null}

      {finding.detail ? <p className="text-sm">{finding.detail}</p> : null}
      {finding.remediation ? <p className="text-sm">Remediation: {finding.remediation}</p> : null}

      <dl className="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">Affected object</dt>
          <dd>{target.length > 0 ? target.join(' · ') : '—'}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">First seen</dt>
          <dd>{timestamp(finding.first_seen)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Last seen</dt>
          <dd>{timestamp(finding.last_seen)}</dd>
        </div>
        {finding.catalogueMissing ? (
          <div>
            <dt className="text-muted-foreground">Catalogue</dt>
            <dd>Rule metadata unavailable</dd>
          </div>
        ) : null}
      </dl>

      <section aria-labelledby={`evidence-${finding.finding_id}`} className="space-y-2">
        <h4 className="text-sm font-medium" id={`evidence-${finding.finding_id}`}>
          Evidence
        </h4>
        {evidence.length > 0 ? (
          <dl className="grid gap-2 text-sm sm:grid-cols-2">
            {evidence.map(([key, value]) => (
              <div key={key}>
                <dt className="text-muted-foreground">{key}</dt>
                <dd className="break-words">{formatEvidence(value)}</dd>
              </div>
            ))}
          </dl>
        ) : (
          <p className="text-muted-foreground text-sm">No evidence reported.</p>
        )}
      </section>
    </article>
  )
}
