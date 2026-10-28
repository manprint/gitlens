import { useRef, useState, type FormEvent } from 'react'
import {
  Activity,
  ArrowRight,
  CircleAlert,
  Eye,
  EyeOff,
  Gauge,
  KeyRound,
  Lightbulb,
  LoaderCircle,
} from 'lucide-react'
import { useLocation, useNavigate } from 'react-router-dom'

import { AuthRequestError, useSignIn } from '@/api/auth'
import { Logo } from '@/components/layout/Logo'

const HIGHLIGHTS = [
  {
    icon: Gauge,
    title: 'Slowest queries',
    text: 'The 20 queries with the highest execution time, with plans and history.',
  },
  {
    icon: Activity,
    title: 'Wait events and locks',
    text: 'What sessions are waiting for and who blocks whom, as it happens.',
  },
  {
    icon: Lightbulb,
    title: 'Automatic advice',
    text: 'Missing indexes, bloat, vacuum and settings worth a second look.',
  },
] as const

function safeNext(value: string | null): string {
  if (value === null || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) {
    return '/'
  }
  return value
}

// Only a rejected password is fixed by typing it again. A network or server
// failure says nothing about the password, so it gets its own message.
function tooManyAttempts(seconds: number | null): string {
  if (seconds === null || seconds === 0) {
    return 'Too many sign-in attempts. Wait a few minutes and try again.'
  }
  return `Too many sign-in attempts. Try again in ${seconds} ${seconds === 1 ? 'second' : 'seconds'}.`
}

function signInFailure(error: unknown): string {
  if (error instanceof AuthRequestError) {
    if (error.kind === 'unauthorized') return 'Invalid password.'
    if (error.failure.kind === 'server' && error.failure.status === 429) {
      return tooManyAttempts(error.retryAfterSeconds)
    }
    if (error.kind === 'network') {
      return 'Cannot reach the server. Check the connection and try again.'
    }
    if (error.kind === 'server') {
      return 'The server could not complete the request. Try again shortly.'
    }
  }
  return 'Sign-in failed. Try again.'
}

export default function LoginPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const signIn = useSignIn()
  const [password, setPassword] = useState('')
  const [failure, setFailure] = useState<string | null>(null)
  const [visible, setVisible] = useState(false)
  const passwordRef = useRef<HTMLInputElement>(null)
  const next = new URLSearchParams(location.search).get('next')

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setFailure(null)

    try {
      await signIn.mutateAsync(password)
      void navigate(safeNext(next), { replace: true })
    } catch (error) {
      setFailure(signInFailure(error))
      passwordRef.current?.focus()
    }
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>): void {
    void submit(event)
  }

  return (
    <main className="bg-background grid min-h-screen lg:grid-cols-[minmax(0,1.05fr)_minmax(0,1fr)]">
      <aside
        aria-hidden="true"
        className="bg-sidebar text-sidebar-accent-foreground relative hidden overflow-hidden lg:flex lg:flex-col lg:justify-between lg:p-12"
      >
        <div className="bg-grid pointer-events-none absolute inset-0 opacity-[0.12]" />
        <div className="bg-sidebar-primary/25 pointer-events-none absolute -top-40 -right-32 size-[28rem] rounded-full blur-3xl" />
        <div className="bg-primary/25 pointer-events-none absolute -bottom-48 -left-24 size-[30rem] rounded-full blur-3xl" />
        <Logo className="relative" />
        <div className="relative max-w-md space-y-8">
          <div className="space-y-3">
            <h2 className="text-3xl leading-tight font-semibold tracking-tight">
              PostgreSQL monitoring, clear at a glance.
            </h2>
            <p className="text-sidebar-foreground text-base leading-relaxed">
              Query performance, waits, locks, replication and tuning advice for your whole fleet,
              in one place.
            </p>
          </div>
          <ul className="space-y-4 text-sm">
            {HIGHLIGHTS.map(({ icon: Icon, title, text }) => (
              <li key={title} className="flex gap-3">
                <span className="bg-sidebar-accent ring-sidebar-border flex size-9 shrink-0 items-center justify-center rounded-lg ring-1">
                  <Icon className="text-sidebar-primary size-4" />
                </span>
                <span>
                  <span className="text-sidebar-accent-foreground block font-medium">{title}</span>
                  <span className="text-sidebar-foreground">{text}</span>
                </span>
              </li>
            ))}
          </ul>
        </div>
        <p className="text-sidebar-foreground/70 relative text-xs">
          pglens · PostgreSQL observability
        </p>
      </aside>

      <section
        aria-labelledby="login-title"
        className="flex items-center justify-center px-6 py-12 sm:px-10"
      >
        <div className="w-full max-w-sm">
          <Logo className="text-foreground mb-10 lg:hidden" />
          <div className="mb-8 space-y-2">
            <h1 id="login-title" className="text-foreground text-2xl font-semibold tracking-tight">
              Sign in to pglens
            </h1>
            <p className="text-muted-foreground text-sm">
              Enter the interface password to continue.
            </p>
          </div>
          <form className="space-y-5" noValidate onSubmit={handleSubmit}>
            <div className="space-y-2">
              <label className="text-foreground text-sm font-medium" htmlFor="ui-password">
                Password
              </label>
              <div className="relative">
                <KeyRound
                  aria-hidden="true"
                  className="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2"
                />
                <input
                  ref={passwordRef}
                  aria-describedby={failure ? 'password-error' : undefined}
                  aria-invalid={failure ? true : undefined}
                  autoComplete="current-password"
                  autoFocus
                  className="border-input bg-card text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/30 aria-invalid:border-destructive aria-invalid:ring-destructive/20 h-11 w-full rounded-lg border pr-11 pl-10 text-sm shadow-xs transition-[color,box-shadow] outline-none focus-visible:ring-4"
                  id="ui-password"
                  name="password"
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="••••••••"
                  type={visible ? 'text' : 'password'}
                  value={password}
                />
                <button
                  aria-label={visible ? 'Hide password' : 'Show password'}
                  aria-pressed={visible}
                  className="text-muted-foreground hover:text-foreground absolute top-1/2 right-1.5 flex size-8 -translate-y-1/2 items-center justify-center rounded-md transition-colors"
                  onClick={() => setVisible((current) => !current)}
                  type="button"
                >
                  {visible ? (
                    <EyeOff aria-hidden="true" className="size-4" />
                  ) : (
                    <Eye aria-hidden="true" className="size-4" />
                  )}
                </button>
              </div>
            </div>
            {failure ? (
              <p
                className="border-destructive/30 bg-destructive/10 text-destructive flex items-start gap-2 rounded-lg border px-3 py-2.5 text-sm"
                id="password-error"
                role="alert"
              >
                <CircleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
                <span>{failure}</span>
              </p>
            ) : null}
            <button
              className="bg-primary text-primary-foreground hover:bg-primary/90 focus-visible:ring-ring/40 inline-flex h-11 w-full items-center justify-center gap-2 rounded-lg text-sm font-semibold shadow-sm transition-colors outline-none focus-visible:ring-4 disabled:cursor-not-allowed disabled:opacity-70"
              disabled={signIn.isPending}
              type="submit"
            >
              {signIn.isPending ? (
                <>
                  <LoaderCircle aria-hidden="true" className="size-4 animate-spin" />
                  Signing in…
                </>
              ) : (
                <>
                  Sign in
                  <ArrowRight aria-hidden="true" className="size-4" />
                </>
              )}
            </button>
          </form>
          <p className="text-muted-foreground mt-10 text-xs leading-relaxed">
            The password is set on the server with <code>PGLENS_UI_PASSWORD</code>. The session
            stays active in this browser until you sign out.
          </p>
        </div>
      </section>
    </main>
  )
}
