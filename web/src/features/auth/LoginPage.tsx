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
    title: 'Query più lente',
    text: 'Le 20 query con il tempo di esecuzione più alto, con piani e storico.',
  },
  {
    icon: Activity,
    title: 'Eventi di attesa e lock',
    text: 'Cosa stanno aspettando le sessioni e chi blocca chi, in tempo reale.',
  },
  {
    icon: Lightbulb,
    title: 'Consigli automatici',
    text: 'Indici mancanti, bloat, vacuum e configurazione da rivedere.',
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
    return 'Troppi tentativi di accesso. Attendi qualche minuto e riprova.'
  }
  return `Troppi tentativi di accesso. Riprova tra ${seconds} ${seconds === 1 ? 'secondo' : 'secondi'}.`
}

function signInFailure(error: unknown): string {
  if (error instanceof AuthRequestError) {
    if (error.kind === 'unauthorized') return 'Password non valida.'
    if (error.failure.kind === 'server' && error.failure.status === 429) {
      return tooManyAttempts(error.retryAfterSeconds)
    }
    if (error.kind === 'network') {
      return 'Impossibile contattare il server. Controlla la connessione e riprova.'
    }
    if (error.kind === 'server') {
      return 'Il server non riesce a completare la richiesta. Riprova tra poco.'
    }
  }
  return 'Accesso non riuscito. Riprova.'
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
        className="bg-sidebar relative hidden overflow-hidden text-white lg:flex lg:flex-col lg:justify-between lg:p-12"
      >
        <div className="bg-grid pointer-events-none absolute inset-0 opacity-[0.12]" />
        <div className="pointer-events-none absolute -top-40 -right-32 size-[28rem] rounded-full bg-sky-500/25 blur-3xl" />
        <div className="pointer-events-none absolute -bottom-48 -left-24 size-[30rem] rounded-full bg-blue-600/25 blur-3xl" />
        <Logo className="relative" />
        <div className="relative max-w-md space-y-8">
          <div className="space-y-3">
            <h2 className="text-3xl leading-tight font-semibold tracking-tight">
              Monitoraggio PostgreSQL, chiaro e immediato.
            </h2>
            <p className="text-sidebar-foreground text-base leading-relaxed">
              Prestazioni delle query, attese, lock, replica e consigli di ottimizzazione per tutta
              la tua flotta, in un unico posto.
            </p>
          </div>
          <ul className="space-y-4 text-sm">
            {HIGHLIGHTS.map(({ icon: Icon, title, text }) => (
              <li key={title} className="flex gap-3">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-white/10 ring-1 ring-white/15">
                  <Icon className="size-4 text-sky-300" />
                </span>
                <span>
                  <span className="block font-medium text-white">{title}</span>
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
              Accedi a pglens
            </h1>
            <p className="text-muted-foreground text-sm">
              Inserisci la password dell’interfaccia per continuare.
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
                  aria-label={
                    visible ? 'Nascondi i caratteri digitati' : 'Mostra i caratteri digitati'
                  }
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
                  Accesso in corso…
                </>
              ) : (
                <>
                  Accedi
                  <ArrowRight aria-hidden="true" className="size-4" />
                </>
              )}
            </button>
          </form>
          <p className="text-muted-foreground mt-10 text-xs leading-relaxed">
            La password è impostata sul server con <code>PGLENS_UI_PASSWORD</code>. La sessione
            resta attiva su questo browser finché non esci.
          </p>
        </div>
      </section>
    </main>
  )
}
