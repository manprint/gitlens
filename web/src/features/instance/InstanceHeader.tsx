import { Link, NavLink } from 'react-router-dom'

import type { Schemas } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Degraded, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { formatLag, formatPostgresVersion, formatTimestamp } from '@/lib/format'

type Instance = Schemas['Instance']

interface InstanceHeaderProps {
  dataUpdatedAt: number
  instance: Instance
  replayLagSeconds?: number | null | undefined
}

const tabs = [
  { label: 'Overview', path: '' },
  { label: 'ASH', path: '/ash' },
  { label: 'Queries', path: '/queries' },
  { label: 'Locks', path: '/locks' },
] as const

function instancePath(instanceId: string, suffix: string): string {
  return `/instances/${encodeURIComponent(instanceId)}${suffix}`
}

function roleLabel(role: string): string {
  return role || 'unknown'
}

export function InstanceHeader({ dataUpdatedAt, instance, replayLagSeconds }: InstanceHeaderProps) {
  const role = roleLabel(instance.role)
  const lastSeen = formatTimestamp(instance.last_seen, 'UTC', 'en-GB')
  const replayLag =
    replayLagSeconds == null ? (
      <Unknown reason="Replay lag was not measured." />
    ) : (
      formatLag(replayLagSeconds)
    )

  return (
    <div className="space-y-4">
      <PageHeader
        title={`${instance.addr}:${instance.port}`}
        subtitle={
          <span>
            Instance <code className="font-mono text-xs">{instance.instance_id}</code>
          </span>
        }
        freshness={<FreshnessBadge dataUpdatedAt={dataUpdatedAt} policy="instance" />}
      />

      <dl className="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
        <div>
          <dt className="text-text-secondary">Role</dt>
          <dd>
            <Badge
              variant={
                role === 'primary' ? 'secondary' : role === 'standby' ? 'outline' : 'destructive'
              }
            >
              {role}
            </Badge>
          </dd>
        </div>
        <div>
          <dt className="text-text-secondary">PostgreSQL</dt>
          <dd>{formatPostgresVersion(instance.pg_version)}</dd>
        </div>
        <div>
          <dt className="text-text-secondary">Permission tier</dt>
          <dd>{instance.perm_tier}</dd>
        </div>
        <div>
          <dt className="text-text-secondary">Last seen</dt>
          <dd>{lastSeen}</dd>
        </div>
        <div>
          <dt className="text-text-secondary">State</dt>
          <dd>
            <Badge variant={instance.up ? 'secondary' : 'destructive'}>
              {instance.up ? 'up' : 'down'}
            </Badge>
          </dd>
        </div>
        <div>
          <dt className="text-text-secondary">Owning cluster</dt>
          <dd>
            <Link
              className="underline underline-offset-2"
              to={`/clusters/${encodeURIComponent(instance.cluster_id)}`}
            >
              <code className="font-mono text-xs">{instance.cluster_id}</code>
            </Link>
          </dd>
        </div>
      </dl>

      {instance.role === 'standby' ? (
        <div role="status" className="border-warning/40 bg-warning/10 text-sm">
          <strong>standby (read-only)</strong>
          <span className="ml-2">Replay lag: {replayLag}</span>
        </div>
      ) : null}

      {!instance.up ? (
        <Degraded reason="Instance is down and its latest values may be stale." />
      ) : null}

      <nav aria-label="Instance views">
        <ul className="flex flex-wrap gap-1 border-b">
          {tabs.map((tab) => (
            <li key={tab.label}>
              <NavLink
                end={tab.path === ''}
                to={instancePath(instance.instance_id, tab.path)}
                className={({ isActive }) =>
                  `inline-flex border-b-2 px-3 py-2 text-sm ${
                    isActive
                      ? 'border-primary text-foreground font-medium'
                      : 'text-muted-foreground hover:text-foreground border-transparent'
                  }`
                }
              >
                {tab.label}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>
    </div>
  )
}
