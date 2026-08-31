import type { ReactNode } from 'react'

import { useQueryClient } from '@tanstack/react-query'
import { Navigate, useNavigate } from 'react-router-dom'

import { configureAuth, useSession } from '@/api/auth'

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

  if (session.error) {
    return <main role="alert">Impossibile verificare la sessione.</main>
  }

  if (session.isPending || !session.data) {
    return (
      <main aria-busy="true" aria-label="Caricamento sessione" data-testid="session-loading">
        <div aria-hidden="true" style={{ minHeight: '100vh' }} />
      </main>
    )
  }

  if (session.data?.configured === false) {
    return (
      <main>
        <h1>Interfaccia non configurata</h1>
        <p>Imposta la password dell’interfaccia nel server usando:</p>
        <code>PGLENS_UI_PASSWORD</code>
      </main>
    )
  }

  if (session.data.authenticated === false) {
    return <Navigate replace to="/login" />
  }

  return <>{children}</>
}
