import { screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PSEUDO_LOCALE } from '@/i18n/locales'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences, updatePreferences } from '@/lib/preferences'
import { base as clusters } from '@/test/fixtures/getClusters'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { AppShell } from './AppShell'

/** Text that is data or a product name, never a catalog message. */
const NOT_MESSAGES = /^(pglens|pg|lens|[\w.-]*node-\d[\w.-]*|.*·.*|[\d\s:/.,–-]+|\?|g|f|q|a|s|\/)$/

/** Every visible text and accessible name in `root` that has letters and no pseudo-locale mark. */
function untranslated(root: HTMLElement): string[] {
  const texts: string[] = []
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    texts.push(node.textContent ?? '')
  }
  for (const element of root.querySelectorAll('[aria-label],[title],[placeholder]')) {
    for (const name of ['aria-label', 'title', 'placeholder']) {
      const value = element.getAttribute(name)
      if (value !== null) texts.push(value)
    }
  }
  return texts
    .map((text) => text.trim())
    .filter((text) => /[A-Za-z]/.test(text) && !NOT_MESSAGES.test(text))
    .filter((text) => !/[\u00C0-\u024F]/.test(text))
}

describe('shell i18n (plan 006 F2)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(ok('getClusters', clusters))
  })
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-020 the shell reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<AppShell />, { route: '/' })

    const nav = await screen.findByRole('navigation', { name: 'Navigazione principale' })
    for (const name of ['Flotta', 'Query più lente', 'Analisi degli indici', 'Segnalazioni']) {
      expect(within(nav).getByRole('link', { name })).toBeInTheDocument()
    }
    expect(screen.getByRole('link', { name: 'Impostazioni' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Percorso' })).toHaveTextContent(
      'Panoramica della flotta',
    )
    const preset = screen.getByRole('combobox', { name: 'Intervallo di tempo predefinito' })
    expect(within(preset).getByRole('option', { name: 'Ultima ora' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Esci' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Scorciatoie da tastiera' })).toBeInTheDocument()
  })

  it('I18N-021 no shell text bypasses the catalog (pseudo-locale)', async () => {
    updatePreferences({ language: PSEUDO_LOCALE })
    await refreshLocale()
    renderWithProviders(<AppShell />, { route: '/' })

    expect(untranslated(await screen.findByTestId('app-sidebar'))).toEqual([])
    expect(untranslated(screen.getByRole('banner'))).toEqual([])
  })
})
