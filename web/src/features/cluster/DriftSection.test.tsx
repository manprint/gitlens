import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderWithProviders } from '@/test/render'

import { DriftSection } from './DriftSection'

describe('DriftSection', () => {
  it('UI-CLUS-022 no drift renders the explicit no-drift state', () => {
    renderWithProviders(<DriftSection entries={[]} />)

    expect(screen.getByText('No drift detected')).toBeInTheDocument()
    expect(screen.getByText(/match across the cluster instances/)).toBeInTheDocument()
  })

  it('UI-CLUS-023 a differing setting highlights the differing instances', () => {
    renderWithProviders(
      <DriftSection
        entries={[
          {
            name: 'max_connections',
            values: [
              { instance_id: 'primary', role: 'primary', value: '100' },
              { instance_id: 'standby-1', role: 'standby', value: '200' },
            ],
          },
        ]}
      />,
    )

    expect(screen.getByText('200')).toHaveAttribute('data-drift-different', 'true')
    expect(screen.getByText('200')).toHaveAttribute(
      'aria-label',
      'standby-1 differs from the reference',
    )
    expect(screen.getByText('100')).not.toHaveAttribute('data-drift-different')
  })

  it('UI-CLUS-027 flags the outlier whatever order the instances arrive in', () => {
    const primary = { instance_id: 'primary-01', role: 'primary', value: '100' }
    const replicas = ['replica-01', 'replica-02', 'replica-03'].map((instance_id) => ({
      instance_id,
      role: 'standby',
      value: '200',
    }))
    const entry = (values: typeof replicas) => ({ name: 'max_connections', values })
    const flaggedInstances = () =>
      screen
        .queryAllByLabelText(/differs from the reference/)
        .map((cell) => cell.getAttribute('aria-label'))

    const primaryFirst = renderWithProviders(
      <DriftSection entries={[entry([primary, ...replicas])]} />,
    )
    expect(flaggedInstances()).toEqual(['primary-01 differs from the reference'])
    primaryFirst.unmount()

    renderWithProviders(<DriftSection entries={[entry([...replicas, primary])]} />)
    expect(flaggedInstances()).toEqual(['primary-01 differs from the reference'])
  })
})
