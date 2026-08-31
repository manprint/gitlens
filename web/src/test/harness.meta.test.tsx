import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from './a11y'
import { CLUSTER } from './fixture-helpers'
import { ok, sequence } from './msw/handlers'
import { renderWithProviders } from './render'
import { NOW } from './time'
import { server } from './msw/server'

const UNHANDLED_URL = 'http://localhost/api/v1/harness-unhandled'

function UnhandledRequestProbe({ onRejected }: { onRejected: (reason: unknown) => void }) {
  useEffect(() => {
    void globalThis.fetch(UNHANDLED_URL).then(() => undefined, onRejected)
  }, [onRejected])

  return null
}

function ClusterCountProbe() {
  const query = useQuery({
    queryKey: ['harness-meta-clusters'],
    queryFn: async () => {
      const response = await globalThis.fetch('http://localhost/api/v1/clusters')
      if (!response.ok) {
        throw new Error(`Cluster request failed with ${response.status}`)
      }
      return (await response.json()) as readonly { name: string }[]
    },
  })

  return <output>cluster {query.data?.[0]?.name ?? 'loading'}</output>
}

function SeriousA11yProbe() {
  return <img src="/logo.png" />
}

describe('test harness defenses', () => {
  it('an unhandled request fails the test', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const consoleWarn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    let rejectRequest: (reason?: unknown) => void = () => undefined
    const rejection = new Promise<never>((_, reject) => {
      rejectRequest = reject
    })

    renderWithProviders(<UnhandledRequestProbe onRejected={rejectRequest} />)

    // The error strategy stops request processing with this InternalError. A
    // warn/bypass regression either resolves or rejects with a network error.
    await expect(rejection).rejects.toThrow('Cannot bypass a request')
    expect(consoleError).toHaveBeenCalled()
    expect(consoleWarn).not.toHaveBeenCalled()
  })

  it('a fixture that violates the contract is rejected', () => {
    expect(() => ok('getClusters', { nonsense: true })).toThrow(
      /OpenAPI contract violation[\s\S]*\/ must be array/,
    )
  })

  it('a console.error fails the test', () => {
    function ConsoleErrorProbe() {
      console.error('intentional harness failure')
      return null
    }

    expect(() => renderWithProviders(<ConsoleErrorProbe />)).toThrow('Unexpected console.error')
  })

  it('leaves an interval behind in its own test', () => {
    expect(setInterval(() => undefined, 1000)).toBeDefined()
  })

  it('a leaked timer does not leak into the next test', () => {
    expect(Date.now()).toBe(NOW.getTime())
    expect(vi.getTimerCount()).toBe(0)
  })

  it('two renders do not share query cache', async () => {
    vi.useRealTimers()
    const firstCluster = { ...CLUSTER, name: 'First cluster' }
    const secondCluster = { ...CLUSTER, name: 'Second cluster' }
    server.use(sequence('getClusters', [firstCluster], [secondCluster]))

    const first = renderWithProviders(<ClusterCountProbe />)
    await waitFor(() => expect(screen.getByText('cluster First cluster')).toBeInTheDocument())
    first.unmount()

    renderWithProviders(<ClusterCountProbe />)
    await waitFor(() => expect(screen.getByText('cluster Second cluster')).toBeInTheDocument())
  })

  it('a serious a11y violation fails', async () => {
    const { container } = renderWithProviders(<SeriousA11yProbe />)
    vi.useRealTimers()

    await expect(expectNoA11yViolations(container)).rejects.toThrow(/image-alt/)
  })

  // The low-coverage defense is exercised by the Go-side coverage gate in §5.6.
})
