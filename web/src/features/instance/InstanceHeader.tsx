import { Link, NavLink } from 'react-router-dom'
import { Activity, Gauge, Lock, Server, Zap } from 'lucide-react'

import type { Schemas } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Degraded, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { formatLag, formatPostgresVersion, formatTimestamp } from '@/lib/format'

type Instance = Schemas['Instance']

/** What the header can say about a standby's replay lag, and why when it cannot say a number. */
export type ReplayLag =
  { state: 'loading' } | { state: 'unavailable' } | { state: 'reading'; seconds: number | null }

interface InstanceHeaderProps {
  dataUpdatedAt: number
  instance: Instance
  replayLag: ReplayLag
}

const tabs = [
  { icon: Server, label: 'Overview', path: '' },
  { icon: Zap, label: 'Query performance', path: '/queries' },
  { icon: Activity, label: 'Wait events', path: '/ash' },
  { icon: Lock, label: 'Locks and activity', path: '/locks' },
] as const

function instancePath(instanceId: string, suffix: string): string {
  return `/instances/${encodeURIComponent(instanceId)}${suffix}`
}

function roleLabel(role: string): string {
  return role || 'unknown'
}

function replayLagText(lag: ReplayLag) {
  if (lag.state === 'loading') return 'loading'
  if (lag.state === 'unavailable') return 'unavailable'
  return lag.seconds === null ? (
    <Unknown reason="Replay lag was not measured." />
  ) : (
    formatLag(lag.seconds)
  )
}

export function InstanceHeader({ dataUpdatedAt, instance, replayLag }: InstanceHeaderProps) {
  const role = roleLabel(instance.role)
  const lastSeen = formatTimestamp(instance.last_seen, 'UTC', 'en-GB')

  return (
    <div className="space-y-5">
      <PageHeader
        eyebrow="Server"
        icon={Server}
        title={`${instance.addr}:${instance.port}`}
        subtitle={
          <span className="inline-flex flex-wrap items-center gap-2">
            <Badge variant={instance.up ? 'success' : 'destructive'}>
              <span
                aria-hidden="true"
                className={`size-1.5 rounded-full ${instance.up ? 'bg-success' : 'bg-current'}`}
              />
              {instance.up ? 'up' : 'down'}
            </Badge>
            <span>
              Instance <code className="font-mono text-xs">{instance.instance_id}</code>
            </span>
          </span>
        }
        freshness={<FreshnessBadge dataUpdatedAt={dataUpdatedAt} policy="instance" />}
        actions={
          <Link
            className="bg-primary text-primary-foreground hover:bg-primary/90 inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-sm font-medium shadow-sm transition-colors"
            to={`/queries?instance=${encodeURIComponent(instance.instance_id)}`}
          >
            <Gauge aria-hidden="true" className="size-4" />
            Slowest queries
          </Link>
        }
      />

      <dl className="bg-card shadow-card grid divide-y rounded-xl border text-sm sm:grid-cols-2 sm:divide-y-0 lg:grid-cols-5 lg:divide-x">
        <div className="px-4 py-3">
          <dt className="text-muted-foreground text-xs font-medium uppercase">Role</dt>
          <dd className="mt-1">
            <Badge
              variant={
                role === 'primary' ? 'secondary' : role === 'standby' ? 'outline' : 'destructive'
              }
            >
              {role}
            </Badge>
          </dd>
        </div>
        <div className="px-4 py-3">
          <dt className="text-muted-foreground text-xs font-medium uppercase">PostgreSQL</dt>
          <dd className="mt-1 font-medium">{formatPostgresVersion(instance.pg_version)}</dd>
        </div>
        <div className="px-4 py-3">
          <dt className="text-muted-foreground text-xs font-medium uppercase">Permission tier</dt>
          <dd className="mt-1 font-medium">{instance.perm_tier}</dd>
        </div>
        <div className="px-4 py-3">
          <dt className="text-muted-foreground text-xs font-medium uppercase">Last seen</dt>
          <dd className="mt-1 font-medium tabular-nums">{lastSeen}</dd>
        </div>
        <div className="min-w-0 px-4 py-3">
          <dt className="text-muted-foreground text-xs font-medium uppercase">Owning cluster</dt>
          <dd className="mt-1 truncate">
            <Link
              className="text-primary hover:underline"
              to={`/clusters/${encodeURIComponent(instance.cluster_id)}`}
            >
              <code className="font-mono text-xs">{instance.cluster_id}</code>
            </Link>
          </dd>
        </div>
      </dl>

      {instance.role === 'standby' ? (
        <div
          role="status"
          className="border-warning/40 bg-warning/10 rounded-lg border px-4 py-2.5 text-sm"
        >
          <strong>standby (read-only)</strong>
          <span className="ml-2">Replay lag: {replayLagText(replayLag)}</span>
        </div>
      ) : null}

      {!instance.up ? (
        <Degraded reason="Instance is down and its latest values may be stale." />
      ) : null}

      <nav aria-label="Instance views">
        <ul className="flex flex-wrap gap-1 border-b">
          {tabs.map(({ icon: Icon, label, path }) => (
            <li key={label}>
              <NavLink
                end={path === ''}
                to={instancePath(instance.instance_id, path)}
                className={({ isActive }) =>
                  `-mb-px inline-flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                    isActive
                      ? 'border-primary text-foreground'
                      : 'text-muted-foreground hover:text-foreground hover:border-border border-transparent'
                  }`
                }
              >
                <Icon aria-hidden="true" className="size-4" />
                {label}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>
    </div>
  )
}
