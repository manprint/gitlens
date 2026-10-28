import { Link } from 'react-router-dom'

import type { Cluster } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Stale, Unknown } from '@/components/state'
import { formatLag } from '@/lib/format'
import { AGENT_STALE_AFTER_SECONDS, describeHealth } from '@/lib/fleet'

interface ClusterCardProps {
  agentStaleAgeSeconds?: number
  // null means the firing-alert feed is unavailable. The card must not show
  // zero alerts then, because that would read as a clean cluster.
  alertCount?: number | null
  cluster: Cluster
}

function healthVariant(health: Cluster['health']): 'success' | 'warning' | 'destructive' {
  if (health === 'critical') return 'destructive'
  if (health === 'degraded') return 'warning'
  return 'success'
}

const HEALTH_ACCENT: Record<Cluster['health'], string> = {
  critical: 'before:bg-status-critical',
  degraded: 'before:bg-status-degraded',
  ok: 'before:bg-status-ok',
}

export function ClusterCard({ agentStaleAgeSeconds, alertCount = 0, cluster }: ClusterCardProps) {
  const explanation = describeHealth(cluster)
  const primary = cluster.primary
    ? cluster.instances.find((instance) => instance.instance_id === cluster.primary)
    : undefined
  const permissionTiers = [
    ...new Set(cluster.instances.map((instance) => instance.perm_tier)),
  ].join(', ')
  const lagUnknown = cluster.max_replay_lag_seconds == null
  const healthBadge = (
    <Badge
      role="status"
      variant={healthVariant(cluster.health)}
      aria-label={`Health: ${cluster.health}. ${explanation}`}
    >
      {cluster.health}
    </Badge>
  )
  const lagValue = lagUnknown ? (
    <Unknown reason="No standby has reported replay lag." />
  ) : (
    formatLag(cluster.max_replay_lag_seconds ?? null)
  )

  return (
    <div className="space-y-2" role="listitem">
      <Card
        className={`has-[a:focus-visible]:ring-ring hover:border-primary/40 hover:shadow-raised relative h-full gap-4 overflow-hidden py-5 shadow-[var(--shadow-card)] transition-all before:absolute before:inset-y-0 before:left-0 before:w-1 has-[a:focus-visible]:ring-2 ${HEALTH_ACCENT[cluster.health]}`}
      >
        <CardHeader className="px-5">
          <div className="flex items-start justify-between gap-4">
            <div>
              <CardTitle className="text-base">
                {/* The link stretches over the whole card, so every part of it opens the cluster. */}
                <Link
                  to={`/clusters/${encodeURIComponent(cluster.cluster_id)}`}
                  aria-label={`${cluster.name ?? 'Unnamed cluster'} (${cluster.cluster_id})`}
                  className="after:absolute after:inset-0 after:rounded-xl focus-visible:outline-none"
                >
                  {cluster.name ?? 'Unnamed cluster'}
                </Link>
              </CardTitle>
              <CardDescription>
                <code className="font-mono text-xs">{cluster.cluster_id}</code>
              </CardDescription>
            </div>
            {agentStaleAgeSeconds === undefined ? (
              healthBadge
            ) : (
              <Stale age={agentStaleAgeSeconds} threshold={AGENT_STALE_AFTER_SECONDS}>
                {healthBadge}
              </Stale>
            )}
          </div>
        </CardHeader>
        <CardContent className="grid gap-3 px-5 text-sm">
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 [&_dd]:text-right [&_dd]:font-medium [&_dd]:tabular-nums">
            <dt className="text-muted-foreground">Primary</dt>
            <dd>{primary?.addr ?? <Unknown reason="No primary address is available." />}</dd>
            <dt className="text-muted-foreground">Instances</dt>
            <dd>{cluster.instance_count}</dd>
            <dt className="text-muted-foreground">Standbys</dt>
            <dd>{cluster.standby_count ?? 0}</dd>
            <dt className="text-muted-foreground">Max replay lag</dt>
            <dd>
              {agentStaleAgeSeconds === undefined ? (
                lagValue
              ) : (
                <Stale age={agentStaleAgeSeconds} threshold={AGENT_STALE_AFTER_SECONDS}>
                  {lagValue}
                </Stale>
              )}
            </dd>
            <dt className="text-muted-foreground">Permission tiers</dt>
            <dd>{permissionTiers || <Unknown reason="No permission tier has been reported." />}</dd>
          </dl>
          {cluster.id_source !== 'system_identifier' ? (
            <p className="text-warning text-xs">
              Identity source: <code>{cluster.id_source}</code>. Grant instructions are linked
              below.
            </p>
          ) : null}
          {alertCount === null ? (
            <Badge variant="outline">Alerts unavailable</Badge>
          ) : alertCount > 0 ? (
            <Badge variant="destructive" aria-label={`${alertCount} firing alerts`}>
              {alertCount} firing alert{alertCount === 1 ? '' : 's'}
            </Badge>
          ) : null}
        </CardContent>
      </Card>
      {cluster.id_source !== 'system_identifier' ? (
        <p className="text-warning text-xs">
          <a href="/README.md#setting-up-the-monitoring-role">
            View monitoring role grant instructions
          </a>
        </p>
      ) : null}
    </div>
  )
}
