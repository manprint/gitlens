import { useQueryClient } from '@tanstack/react-query'
import { screen } from '@testing-library/react'
import { useLocation } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { renderRoute, renderWithProviders } from './render'

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
  it('mounts the application route registry', () => {
    renderRoute('/')

    expect(
      screen.getByRole('heading', { name: 'Interface under construction' }),
    ).toBeInTheDocument()
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
