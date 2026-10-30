import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useTheme } from '@/hooks/useTheme'
import { DEFAULT_PREFERENCES, PREFERENCES_STORAGE_KEY, readPreferences } from '@/lib/preferences'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

import { PreferencesPanel } from './PreferencesPanel'

/** The header's theme state, read through the same hook as the header toggle. */
function HeaderThemeProbe() {
  const { theme, setTheme } = useTheme()
  return (
    <button onClick={() => setTheme('system')} type="button">
      Header theme: {theme}
    </button>
  )
}

function renderPanel() {
  return renderWithProviders(
    <>
      <HeaderThemeProbe />
      <PreferencesPanel />
    </>,
  )
}

const CONTROLS = [
  { name: 'Theme', help: /The theme button in the header changes the same preference/ },
  { name: 'Default time range', help: /time range pages open with when the address names none/ },
  { name: 'Refresh interval', help: /Automatic keeps each page’s own interval/ },
  { name: 'Table page size', help: /Rows per page of the tables when the address sets no page/ },
] as const

beforeEach(() => {
  localStorage.clear()
  document.documentElement.classList.remove('dark')
})
afterEach(() => {
  localStorage.clear()
  document.documentElement.classList.remove('dark')
})

describe('PreferencesPanel', () => {
  it('PREF-008 every control is labelled, carries helper text, and the panel has no axe violations', async () => {
    const view = renderPanel()

    const section = screen.getByRole('region', { name: 'Preferences' })
    expect(section).toHaveAttribute('data-caption-id', 'settings.preferences')
    for (const { name, help } of CONTROLS) {
      const control = screen.getByRole('combobox', { name })
      expect(control).toHaveAccessibleDescription(help)
    }
    expect(screen.getByRole('combobox', { name: 'Default time range' })).toHaveValue(
      DEFAULT_PREFERENCES.defaultTimeRange,
    )
    const values = (name: string) =>
      within(screen.getByRole('combobox', { name }))
        .getAllByRole('option')
        .map((option) => (option as HTMLOptionElement).value)
    expect(values('Theme')).toEqual(['light', 'dark', 'system'])
    // 15m is deliberately not offered (P2).
    expect(values('Default time range')).toEqual(['1h', '6h', '24h', '7d'])
    expect(values('Refresh interval')).toEqual(['auto', 'off', '15s', '30s', '60s'])
    expect(values('Table page size')).toEqual(['20', '50', '100'])
    expect(screen.getByRole('button', { name: 'Reset to defaults' })).toBeDisabled()

    // axe schedules on real timers.
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })

  it('applies and stores each change at once, shares the theme with the header, and resets', () => {
    renderPanel()

    fireEvent.change(screen.getByRole('combobox', { name: 'Default time range' }), {
      target: { value: '6h' },
    })
    fireEvent.change(screen.getByRole('combobox', { name: 'Refresh interval' }), {
      target: { value: 'off' },
    })
    fireEvent.change(screen.getByRole('combobox', { name: 'Table page size' }), {
      target: { value: '50' },
    })
    fireEvent.change(screen.getByRole('combobox', { name: 'Theme' }), {
      target: { value: 'dark' },
    })
    expect(readPreferences()).toEqual({
      theme: 'dark',
      defaultTimeRange: '6h',
      refreshInterval: 'off',
      tablePageSize: 50,
    })
    expect(screen.getByRole('combobox', { name: 'Table page size' })).toHaveValue('50')
    expect(document.documentElement).toHaveClass('dark')
    expect(screen.getByRole('button', { name: 'Header theme: dark' })).toBeInTheDocument()

    // The header toggle and the panel show the same theme.
    fireEvent.click(screen.getByRole('button', { name: 'Header theme: dark' }))
    expect(screen.getByRole('combobox', { name: 'Theme' })).toHaveValue('system')

    fireEvent.click(screen.getByRole('button', { name: 'Reset to defaults' }))
    expect(readPreferences()).toEqual(DEFAULT_PREFERENCES)
    expect(JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}')).toEqual(
      DEFAULT_PREFERENCES,
    )
    expect(screen.getByRole('combobox', { name: 'Default time range' })).toHaveValue('1h')
    expect(screen.getByRole('button', { name: 'Header theme: light' })).toBeInTheDocument()
    expect(document.documentElement).not.toHaveClass('dark')
    expect(screen.getByRole('button', { name: 'Reset to defaults' })).toBeDisabled()
  })
})
