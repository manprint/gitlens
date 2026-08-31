import { Link, useLocation } from 'react-router-dom'

const segmentLabels: Record<string, string> = {
  alerts: 'Alerts',
  ash: 'ASH',
  clusters: 'Clusters',
  findings: 'Findings',
  instances: 'Instances',
  locks: 'Locks',
  queries: 'Queries',
  settings: 'Settings',
}

function labelForSegment(segment: string): string {
  return segmentLabels[segment] ?? decodeURIComponent(segment)
}

export function Breadcrumbs() {
  const { pathname } = useLocation()
  const segments = pathname.split('/').filter(Boolean)
  const items =
    segments.length === 0
      ? [{ label: 'Fleet overview', path: '/' }]
      : segments.map((segment, index) => ({
          label: labelForSegment(segment),
          path: `/${segments.slice(0, index + 1).join('/')}`,
        }))

  return (
    <nav aria-label="Breadcrumb">
      <ol className="text-text-secondary flex flex-wrap items-center gap-2 text-sm">
        {items.map((item, index) => {
          const current = index === items.length - 1
          return (
            <li key={item.path} className="flex items-center gap-2">
              {index > 0 ? <span aria-hidden="true">/</span> : null}
              {current ? (
                <span aria-current="page">{item.label}</span>
              ) : (
                <Link to={item.path}>{item.label}</Link>
              )}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}
