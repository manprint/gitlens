import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query'
import { useCallback, useRef, useState } from 'react'

import { client, toApiFailure, type ApiFailure } from './client'
import type { components } from './generated'
import { qk } from './keys'
import { REFRESH } from './policy'
import { isTerminalCommandState, type CommandState } from '@/lib/commands'

export type CommandKind = components['schemas']['CreateCommandRequest']['kind']
export type CommandArgs = Omit<components['schemas']['CommandArgs'], 'queryid'> & {
  /** Keep int64 query identifiers lossless across the browser/API boundary. */
  queryid?: number | string
}
export type GeneratedCommandResponse = components['schemas']['CommandResponse']
export type CommandResponse = Omit<GeneratedCommandResponse, 'state'> & { state: CommandState }
export type CreateCommandResponse = components['schemas']['CreateCommandResponse']

// The server reports the same expired state whether or not an agent took the
// command. Once the client has seen a claim, it says so, because the agent may
// still be running the statement and a second run would repeat its work. The
// TTL is set by the server (PGLENS_COMMAND_TTL), so the copy does not name it.
const CLAIMED_THEN_EXPIRED =
  'the agent claimed it, but its TTL ended before a result arrived. It may still be running, so wait before running it again.'

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
  const claimedFor = useRef<string | null>(null)

  const query = useQuery<CommandResponse, ApiFailure>({
    enabled: Boolean(commandId),
    queryFn: async () => {
      const command = normaliseCommand(
        await requestApi(() =>
          client.GET('/api/v1/commands/{id}', { params: { path: { id: commandId } } }),
        ),
      )
      if (command.state === 'claimed') claimedFor.current = commandId
      if (command.state === 'expired' && claimedFor.current === commandId) {
        return { ...command, error: CLAIMED_THEN_EXPIRED }
      }
      return command
    },
    queryKey: qk.command(commandId),
    refetchInterval: (current) => {
      // Only the server ends a command. It expires a pending or claimed command
      // once the TTL has passed, and every GET runs that expiry first, so polling
      // stops on the server's terminal state. The client does not time a command
      // out itself: its TTL can differ from the client's guess, and a command still
      // pending on the server must keep its Run controls disabled.
      if (isTerminalCommandState(current.state.data?.state)) {
        return false
      }
      return REFRESH.command.interval
    },
    retry: shouldRetry,
    retryDelay: 0,
  })

  return query
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
