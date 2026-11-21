import { Link } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'

import type { Cluster } from '@/api/types'
import { InfoHint } from '@/components/layout/InfoHint'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Stale, Unknown } from '@/components/state'
import { useT } from '@/i18n/react'
import { formatLag } from '@/lib/format'
import { AGENT_STALE_AFTER_SECONDS, describeHealth } from '@/lib/fleet'
import { clusterLabel, instanceAddress, instanceLabel } from '@/lib/names'
import { ReadinessBadge } from '@/features/readiness/ReadinessBadge'
import { readinessFixPath, type ClusterReadiness } from '@/lib/readiness'

interface ClusterCardProps {
  agentStaleAgeSeconds?: number
  // null means the firing-alert feed is unavailable. The card must not show
  // zero alerts then, because that would read as a clean cluster.
  alertCount?: number | null
  cluster: Cluster
  // The cluster's monitoring readiness; absent means unknown, so no badge.
  readiness?: ClusterReadiness | null
}

function healthVariant(health: Cluster['health']): 'success' | 'warning' | 'destructive' {
  if (health === 'critical') return 'destructive'
  if (health === 'degraded') return 'warning'
  return 'success'
}

const HEALTH_ACCENT: Record<Cluster['health'], string> = {
  critical: 'before:bg-destructive',
  degraded: 'before:bg-warning',
  ok: 'before:bg-success',
}

const HEALTH_KEY = {
  critical: 'fleet.health.critical',
  degraded: 'fleet.health.degraded',
  ok: 'fleet.health.ok',
} as const

export function ClusterCard({
  agentStaleAgeSeconds,
  alertCount = 0,
  cluster,
  readiness = null,
}: ClusterCardProps) {
  const t = useT()
  const explanation = describeHealth(cluster)
  const label = clusterLabel(cluster, t('fleet.card.unnamed'))
  const primary = cluster.primary
    ? cluster.instances.find((instance) => instance.instance_id === cluster.primary)
    : undefined
  const permissionTiers = [
    ...new Set(cluster.instances.map((instance) => instance.perm_tier)),
  ].join(', ')
  const lagUnknown = cluster.max_replay_lag_seconds == null
  const fixInstanceId = readiness?.level === 'partial' ? (readiness.fixInstanceId ?? null) : null
  const fixInstance = cluster.instances.find((instance) => instance.instance_id === fixInstanceId)
  const fixTarget = fixInstance ? instanceLabel(fixInstance) : fixInstanceId
  const healthBadge = (
    <Badge
      role="status"
      variant={healthVariant(cluster.health)}
      aria-label={t('fleet.card.healthAria', {
        health: t(HEALTH_KEY[cluster.health]),
        explanation,
      })}
    >
      {t(HEALTH_KEY[cluster.health])}
    </Badge>
  )
  const lagValue = lagUnknown ? (
    <Unknown reason={t('fleet.card.noLag')} />
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
            {/* A long name stays on one line; the whole name is the link's title. */}
            <div className="min-w-0">
              <CardTitle className="truncate text-base">
                {/* The link stretches over the whole card, so every part of it opens the cluster. */}
                <Link
                  to={`/clusters/${encodeURIComponent(cluster.cluster_id)}`}
                  aria-label={`${label} (${cluster.cluster_id})`}
                  className="after:absolute after:inset-0 after:rounded-xl focus-visible:outline-none"
                  title={label}
                >
                  {label}
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
            <dt className="text-muted-foreground">{t('fleet.card.primary')}</dt>
            <dd>
              {primary ? (
                <span title={instanceAddress(primary)}>{instanceLabel(primary)}</span>
              ) : (
                <Unknown reason={t('fleet.card.noPrimary')} />
              )}
            </dd>
            <dt className="text-muted-foreground">{t('fleet.card.instances')}</dt>
            <dd>{cluster.instance_count}</dd>
            <dt className="text-muted-foreground">{t('fleet.card.standbys')}</dt>
            <dd>{cluster.standby_count ?? 0}</dd>
            <dt className="text-muted-foreground">{t('fleet.card.maxReplayLag')}</dt>
            <dd>
              {agentStaleAgeSeconds === undefined ? (
                lagValue
              ) : (
                <Stale age={agentStaleAgeSeconds} threshold={AGENT_STALE_AFTER_SECONDS}>
                  {lagValue}
                </Stale>
              )}
            </dd>
            <dt className="text-muted-foreground flex items-center gap-1">
              {t('fleet.card.permissionTiers')}
              <InfoHint
                className="relative z-10 shrink-0"
                id="fleet.permission-tiers"
                label={t('fleet.card.permissionTiers')}
              />
            </dt>
            <dd>{permissionTiers || <Unknown reason={t('fleet.card.noTiers')} />}</dd>
          </dl>
          {cluster.id_source !== 'system_identifier' ? (
            <p className="text-warning-foreground text-xs">
              {t('fleet.card.identityBefore')}
              <code>{cluster.id_source}</code>
              {t('fleet.card.identityAfter')}
            </p>
          ) : null}
          {alertCount === null ? (
            <Badge variant="outline">{t('fleet.card.alertsUnavailable')}</Badge>
          ) : alertCount > 0 ? (
            <Badge
              variant="destructive"
              aria-label={t('fleet.card.firingAlertsAria', { count: alertCount })}
            >
              {t('fleet.card.firingAlerts', { count: alertCount })}
            </Badge>
          ) : null}
          {readiness === null ? null : (
            <div className="flex items-center gap-1">
              {fixInstanceId === null ? (
                <ReadinessBadge
                  infos={readiness.infos}
                  level={readiness.level}
                  warnings={readiness.warnings}
                />
              ) : (
                // Above the card's stretched link: the warning opens the tab that fixes it.
                <Link
                  aria-label={t('fleet.card.fixAria', {
                    count: readiness.warnings,
                    target: fixTarget ?? '',
                  })}
                  className="focus-visible:ring-ring relative z-10 inline-flex items-center gap-1 rounded-full text-xs font-medium hover:underline focus-visible:ring-2 focus-visible:outline-none"
                  data-testid="readiness-fix-link"
                  title={t('fleet.card.fixTitle', { target: fixTarget ?? '' })}
                  to={readinessFixPath(fixInstanceId)}
                >
                  <ReadinessBadge
                    infos={readiness.infos}
                    level={readiness.level}
                    warnings={readiness.warnings}
                  />
                  {t('fleet.card.fix')}
                  <ChevronRight aria-hidden="true" className="size-3.5" />
                </Link>
              )}
              <InfoHint
                className="relative z-10 shrink-0"
                id="fleet.readiness"
                label={t('fleet.card.readiness')}
              />
            </div>
          )}
        </CardContent>
      </Card>
      {cluster.id_source !== 'system_identifier' ? (
        <p className="text-warning-foreground text-xs">
          <a href="/README.md#setting-up-the-monitoring-role">{t('fleet.card.roleGrant')}</a>
        </p>
      ) : null}
    </div>
  )
}
