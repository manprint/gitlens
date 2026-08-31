import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { useSignOut } from '@/api/auth'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { useTheme } from '@/hooks/useTheme'
import { formatRelative } from '@/lib/format/relative'

import { Breadcrumbs } from './Breadcrumbs'
import { TimeRangePicker } from './TimeRangePicker'

export interface ConnectionState {
  lastPollFailed: boolean
  lastSuccessAt: number | null
}

export interface HeaderProps {
  connection?: ConnectionState
  dataUpdatedAt?: number
}

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

export function ConnectionIndicator({ lastPollFailed, lastSuccessAt }: ConnectionState) {
  const status = lastPollFailed ? 'not reachable' : 'reachable'
  const variant = lastPollFailed ? 'destructive' : 'secondary'

  return (
    <div className="flex items-center gap-2" data-testid="connection-indicator">
      <Badge role="status" variant={variant} aria-label={`Connection: ${status}`}>
        {status}
      </Badge>
      <span className="text-text-secondary text-xs">{lastSuccessLabel(lastSuccessAt)}</span>
    </div>
  )
}

export function Header({ connection = defaultConnection, dataUpdatedAt = 0 }: HeaderProps) {
  const navigate = useNavigate()
  const signOut = useSignOut()
  const { resolvedTheme, setTheme, theme } = useTheme()
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
    const next = theme === 'dark' ? 'light' : theme === 'light' ? 'system' : 'dark'
    setTheme(next)
  }

  return (
    <header className="bg-background/95 sticky top-0 z-10 flex min-h-16 flex-wrap items-center justify-between gap-4 border-b px-4 py-3 backdrop-blur">
      <Breadcrumbs />
      <div className="flex flex-wrap items-center gap-3">
        <TimeRangePicker />
        <FreshnessBadge dataUpdatedAt={dataUpdatedAt} policy="fleet" />
        <ConnectionIndicator {...connection} />
        <Button aria-label="Toggle theme" onClick={toggleTheme} size="sm" variant="ghost">
          {theme === 'system'
            ? `System (${resolvedTheme})`
            : theme === 'dark'
              ? 'Light theme'
              : 'Dark theme'}
        </Button>
        <Button
          aria-label="Sign out"
          disabled={signOut.isPending}
          onClick={() => signOut.mutate()}
          size="sm"
          variant="outline"
        >
          Sign out
        </Button>
      </div>
      {shortcutSheetOpen ? (
        <section
          aria-label="Keyboard shortcuts"
          className="bg-surface basis-full rounded-md border p-3 text-sm"
          role="dialog"
        >
          <div className="flex items-center justify-between gap-4">
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
          <p>g f Fleet · g a Alerts · g s Settings · / Focus filter</p>
        </section>
      ) : null}
    </header>
  )
}
