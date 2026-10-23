import { useRef, useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { AuthRequestError, useSignIn } from '@/api/auth'

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
    <main>
      <section aria-labelledby="login-title">
        <h1 id="login-title">Accedi a pglens</h1>
        <form onSubmit={handleSubmit}>
          <label htmlFor="ui-password">Password</label>
          <input
            ref={passwordRef}
            autoComplete="current-password"
            id="ui-password"
            name="password"
            onChange={(event) => setPassword(event.target.value)}
            type="password"
            value={password}
          />
          {failure ? (
            <p id="password-error" role="alert">
              {failure}
            </p>
          ) : null}
          <button disabled={signIn.isPending} type="submit">
            {signIn.isPending ? 'Accesso in corso…' : 'Accedi'}
          </button>
        </form>
      </section>
    </main>
  )
}
