import { useId } from 'react'
import { Link } from 'react-router-dom'

import { CopyButton } from '@/components/layout/CopyButton'
import { InfoHint } from '@/components/layout/InfoHint'
import { SqlText } from '@/components/layout/SqlText'
import { Degraded } from '@/components/state'
import { isTruncatedQuery } from '@/lib/format'
import { formatTimestamp } from '@/lib/format/timestamp'
import { findingCheck } from '@/lib/findingCheck'
import { evidenceRows } from '@/lib/findingEvidence'
import { muteExpiry, type JoinedFinding } from '@/lib/findings'
import { findingSubject, subjectLabel, type FindingSubject } from '@/lib/findingSubject'
import { instanceNameOrId, type NameLookup } from '@/lib/names'
import { needText, ruleText } from '@/lib/ruleText'
import { useT, type Translate } from '@/i18n/react'

import {
  findingPlace,
  scopeText,
  severityAccent,
  severityBadge,
  severityText,
  stateText,
} from './findingDisplay'
import { MuteDialog } from './MuteDialog'

interface FindingCardProps {
  finding: JoinedFinding
  /** Names of the clusters and instances; ids are shown without it. */
  names?: NameLookup | undefined
}

function formatNeed(t: Translate, need: string): string {
  const normalized = need.toLowerCase()
  if (normalized === 'history_7d' || normalized === 'history7d') {
    return t('findings.card.needHistory')
  }
  if (normalized.includes('metric')) {
    return t('findings.card.needMetric', { name: need.replace(/^metric[:.]/i, '') })
  }
  if (normalized.includes('check')) {
    return t('findings.card.needCheck', { name: need.replace(/^check[:.]/i, '') })
  }
  if (normalized.includes('host')) return t('findings.card.needHost')
  return needText(need)
}

function requirementText(t: Translate, finding: JoinedFinding): string | undefined {
  const requirements = finding.needs.map((need) => formatNeed(t, need))
  if (finding.min_tier && finding.min_tier !== 'T0') {
    requirements.push(t('findings.card.permissionTier', { tier: finding.min_tier }))
  }
  return requirements.length > 0 ? requirements.join(', ') : undefined
}

function remedyText(t: Translate, finding: JoinedFinding): string | undefined {
  if (finding.needs.some((need) => /history[_-]?7d/i.test(need))) {
    return t('findings.card.remedyHistory')
  }
  if (finding.min_tier && finding.min_tier !== 'T0') {
    return t('findings.card.remedyTier', { tier: finding.min_tier })
  }
  if (finding.needs.length > 0 || finding.catalogueMissing) {
    return t('findings.card.remedyInputs')
  }
  return undefined
}

function timestamp(value: string | undefined): React.ReactNode {
  if (!value) return '—'
  return <time dateTime={value}>{formatTimestamp(value)}</time>
}

function remainingMuteTime(t: Translate, finding: JoinedFinding): string {
  const seconds = muteExpiry(finding, new Date())
  if (seconds === null) return t('findings.card.unavailable')
  if (seconds < 60) return t('findings.card.muteSeconds', { count: Math.ceil(seconds) })
  if (seconds < 60 * 60) {
    return t('findings.card.muteMinutes', { count: Math.ceil(seconds / 60) })
  }
  if (seconds < 24 * 60 * 60) {
    return t('findings.card.muteHours', { count: Math.ceil(seconds / (60 * 60)) })
  }
  return t('findings.card.muteDays', { count: Math.ceil(seconds / (24 * 60 * 60)) })
}

function queryPath(instanceId: string, queryid: string, datname: string | null | undefined) {
  const path = `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(queryid)}`
  return datname ? `${path}?db=${encodeURIComponent(datname)}` : path
}

function Heading({ id, children }: { id: string; children: React.ReactNode }) {
  return (
    <h4 className="text-muted-foreground text-xs font-semibold tracking-wide uppercase" id={id}>
      {children}
    </h4>
  )
}

function Statement({
  finding,
  subject,
  headingId,
}: {
  finding: JoinedFinding
  subject: Extract<FindingSubject, { kind: 'statement' }>
  headingId: string
}) {
  const t = useT()
  const instanceId = finding.instance_id ? String(finding.instance_id) : null
  return (
    <section aria-labelledby={headingId} className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Heading id={headingId}>{t('findings.card.statement')}</Heading>
        <div className="flex items-center gap-2 text-xs">
          <span className="text-muted-foreground">
            {t('findings.card.queryid')} <code className="font-mono">{subject.queryid}</code>
          </span>
          {instanceId && (
            <Link
              className="text-primary font-medium hover:underline"
              to={queryPath(instanceId, subject.queryid, finding.datname)}
            >
              {t('findings.card.openQuery')}
            </Link>
          )}
        </div>
      </div>
      {subject.text ? (
        <div className="relative">
          <div className="absolute top-2 right-2">
            <CopyButton text={subject.text} />
          </div>
          <pre
            aria-label={t('findings.card.statementText')}
            className="bg-muted/30 max-h-80 overflow-auto rounded-lg border p-3 pr-12 break-words whitespace-pre-wrap"
            tabIndex={0}
          >
            <SqlText sql={subject.text} />
          </pre>
          {isTruncatedQuery(subject.text) && (
            <p className="text-muted-foreground mt-1 text-xs">{t('findings.card.truncated')}</p>
          )}
        </div>
      ) : (
        <p className="text-muted-foreground text-sm">{t('findings.card.notKept')}</p>
      )}
    </section>
  )
}

export function FindingCard({ finding, names }: FindingCardProps) {
  const t = useT()
  // Not the finding id: it holds database and object names, which can
  // contain spaces and would split the aria-labelledby references.
  const domId = useId().replaceAll(':', '')
  const requirements = requirementText(t, finding)
  const subject = findingSubject(finding)
  const objectLabel = subjectLabel(subject, (id) => (names ? instanceNameOrId(names, id) : id))
  const place = findingPlace(finding, names)
  const check = findingCheck(finding)
  const evidence = evidenceRows(
    finding.evidence,
    finding.rule_id,
    subject.kind === 'statement' ? ['queryid'] : [],
  )
  const rule = ruleText(finding.rule_id)

  return (
    <article
      aria-labelledby={`finding-${domId}`}
      className={`bg-card shadow-card relative space-y-5 overflow-hidden rounded-xl border p-5 pl-6 before:absolute before:inset-y-0 before:left-0 before:w-1 ${severityAccent(finding.severity)}`}
      data-finding-id={finding.finding_id}
    >
      <header className="space-y-2">
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span
            className={`rounded-full border px-2.5 py-0.5 font-semibold uppercase ${severityBadge(finding.severity)}`}
          >
            {severityText(t, finding.severity)}
          </span>
          <span className="bg-muted text-muted-foreground rounded-full border px-2.5 py-0.5">
            {stateText(t, finding.state)}
          </span>
          <span className="bg-muted text-muted-foreground rounded-full border px-2.5 py-0.5">
            {scopeText(t, finding.scope)}
          </span>
        </div>
        <h3 className="text-base font-semibold break-words" id={`finding-${domId}`}>
          {finding.title}
        </h3>
        <p className="text-muted-foreground text-sm">
          {rule}
          {rule === undefined ? null : ' '}
          <span className="font-mono text-xs">
            {t('findings.card.ruleId', { id: finding.rule_id })}
          </span>
        </p>
        <div>
          <MuteDialog finding={finding} />
        </div>
      </header>

      {finding.state === 'muted' ? (
        <div
          className="border-muted bg-muted/30 space-y-1 rounded-md border p-3 text-sm"
          role="status"
        >
          <strong>{t('findings.card.muted')}</strong>
          <p>
            {t('findings.card.reasonLine', {
              reason: finding.mute_reason ?? t('findings.card.noReason'),
            })}
          </p>
          <p>
            {t('findings.card.remainingLine', { time: remainingMuteTime(t, finding) })}
            {finding.muted_until ? (
              <>
                {' '}
                ({t('findings.card.expires')} {timestamp(finding.muted_until)})
              </>
            ) : null}
          </p>
        </div>
      ) : null}

      {finding.state === 'degraded' ? (
        <div className="space-y-2">
          <Degraded
            reason={finding.degraded_reason ?? t('findings.card.degradedReason')}
            {...(requirements ? { requires: requirements } : {})}
          />
          {remedyText(t, finding) ? <p className="text-sm">{remedyText(t, finding)}</p> : null}
        </div>
      ) : null}

      {finding.detail ? <p className="text-sm break-words">{finding.detail}</p> : null}

      {subject.kind === 'statement' ? (
        <Statement finding={finding} subject={subject} headingId={`statement-${domId}`} />
      ) : null}

      <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-4">
        {subject.kind !== 'statement' ? (
          <div className="sm:col-span-2">
            <dt className="text-muted-foreground">{t('findings.card.affectedObject')}</dt>
            <dd className="break-words">
              {objectLabel === '' ? t('findings.card.wholeInstance') : objectLabel}
            </dd>
          </div>
        ) : null}
        <div className={subject.kind === 'statement' ? 'sm:col-span-2' : ''}>
          <dt className="text-muted-foreground">{t('findings.card.where')}</dt>
          <dd className="break-words">{place.length > 0 ? place.join(' · ') : '—'}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('findings.card.firstSeen')}</dt>
          <dd>{timestamp(finding.first_seen)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('findings.table.lastSeen')}</dt>
          <dd>{timestamp(finding.last_seen)}</dd>
        </div>
        {finding.catalogueMissing ? (
          <div>
            <dt className="text-muted-foreground">{t('findings.card.catalogue')}</dt>
            <dd>{t('findings.card.metadataUnavailable')}</dd>
          </div>
        ) : null}
      </dl>

      {finding.remediation ? (
        <section aria-labelledby={`remedy-${domId}`} className="space-y-1">
          <Heading id={`remedy-${domId}`}>{t('findings.card.whatToDo')}</Heading>
          <p className="text-sm break-words">{finding.remediation}</p>
        </section>
      ) : null}

      {check ? (
        <section
          aria-labelledby={`verify-${domId}`}
          className="space-y-2"
          data-caption-id="findings.verify"
        >
          <div className="flex items-center gap-1">
            <Heading id={`verify-${domId}`}>{t('findings.card.verifyTitle')}</Heading>
            <InfoHint id="findings.verify" label={t('findings.card.verifyTitle')} />
          </div>
          <ol className="list-decimal space-y-1 pl-5 text-sm">
            {check.database ? (
              <li>
                {t('findings.card.connectDatabase')}{' '}
                <code className="font-mono">{check.database}</code>{' '}
                {t('findings.card.connectAfterDatabase')}
              </li>
            ) : (
              <li>{t('findings.card.connectAnyDatabase')}</li>
            )}
            {check.steps.map((step) => (
              <li key={step}>{step}</li>
            ))}
          </ol>
          <div className="relative">
            <div className="absolute top-2 right-2">
              <CopyButton text={check.sql} subject="SQL" />
            </div>
            <pre
              aria-label={t('findings.card.verifySql')}
              className="bg-muted/30 max-h-96 overflow-auto rounded-lg border p-3 pr-12 font-mono text-xs whitespace-pre"
              tabIndex={0}
            >
              {check.sql}
            </pre>
          </div>
          <p className="text-muted-foreground text-xs">{t('findings.card.verifyNote')}</p>
        </section>
      ) : null}

      <section aria-labelledby={`evidence-${domId}`} className="space-y-2">
        <Heading id={`evidence-${domId}`}>{t('findings.card.evidence')}</Heading>
        {evidence.length > 0 ? (
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-3">
            {evidence.map((row) => (
              <div key={row.key}>
                <dt className="text-muted-foreground" title={row.key}>
                  {row.label}
                </dt>
                <dd className="break-words tabular-nums">{row.value}</dd>
              </div>
            ))}
          </dl>
        ) : (
          <p className="text-muted-foreground text-sm">{t('findings.card.noEvidence')}</p>
        )}
      </section>
    </article>
  )
}
