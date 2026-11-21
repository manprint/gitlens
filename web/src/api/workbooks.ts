import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { client, toApiFailure, type ApiFailure } from './client'
import type { components } from './generated'
import { REFRESH } from './policy'

export type Workbook = components['schemas']['Workbook']
export type WorkbookVariant = components['schemas']['WorkbookVariant']
export type WorkbookVariantRequest = components['schemas']['WorkbookVariantRequest']
export type CreateWorkbookRequest = components['schemas']['CreateWorkbookRequest']

interface ClientResponse<T> {
  data?: T
  error?: unknown
  response: Response
}

/** A failed workbook request; `failure` keeps the server's error and detail. */
export class WorkbookRequestError extends Error {
  readonly failure: ApiFailure

  constructor(failure: ApiFailure) {
    super(
      'error' in failure
        ? `${failure.error}: ${failure.detail}`
        : 'message' in failure
          ? failure.message
          : failure.kind,
    )
    this.name = 'WorkbookRequestError'
    this.failure = failure
  }

  /** The server's error title (`limit_reached`, `variant_pending`, …), if any. */
  get code(): string | undefined {
    return 'error' in this.failure ? this.failure.error : undefined
  }
}

async function request<T>(run: () => Promise<ClientResponse<T>>): Promise<T> {
  try {
    const { data, error, response } = await run()
    if (error !== undefined) throw new WorkbookRequestError(toApiFailure(response, error))
    return data as T
  } catch (error) {
    if (error instanceof WorkbookRequestError) throw error
    throw new WorkbookRequestError(toApiFailure(error, null))
  }
}

export const workbookKeys = {
  list: (instanceId: string, queryid: string) => ['getWorkbooks', instanceId, queryid] as const,
  one: (id: string) => ['getWorkbook', id] as const,
}

export function useWorkbooks(instanceId: string, queryid: string, enabled = true) {
  return useQuery<Workbook[], WorkbookRequestError>({
    enabled: enabled && Boolean(instanceId && queryid),
    queryKey: workbookKeys.list(instanceId, queryid),
    queryFn: async ({ signal }) =>
      (
        await request(() =>
          client.GET('/api/v1/workbooks', {
            params: { query: { instance_id: instanceId, queryid } },
            signal,
          }),
        )
      ).workbooks,
  })
}

/** One workbook; polls like the EXPLAIN action while a variant is pending. */
export function useWorkbook(id: string) {
  return useQuery<Workbook, WorkbookRequestError>({
    enabled: Boolean(id),
    queryKey: workbookKeys.one(id),
    queryFn: ({ signal }) =>
      request(() => client.GET('/api/v1/workbooks/{id}', { params: { path: { id } }, signal })),
    refetchInterval: (query) =>
      query.state.data?.variants.some((variant) => variant.status === 'pending')
        ? REFRESH.command.interval
        : false,
  })
}

function useInvalidate(id?: string) {
  const queryClient = useQueryClient()
  return () => {
    void queryClient.invalidateQueries({ queryKey: ['getWorkbooks'] })
    if (id) void queryClient.invalidateQueries({ queryKey: workbookKeys.one(id) })
  }
}

export function useCreateWorkbook() {
  const invalidate = useInvalidate()
  return useMutation<Workbook, WorkbookRequestError, CreateWorkbookRequest>({
    mutationFn: (body) => request(() => client.POST('/api/v1/workbooks', { body })),
    onSuccess: invalidate,
  })
}

export function useUpdateWorkbook(id: string) {
  const invalidate = useInvalidate(id)
  return useMutation<Workbook, WorkbookRequestError, { name?: string; notes?: string }>({
    mutationFn: (body) =>
      request(() => client.PATCH('/api/v1/workbooks/{id}', { params: { path: { id } }, body })),
    onSuccess: invalidate,
  })
}

export function useDeleteWorkbook(id: string) {
  const invalidate = useInvalidate()
  return useMutation<unknown, WorkbookRequestError, void>({
    mutationFn: () =>
      request(() => client.DELETE('/api/v1/workbooks/{id}', { params: { path: { id } } })),
    onSuccess: invalidate,
  })
}

/** Adds a variant, or replaces one when `variantId` is given. */
export function useSaveVariant(id: string) {
  const invalidate = useInvalidate(id)
  return useMutation<
    WorkbookVariant,
    WorkbookRequestError,
    { variantId?: string; body: WorkbookVariantRequest }
  >({
    mutationFn: ({ variantId, body }) =>
      variantId
        ? request(() =>
            client.PUT('/api/v1/workbooks/{id}/variants/{variant_id}', {
              params: { path: { id, variant_id: variantId } },
              body,
            }),
          )
        : request(() =>
            client.POST('/api/v1/workbooks/{id}/variants', { params: { path: { id } }, body }),
          ),
    onSuccess: invalidate,
  })
}

export function useDeleteVariant(id: string) {
  const invalidate = useInvalidate(id)
  return useMutation<unknown, WorkbookRequestError, string>({
    mutationFn: (variantId) =>
      request(() =>
        client.DELETE('/api/v1/workbooks/{id}/variants/{variant_id}', {
          params: { path: { id, variant_id: variantId } },
        }),
      ),
    onSuccess: invalidate,
  })
}

/** Queues variants one after the other; a single run is a list of one. */
export function useRunVariants(id: string) {
  const invalidate = useInvalidate(id)
  return useMutation<string[], WorkbookRequestError, readonly string[]>({
    mutationFn: async (variantIds) => {
      const commands: string[] = []
      for (const variantId of variantIds) {
        const accepted = await request(() =>
          client.POST('/api/v1/workbooks/{id}/variants/{variant_id}/run', {
            params: { path: { id, variant_id: variantId } },
          }),
        )
        commands.push(accepted.command_id)
      }
      return commands
    },
    onSettled: invalidate,
  })
}
