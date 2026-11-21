import { describe, expect, it } from 'vitest'

import {
  logExtraEntries,
  logQueryHref,
  logSeverityVariant,
  logsRequest,
  readLogFilters,
  withLogFilter,
} from './logs'

describe('logs', () => {
  it('reads known filters from the URL and drops unknown ones', () => {
    const filters = readLogFilters(
      new URLSearchParams('class=lock.deadlock&severity=LOUD&db=app&q=%20dead%20&before=x'),
    )
    expect(filters).toEqual({
      class: 'lock.deadlock',
      severity: '',
      database: 'app',
      q: 'dead',
      before: 'x',
    })
    expect(readLogFilters(new URLSearchParams('class=nope')).class).toBe('')
  })

  it('a filter change starts on the newest lines', () => {
    const params = new URLSearchParams('before=2026-11-21T10:00:00Z&class=panic&range=1h')
    const next = withLogFilter(params, 'severity', 'ERROR')
    expect(next.get('before')).toBeNull()
    expect(next.get('range')).toBe('1h')
    expect(withLogFilter(next, 'class', '').has('class')).toBe(false)
    expect(withLogFilter(params, 'before', 'y').get('class')).toBe('panic')
  })

  it('leaves empty filters out of the request', () => {
    expect(
      logsRequest('i', { class: '', severity: 'ERROR', database: '', q: 'x', before: '' }),
    ).toEqual({ instance_id: 'i', limit: 100, severity: 'ERROR', q: 'x' })
  })

  it('sorts extra fields and links a query', () => {
    expect(logExtraEntries({ statement: 's', detail: 'd', hint: '' })).toEqual([
      ['detail', 'd'],
      ['statement', 's'],
    ])
    expect(logExtraEntries(null)).toEqual([])
    expect(logQueryHref('a b', '12')).toBe('/instances/a%20b/queries/12')
    expect(logQueryHref('a', '0')).toBeNull()
    expect(logQueryHref('a', null)).toBeNull()
    expect(logSeverityVariant('PANIC')).toBe('destructive')
    expect(logSeverityVariant('WARNING')).toBe('warning')
    expect(logSeverityVariant('LOG')).toBe('outline')
  })
})
