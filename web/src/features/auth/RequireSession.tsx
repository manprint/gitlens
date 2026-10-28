import type { ReactNode } from 'react'

import { useQueryClient } from '@tanstack/react-query'
import { Navigate, useNavigate } from 'react-router-dom'

import { configureAuth, useSession } from '@/api/auth'
import { Logo } from '@/components/layout/Logo'
import { Button } from '@/components/ui/button'

interface RequireSessionProps {
  children: ReactNode
}

export default function RequireSession({ children }: RequireSessionProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  configureAuth(queryClient, navigate)
  const session = useSession()

  if (session.error?.kind === 'unauthorized') {
    return <Navigate replace to="/login" />
  }

  // Window focus and reconnect refetch a failed check, but an operator who stays
  // on this screen otherwise has no way to ask again from the page.
  if (session.error) {
    return (
      <main className="bg-background flex min-h-screen items-center justify-center p-6">
        <div
          className="bg-card shadow-card w-full max-w-sm space-y-4 rounded-xl border p-6 text-center"
          role="alert"
        >
          <Logo className="text-foreground justify-center" />
          <p className="text-foreground font-medium">Impossibile verificare la sessione.</p>
          <p className="text-muted-foreground text-sm">
            Il server non ha risposto. Controlla la connessione e riprova.
          </p>
          <Button className="w-full" onClick={() => void session.refetch()} type="button">
            Riprova
          </Button>
        </div>
      </main>
    )
  }

  if (session.isPending || !session.data) {
    return (
      <main
        aria-busy="true"
        aria-label="Caricamento sessione"
        className="bg-background flex min-h-screen items-center justify-center"
        data-testid="session-loading"
      >
        <div aria-hidden="true" className="flex flex-col items-center gap-4">
          <Logo className="text-foreground" />
          <span className="border-primary/25 border-t-primary size-5 animate-spin rounded-full border-2" />
        </div>
      </main>
    )
  }

  if (session.data?.configured === false) {
    return (
      <main className="bg-background flex min-h-screen items-center justify-center p-6">
        <div className="bg-card shadow-card w-full max-w-md space-y-4 rounded-xl border p-8">
          <Logo className="text-foreground" />
          <h1 className="text-foreground text-xl font-semibold">Interfaccia non configurata</h1>
          <p className="text-muted-foreground text-sm">
            Imposta la password dell’interfaccia nel server usando:
          </p>
          <code className="bg-muted block rounded-lg px-3 py-2 font-mono text-sm">
            PGLENS_UI_PASSWORD
          </code>
        </div>
      </main>
    )
  }

  if (session.data.authenticated === false) {
    return <Navigate replace to="/login" />
  }

  return <>{children}</>
}
