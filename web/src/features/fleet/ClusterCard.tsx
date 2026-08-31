import { Link } from 'react-router-dom'

import type { Cluster } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Unknown } from '@/components/state'
import { formatLag } from '@/lib/format'
import { describeHealth } from '@/lib/fleet'

interface ClusterCardProps {
  alertCount?: number
  cluster: Cluster
}

function healthVariant(health: Cluster['health']): 'secondary' | 'outline' | 'destructive' {
  if (health === 'critical') return 'destructive'
  if (health === 'degraded') return 'outline'
  return 'secondary'
}

export function ClusterCard({ alertCount = 0, cluster }: ClusterCardProps) {
  const explanation = describeHealth(cluster)
  const primary = cluster.primary
    ? cluster.instances.find((instance) => instance.instance_id === cluster.primary)
    : undefined
  const permissionTiers = [
    ...new Set(cluster.instances.map((instance) => instance.perm_tier)),
  ].join(', ')
  const lagUnknown = cluster.max_replay_lag_seconds == null

  return (
    <div className="space-y-2">
      <Link
        to={`/clusters/${encodeURIComponent(cluster.cluster_id)}`}
        aria-label={`${cluster.name ?? 'Unnamed cluster'} (${cluster.cluster_id})`}
        className="focus-visible:ring-ring block rounded-xl focus-visible:ring-2 focus-visible:outline-none"
      >
        <Card className="hover:border-primary/60 h-full transition-colors">
          <CardHeader>
            <div className="flex items-start justify-between gap-4">
              <div>
                <CardTitle>{cluster.name ?? 'Unnamed cluster'}</CardTitle>
                <CardDescription>
                  <code className="font-mono text-xs">{cluster.cluster_id}</code>
                </CardDescription>
              </div>
              <Badge
                role="status"
                variant={healthVariant(cluster.health)}
                aria-label={`Health: ${cluster.health}. ${explanation}`}
              >
                {cluster.health}
              </Badge>
            </div>
          </CardHeader>
          <CardContent className="grid gap-3 text-sm">
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
              <dt className="text-text-secondary">Primary</dt>
              <dd>{primary?.addr ?? <Unknown reason="No primary address is available." />}</dd>
              <dt className="text-text-secondary">Instances</dt>
              <dd>{cluster.instance_count}</dd>
              <dt className="text-text-secondary">Standbys</dt>
              <dd>{cluster.standby_count ?? 0}</dd>
              <dt className="text-text-secondary">Max replay lag</dt>
              <dd>
                {lagUnknown ? (
                  <Unknown reason="No standby has reported replay lag." />
                ) : (
                  formatLag(cluster.max_replay_lag_seconds ?? null)
                )}
              </dd>
              <dt className="text-text-secondary">Permission tiers</dt>
              <dd>
                {permissionTiers || <Unknown reason="No permission tier has been reported." />}
              </dd>
            </dl>
            {cluster.id_source !== 'system_identifier' ? (
              <p className="text-warning text-xs">
                Identity source: <code>{cluster.id_source}</code>. Grant instructions are linked
                below.
              </p>
            ) : null}
            {alertCount > 0 ? (
              <Badge variant="destructive" aria-label={`${alertCount} firing alerts`}>
                {alertCount} firing alert{alertCount === 1 ? '' : 's'}
              </Badge>
            ) : null}
          </CardContent>
        </Card>
      </Link>
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
