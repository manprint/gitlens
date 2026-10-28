import { useEffect, type ComponentType } from 'react'
import {
  Link,
  NavLink,
  useLocation,
  useMatch,
  useNavigate,
  useSearchParams,
} from 'react-router-dom'
import {
  Activity,
  Bell,
  BellOff,
  Gauge,
  History,
  LayoutDashboard,
  Lightbulb,
  ListChecks,
  Lock,
  Server,
  Settings,
  Zap,
} from 'lucide-react'

import type { Cluster } from '@/api/types'
import { chooseInstance, readLastInstance, writeLastInstance } from '@/lib/lastInstance'
import { cn } from '@/lib/utils'

import { Logo } from './Logo'

interface Destination {
  /** The accessible name of the link; it matches the visible label. */
  label: string
  path: string
  icon: ComponentType<{ className?: string }>
  /** Marks the link active only on an exact match. */
  end?: boolean
}

interface NavGroup {
  title: string
  items: readonly Destination[]
}

const monitoringGroup: NavGroup = {
  title: 'Monitoring',
  items: [
    { icon: LayoutDashboard, label: 'Fleet', path: '/', end: true },
    { icon: Gauge, label: 'Slowest queries', path: '/queries', end: true },
    { icon: Lightbulb, label: 'Findings', path: '/findings' },
  ],
}

const alertingGroup: NavGroup = {
  title: 'Alerting',
  items: [
    { icon: Bell, label: 'Alerts', path: '/alerts', end: true },
    { icon: History, label: 'Events', path: '/events' },
    { icon: ListChecks, label: 'Alert rules', path: '/alerts/rules' },
    { icon: BellOff, label: 'Silences', path: '/alerts/silences' },
  ],
}

function instanceGroup(instanceId: string): NavGroup {
  const base = `/instances/${encodeURIComponent(instanceId)}`
  return {
    title: 'Instance',
    items: [
      { icon: Server, label: 'Instance overview', path: base, end: true },
      { icon: Zap, label: 'Query performance', path: `${base}/queries` },
      { icon: Activity, label: 'Wait events', path: `${base}/ash` },
      { icon: Lock, label: 'Locks and activity', path: `${base}/locks` },
    ],
  }
}

interface SidebarProps {
  clusters?: readonly Cluster[]
  /** Called after a destination is chosen, so a mobile drawer can close. */
  onNavigate?: () => void
  /** Always shows labels, as the mobile drawer does. */
  expanded?: boolean
}

function NavItem({
  destination,
  expanded,
  onNavigate,
}: {
  destination: Destination
  expanded: boolean
  onNavigate: (() => void) | undefined
}) {
  const { icon: Icon, label, path, end } = destination
  return (
    <li>
      <NavLink
        aria-label={label}
        className={({ isActive }) =>
          cn(
            'group relative flex h-9 items-center gap-3 rounded-md px-2.5 text-sm font-medium transition-colors',
            isActive
              ? 'bg-sidebar-accent text-sidebar-accent-foreground'
              : 'text-sidebar-foreground/85 hover:bg-sidebar-accent/50 hover:text-sidebar-accent-foreground',
            !expanded && 'justify-center lg:justify-start',
          )
        }
        end={end ?? false}
        onClick={onNavigate}
        title={label}
        to={path}
      >
        {({ isActive }) => (
          <>
            {isActive ? (
              <span
                aria-hidden="true"
                className="bg-sidebar-primary absolute inset-y-1.5 -left-2 w-1 rounded-r"
              />
            ) : null}
            <Icon
              className={cn(
                'size-4 shrink-0',
                isActive
                  ? 'text-sidebar-primary'
                  : 'text-sidebar-foreground/70 group-hover:text-sidebar-accent-foreground',
              )}
            />
            <span className={cn('truncate', !expanded && 'hidden lg:inline')}>{label}</span>
          </>
        )}
      </NavLink>
    </li>
  )
}

function Group({
  expanded,
  group,
  onNavigate,
}: {
  expanded: boolean
  group: NavGroup
  onNavigate: (() => void) | undefined
}) {
  return (
    <div className="space-y-1">
      <p
        className={cn(
          'text-sidebar-foreground/75 px-2.5 pt-4 pb-1 text-[0.6875rem] font-semibold tracking-wider uppercase',
          !expanded && 'hidden lg:block',
        )}
      >
        {group.title}
      </p>
      <ul className="flex flex-col gap-0.5">
        {group.items.map((destination) => (
          <NavItem
            key={destination.path}
            destination={destination}
            expanded={expanded}
            onNavigate={onNavigate}
          />
        ))}
      </ul>
    </div>
  )
}

function InstanceSwitcher({
  clusters,
  current,
}: {
  clusters: readonly Cluster[]
  current: string | null
}) {
  const navigate = useNavigate()
  const { pathname, search } = useLocation()
  const instances = clusters.flatMap((cluster) =>
    cluster.instances.map((instance) => ({ cluster, instance })),
  )

  if (instances.length === 0) return null

  function choose(instanceId: string) {
    if (!instanceId) return
    // The slowest-queries page is fleet-level and names its server in the URL.
    if (pathname === '/queries') {
      const next = new URLSearchParams(search)
      next.set('instance', instanceId)
      next.delete('db')
      void navigate(`/queries?${next.toString()}`)
      return
    }
    // Keep the operator on the same instance view when they switch servers.
    const view = /^\/instances\/[^/]+(\/(?:ash|queries|locks))/.exec(pathname)?.[1] ?? ''
    void navigate(`/instances/${encodeURIComponent(instanceId)}${view}`)
  }

  return (
    <div className="px-3 pt-4">
      <label
        className="text-sidebar-foreground/75 mb-1.5 block px-1 text-[0.6875rem] font-semibold tracking-wider uppercase"
        htmlFor="sidebar-instance"
      >
        Server
      </label>
      <div className="relative">
        <Server
          aria-hidden="true"
          className="text-sidebar-foreground/60 pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
        />
        <select
          className="border-sidebar-border bg-sidebar-accent/40 text-sidebar-accent-foreground hover:bg-sidebar-accent/70 focus-visible:ring-sidebar-ring [&>option]:bg-sidebar [&>option]:text-sidebar-accent-foreground h-9 w-full cursor-pointer appearance-none truncate rounded-md border pr-8 pl-8 text-sm scheme-dark transition-colors outline-none focus-visible:ring-2"
          id="sidebar-instance"
          onChange={(event) => choose(event.target.value)}
          value={current ?? ''}
        >
          <option disabled value="">
            Choose a server…
          </option>
          {instances.map(({ cluster, instance }) => (
            <option key={instance.instance_id} value={instance.instance_id}>
              {instance.addr}:{instance.port}
              {cluster.name ? ` · ${cluster.name}` : ''}
              {instance.up ? '' : ' (down)'}
            </option>
          ))}
        </select>
        <svg
          aria-hidden="true"
          className="text-sidebar-foreground/60 pointer-events-none absolute top-1/2 right-2.5 size-4 -translate-y-1/2"
          fill="none"
          stroke="currentColor"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth="2"
          viewBox="0 0 24 24"
        >
          <path d="m7 15 5 5 5-5M7 9l5-5 5 5" />
        </svg>
      </div>
    </div>
  )
}

/** The navigation body, shared by the fixed rail and the mobile drawer. */
export function SidebarContent({ clusters = [], expanded = false, onNavigate }: SidebarProps) {
  const match = useMatch('/instances/:instanceId/*')
  const routeInstance = match?.params.instanceId ?? null

  const [searchParams] = useSearchParams()
  const { pathname } = useLocation()
  const requested = routeInstance ?? (pathname === '/queries' ? searchParams.get('instance') : null)
  const currentInstance = chooseInstance(clusters, requested, readLastInstance())

  // Only a server the operator opened is remembered, not a fallback choice.
  useEffect(() => {
    if (requested !== null && requested === currentInstance) writeLastInstance(requested)
  }, [currentInstance, requested])

  return (
    <div className="flex h-full flex-col">
      <div className="border-sidebar-border flex h-16 shrink-0 items-center border-b px-4">
        <Link
          aria-label="pglens home"
          className="text-sidebar-accent-foreground"
          onClick={onNavigate}
          to="/"
        >
          <Logo wordmarkClassName={cn(!expanded && 'hidden lg:inline')} />
        </Link>
      </div>

      <div className={cn(!expanded && 'hidden lg:block')}>
        <InstanceSwitcher clusters={clusters} current={currentInstance} />
      </div>

      <nav aria-label="Primary navigation" className="flex-1 overflow-y-auto px-3 pb-4">
        <Group expanded={expanded} group={monitoringGroup} onNavigate={onNavigate} />
        {currentInstance ? (
          <Group
            expanded={expanded}
            group={instanceGroup(currentInstance)}
            onNavigate={onNavigate}
          />
        ) : null}
        <Group expanded={expanded} group={alertingGroup} onNavigate={onNavigate} />
      </nav>

      <div className="border-sidebar-border border-t px-3 py-3">
        <ul>
          <NavItem
            destination={{ icon: Settings, label: 'Settings', path: '/settings' }}
            expanded={expanded}
            onNavigate={onNavigate}
          />
        </ul>
      </div>
    </div>
  )
}

export function Sidebar({ clusters }: { clusters?: readonly Cluster[] }) {
  return (
    <aside
      className="bg-sidebar text-sidebar-foreground border-sidebar-border fixed inset-y-0 left-0 z-30 hidden w-16 flex-col border-r md:flex lg:w-64"
      data-collapsed-below="1024px"
      data-testid="app-sidebar"
    >
      <SidebarContent {...(clusters === undefined ? {} : { clusters })} />
    </aside>
  )
}
