import { describe, expect, it } from 'vitest'

import { parseLargeIntStrings, toApiFailure } from './client'

describe('toApiFailure', () => {
  it('UI-API-001 maps 401 to unauthorized', () => {
    expect(toApiFailure(new Response(null, { status: 401 }), null)).toEqual({
      kind: 'unauthorized',
    })
  })

  it('UI-API-002 maps 422 with the error envelope fields', () => {
    expect(
      toApiFailure(new Response(null, { status: 422 }), {
        error: 'invalid_range',
        detail: 'from must precede to',
      }),
    ).toEqual({
      kind: 'unprocessable',
      error: 'invalid_range',
      detail: 'from must precede to',
    })
  })

  it('UI-API-003 maps a thrown TypeError to network', () => {
    expect(toApiFailure(new TypeError('Failed to fetch'), null)).toEqual({
      kind: 'network',
      message: 'Failed to fetch',
    })
  })

  it('UI-API-004 maps invalid JSON to malformed', () => {
    expect(toApiFailure(new SyntaxError('Unexpected end of JSON input'), null)).toEqual({
      kind: 'malformed',
      message: 'Unexpected end of JSON input',
    })
  })

  it('maps an empty network error to the fallback message', () => {
    expect(toApiFailure(new TypeError(), null)).toEqual({
      kind: 'network',
      message: 'Network request failed',
    })
  })

  it('maps an empty JSON error to the fallback message', () => {
    expect(toApiFailure(new SyntaxError(), null)).toEqual({
      kind: 'malformed',
      message: 'Malformed JSON response',
    })
  })

  it('maps 403 to forbidden', () => {
    expect(toApiFailure(new Response(null, { status: 403 }), null)).toEqual({
      kind: 'forbidden',
    })
  })

  it('maps 404 to not_found', () => {
    expect(toApiFailure(new Response(null, { status: 404 }), null)).toEqual({
      kind: 'not_found',
    })
  })

  it('maps a valid non-422 error envelope to server', () => {
    expect(
      toApiFailure(new Response(null, { status: 500 }), {
        error: 'internal_error',
        detail: 'database unavailable',
      }),
    ).toEqual({
      kind: 'server',
      status: 500,
      error: 'internal_error',
      detail: 'database unavailable',
    })
  })

  it('maps an invalid error envelope to malformed', () => {
    expect(toApiFailure(new Response(null, { status: 400 }), { error: 'bad_request' })).toEqual({
      kind: 'malformed',
      message: 'API error response is not a valid error envelope',
    })
  })

  it('maps an unknown response value to malformed', () => {
    expect(toApiFailure({}, null)).toEqual({
      kind: 'malformed',
      message: 'Malformed API response',
    })
  })
})

describe('parseLargeIntStrings', () => {
  it('UI-API-005 preserves 9007199254740993 as a string', () => {
    const raw = '{"queryid":9007199254740993}'

    expect(JSON.parse(raw)).toEqual({ queryid: 9007199254740992 })
    expect(parseLargeIntStrings(raw)).toEqual({ queryid: '9007199254740993' })
  })

  it('leaves other numbers untouched', () => {
    expect(parseLargeIntStrings('{"queryid":42,"samples":7}')).toEqual({
      queryid: 42,
      samples: 7,
    })
  })

  it('does not corrupt a queryid inside a query text string', () => {
    const raw = '{"query_text":"select \\"queryid\\": 9007199254740993","queryid":9007199254740993}'

    expect(parseLargeIntStrings(raw)).toEqual({
      query_text: 'select "queryid": 9007199254740993',
      queryid: '9007199254740993',
    })
  })
})
