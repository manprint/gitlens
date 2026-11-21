import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { loadHelp } from '@/lib/help'
import { resetPreferences } from '@/lib/preferences'

import { renderRoutes } from '@/test/render'

import HelpPage from './HelpPage'

function renderHelp(route: string) {
  return renderRoutes(
    [
      { path: '/help', element: <HelpPage /> },
      { path: '/help/:topicId', element: <HelpPage /> },
    ],
    { route },
  )
}

afterEach(async () => {
  window.localStorage.clear()
  resetPreferences()
  await setServerLanguage(undefined)
  await refreshLocale()
})

describe('HelpPage', () => {
  it('HELP-UI-001 the home lists every chapter by group and the chapter menu', () => {
    renderHelp('/help')

    expect(screen.getByRole('heading', { level: 1, name: 'User guide' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Alerting' })).toBeInTheDocument()
    const chapters = screen.getByRole('navigation', { name: 'Chapters' })
    expect(within(chapters).getByRole('link', { name: 'Index analysis' })).toHaveAttribute(
      'href',
      '/help/indexes',
    )
    expect(within(chapters).getAllByRole('link')).toHaveLength(16)
  })

  it('HELP-UI-002 a chapter shows its contents, sections and the current menu entry', () => {
    renderHelp('/help/indexes')

    expect(screen.getByRole('heading', { level: 1, name: 'Index analysis' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'On this page' })).toBeInTheDocument()
    expect(
      within(screen.getByRole('navigation', { name: 'Chapters' })).getByRole('link', {
        name: 'Index analysis',
      }),
    ).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: 'Open Index analysis' })).toHaveAttribute(
      'href',
      '/indexes',
    )
  })

  it('HELP-UI-003 the help reads in the console language, with no language switch of its own', async () => {
    await setServerLanguage('it')
    await loadHelp('it')
    renderHelp('/help/indexes')

    expect(
      screen.getByRole('heading', { level: 1, name: 'Analisi degli indici' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Capitoli' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'In questa pagina' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Italiano' })).not.toBeInTheDocument()
    expect(screen.queryByText('EN')).not.toBeInTheDocument()
  })

  it('HELP-UI-004 the search lists matching sections and links to them', () => {
    renderHelp('/help')

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search the guide' }), {
      target: { value: 'dead rows' },
    })

    const results = screen.getByRole('region', { name: 'Search the guide' })
    expect(within(results).getByText(/sections? match/)).toBeInTheDocument()
    expect(
      within(results)
        .getAllByRole('link')
        .some((link) => link.getAttribute('href') === '/help/indexes#dead-rows'),
    ).toBe(true)
  })

  it('HELP-UI-005 an unknown chapter says so', () => {
    renderHelp('/help/no-such-chapter')

    expect(screen.getByText(/There is no help chapter with this name/)).toBeInTheDocument()
  })

  it('HELP-UI-006 a server page opens on the server opened last, or on Fleet to pick one', () => {
    const first = renderHelp('/help/locks')
    expect(screen.getByRole('link', { name: /Open Fleet and pick a server/ })).toHaveAttribute(
      'href',
      '/',
    )
    first.unmount()

    window.localStorage.setItem('pglens.lastInstance', 'db 1')
    renderHelp('/help/locks')
    expect(screen.getByRole('link', { name: 'Open Locks and activity' })).toHaveAttribute(
      'href',
      '/instances/db%201/locks',
    )
  })
})
