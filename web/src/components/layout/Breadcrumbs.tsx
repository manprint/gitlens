import { Link, useLocation } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import { useMemo } from 'react'

import { useCachedClusters } from '@/api/queries'
import { clusterLabel, instanceLabel, nameLookup, type NameLookup } from '@/lib/names'

const segmentLabels: Record<string, string> = {
  alerts: 'Alerts',
  ash: 'ASH',
  clusters: 'Clusters',
  events: 'Events',
  findings: 'Findings',
  indexes: 'Index analysis',
  instances: 'Instances',
  locks: 'Locks',
  queries: 'Queries',
  rules: 'Rules',
  settings: 'Settings',
  silences: 'Silences',
}

// A mangled or hand-typed link can carry a malformed escape such as %E0. The
// breadcrumb then shows the segment as written, so the header never throws
// and takes the whole shell down with it.
function decodeSegment(segment: string): string {
  try {
    return decodeURIComponent(segment)
  } catch {
    return segment
  }
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const LONG_NUMBER = /^-?\d{13,}$/

// A full instance UUID or query id fills the header; its first characters are
// enough to tell entries apart in the trail, and the title keeps the whole value.
function labelForSegment(segment: string): string {
  const label = segmentLabels[segment] ?? decodeSegment(segment)
  if (UUID.test(label)) return `${label.slice(0, 8)}…`
  if (LONG_NUMBER.test(label)) return `${label.slice(0, 10)}…`
  return label
}

/**
 * The crumb of a cluster or instance id shows its name (D14, D15), read from the
 * cluster list the shell polls; a miss keeps the shortened id.
 */
function namedLabel(parent: string | undefined, segment: string, lookup: NameLookup): string {
  const fallback = labelForSegment(segment)
  const id = decodeSegment(segment)
  if (parent === 'clusters') {
    const cluster = lookup.cluster(id)
    return cluster ? clusterLabel(cluster, fallback) : fallback
  }
  if (parent === 'instances') {
    const instance = lookup.instance(id)
    return instance ? instanceLabel(instance) : fallback
  }
  return fallback
}

export function Breadcrumbs() {
  const { pathname } = useLocation()
  const clusters = useCachedClusters()
  const lookup = useMemo(() => nameLookup(clusters), [clusters])
  const segments = pathname.split('/').filter(Boolean)
  const items =
    segments.length === 0
      ? [{ label: 'Fleet overview', path: '/', title: 'Fleet overview', titled: false }]
      : segments.map((segment, index) => {
          const label = namedLabel(segments[index - 1], segment, lookup)
          return {
            label,
            title: decodeSegment(segment),
            path: `/${segments.slice(0, index + 1).join('/')}`,
            // A shortened id or a name keeps the raw id in its tooltip.
            titled: label.endsWith('…') || label !== labelForSegment(segment),
          }
        })

  return (
    <nav aria-label="Breadcrumb" className="min-w-0">
      <ol className="text-muted-foreground flex min-w-0 items-center gap-1.5 text-sm">
        {items.map((item, index) => {
          const current = index === items.length - 1
          return (
            <li
              key={item.path}
              className={`flex items-center gap-1.5 ${current ? 'min-w-0' : 'shrink-0'}`}
            >
              {index > 0 ? (
                <ChevronRight aria-hidden="true" className="text-muted-foreground/60 size-3.5" />
              ) : null}
              {current ? (
                <span
                  aria-current="page"
                  className="text-foreground truncate font-semibold"
                  title={item.title}
                >
                  {item.label}
                </span>
              ) : (
                <Link
                  className="hover:text-foreground transition-colors"
                  {...(item.titled ? { title: item.title } : {})}
                  to={item.path}
                >
                  {item.label}
                </Link>
              )}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}
