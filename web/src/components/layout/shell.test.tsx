import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AppShell } from './AppShell'
import { Header } from './Header'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

describe('application shell', () => {
  it('UI-SHELL-010 keeps the active primary destination marked', () => {
    renderWithProviders(<AppShell />, { route: '/findings' })

    expect(screen.getByRole('link', { name: 'Findings' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: 'Fleet' })).not.toHaveAttribute('aria-current')
  })

  it('UI-SHELL-011 derives breadcrumbs from the current route', () => {
    renderWithProviders(<AppShell />, { route: '/instances/node-1/ash' })

    const breadcrumbs = screen.getByRole('navigation', { name: 'Breadcrumb' })
    expect(breadcrumbs).toHaveTextContent('Instances')
    expect(breadcrumbs).toHaveTextContent('node-1')
    expect(breadcrumbs).toHaveTextContent('ASH')
    expect(within(breadcrumbs).getByText('ASH')).toHaveAttribute('aria-current', 'page')
  })

  it('UI-SHELL-012 reports an unreachable connection and the last success age', () => {
    renderWithProviders(
      <Header connection={{ lastPollFailed: true, lastSuccessAt: Date.now() - 120_000 }} />,
    )

    expect(screen.getByRole('status', { name: 'Connection: not reachable' })).toBeInTheDocument()
    expect(screen.getByTestId('connection-indicator')).toHaveTextContent('2 minutes ago')
  })

  it('UI-SHELL-013 makes skip to content the first focusable control', () => {
    renderWithProviders(<AppShell />)
    const skipLink = screen.getByRole('link', { name: 'Skip to content' })
    const focusables = screen.getAllByRole('link')

    expect(focusables[0]).toBe(skipLink)
    fireEvent.click(skipLink)
    expect(screen.getByRole('main')).toHaveFocus()
  })

  it('UI-SHELL-014 navigates to Fleet with the g f shortcut', () => {
    const result = renderWithProviders(<AppShell />, { route: '/findings' })

    fireEvent.keyDown(window, { key: 'g' })
    fireEvent.keyDown(window, { key: 'f' })

    expect(result.router.state.location.pathname).toBe('/')
  })

  it('UI-SHELL-015 disables shortcuts while a text input is focused', () => {
    function InputWithShell() {
      return (
        <>
          <input aria-label="Primary filter" />
          <AppShell />
        </>
      )
    }

    const result = renderWithProviders(<InputWithShell />, { route: '/alerts' })
    const filter = screen.getByRole('textbox', { name: 'Primary filter' })

    filter.focus()
    fireEvent.keyDown(filter, { key: 'g' })
    fireEvent.keyDown(filter, { key: 'f' })

    expect(result.router.state.location.pathname).toBe('/alerts')
  })

  it('has no serious or critical accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = renderWithProviders(<AppShell />, { route: '/findings' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
