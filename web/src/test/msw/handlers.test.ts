import { describe, expect, it } from 'vitest'

import { CLUSTER_ID } from '../fixture-helpers'
import { base as clusters, empty as emptyClusters } from '../fixtures/getClusters'
import { base as topology } from '../fixtures/getClusterTopology'
import { server } from './server'
import { ok, sequence } from './handlers'

const apiUrl = (path: string) => `http://localhost${path}`

async function readJson(response: Response): Promise<unknown> {
  const parsed: unknown = JSON.parse(await response.text()) as unknown
  return parsed
}

describe('MSW handler factory', () => {
  it('resolves the URL pattern from the operationId', async () => {
    server.use(ok('getClusterTopology', topology))

    const response = await globalThis.fetch(apiUrl(`/api/v1/clusters/${CLUSTER_ID}/topology`))
    const payload = await readJson(response)

    expect(response.status).toBe(200)
    expect(payload).toEqual(topology)
  })

  it('rejects a body that violates the contract', () => {
    const invalid = [{ ...clusters[0], cluster_id: 123 }]

    expect(() => ok('getClusters', invalid)).toThrow('/0/cluster_id')
  })

  it('sequence returns each response in order', async () => {
    server.use(sequence('getClusters', clusters, emptyClusters))

    const first = await globalThis.fetch(apiUrl('/api/v1/clusters'))
    const second = await globalThis.fetch(apiUrl('/api/v1/clusters'))
    const firstPayload = await readJson(first)
    const secondPayload = await readJson(second)

    expect(first.status).toBe(200)
    expect(second.status).toBe(200)
    expect(firstPayload).toEqual(clusters)
    expect(secondPayload).toEqual(emptyClusters)
  })

  it('an unhandled request fails the test', async () => {
    const response = await globalThis.fetch(apiUrl('/api/v1/not-declared'))
    const payload = await readJson(response)

    expect(response.status).toBe(500)
    expect(payload).toMatchObject({ name: 'Error' })
  })
})
