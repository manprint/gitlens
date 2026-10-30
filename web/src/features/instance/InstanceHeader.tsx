import type { ReactNode } from 'react'
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
  /** The page's section tab list, placed beside the metrics from 1600 px. */
  sectionTabs?: ReactNode
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

export function InstanceHeader({
  dataUpdatedAt,
  instance,
  replayLag,
  sectionTabs,
}: InstanceHeaderProps) {
  const role = roleLabel(instance.role)
  const lastSeen = formatTimestamp(instance.last_seen, 'UTC', 'en-GB')

  return (
    // From 1600 px (D26) the header takes two lines: the title beside the instance
    // views, then the section tabs beside the metrics.
    <div
      data-testid="instance-header"
      className="space-y-5 min-[1600px]:grid min-[1600px]:grid-cols-[auto_minmax(0,1fr)_auto] min-[1600px]:items-center min-[1600px]:space-y-0 min-[1600px]:gap-x-4 min-[1600px]:gap-y-2"
    >
      <div className="min-[1600px]:col-span-2 min-[1600px]:col-start-1 min-[1600px]:row-start-1 min-[1600px]:min-w-0">
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
      </div>

      <dl className="bg-card shadow-card grid divide-y rounded-xl border text-sm min-[1600px]:col-span-2 min-[1600px]:col-start-2 min-[1600px]:row-start-2 min-[1600px]:flex min-[1600px]:max-w-full min-[1600px]:min-w-0 min-[1600px]:justify-self-end min-[1600px]:overflow-hidden sm:grid-cols-2 sm:divide-y-0 lg:grid-cols-5 lg:divide-x">
        <div className="px-4 py-3 min-[1600px]:flex min-[1600px]:shrink-0 min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:px-3 min-[1600px]:py-1.5">
          <dt className="text-muted-foreground text-xs font-medium uppercase min-[1600px]:shrink-0 min-[1600px]:text-[11px]">
            Role
          </dt>
          <dd className="mt-1 min-[1600px]:mt-0">
            <Badge
              variant={
                role === 'primary' ? 'secondary' : role === 'standby' ? 'outline' : 'destructive'
              }
            >
              {role}
            </Badge>
          </dd>
        </div>
        <div className="px-4 py-3 min-[1600px]:flex min-[1600px]:shrink-0 min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:px-3 min-[1600px]:py-1.5">
          <dt className="text-muted-foreground text-xs font-medium uppercase min-[1600px]:shrink-0 min-[1600px]:text-[11px]">
            PostgreSQL
          </dt>
          <dd className="mt-1 font-medium min-[1600px]:mt-0">
            {formatPostgresVersion(instance.pg_version)}
          </dd>
        </div>
        <div className="px-4 py-3 min-[1600px]:flex min-[1600px]:shrink-0 min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:px-3 min-[1600px]:py-1.5">
          <dt className="text-muted-foreground text-xs font-medium uppercase min-[1600px]:shrink-0 min-[1600px]:text-[11px]">
            Permission tier
          </dt>
          <dd className="mt-1 font-medium min-[1600px]:mt-0">{instance.perm_tier}</dd>
        </div>
        <div className="px-4 py-3 min-[1600px]:flex min-[1600px]:shrink-0 min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:px-3 min-[1600px]:py-1.5">
          <dt className="text-muted-foreground text-xs font-medium uppercase min-[1600px]:shrink-0 min-[1600px]:text-[11px]">
            Last seen
          </dt>
          <dd className="mt-1 font-medium tabular-nums min-[1600px]:mt-0 min-[1600px]:whitespace-nowrap">
            {lastSeen}
          </dd>
        </div>
        {/* The cluster id is the one value that gives way on a narrow Full HD line. */}
        <div className="min-w-0 px-4 py-3 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:px-3 min-[1600px]:py-1.5">
          <dt className="text-muted-foreground text-xs font-medium uppercase min-[1600px]:shrink-0 min-[1600px]:text-[11px]">
            Owning cluster
          </dt>
          <dd className="mt-1 truncate min-[1600px]:mt-0 min-[1600px]:min-w-0">
            <Link
              className="text-primary hover:underline"
              title={instance.cluster_id}
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
          className="border-warning/40 bg-warning/10 rounded-lg border px-4 py-2.5 text-sm min-[1600px]:col-span-3 min-[1600px]:row-start-3 min-[1600px]:py-1"
        >
          <strong>standby (read-only)</strong>
          <span className="ml-2">Replay lag: {replayLagText(replayLag)}</span>
        </div>
      ) : null}

      {!instance.up ? (
        <div className="min-[1600px]:col-span-3 min-[1600px]:row-start-4">
          <Degraded reason="Instance is down and its latest values may be stale." />
        </div>
      ) : null}

      <nav
        aria-label="Instance views"
        className="min-[1600px]:col-start-3 min-[1600px]:row-start-1 min-[1600px]:justify-self-end"
      >
        <ul className="flex flex-wrap gap-1 border-b min-[1600px]:flex-nowrap min-[1600px]:border-b-0">
          {tabs.map(({ icon: Icon, label, path }) => (
            <li key={label}>
              <NavLink
                end={path === ''}
                to={instancePath(instance.instance_id, path)}
                className={({ isActive }) =>
                  `-mb-px inline-flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors min-[1600px]:mb-0 min-[1600px]:gap-1.5 min-[1600px]:px-2 min-[1600px]:py-1.5 min-[1600px]:whitespace-nowrap ${
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

      {sectionTabs ? (
        // Below 1600 px the tabs keep their 32 px gap under the instance views
        // (the margin collapses with the 20 px of the stack).
        <div
          data-testid="instance-section-tabs"
          className="mt-8 min-w-0 min-[1600px]:col-start-1 min-[1600px]:row-start-2 min-[1600px]:mt-0"
        >
          {sectionTabs}
        </div>
      ) : null}
    </div>
  )
}
