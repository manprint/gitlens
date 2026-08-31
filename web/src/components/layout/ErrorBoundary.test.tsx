import { screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ErrorBoundary } from './ErrorBoundary'
import { renderWithProviders } from '@/test/render'

function BrokenPage(): never {
  throw new Error('render exploded')
}

describe('ErrorBoundary', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('UI-SHELL-020 renders an ErrorState, build id, and reload action', () => {
    vi.spyOn(console, 'error').mockImplementation((...args) => {
      void args
    })

    renderWithProviders(
      <ErrorBoundary>
        <BrokenPage />
      </ErrorBoundary>,
    )

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load pglens.')
    expect(screen.getByText(`Build ${__PGLENS_BUILD__}`)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry pglens' })).toBeInTheDocument()
  })

  it('renders children while no error has occurred', () => {
    renderWithProviders(
      <ErrorBoundary>
        <p>Healthy application</p>
      </ErrorBoundary>,
    )

    expect(screen.getByText('Healthy application')).toBeInTheDocument()
  })
})
