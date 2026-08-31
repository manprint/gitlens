import { useRef, useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { useSignIn } from '@/api/auth'

function safeNext(value: string | null): string {
  if (value === null || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) {
    return '/'
  }
  return value
}

export default function LoginPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const signIn = useSignIn()
  const [password, setPassword] = useState('')
  const [failed, setFailed] = useState(false)
  const passwordRef = useRef<HTMLInputElement>(null)
  const next = new URLSearchParams(location.search).get('next')

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setFailed(false)

    try {
      await signIn.mutateAsync(password)
      void navigate(safeNext(next), { replace: true })
    } catch {
      setFailed(true)
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
          {failed ? (
            <p id="password-error" role="alert">
              Password non valida.
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
