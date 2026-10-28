import { Fragment, useEffect, useState } from 'react'
import { Keyboard, LogOut, Menu, Monitor, Moon, Sun } from 'lucide-react'
import { useNavigate } from 'react-router-dom'

import { useSignOut } from '@/api/auth'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { useTheme } from '@/hooks/useTheme'
import { useOnline } from '@/hooks/useOnline'
import { formatRelative } from '@/lib/format/relative'
import { cn } from '@/lib/utils'

import { Breadcrumbs } from './Breadcrumbs'
import { TimeRangePicker } from './TimeRangePicker'

export interface ConnectionState {
  lastPollFailed: boolean
  lastSuccessAt: number | null
  online?: boolean
}

export interface HeaderProps {
  connection?: ConnectionState
  dataUpdatedAt?: number
  /** Opens the navigation drawer on narrow screens. */
  onOpenNavigation?: () => void
}

const SHORTCUTS: readonly (readonly [readonly string[], string])[] = [
  [['g', 'f'], 'Fleet'],
  [['g', 'q'], 'Slowest queries'],
  [['g', 'a'], 'Alerts'],
  [['g', 's'], 'Settings'],
  [['/'], 'Focus the page filter'],
  [['?'], 'Show this help'],
]

const defaultConnection: ConnectionState = {
  lastPollFailed: false,
  lastSuccessAt: null,
}

function lastSuccessLabel(lastSuccessAt: number | null): string {
  if (lastSuccessAt === null) return 'No successful response yet.'

  const date = new Date(lastSuccessAt)
  if (!Number.isFinite(date.getTime())) return 'Last success age unknown.'
  return `Last success ${formatRelative(date.toISOString(), new Date())}`
}

export function ConnectionIndicator({
  lastPollFailed,
  lastSuccessAt,
  online = true,
}: ConnectionState) {
  const status = !online ? 'browser is offline' : lastPollFailed ? 'not reachable' : 'reachable'
  const variant = !online || lastPollFailed ? 'destructive' : 'secondary'

  const healthy = online && !lastPollFailed

  return (
    <div
      className="flex items-center gap-2"
      data-testid="connection-indicator"
      title={online ? lastSuccessLabel(lastSuccessAt) : 'Browser is offline.'}
    >
      <Badge
        aria-label={`Connection: ${status}`}
        className={cn(
          'gap-1.5 font-medium',
          healthy && 'border-success/30 bg-success/10 text-success-foreground dark:text-success',
        )}
        role="status"
        variant={variant}
      >
        <span
          aria-hidden="true"
          className={cn(
            'size-1.5 rounded-full',
            healthy ? 'bg-success animate-pulse' : 'bg-current',
          )}
        />
        {status}
      </Badge>
      <span className="text-muted-foreground hidden text-xs 2xl:inline">
        {online ? lastSuccessLabel(lastSuccessAt) : 'Browser is offline.'}
      </span>
    </div>
  )
}

export function Header({
  connection = defaultConnection,
  dataUpdatedAt = 0,
  onOpenNavigation,
}: HeaderProps) {
  const navigate = useNavigate()
  const signOut = useSignOut()
  const { resolvedTheme, setTheme, theme } = useTheme()
  const online = useOnline()
  const [shortcutSheetOpen, setShortcutSheetOpen] = useState(false)
  const [pendingShortcut, setPendingShortcut] = useState(false)

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      const target = event.target
      const isEditable =
        target instanceof HTMLElement &&
        (target.isContentEditable || ['INPUT', 'SELECT', 'TEXTAREA'].includes(target.tagName))

      if (isEditable) {
        setPendingShortcut(false)
        return
      }

      const key = event.key.toLowerCase()
      if (pendingShortcut) {
        setPendingShortcut(false)
        const destinations: Record<string, string> = {
          a: '/alerts',
          f: '/',
          q: '/queries',
          s: '/settings',
        }
        const destination = destinations[key]
        if (destination) {
          event.preventDefault()
          void navigate(destination)
        }
        return
      }

      if (key === 'g') {
        setPendingShortcut(true)
        return
      }

      if (event.key === '?') {
        event.preventDefault()
        setShortcutSheetOpen(true)
        return
      }

      if (event.key === '/') {
        const filter = document.querySelector<HTMLElement>('[data-primary-filter]')
        if (filter) {
          event.preventDefault()
          filter.focus()
        }
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [navigate, pendingShortcut])

  function toggleTheme() {
    const next = theme === 'light' ? 'dark' : theme === 'dark' ? 'system' : 'light'
    setTheme(next)
  }

  // The label names the current theme, and each click moves to the next one in
  // the cycle light, dark, system.
  const themeLabel =
    theme === 'system'
      ? `System (${resolvedTheme})`
      : theme === 'dark'
        ? 'Dark theme'
        : 'Light theme'
  const ThemeIcon = theme === 'system' ? Monitor : theme === 'dark' ? Moon : Sun

  return (
    <header className="bg-card/85 supports-[backdrop-filter]:bg-card/70 sticky top-0 z-20 border-b backdrop-blur">
      <div className="flex min-h-14 flex-wrap items-center justify-between gap-3 px-4 py-2 sm:px-6 xl:flex-nowrap">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          {onOpenNavigation ? (
            <Button
              aria-label="Open navigation"
              className="md:hidden"
              onClick={onOpenNavigation}
              size="icon-sm"
              variant="ghost"
            >
              <Menu aria-hidden="true" />
            </Button>
          ) : null}
          <Breadcrumbs />
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-2 xl:shrink-0">
          <TimeRangePicker />
          <span aria-hidden="true" className="bg-border mx-1 hidden h-6 w-px sm:block" />
          <span className="hidden sm:contents">
            <FreshnessBadge dataUpdatedAt={dataUpdatedAt} policy="fleet" />
          </span>
          <ConnectionIndicator {...connection} online={online} />
          <span aria-hidden="true" className="bg-border mx-1 hidden h-6 w-px sm:block" />
          <Button
            aria-label="Keyboard shortcuts"
            className="hidden sm:inline-flex"
            onClick={() => setShortcutSheetOpen((open) => !open)}
            size="icon-sm"
            title="Keyboard shortcuts (?)"
            variant="ghost"
          >
            <Keyboard aria-hidden="true" />
          </Button>
          {/* The accessible name keeps the visible label (WCAG 2.5.3), so the
              screen reader hears the current theme along with the control. */}
          <Button
            aria-label={`Toggle theme: ${themeLabel}`}
            onClick={toggleTheme}
            size="sm"
            title={themeLabel}
            variant="ghost"
          >
            <ThemeIcon aria-hidden="true" />
            <span className="sr-only">{themeLabel}</span>
          </Button>
          <Button
            aria-label="Sign out"
            disabled={signOut.isPending}
            onClick={() => signOut.mutate()}
            size="sm"
            variant="outline"
          >
            <LogOut aria-hidden="true" />
            <span className="hidden sm:inline">Sign out</span>
          </Button>
        </div>
      </div>
      {signOut.isError ? (
        <p
          className="border-destructive/30 bg-destructive/10 text-destructive border-t px-6 py-2 text-sm"
          role="alert"
        >
          Sign-out failed: the server session may still be active. Try signing out again.
        </p>
      ) : null}
      {shortcutSheetOpen ? (
        <section
          aria-label="Keyboard shortcuts"
          className="bg-popover shadow-raised absolute top-full right-4 z-30 mt-2 w-80 rounded-lg border p-4 text-sm"
          role="dialog"
        >
          <div className="mb-3 flex items-center justify-between gap-4">
            <strong>Keyboard shortcuts</strong>
            <Button
              aria-label="Close keyboard shortcuts"
              onClick={() => setShortcutSheetOpen(false)}
              size="sm"
              variant="ghost"
            >
              Close
            </Button>
          </div>
          <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-2">
            {SHORTCUTS.map(([keys, label]) => (
              <Fragment key={label}>
                <dt className="flex gap-1">
                  {keys.map((key) => (
                    <kbd
                      key={key}
                      className="bg-muted min-w-6 rounded border px-1.5 py-0.5 text-center font-mono text-xs"
                    >
                      {key}
                    </kbd>
                  ))}
                </dt>
                <dd className="text-muted-foreground">{label}</dd>
              </Fragment>
            ))}
          </dl>
        </section>
      ) : null}
    </header>
  )
}
