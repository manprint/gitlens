import { Link } from 'react-router-dom'

import { useSession } from '@/api/queries'
import { Section } from '@/components/layout/Section'
import { Unknown } from '@/components/state'
import { formatTimestamp } from '@/lib/format'

const LIMITS_URL = '/docs/LIMITS.md'

/** The Data retention tab, where each metrics family's retention is read and set (11.3). */
const RETENTION_TAB = '/settings?tab=retention'

const limits = [
  [
    'Raw retention',
    'Raw data is retained for 30 days by default (bloat estimates 90 days); the Data retention tab shows and sets the live value of each group of metrics. pglens keeps no summaries: data older than its retention is deleted, not condensed into a coarser history.',
  ],
  ['PostgreSQL versions', 'PostgreSQL 15 through 19 are supported.'],
  ['Replication', 'Streaming replication is supported; other replication modes are not.'],
  ['Databases per instance', 'At most 10 databases per instance are monitored by default.'],
  [
    'Table and index lists',
    'For each database only the top-ranked tables and indexes are kept, up to a fixed number per check; the ones left out are not counted and not collected later.',
  ],
  [
    'Bloat',
    'Bloat, the space wasted inside tables by old row versions, is an estimate worked out from statistics; it can be wrong in either direction.',
  ],
  [
    'Active session history',
    'pglens regularly samples what busy sessions are doing; it is a sample, not a complete trace.',
  ],
  [
    'Connection poolers',
    'A pooler such as PgBouncer shares database connections between many clients; there is no pooler view in this release.',
  ],
] as const

function DocumentationLink() {
  return (
    <a className="text-primary underline underline-offset-2" href={LIMITS_URL}>
      Product limits documentation
    </a>
  )
}

function SessionExpiry() {
  const sessionQuery = useSession()
  const expiresAt = sessionQuery.data?.expires_at

  if (sessionQuery.isPending) {
    return <span>Loading…</span>
  }
  if (expiresAt) {
    return <time dateTime={expiresAt}>{formatTimestamp(expiresAt, 'UTC')}</time>
  }
  return <Unknown reason="The session endpoint did not provide an expiry." />
}

export function ServerInfo() {
  return (
    // From 1600 px the cards and the limit rows are tighter, so the view fits a Full HD screen (D26).
    <div className="grid gap-8 min-[1600px]:gap-4 lg:grid-cols-2">
      <Section captionId="settings.server-info" title="Server information">
        <dl className="grid gap-3 sm:grid-cols-3">
          <div className="rounded-md border p-3">
            <dt className="text-muted-foreground text-sm">Interface build</dt>
            <dd className="mt-1 font-medium">{__PGLENS_BUILD__}</dd>
          </div>
          <div className="rounded-md border p-3">
            <dt className="text-muted-foreground text-sm">API contract</dt>
            <dd className="mt-1 font-medium">{__PGLENS_API_CONTRACT_VERSION__}</dd>
          </div>
          <div className="rounded-md border p-3">
            <dt className="text-muted-foreground text-sm">Session expiry</dt>
            <dd className="mt-1 font-medium">
              <SessionExpiry />
            </dd>
          </div>
        </dl>
      </Section>

      <Section
        actions={<DocumentationLink />}
        captionId="settings.product-limits"
        title="Product limits"
      >
        <ul className="space-y-3 text-sm min-[1600px]:space-y-1.5">
          {limits.map(([label, detail]) => (
            <li
              className="flex items-start justify-between gap-4 rounded-md border p-3 min-[1600px]:py-2"
              key={label}
            >
              <span>
                <strong>{label}.</strong> {detail}
              </span>
              {label === 'Raw retention' ? (
                <Link
                  className="text-primary shrink-0 underline underline-offset-2"
                  to={RETENTION_TAB}
                >
                  Data retention
                </Link>
              ) : (
                <a className="text-primary shrink-0 underline underline-offset-2" href={LIMITS_URL}>
                  Details
                </a>
              )}
            </li>
          ))}
        </ul>
      </Section>

      <Section captionId="settings.authentication" title="Authentication">
        <ul className="list-disc space-y-2 pl-5 text-sm min-[1600px]:space-y-1">
          <li>One shared password protects the browser session.</li>
          <li>No user accounts exist.</li>
          <li>No roles or per-user audit exist.</li>
          <li>Sessions are held in server memory and do not survive a restart.</li>
          <li>
            The agent signs in with its own secret token (a bearer token sent with its requests),
            not with the console password.
          </li>
          <li>There is no enrollment queue or browser revocation control.</li>
        </ul>
      </Section>

      <Section captionId="settings.references" title="References">
        <nav aria-label="Settings references" className="flex flex-wrap gap-4 text-sm">
          <a className="text-primary underline underline-offset-2" href="/docs/api.md">
            API reference
          </a>
          <Link className="text-primary underline underline-offset-2" to="/alerts/rules">
            Alert rules
          </Link>
          <Link className="text-primary underline underline-offset-2" to="/alerts/silences">
            Silences
          </Link>
        </nav>
      </Section>
    </div>
  )
}
