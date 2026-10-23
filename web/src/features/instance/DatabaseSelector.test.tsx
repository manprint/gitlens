import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderWithProviders } from '@/test/render'

import { DatabaseSelector } from './DatabaseSelector'

describe('DatabaseSelector', () => {
  it('UI-INST-048 links the README guidance as a page load, not a client route', () => {
    renderWithProviders(<DatabaseSelector databases={[]} notMonitoredCount={2} />, {
      route: '/instances/instance-a?range=1h',
    })
    const link = screen.getByRole('link', { name: 'databases.max' })
    expect(link).toHaveAttribute('href', '/README.md#agent-configuration')

    // By the time the event reaches window, React's root has run. A router Link
    // prevents the default action there, and a plain anchor leaves it to the
    // browser. Preventing it here keeps jsdom from reporting an unimplemented
    // navigation, which the test setup turns into a failure.
    let defaultPrevented: boolean | undefined
    const record = (event: Event) => {
      defaultPrevented = event.defaultPrevented
      event.preventDefault()
    }
    window.addEventListener('click', record)
    try {
      fireEvent.click(link)
    } finally {
      window.removeEventListener('click', record)
    }

    expect(defaultPrevented).toBe(false)
  })
})
