import { useMutation, useQueryClient } from '@tanstack/react-query'

import { client, toApiFailure, type ApiFailure } from './client'
import type { components } from './generated'
import { qk } from './keys'

type MuteFindingResponse = components['schemas']['MuteFindingResponse']

interface ClientResponse<T> {
  data?: T
  error?: unknown
  response: Response
}

interface MuteFindingVariables {
  findingId: string
  reason: string
  until: string
}

interface UnmuteFindingVariables {
  findingId: string
}

function failureMessage(failure: ApiFailure): string {
  switch (failure.kind) {
    case 'unprocessable':
    case 'server':
      return `${failure.error}: ${failure.detail}`
    case 'network':
    case 'malformed':
      return failure.message
    case 'unauthorized':
    case 'forbidden':
    case 'not_found':
      return `API request failed: ${failure.kind}`
  }
}

class FindingMutationError extends Error {
  readonly failure: ApiFailure

  constructor(failure: ApiFailure) {
    super(failureMessage(failure))
    this.name = 'FindingMutationError'
    this.failure = failure
  }
}

async function requestData<T>(run: () => Promise<ClientResponse<T>>): Promise<T> {
  try {
    const { data, error, response } = await run()
    if (error !== undefined) {
      throw new FindingMutationError(toApiFailure(response, error))
    }
    if (data === undefined) {
      throw new FindingMutationError({ kind: 'malformed', message: 'API response did not contain data' })
    }
    return data
  } catch (error) {
    if (error instanceof FindingMutationError) {
      throw error
    }
    throw new FindingMutationError(toApiFailure(error, null))
  }
}

async function requestEmpty(run: () => Promise<ClientResponse<unknown>>): Promise<void> {
  try {
    const { error, response } = await run()
    if (error !== undefined) {
      throw new FindingMutationError(toApiFailure(response, error))
    }
  } catch (error) {
    if (error instanceof FindingMutationError) {
      throw error
    }
    throw new FindingMutationError(toApiFailure(error, null))
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isUnknownArray(value: unknown): value is unknown[] {
  return Array.isArray(value)
}

function updateFindingCache(
  queryClient: ReturnType<typeof useQueryClient>,
  findingId: string,
  update: Record<string, unknown>,
) {
  const updateValue = (value: unknown) => {
    if (!isRecord(value) || value.finding_id !== findingId) {
      return value
    }
    return { ...value, ...update }
  }

  queryClient.setQueriesData<unknown>({ queryKey: ['getFindings'] }, (current: unknown) => {
    return isUnknownArray(current) ? current.map((value: unknown) => updateValue(value)) : current
  })
  queryClient.setQueryData<unknown>(qk.finding(findingId), updateValue)
}

function invalidateFindingQueries(
  queryClient: ReturnType<typeof useQueryClient>,
  findingId: string,
) {
  void queryClient.invalidateQueries({ queryKey: ['getFindings'] })
  void queryClient.invalidateQueries({ queryKey: qk.finding(findingId) })
}

export function useMuteFinding() {
  const queryClient = useQueryClient()

  return useMutation<MuteFindingResponse, FindingMutationError, MuteFindingVariables>({
    mutationFn: ({ findingId, reason, until }) =>
      requestData(() =>
        client.POST('/api/v1/findings/{finding-id}/mute', {
          params: { path: { 'finding-id': findingId } },
          body: { reason, until },
        }),
      ),
    onSuccess: (response) => {
      updateFindingCache(queryClient, response.finding_id, {
        state: response.state,
        muted_until: response.muted_until,
        mute_reason: response.mute_reason,
      })
      invalidateFindingQueries(queryClient, response.finding_id)
    },
    retry: false,
  })
}

export function useUnmuteFinding() {
  const queryClient = useQueryClient()

  return useMutation<void, FindingMutationError, UnmuteFindingVariables>({
    mutationFn: ({ findingId }) =>
      requestEmpty(() =>
        client.DELETE('/api/v1/findings/{finding-id}/mute', {
          params: { path: { 'finding-id': findingId } },
        }),
      ),
    onSuccess: (_response, { findingId }) => {
      queryClient.setQueriesData<unknown>({ queryKey: ['getFindings'] }, (current: unknown) => {
        if (!isUnknownArray(current)) return current

        return current.map((value: unknown) => {
          if (!isRecord(value) || value.finding_id !== findingId) return value
          return {
            ...value,
            state: value.resolved_at ? 'resolved' : 'open',
            muted_until: null,
            mute_reason: null,
          }
        })
      })
      queryClient.setQueryData<unknown>(qk.finding(findingId), (current: unknown) => {
        if (!isRecord(current)) return current
        return {
          ...current,
          state: current.resolved_at ? 'resolved' : 'open',
          muted_until: null,
          mute_reason: null,
        }
      })
      invalidateFindingQueries(queryClient, findingId)
    },
    retry: false,
  })
}
