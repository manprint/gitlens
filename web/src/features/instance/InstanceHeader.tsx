import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { Gauge, Server } from 'lucide-react'

import type { Schemas } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { Degraded, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { useT, type Translate } from '@/i18n/react'
import {
  formatLag,
  formatPostgresVersion,
  formatRecentTimestamp,
  formatTimestamp,
} from '@/lib/format'
import { useCachedClusters } from '@/api/queries'
import {
  clusterNameOrId,
  hasDisplayName,
  instanceAddress,
  instanceLabel,
  nameLookup,
} from '@/lib/names'

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

function roleLabel(role: string, t: Translate): string {
  if (role === 'primary') return t('instance.header.role.primary')
  if (role === 'standby') return t('instance.header.role.standby')
  return role || t('instance.header.role.unknown')
}

function replayLagText(lag: ReplayLag, t: Translate) {
  if (lag.state === 'loading') return t('instance.header.replayLoading')
  if (lag.state === 'unavailable') return t('instance.header.replayUnavailable')
  return lag.seconds === null ? (
    <Unknown reason={t('instance.header.replayNotMeasured')} />
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
  const t = useT()
  const role = roleLabel(instance.role, t)
  const lastSeen = formatTimestamp(instance.last_seen, undefined, 'en-GB')
  // Today's date is implied: the time alone leaves the Full HD line room for the cluster.
  const lastSeenShort = formatRecentTimestamp(instance.last_seen, new Date(), undefined, 'en-GB')
  const address = instanceAddress(instance)
  // The owning cluster's name, from the list the shell polls; its id until that arrives.
  const clusters = useCachedClusters()
  const owningCluster = clusterNameOrId(nameLookup(clusters), instance.cluster_id)

  return (
    // From 1600 px (D26) the header takes two lines: the title, then the section
    // tabs beside the metrics. The views of the instance are in the sidebar.
    <div
      data-testid="instance-header"
      className="space-y-5 min-[1600px]:grid min-[1600px]:grid-cols-[auto_minmax(0,1fr)_auto] min-[1600px]:items-center min-[1600px]:space-y-0 min-[1600px]:gap-x-4 min-[1600px]:gap-y-2"
    >
      <div className="min-[1600px]:col-span-3 min-[1600px]:col-start-1 min-[1600px]:row-start-1 min-[1600px]:min-w-0">
        <PageHeader
          help={<HelpLink anchor="instance-overview" label={t('instance.header.pages')} />}
          eyebrow={t('instance.header.eyebrow')}
          icon={Server}
          // A display name is the title and the address moves to the subtitle (D15);
          // from 1600 px the name is cut to one line, so the header keeps its height.
          title={instanceLabel(instance)}
          truncateTitle
          subtitle={
            // From 1600 px the subtitle is one clipped line: it never wraps the header.
            <span className="inline-flex flex-wrap items-center gap-2 min-[1600px]:flex-nowrap">
              <Badge variant={instance.up ? 'success' : 'destructive'}>
                <span
                  aria-hidden="true"
                  className={`size-1.5 rounded-full ${instance.up ? 'bg-success' : 'bg-current'}`}
                />
                {instance.up ? t('instance.header.up') : t('instance.header.down')}
              </Badge>
              {hasDisplayName(instance) ? (
                <span data-testid="instance-address" title={address}>
                  <code className="font-mono text-xs">{address}</code>
                </span>
              ) : null}
              {/* The id is for support and the API only: a copy button, the id in its tooltip. */}
              <span
                data-testid="instance-id"
                title={t('instance.header.idTitle', { id: instance.instance_id })}
              >
                <CopyButton
                  label="ID"
                  subject={t('instance.header.idSubject')}
                  text={instance.instance_id}
                />
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
              {t('instance.header.slowest')}
            </Link>
          }
        />
      </div>

      <dl className="bg-card shadow-card grid divide-y rounded-xl border text-sm min-[1600px]:col-span-2 min-[1600px]:col-start-2 min-[1600px]:row-start-2 min-[1600px]:flex min-[1600px]:max-w-full min-[1600px]:min-w-0 min-[1600px]:justify-self-end min-[1600px]:overflow-hidden sm:grid-cols-2 sm:divide-y-0 lg:grid-cols-5 lg:divide-x">
        <div className="px-4 py-3 min-[1600px]:flex min-[1600px]:shrink-0 min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:px-3 min-[1600px]:py-1.5">
          <dt className="text-muted-foreground text-xs font-medium uppercase min-[1600px]:shrink-0 min-[1600px]:text-[11px]">
            {t('instance.header.label.role')}
          </dt>
          <dd className="mt-1 min-[1600px]:mt-0">
            <Badge
              variant={
                instance.role === 'primary'
                  ? 'secondary'
                  : instance.role === 'standby'
                    ? 'outline'
                    : 'destructive'
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
            {t('instance.header.label.tier')}
          </dt>
          <dd className="mt-1 font-medium min-[1600px]:mt-0">{instance.perm_tier}</dd>
        </div>
        <div className="px-4 py-3 min-[1600px]:flex min-[1600px]:shrink-0 min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:px-3 min-[1600px]:py-1.5">
          <dt className="text-muted-foreground text-xs font-medium uppercase min-[1600px]:shrink-0 min-[1600px]:text-[11px]">
            {t('instance.header.label.lastSeen')}
          </dt>
          <dd className="mt-1 font-medium tabular-nums min-[1600px]:mt-0 min-[1600px]:whitespace-nowrap">
            <time dateTime={instance.last_seen} title={lastSeen}>
              {lastSeenShort}
            </time>
          </dd>
        </div>
        {/* The cluster label is the one value that gives way on a narrow Full HD line. */}
        <div className="min-w-0 px-4 py-3 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:px-3 min-[1600px]:py-1.5">
          <dt className="text-muted-foreground text-xs font-medium uppercase min-[1600px]:shrink-0 min-[1600px]:text-[11px]">
            {t('instance.header.label.cluster')}
          </dt>
          <dd className="mt-1 truncate min-[1600px]:mt-0 min-[1600px]:min-w-0">
            <Link
              className="text-primary hover:underline"
              title={instance.cluster_id}
              to={`/clusters/${encodeURIComponent(instance.cluster_id)}`}
            >
              {owningCluster === instance.cluster_id ? (
                <code className="font-mono text-xs">{instance.cluster_id}</code>
              ) : (
                owningCluster
              )}
            </Link>
          </dd>
        </div>
      </dl>

      {instance.role === 'standby' ? (
        <div
          role="status"
          className="border-warning/40 bg-warning/10 rounded-lg border px-4 py-2.5 text-sm min-[1600px]:col-span-3 min-[1600px]:row-start-3 min-[1600px]:py-1"
        >
          <strong>{t('instance.header.standbyReadOnly')}</strong>
          <span className="ml-2">
            {t('instance.header.replayLagLabel')} {replayLagText(replayLag, t)}
          </span>
        </div>
      ) : null}

      {!instance.up ? (
        <div className="min-[1600px]:col-span-3 min-[1600px]:row-start-4">
          <Degraded reason={t('instance.header.downNotice')} />
        </div>
      ) : null}

      {sectionTabs ? (
        <div
          data-testid="instance-section-tabs"
          className="min-w-0 min-[1600px]:col-start-1 min-[1600px]:row-start-2"
        >
          {sectionTabs}
        </div>
      ) : null}
    </div>
  )
}
