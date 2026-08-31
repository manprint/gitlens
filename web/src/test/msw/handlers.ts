import { delay, http, HttpResponse, type HttpHandler } from 'msw'

import {
  assertMatchesContract,
  getOperationRoute,
  hasDocumentedResponse,
  type ContractHttpMethod,
} from '../contract'

export type HandlerInit = Omit<ResponseInit, 'body'>

export interface ResponseDescriptor {
  body?: unknown
  headers?: HeadersInit
  status?: number
  statusText?: string
}

export type SequenceResponse =
  ResponseDescriptor | object | string | number | boolean | null | undefined

type Resolver = Parameters<typeof http.get>[1]

function toMswPath(path: string): string {
  return `*${path.replace(/\{([^}]+)\}/g, ':$1')}`
}

function createResponse(body: unknown, init: HandlerInit): Response {
  if (body === undefined) {
    return new HttpResponse(null, init)
  }
  return HttpResponse.json(body, init)
}

function validateDocumentedBody(operationId: string, statusCode: number, body: unknown): void {
  if (body !== undefined && hasDocumentedResponse(operationId, statusCode)) {
    assertMatchesContract(operationId, statusCode, body)
  }
}

function handlerFor(operationId: string, resolver: Resolver): HttpHandler {
  const route = getOperationRoute(operationId)
  const path = toMswPath(route.path)

  const handlers: Record<ContractHttpMethod, (url: string, callback: Resolver) => HttpHandler> = {
    delete: http.delete,
    get: http.get,
    patch: http.patch,
    post: http.post,
    put: http.put,
  }
  return handlers[route.method](path, resolver)
}

export function ok(operationId: string, body: unknown, init: HandlerInit = {}): HttpHandler {
  const statusCode = init.status ?? 200
  assertMatchesContract(operationId, statusCode, body)
  return handlerFor(operationId, () => createResponse(body, init))
}

export function status(operationId: string, statusCode: number, body?: unknown): HttpHandler {
  validateDocumentedBody(operationId, statusCode, body)
  return handlerFor(operationId, () => createResponse(body, { status: statusCode }))
}

export function slow(operationId: string, body: unknown, delayMs: number): HttpHandler {
  assertMatchesContract(operationId, 200, body)
  return handlerFor(operationId, async () => {
    await delay(delayMs)
    return createResponse(body, { status: 200 })
  })
}

function isResponseDescriptor(value: SequenceResponse): value is ResponseDescriptor {
  return (
    typeof value === 'object' &&
    value !== null &&
    ('body' in value || 'headers' in value || 'status' in value || 'statusText' in value)
  )
}

function normalizeResponse(
  value: SequenceResponse,
): Required<Pick<ResponseDescriptor, 'status'>> & ResponseDescriptor {
  if (isResponseDescriptor(value)) {
    return { ...value, status: value.status ?? 200 }
  }
  return { body: value, status: 200 }
}

export function sequence(operationId: string, ...responses: SequenceResponse[]): HttpHandler {
  if (responses.length === 0) {
    throw new Error(`MSW response sequence for ${operationId} must not be empty`)
  }

  const normalized = responses.map(normalizeResponse)
  for (const response of normalized) {
    validateDocumentedBody(operationId, response.status, response.body)
  }

  let call = 0
  return handlerFor(operationId, () => {
    const response = normalized[Math.min(call++, normalized.length - 1)]
    if (!response) {
      throw new Error(`MSW response sequence for ${operationId} has no current response`)
    }
    const { body, status: statusCode, headers, statusText } = response
    const init: HandlerInit = { status: statusCode }
    if (headers !== undefined) {
      init.headers = headers
    }
    if (statusText !== undefined) {
      init.statusText = statusText
    }
    return createResponse(body, init)
  })
}

export function never(operationId: string): HttpHandler {
  return handlerFor(operationId, async () => {
    await delay('infinite')
    return new HttpResponse(null)
  })
}
