import { describe, expect, it } from 'vitest'

import { base as clusters } from './fixtures/getClusters'
import { assertMatchesContract } from './contract'

interface FixtureModule {
  responseStatus?: unknown
  [name: string]: unknown
}

const fixtureModules = import.meta.glob<FixtureModule>('./fixtures/*.ts', {
  eager: true,
})

describe('OpenAPI fixtures', () => {
  for (const [file, fixture] of Object.entries(fixtureModules)) {
    const operationId = file.split('/').pop()?.replace(/\.ts$/, '') ?? file
    const statusCode = typeof fixture.responseStatus === 'number' ? fixture.responseStatus : 200

    for (const [name, value] of Object.entries(fixture)) {
      if (name === 'responseStatus' || name.startsWith('make') || typeof value === 'function') {
        continue
      }

      it(`${operationId}.${name} matches the published response`, () => {
        expect(() => assertMatchesContract(operationId, statusCode, value)).not.toThrow()
      })
    }
  }
})

describe('assertMatchesContract', () => {
  it('resolves $ref schemas', () => {
    expect(() => assertMatchesContract('getClusters', 200, clusters)).not.toThrow()
  })

  it('reports the instancePath of the failing field', () => {
    const invalid = [{ ...clusters[0], cluster_id: 123 }]

    expect(() => assertMatchesContract('getClusters', 200, invalid)).toThrow('/0/cluster_id')
  })

  it('rejects a fixture with a wrong field type', () => {
    expect(() => assertMatchesContract('getSession', 200, { authenticated: 'yes' })).toThrow(
      '/authenticated',
    )
  })

  it('throws for an unknown operationId', () => {
    expect(() => assertMatchesContract('missingOperation', 200, {})).toThrow(
      'Unknown OpenAPI operationId',
    )
  })
})
