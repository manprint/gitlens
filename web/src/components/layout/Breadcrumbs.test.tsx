import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Breadcrumbs } from './Breadcrumbs'
import { renderWithProviders } from '@/test/render'

describe('Breadcrumbs', () => {
  it('WU-17 shows a malformed percent-encoded segment as written instead of throwing', () => {
    // React Router warns that the path is malformed and still matches the
    // route, so the warning is the expected outcome for this path.
    vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    renderWithProviders(<Breadcrumbs />, { route: '/instances/%E0' })

    expect(screen.getByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent(
      'Instances/%E0',
    )
  })

  it('WU-17 still decodes a well-formed segment', () => {
    renderWithProviders(<Breadcrumbs />, { route: '/instances/app%20one' })

    expect(screen.getByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent(
      'Instances/app one',
    )
  })
})
