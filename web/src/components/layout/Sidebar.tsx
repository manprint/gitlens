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
  BookOpen,
  Gauge,
  History,
  LayoutDashboard,
  Lightbulb,
  ListChecks,
  ListTree,
  Lock,
  ScrollText,
  Server,
  Settings,
  Zap,
} from 'lucide-react'

import type { Cluster } from '@/api/types'
import type { MessageKey } from '@/i18n/messages/en'
import { useT } from '@/i18n/react'
import { chooseInstance, readLastInstance, writeLastInstance } from '@/lib/lastInstance'
import { clusterLabel, instanceLabel } from '@/lib/names'
import { cn } from '@/lib/utils'

import { Logo } from './Logo'

interface Destination {
  /** The accessible name of the link; it matches the visible label. */
  label: MessageKey
  path: string
  icon: ComponentType<{ className?: string }>
  /** Marks the link active only on an exact match. */
  end?: boolean
}

interface NavGroup {
  title: MessageKey
  items: readonly Destination[]
}

const monitoringGroup: NavGroup = {
  title: 'shell.nav.monitoring',
  items: [
    { icon: LayoutDashboard, label: 'shell.nav.fleet', path: '/', end: true },
    { icon: Gauge, label: 'shell.nav.slowestQueries', path: '/queries', end: true },
    { icon: ListTree, label: 'shell.nav.indexAnalysis', path: '/indexes', end: true },
    { icon: Lightbulb, label: 'shell.nav.findings', path: '/findings' },
  ],
}

const alertingGroup: NavGroup = {
  title: 'shell.nav.alerting',
  items: [
    { icon: Bell, label: 'shell.nav.alerts', path: '/alerts', end: true },
    { icon: History, label: 'shell.nav.events', path: '/events' },
    { icon: ListChecks, label: 'shell.nav.alertRules', path: '/alerts/rules' },
    { icon: BellOff, label: 'shell.nav.silences', path: '/alerts/silences' },
  ],
}

const helpGroup: NavGroup = {
  title: 'shell.nav.inlineHelp',
  items: [{ icon: BookOpen, label: 'shell.nav.userGuide', path: '/help' }],
}

/** Fleet-level pages that name their server in the `instance` search parameter. */
function isFleetServerPage(pathname: string): boolean {
  return pathname === '/queries' || pathname === '/indexes'
}

/**
 * The views of one server. A database chosen on one view (`db`) is carried to
 * the others, so moving between them keeps the same scope.
 */
function instanceGroup(instanceId: string, database: string | null): NavGroup {
  const base = `/instances/${encodeURIComponent(instanceId)}`
  const search = database ? `?${new URLSearchParams({ db: database }).toString()}` : ''
  return {
    title: 'shell.nav.instance',
    items: [
      { icon: Server, label: 'shell.nav.instanceOverview', path: `${base}${search}`, end: true },
      { icon: Zap, label: 'shell.nav.queryPerformance', path: `${base}/queries${search}` },
      { icon: Activity, label: 'shell.nav.waitEvents', path: `${base}/ash${search}` },
      { icon: Lock, label: 'shell.nav.locks', path: `${base}/locks${search}` },
      { icon: ScrollText, label: 'shell.nav.logs', path: `${base}/logs${search}` },
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
  const t = useT()
  const { icon: Icon, path, end } = destination
  const label = t(destination.label)
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
  const t = useT()
  return (
    <div className="space-y-1">
      <p
        className={cn(
          'text-sidebar-foreground/75 px-2.5 pt-4 pb-1 text-[0.6875rem] font-semibold tracking-wider uppercase',
          !expanded && 'hidden lg:block',
        )}
      >
        {t(group.title)}
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
  const t = useT()
  const navigate = useNavigate()
  const { pathname, search } = useLocation()
  const instances = clusters.flatMap((cluster) =>
    cluster.instances.map((instance) => ({ cluster, instance })),
  )

  if (instances.length === 0) return null

  function choose(instanceId: string) {
    if (!instanceId) return
    // The slowest-queries and index-analysis pages are fleet-level and name their server in the URL.
    if (isFleetServerPage(pathname)) {
      const next = new URLSearchParams(search)
      next.set('instance', instanceId)
      next.delete('db')
      void navigate(`${pathname}?${next.toString()}`)
      return
    }
    // Keep the operator on the same instance view when they switch servers.
    const view = /^\/instances\/[^/]+(\/(?:ash|queries|locks|logs))/.exec(pathname)?.[1] ?? ''
    void navigate(`/instances/${encodeURIComponent(instanceId)}${view}`)
  }

  return (
    <div className="px-3 pt-4">
      <label
        className="text-sidebar-foreground/75 mb-1.5 block px-1 text-[0.6875rem] font-semibold tracking-wider uppercase"
        htmlFor="sidebar-instance"
      >
        {t('shell.nav.server')}
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
            {t('shell.nav.chooseServer')}
          </option>
          {instances.map(({ cluster, instance }) => (
            <option key={instance.instance_id} value={instance.instance_id}>
              {instanceLabel(instance)}
              {cluster.name ? ` · ${clusterLabel(cluster)}` : ''}
              {instance.up ? '' : ` ${t('shell.nav.down')}`}
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
  const t = useT()
  const match = useMatch('/instances/:instanceId/*')
  const routeInstance = match?.params.instanceId ?? null

  const [searchParams] = useSearchParams()
  const { pathname } = useLocation()
  const requested =
    routeInstance ?? (isFleetServerPage(pathname) ? searchParams.get('instance') : null)
  const currentInstance = chooseInstance(clusters, requested, readLastInstance())
  const database =
    routeInstance !== null && routeInstance === currentInstance ? searchParams.get('db') : null

  // Only a server the operator opened is remembered, not a fallback choice.
  useEffect(() => {
    if (requested !== null && requested === currentInstance) writeLastInstance(requested)
  }, [currentInstance, requested])

  return (
    <div className="flex h-full flex-col">
      <div className="border-sidebar-border flex h-16 shrink-0 items-center border-b px-4">
        <Link
          aria-label={t('shell.nav.home')}
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

      <nav aria-label={t('shell.nav.primary')} className="flex-1 overflow-y-auto px-3 pb-4">
        <Group expanded={expanded} group={monitoringGroup} onNavigate={onNavigate} />
        {currentInstance ? (
          <Group
            expanded={expanded}
            group={instanceGroup(currentInstance, database)}
            onNavigate={onNavigate}
          />
        ) : null}
        <Group expanded={expanded} group={alertingGroup} onNavigate={onNavigate} />
        <Group expanded={expanded} group={helpGroup} onNavigate={onNavigate} />
      </nav>

      <div className="border-sidebar-border border-t px-3 py-3">
        <ul>
          <NavItem
            destination={{ icon: Settings, label: 'shell.nav.settings', path: '/settings' }}
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
