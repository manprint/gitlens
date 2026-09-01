import { useMutation, useQueryClient } from '@tanstack/react-query'

import { client, toApiFailure, type ApiFailure } from './client'
import type { components } from './generated'

type Silence = components['schemas']['Silence']
type CreateSilenceRequest = components['schemas']['CreateSilenceRequest']

interface ClientResponse<T> {
  data?: T
  error?: unknown
  response: Response
}

export type CreateSilenceVariables = CreateSilenceRequest

export interface DeleteSilenceVariables {
  silenceId: string
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

export class SilenceMutationError extends Error {
  readonly failure: ApiFailure

  constructor(failure: ApiFailure) {
    super(failureMessage(failure))
    this.name = 'SilenceMutationError'
    this.failure = failure
  }
}

async function requestData<T>(run: () => Promise<ClientResponse<T>>): Promise<T> {
  try {
    const { data, error, response } = await run()
    if (error !== undefined) {
      throw new SilenceMutationError(toApiFailure(response, error))
    }
    if (data === undefined) {
      throw new SilenceMutationError({
        kind: 'malformed',
        message: 'API response did not contain data',
      })
    }
    return data
  } catch (error) {
    if (error instanceof SilenceMutationError) throw error
    throw new SilenceMutationError(toApiFailure(error, null))
  }
}

async function requestEmpty(run: () => Promise<ClientResponse<unknown>>): Promise<void> {
  try {
    const { error, response } = await run()
    if (error !== undefined) {
      throw new SilenceMutationError(toApiFailure(response, error))
    }
  } catch (error) {
    if (error instanceof SilenceMutationError) throw error
    throw new SilenceMutationError(toApiFailure(error, null))
  }
}

function invalidateSilenceQueries(queryClient: ReturnType<typeof useQueryClient>) {
  void queryClient.invalidateQueries({ queryKey: ['getSilences'] })
  void queryClient.invalidateQueries({ queryKey: ['getAlerts'] })
}

export function useCreateSilence() {
  const queryClient = useQueryClient()

  return useMutation<Silence, SilenceMutationError, CreateSilenceVariables>({
    mutationFn: (body) =>
      requestData(() =>
        client.POST('/api/v1/silences', {
          body,
        }),
      ),
    onSuccess: () => invalidateSilenceQueries(queryClient),
    retry: false,
  })
}

export function useDeleteSilence() {
  const queryClient = useQueryClient()

  return useMutation<void, SilenceMutationError, DeleteSilenceVariables>({
    mutationFn: ({ silenceId }) =>
      requestEmpty(() =>
        client.DELETE('/api/v1/silences/{id}', {
          params: { path: { id: silenceId } },
        }),
      ),
    onSuccess: () => invalidateSilenceQueries(queryClient),
    retry: false,
  })
}
