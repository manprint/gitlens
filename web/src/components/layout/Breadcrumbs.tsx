import { Link, useLocation } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'

const segmentLabels: Record<string, string> = {
  alerts: 'Alerts',
  ash: 'ASH',
  clusters: 'Clusters',
  events: 'Events',
  findings: 'Findings',
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

export function Breadcrumbs() {
  const { pathname } = useLocation()
  const segments = pathname.split('/').filter(Boolean)
  const items =
    segments.length === 0
      ? [{ label: 'Fleet overview', path: '/', title: 'Fleet overview' }]
      : segments.map((segment, index) => ({
          label: labelForSegment(segment),
          title: decodeSegment(segment),
          path: `/${segments.slice(0, index + 1).join('/')}`,
        }))

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
                  {...(item.label.endsWith('…') ? { title: item.title } : {})}
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
