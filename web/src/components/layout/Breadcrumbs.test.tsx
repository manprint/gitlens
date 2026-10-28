import { screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Breadcrumbs } from './Breadcrumbs'
import { renderWithProviders } from '@/test/render'

// The separators are icons, so each crumb is read from its own list item.
function crumbs(): string[] {
  const navigation = screen.getByRole('navigation', { name: 'Breadcrumb' })
  return within(navigation)
    .getAllByRole('listitem')
    .map((item) => item.textContent ?? '')
}

describe('Breadcrumbs', () => {
  it('WU-17 shows a malformed percent-encoded segment as written instead of throwing', () => {
    // React Router warns that the path is malformed and still matches the
    // route, so the warning is the expected outcome for this path.
    vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    renderWithProviders(<Breadcrumbs />, { route: '/instances/%E0' })

    expect(crumbs()).toEqual(['Instances', '%E0'])
  })

  it('WU-17 still decodes a well-formed segment', () => {
    renderWithProviders(<Breadcrumbs />, { route: '/instances/app%20one' })

    expect(crumbs()).toEqual(['Instances', 'app one'])
  })
})
