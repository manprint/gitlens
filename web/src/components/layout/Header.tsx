import { Fragment, useEffect, useState, type Ref } from 'react'
import { Keyboard, LogOut, Menu, Monitor, Moon, Sun } from 'lucide-react'
import { useNavigate } from 'react-router-dom'

import { useSignOut } from '@/api/auth'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useTheme } from '@/hooks/useTheme'
import { useOnline } from '@/hooks/useOnline'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
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
  /** Opens the navigation drawer on narrow screens. */
  onOpenNavigation?: () => void
  /**
   * When set, the header row has a status slot after the breadcrumbs, in the
   * space the row leaves free, and this ref receives it: the shell moves the
   * instance status banners into it from 1600 px (13.3). The slot shrinks to
   * nothing before it moves the controls or wraps the row; it clips what does
   * not fit, with room for a focus ring.
   */
  statusSlot?: Ref<HTMLDivElement> | undefined
}

const SHORTCUTS: readonly (readonly [readonly string[], MessageKey])[] = [
  [['g', 'f'], 'shell.header.shortcut.fleet'],
  [['g', 'q'], 'shell.header.shortcut.queries'],
  [['g', 'a'], 'shell.header.shortcut.alerts'],
  [['g', 's'], 'shell.header.shortcut.settings'],
  [['/'], 'shell.header.shortcut.filter'],
  [['?'], 'shell.header.shortcut.help'],
]

const defaultConnection: ConnectionState = {
  lastPollFailed: false,
  lastSuccessAt: null,
}

function lastSuccessLabel(t: Translate, lastSuccessAt: number | null): string {
  if (lastSuccessAt === null) return t('shell.header.noSuccess')

  const date = new Date(lastSuccessAt)
  if (!Number.isFinite(date.getTime())) return t('shell.header.successUnknown')
  return t('shell.header.lastSuccess', { age: formatRelative(date.toISOString(), new Date()) })
}

export function ConnectionIndicator({
  lastPollFailed,
  lastSuccessAt,
  online = true,
}: ConnectionState) {
  const t = useT()
  const status = t(
    !online
      ? 'shell.header.status.offline'
      : lastPollFailed
        ? 'shell.header.status.down'
        : 'shell.header.status.up',
  )
  const detail = online ? lastSuccessLabel(t, lastSuccessAt) : t('shell.header.offline')
  const variant = !online || lastPollFailed ? 'destructive' : 'secondary'

  const healthy = online && !lastPollFailed

  return (
    <div className="flex items-center gap-2" data-testid="connection-indicator" title={detail}>
      <Badge
        aria-label={t('shell.header.connection', { status })}
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
        {/* Names the pglens server, so it is not read as the instance's own reachability. */}
        <span className="opacity-70">{t('shell.header.server')}</span>
        {status}
      </Badge>
      <span className="text-muted-foreground hidden text-xs 2xl:inline">{detail}</span>
    </div>
  )
}

export function Header({
  connection = defaultConnection,
  onOpenNavigation,
  statusSlot,
}: HeaderProps) {
  const t = useT()
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

      // Ctrl, Cmd and Alt combinations belong to the browser and the system.
      if (isEditable || event.ctrlKey || event.metaKey || event.altKey) {
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
    // A "g" left alone expires, so a later single key is not read as its second half.
    const expiry = pendingShortcut ? window.setTimeout(() => setPendingShortcut(false), 1_500) : 0
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      window.clearTimeout(expiry)
    }
  }, [navigate, pendingShortcut])

  function toggleTheme() {
    const next = theme === 'light' ? 'dark' : theme === 'dark' ? 'system' : 'light'
    setTheme(next)
  }

  // The label names the current theme, and each click moves to the next one in
  // the cycle light, dark, system.
  const themeLabel = t(
    theme === 'system'
      ? resolvedTheme === 'dark'
        ? 'shell.header.theme.systemDark'
        : 'shell.header.theme.systemLight'
      : theme === 'dark'
        ? 'shell.header.theme.dark'
        : 'shell.header.theme.light',
  )
  const ThemeIcon = theme === 'system' ? Monitor : theme === 'dark' ? Moon : Sun

  return (
    <header className="bg-card/85 supports-[backdrop-filter]:bg-card/70 sticky top-0 z-20 border-b backdrop-blur">
      <div
        className="flex min-h-14 flex-wrap items-center justify-between gap-3 px-4 py-2 sm:px-6 xl:flex-nowrap"
        data-testid="header-row"
      >
        <div className="flex min-w-0 flex-1 items-center gap-2" data-testid="header-start">
          {onOpenNavigation ? (
            <Button
              aria-label={t('shell.header.openNavigation')}
              className="md:hidden"
              onClick={onOpenNavigation}
              size="icon-sm"
              variant="ghost"
            >
              <Menu aria-hidden="true" />
            </Button>
          ) : null}
          <Breadcrumbs />
          {statusSlot ? (
            <div
              ref={statusSlot}
              className="flex min-w-min flex-1 items-center overflow-x-clip p-[3px]"
              data-testid="header-status"
            />
          ) : null}
        </div>
        <div
          className="flex min-w-0 flex-wrap items-center gap-2 xl:shrink-0"
          data-testid="header-controls"
        >
          <TimeRangePicker />
          <span aria-hidden="true" className="bg-border mx-1 hidden h-6 w-px sm:block" />
          <ConnectionIndicator {...connection} online={online} />
          <span aria-hidden="true" className="bg-border mx-1 hidden h-6 w-px sm:block" />
          <Button
            aria-label={t('shell.header.shortcuts')}
            className="hidden sm:inline-flex"
            onClick={() => setShortcutSheetOpen((open) => !open)}
            size="icon-sm"
            title={t('shell.header.shortcutsTitle')}
            variant="ghost"
          >
            <Keyboard aria-hidden="true" />
          </Button>
          {/* The accessible name keeps the visible label (WCAG 2.5.3), so the
              screen reader hears the current theme along with the control. */}
          <Button
            aria-label={t('shell.header.toggleTheme', { theme: themeLabel })}
            onClick={toggleTheme}
            size="sm"
            title={themeLabel}
            variant="ghost"
          >
            <ThemeIcon aria-hidden="true" />
            <span className="sr-only">{themeLabel}</span>
          </Button>
          <Button
            aria-label={t('shell.header.signOut')}
            disabled={signOut.isPending}
            onClick={() => signOut.mutate()}
            size="sm"
            variant="outline"
          >
            <LogOut aria-hidden="true" />
            <span className="hidden sm:inline">{t('shell.header.signOut')}</span>
          </Button>
        </div>
      </div>
      {signOut.isError ? (
        <p
          className="border-destructive/30 bg-destructive/10 text-destructive border-t px-6 py-2 text-sm"
          role="alert"
        >
          {t('shell.header.signOutFailed')}
        </p>
      ) : null}
      {shortcutSheetOpen ? (
        <section
          aria-label={t('shell.header.shortcuts')}
          className="bg-popover shadow-raised absolute top-full right-4 z-30 mt-2 w-80 rounded-lg border p-4 text-sm"
          role="dialog"
        >
          <div className="mb-3 flex items-center justify-between gap-4">
            <strong>{t('shell.header.shortcuts')}</strong>
            <Button
              aria-label={t('shell.header.closeShortcuts')}
              onClick={() => setShortcutSheetOpen(false)}
              size="sm"
              variant="ghost"
            >
              {t('shell.header.close')}
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
                <dd className="text-muted-foreground">{t(label)}</dd>
              </Fragment>
            ))}
          </dl>
        </section>
      ) : null}
    </header>
  )
}
