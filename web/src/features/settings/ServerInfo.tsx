import { Link } from 'react-router-dom'

import { useSession } from '@/api/queries'
import { Section } from '@/components/layout/Section'
import { Unknown } from '@/components/state'
import { formatTimestamp } from '@/lib/format'

const API_CONTRACT_VERSION = '1.0.0'
const LIMITS_URL = '/docs/LIMITS.md'

const limits = [
  ['Raw retention', 'Raw data is retained for 30 days; there are no rollups.'],
  ['PostgreSQL versions', 'PostgreSQL 15 through 18 are supported.'],
  ['Replication', 'Streaming replication is supported; other replication modes are not.'],
  ['Databases per instance', 'At most 10 databases per instance are monitored by default.'],
  ['Relation data', 'Relation views are constrained by top-N budgets.'],
  ['Bloat', 'Bloat is an estimate, not a table-by-table guarantee.'],
  ['ASH', 'Active session history is statistical sampling, not a complete trace.'],
  ['Poolers', 'There is no pooler view in this release.'],
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
    <div className="grid gap-8 lg:grid-cols-2">
      <Section
        description="Build and session details reported by this pglens server."
        title="Server information"
      >
        <dl className="grid gap-3 sm:grid-cols-3">
          <div className="rounded-md border p-3">
            <dt className="text-muted-foreground text-sm">Interface build</dt>
            <dd className="mt-1 font-medium">{__PGLENS_BUILD__}</dd>
          </div>
          <div className="rounded-md border p-3">
            <dt className="text-muted-foreground text-sm">API contract</dt>
            <dd className="mt-1 font-medium">{API_CONTRACT_VERSION}</dd>
          </div>
          <div className="rounded-md border p-3">
            <dt className="text-muted-foreground text-sm">Session expiry</dt>
            <dd className="mt-1 font-medium">
              <SessionExpiry />
            </dd>
          </div>
        </dl>
      </Section>

      <Section description={<DocumentationLink />} title="Product limits">
        <ul className="space-y-3 text-sm">
          {limits.map(([label, detail]) => (
            <li
              className="flex items-start justify-between gap-4 rounded-md border p-3"
              key={label}
            >
              <span>
                <strong>{label}.</strong> {detail}
              </span>
              <a className="text-primary shrink-0 underline underline-offset-2" href={LIMITS_URL}>
                Details
              </a>
            </li>
          ))}
        </ul>
      </Section>

      <Section
        description="The current authentication boundary is deliberately small and shared."
        title="Authentication"
      >
        <ul className="list-disc space-y-2 pl-5 text-sm">
          <li>One shared password protects the browser session.</li>
          <li>No user accounts exist.</li>
          <li>No roles or per-user audit exist.</li>
          <li>Sessions are held in server memory and do not survive a restart.</li>
          <li>The agent uses a separate bearer token.</li>
          <li>There is no enrollment queue or browser revocation control.</li>
        </ul>
      </Section>

      <Section description="Navigate to the supporting operator surfaces." title="References">
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
