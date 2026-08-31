import createClient from 'openapi-fetch'

import type { paths } from './generated'

export const client = createClient<paths>({
  baseUrl: '/',
  credentials: 'same-origin',
  headers: { Accept: 'application/json' },
})

export type ApiFailure =
  | { kind: 'unauthorized' }
  | { kind: 'forbidden' }
  | { kind: 'not_found' }
  | { kind: 'unprocessable'; error: string; detail: string }
  | { kind: 'server'; status: number; error: string; detail: string }
  | { kind: 'network'; message: string }
  | { kind: 'malformed'; message: string }

interface ErrorEnvelope {
  error: string
  detail: string
}

function isErrorEnvelope(body: unknown): body is ErrorEnvelope {
  if (typeof body !== 'object' || body === null) {
    return false
  }
  const record = body as Record<string, unknown>
  return typeof record.error === 'string' && typeof record.detail === 'string'
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}

export function toApiFailure(response: unknown, body: unknown): ApiFailure {
  if (response instanceof TypeError) {
    return { kind: 'network', message: errorMessage(response, 'Network request failed') }
  }
  if (response instanceof SyntaxError) {
    return { kind: 'malformed', message: errorMessage(response, 'Malformed JSON response') }
  }
  if (!(response instanceof Response)) {
    return { kind: 'malformed', message: 'Malformed API response' }
  }

  switch (response.status) {
    case 401:
      return { kind: 'unauthorized' }
    case 403:
      return { kind: 'forbidden' }
    case 404:
      return { kind: 'not_found' }
  }

  if (!isErrorEnvelope(body)) {
    return { kind: 'malformed', message: 'API error response is not a valid error envelope' }
  }

  if (response.status === 422) {
    return { kind: 'unprocessable', error: body.error, detail: body.detail }
  }
  return {
    kind: 'server',
    status: response.status,
    error: body.error,
    detail: body.detail,
  }
}

/**
 * Parse responses from /api/v1/statements, /api/v1/ash, /api/v1/ash/top, and
 * /api/v1/plans. Those are the only API endpoints carrying queryid. Rewriting
 * unsafe integer literals before JSON.parse preserves their exact string form.
 */
export function parseLargeIntStrings(raw: string): unknown {
  const rewritten = raw.replace(/(?<!\\)("queryid"\s*:\s*)(-?\d{16,})(?=\s*[,}])/g, '$1"$2"')
  return JSON.parse(rewritten) as unknown
}
