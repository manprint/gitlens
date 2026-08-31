import { Link } from 'react-router-dom'

import { formatDuration } from '@/lib/format'
import type { AgentHealthIssue } from '@/lib/fleet'

interface AgentHealthStripProps {
  issues: readonly AgentHealthIssue[]
}

const troubleshootingHref = '/README.md#troubleshooting'

export function AgentHealthStrip({ issues }: AgentHealthStripProps) {
  if (issues.length === 0) return null

  return (
    <section
      aria-labelledby="agent-health-title"
      className="border-destructive bg-destructive/10 space-y-4 rounded-xl border-2 p-5"
    >
      <div>
        <p className="text-destructive text-xs font-semibold tracking-wide uppercase">
          Agent health
        </p>
        <h2 id="agent-health-title" className="mt-1 text-xl font-semibold">
          Agents requiring attention
        </h2>
        <p className="text-muted-foreground mt-1 text-sm">
          These instances are not reporting reliably. Values for their clusters may be stale.
        </p>
      </div>

      <ul className="grid gap-3 lg:grid-cols-2">
        {issues.map((issue) => {
          const clusterLabel = issue.cluster.name ?? issue.cluster.cluster_id
          const instanceLabel = `${issue.instance.addr} (${clusterLabel})`

          return (
            <li
              key={`${issue.cluster.cluster_id}:${issue.instance.instance_id}`}
              className="bg-background/70 rounded-lg border p-4"
            >
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <Link
                  to={`/instances/${encodeURIComponent(issue.instance.instance_id)}`}
                  aria-label={`Open instance ${instanceLabel}`}
                  className="font-semibold underline underline-offset-2"
                >
                  {issue.instance.addr}
                </Link>
                <span className="text-muted-foreground text-xs">{clusterLabel}</span>
              </div>
              <p className="mt-2 text-sm">
                <span className="font-medium">Likely cause:</span> {issue.reason}
              </p>
              <p className="text-muted-foreground mt-1 text-xs">
                Last seen: {formatDuration(issue.ageSeconds)} ago
              </p>
              <a
                href={troubleshootingHref}
                className="text-destructive mt-3 inline-block text-sm font-medium underline underline-offset-2"
              >
                View agent troubleshooting
              </a>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
