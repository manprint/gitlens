import { useQueryClient } from '@tanstack/react-query'
import { act, screen } from '@testing-library/react'
import { useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { renderRoute, renderWithProviders } from './render'
import { base as session } from './fixtures/getSession'
import { ok } from './msw/handlers'
import { server } from './msw/server'

function ProviderProbe() {
  const queryClient = useQueryClient()
  return <p>query client {queryClient ? 'ready' : 'missing'}</p>
}

function LocationProbe() {
  const location = useLocation()
  return <p>path {location.pathname}</p>
}

function ConsoleErrorProbe() {
  console.error('intentional harness failure')
  return null
}

describe('renderWithProviders', () => {
  it('mounts the application route registry', async () => {
    server.use(ok('getSession', session))
    renderRoute('/')
    await act(async () => {
      await import('../routes/pages')
      await vi.advanceTimersByTimeAsync(1)
      for (let index = 0; index < 10; index += 1) {
        await Promise.resolve()
      }
    })

    expect(screen.getByRole('heading', { name: 'Fleet overview' })).toBeInTheDocument()
    expect(screen.getByText(/Build dev/)).toBeInTheDocument()
  })

  it('renders a component with providers', () => {
    renderWithProviders(<ProviderProbe />)

    expect(screen.getByText('query client ready')).toBeInTheDocument()
  })

  it('gives each render a fresh QueryClient', () => {
    const first = renderWithProviders(<ProviderProbe />)
    first.queryClient.setQueryData(['isolation'], 'cached')

    const second = renderWithProviders(<ProviderProbe />)

    expect(second.queryClient).not.toBe(first.queryClient)
    expect(second.queryClient.getQueryData(['isolation'])).toBeUndefined()
  })

  it('seeds the router at the requested route', () => {
    const result = renderWithProviders(<LocationProbe />, { route: '/clusters' })

    expect(result.router.state.location.pathname).toBe('/clusters')
    expect(screen.getByText('path /clusters')).toBeInTheDocument()
  })

  it('fails the test when the component logs a console.error', () => {
    expect(() => renderWithProviders(<ConsoleErrorProbe />)).toThrow('Unexpected console.error')
  })
})
