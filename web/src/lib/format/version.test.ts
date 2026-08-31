import { describe, expect, it } from 'vitest'

import { formatPostgresVersion } from './version'

describe('formatPostgresVersion', () => {
  it.each([
    [150011, '15.11'],
    [160002, '16.2'],
    [170011, '17.11'],
    [180000, '18.0'],
  ])('formats %s as %s', (version, expected) => {
    expect(formatPostgresVersion(version)).toBe(expected)
  })

  it('uses the unknown marker for an invalid version', () => {
    expect(formatPostgresVersion(null)).toBe('—')
  })
})
