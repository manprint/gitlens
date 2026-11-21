import { describe, expect, it } from 'vitest'

import { shortTraceId, traceLink } from './traceLink'

const ID = '5bd66ef5095369c7b0d1f8f4bd33716a'

describe('UI-QTAG-002 traceLink', () => {
  it('replaces the placeholder of an http(s) template', () => {
    expect(traceLink('https://tracing.example.com/trace/{trace_id}', ID)).toBe(
      `https://tracing.example.com/trace/${ID}`,
    )
    expect(traceLink('http://jaeger:16686/trace/{trace_id}', ID)).toBe(
      `http://jaeger:16686/trace/${ID}`,
    )
  })

  it('replaces every placeholder', () => {
    expect(traceLink('https://t.example/{trace_id}?q={trace_id}', ID)).toBe(
      `https://t.example/${ID}?q=${ID}`,
    )
  })

  it('refuses a bad id', () => {
    for (const bad of ['xyz', ID.toUpperCase(), `${ID}0`, '', null, undefined]) {
      expect(traceLink('https://t.example/{trace_id}', bad)).toBeNull()
    }
  })

  it('refuses a bad template', () => {
    for (const bad of [
      'javascript:alert("{trace_id}")',
      'ftp://t.example/{trace_id}',
      '/trace/{trace_id}',
      'https://t.example/trace',
      '',
      null,
    ]) {
      expect(traceLink(bad, ID)).toBeNull()
    }
  })

  it('shortens to the first 8 characters', () => {
    expect(shortTraceId(ID)).toBe('5bd66ef5')
  })
})
