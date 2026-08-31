import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query'
import { useNavigate, type NavigateFunction } from 'react-router-dom'

import { client, toApiFailure, type ApiFailure } from './client'
import type { components } from './generated'
import { qk } from './keys'
import { REFRESH } from './policy'

export type SessionStatus = components['schemas']['SessionStatus']

export class AuthRequestError extends Error {
  readonly failure: ApiFailure
  readonly kind: ApiFailure['kind']

  constructor(failure: ApiFailure) {
    super('message' in failure ? failure.message : `API request failed: ${failure.kind}`)
    this.name = 'AuthRequestError'
    this.failure = failure
    this.kind = failure.kind
  }
}

interface AuthRuntime {
  navigate?: NavigateFunction
  queryClient?: ReturnType<typeof useQueryClient>
}

let authRuntime: AuthRuntime = {}
let unauthorizedLatched = false

function currentLocation(): string {
  if (typeof window === 'undefined') {
    return '/'
  }
  return `${window.location.pathname}${window.location.search}${window.location.hash}`
}

function loginLocation(): string {
  return `/login?next=${encodeURIComponent(currentLocation())}`
}

function isSessionStatus(value: unknown): value is SessionStatus {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { authenticated?: unknown }).authenticated === 'boolean'
  )
}

function setUnauthenticated(): void {
  const queryClient = authRuntime.queryClient
  queryClient?.clear()
  queryClient?.setQueryData(qk.session(), { authenticated: false, configured: true })
}

/** Handle one expired session and suppress duplicate redirects until success. */
export function onUnauthorized(): void {
  if (unauthorizedLatched) {
    return
  }

  unauthorizedLatched = true
  setUnauthenticated()
  void authRuntime.navigate?.(loginLocation(), { replace: true })
}

/** Bind the current React Query cache and router navigation to the singleton middleware. */
export function configureAuth(
  queryClient: ReturnType<typeof useQueryClient>,
  navigate?: NavigateFunction,
): void {
  if (authRuntime.queryClient !== queryClient) {
    unauthorizedLatched = false
  }
  const navigateHandler =
    navigate ?? (authRuntime.queryClient === queryClient ? authRuntime.navigate : undefined)
  authRuntime = { queryClient }
  if (navigateHandler !== undefined) {
    authRuntime.navigate = navigateHandler
  }
}

client.use({
  onResponse: ({ request, response }) => {
    const isSessionRequest = new URL(request.url).pathname === '/api/v1/session'

    if (response.status === 401 && !isSessionRequest) {
      onUnauthorized()
    } else if (response.ok) {
      unauthorizedLatched = false
    }
    return response
  },
})

async function fetchSession(): Promise<SessionStatus> {
  try {
    const result = await client.GET('/api/v1/session')
    if (result.response.status === 401 && isSessionStatus(result.error)) {
      return result.error
    }
    if (result.error !== undefined) {
      throw new AuthRequestError(toApiFailure(result.response, result.error))
    }
    if (result.data === undefined) {
      throw new AuthRequestError({
        kind: 'malformed',
        message: 'API response did not contain data',
      })
    }
    return result.data
  } catch (error) {
    if (error instanceof AuthRequestError) {
      throw error
    }
    throw new AuthRequestError(toApiFailure(error, null))
  }
}

async function createSession(password: string): Promise<void> {
  try {
    const result = await client.POST('/api/v1/session', { body: { password } })
    if (result.error !== undefined) {
      throw new AuthRequestError(toApiFailure(result.response, result.error))
    }
  } catch (error) {
    if (error instanceof AuthRequestError) {
      throw error
    }
    throw new AuthRequestError(toApiFailure(error, null))
  }
}

async function deleteSession(): Promise<void> {
  try {
    const result = await client.DELETE('/api/v1/session')
    if (result.error !== undefined) {
      throw new AuthRequestError(toApiFailure(result.response, result.error))
    }
  } catch (error) {
    if (error instanceof AuthRequestError) {
      throw error
    }
    throw new AuthRequestError(toApiFailure(error, null))
  }
}

export function useSignIn(): UseMutationResult<void, AuthRequestError, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: createSession,
    onSuccess: () => {
      queryClient.setQueryData(qk.session(), { authenticated: true, configured: true })
    },
    retry: false,
  })
}

export function useSignOut(): UseMutationResult<void, AuthRequestError, void> {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: deleteSession,
    onSettled: () => {
      queryClient.clear()
      void navigate?.('/login', { replace: true })
    },
    retry: false,
  })
}

export function useSession() {
  const queryClient = useQueryClient()
  configureAuth(queryClient)
  const query = useQuery<SessionStatus, AuthRequestError>({
    queryFn: fetchSession,
    queryKey: qk.session(),
    refetchInterval: REFRESH.static.interval === 0 ? false : REFRESH.static.interval,
    retry: false,
  })
  const signInMutation = useSignIn()
  const signOutMutation = useSignOut()

  return {
    ...query,
    signIn: (password: string) => signInMutation.mutateAsync(password),
    signInMutation,
    signOut: () => signOutMutation.mutateAsync(),
    signOutMutation,
  }
}
