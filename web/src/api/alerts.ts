import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query'

import { client, toApiFailure, type ApiFailure } from './client'
import type { components } from './generated'
import { qk } from './keys'

export type AlertRule = components['schemas']['AlertRule'] & {
  enabled?: boolean
  threshold?: number
  for_seconds?: number
  tier?: number
}

export type AlertRuleUpdate = components['schemas']['AlertRuleUpdate']
export type AlertRuleUpdateResponse = components['schemas']['AlertRuleUpdateResponse']

export interface UpdateAlertRuleVariables {
  ruleId: string
  update: AlertRuleUpdate
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

export class AlertRuleMutationError extends Error {
  readonly failure: ApiFailure

  constructor(failure: ApiFailure) {
    super(failureMessage(failure))
    this.name = 'AlertRuleMutationError'
    this.failure = failure
  }
}

interface ClientResponse<T> {
  data?: T
  error?: unknown
  response: Response
}

async function requestData<T>(run: () => Promise<ClientResponse<T>>): Promise<T> {
  try {
    const { data, error, response } = await run()
    if (error !== undefined) {
      throw new AlertRuleMutationError(toApiFailure(response, error))
    }
    if (data === undefined) {
      throw new AlertRuleMutationError({
        kind: 'malformed',
        message: 'API response did not contain data',
      })
    }
    return data
  } catch (error) {
    if (error instanceof AlertRuleMutationError) {
      throw error
    }
    throw new AlertRuleMutationError(toApiFailure(error, null))
  }
}

export function useUpdateAlertRule(): UseMutationResult<
  AlertRuleUpdateResponse,
  AlertRuleMutationError,
  UpdateAlertRuleVariables
> {
  const queryClient = useQueryClient()

  return useMutation<AlertRuleUpdateResponse, AlertRuleMutationError, UpdateAlertRuleVariables>({
    mutationFn: ({ ruleId, update }) =>
      requestData(() =>
        client.PUT('/api/v1/alert-rules/{rule_id}', {
          params: { path: { rule_id: ruleId } },
          body: update,
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: qk.alertRules() })
    },
    retry: false,
  })
}
