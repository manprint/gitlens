import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'

import { client, toApiFailure, type ApiFailure } from './client'
import type { components } from './generated'
import { qk } from './keys'
import { REFRESH } from './policy'
import {
  COMMAND_CLIENT_TIMEOUT_MS,
  describeCommandState,
  isTerminalCommandState,
  type CommandState,
} from '@/lib/commands'

export type CommandKind = components['schemas']['CreateCommandRequest']['kind']
export type CommandArgs = components['schemas']['CommandArgs']
export type GeneratedCommandResponse = components['schemas']['CommandResponse']
export type CommandResponse = Omit<GeneratedCommandResponse, 'state'> & { state: CommandState }
export type CreateCommandResponse = components['schemas']['CreateCommandResponse']

export interface CreateCommandVariables {
  kind: CommandKind
  args: CommandArgs
  instanceId?: string
}

interface ClientResponse<T> {
  data?: T
  error?: unknown
  response: Response
}

class CommandRequestError extends Error {
  readonly failure: ApiFailure
  readonly kind: ApiFailure['kind']

  constructor(failure: ApiFailure) {
    super('message' in failure ? failure.message : `API request failed: ${failure.kind}`)
    this.name = 'CommandRequestError'
    this.failure = failure
    this.kind = failure.kind
    Object.assign(this, failure)
  }
}

function isCommandRequestError(error: unknown): error is CommandRequestError {
  return error instanceof CommandRequestError
}

async function requestApi<T>(run: () => Promise<ClientResponse<T>>): Promise<T> {
  try {
    const { data, error, response } = await run()
    if (error !== undefined) {
      throw new CommandRequestError(toApiFailure(response, error))
    }
    if (data === undefined) {
      throw new CommandRequestError({
        kind: 'malformed',
        message: 'API response did not contain data',
      })
    }
    return data
  } catch (error) {
    if (isCommandRequestError(error)) {
      throw error
    }
    throw new CommandRequestError(toApiFailure(error, null))
  }
}

function shouldRetry(failureCount: number, error: unknown): boolean {
  return isCommandRequestError(error) && error.kind === 'network' && failureCount < 2
}

function normaliseCommand(data: GeneratedCommandResponse): CommandResponse {
  return { ...data, state: data.state }
}

export function useCommand(commandId: string) {
  const [expiry, setExpiry] = useState(() => ({ commandId, expired: false }))
  const clientExpired = expiry.commandId === commandId && expiry.expired
  const startedAt = useRef<number | null>(null)
  const startedFor = useRef<string | null>(null)

  const query = useQuery<CommandResponse, ApiFailure>({
    enabled: Boolean(commandId) && !clientExpired,
    queryFn: async () =>
      normaliseCommand(
        await requestApi(() =>
          client.GET('/api/v1/commands/{id}', { params: { path: { id: commandId } } }),
        ),
      ),
    queryKey: qk.command(commandId),
    refetchInterval: (current) => {
      if (clientExpired || isTerminalCommandState(current.state.data?.state)) {
        return false
      }
      return REFRESH.command.interval
    },
    retry: shouldRetry,
    retryDelay: 0,
  })

  useEffect(() => {
    if (!commandId || isTerminalCommandState(query.data?.state)) {
      return
    }
    if (startedFor.current !== commandId) {
      startedFor.current = commandId
      startedAt.current = Date.now()
    }
    const began = startedAt.current ?? Date.now()
    const remaining = COMMAND_CLIENT_TIMEOUT_MS - (Date.now() - began)
    if (remaining <= 0) {
      setExpiry({ commandId, expired: true })
      return
    }
    const timer = window.setTimeout(() => setExpiry({ commandId, expired: true }), remaining)
    return () => window.clearTimeout(timer)
  }, [commandId, query.data?.state])

  const data =
    clientExpired && query.data !== undefined && !isTerminalCommandState(query.data.state)
      ? {
          ...query.data,
          state: 'expired' as const,
          error: describeCommandState('expired'),
        }
      : query.data

  return { ...query, data, clientExpired }
}

export function useCreateCommand(defaultInstanceId?: string): UseMutationResult<
  CreateCommandResponse,
  CommandRequestError,
  CreateCommandVariables
> & {
  commandId: string | undefined
  command: ReturnType<typeof useCommand>
} {
  const [commandId, setCommandId] = useState<string>()
  const mutation = useMutation<CreateCommandResponse, CommandRequestError, CreateCommandVariables>({
    mutationFn: async ({ instanceId, kind, args }) => {
      const targetInstanceId = instanceId ?? defaultInstanceId
      if (!targetInstanceId) {
        throw new CommandRequestError({ kind: 'malformed', message: 'instanceId is required' })
      }
      return requestApi(() =>
        client.POST('/api/v1/instances/{id}/commands', {
          params: { path: { id: targetInstanceId } },
          body: { kind, args },
        }),
      )
    },
    onMutate: () => setCommandId(undefined),
    onSuccess: (response) => setCommandId(response.command_id),
    retry: false,
  })
  const command = useCommand(commandId ?? '')
  const queryClient = useQueryClient()
  const reset = useCallback(() => {
    setCommandId(undefined)
    mutation.reset()
    if (commandId) {
      queryClient.removeQueries({ queryKey: qk.command(commandId) })
    }
  }, [commandId, mutation, queryClient])

  return { ...mutation, reset, commandId, command }
}
