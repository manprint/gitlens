import { fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AppShell } from './AppShell'
import { Header } from './Header'
import { base as clusters } from '@/test/fixtures/getClusters'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

describe('application shell', () => {
  beforeEach(() => {
    // The shell polls the fleet on every page, so each render needs an answer.
    server.use(ok('getClusters', clusters))
  })

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

  it('WU-05 reports the shell connection as not reachable when the fleet poll fails', async () => {
    server.use(http.get('*/api/v1/clusters', () => HttpResponse.error()))
    renderWithProviders(<AppShell />, { route: '/' })

    await vi.waitFor(() =>
      expect(screen.getByRole('status', { name: 'Connection: not reachable' })).toBeInTheDocument(),
    )
    expect(screen.getByTestId('connection-indicator')).toHaveTextContent(
      'No successful response yet.',
    )
  })

  it('WU-05 reports the shell connection as reachable with the data age after a poll', async () => {
    renderWithProviders(<AppShell />, { route: '/' })

    await vi.waitFor(() =>
      expect(screen.getByRole('status', { name: /Data freshness: Updated/ })).toBeInTheDocument(),
    )
    expect(screen.getByRole('status', { name: 'Connection: reachable' })).toBeInTheDocument()
  })

  it('WU-20 says the session may still be active when sign-out fails', async () => {
    server.use(http.delete('*/api/v1/session', () => HttpResponse.error()))
    renderWithProviders(<Header />, { route: '/' })

    fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))

    await vi.waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('the server session may still be active'),
    )
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeEnabled()
  })

  it('UI-SHELL-021 distinguishes browser offline from an unreachable server', () => {
    renderWithProviders(
      <Header connection={{ lastPollFailed: true, lastSuccessAt: Date.now() - 120_000 }} />,
    )

    fireEvent(window, new Event('offline'))
    expect(
      screen.getByRole('status', { name: 'Connection: browser is offline' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('connection-indicator')).toHaveTextContent('Browser is offline.')

    fireEvent(window, new Event('online'))
    expect(screen.getByRole('status', { name: 'Connection: not reachable' })).toBeInTheDocument()
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
