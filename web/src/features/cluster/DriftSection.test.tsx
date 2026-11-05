import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, searchTable, seq, sortTable } from '@/test/table'

import { DriftSection } from './DriftSection'

describe('DriftSection', () => {
  it('UI-CLUS-022 no drift renders the explicit no-drift state', () => {
    renderWithProviders(<DriftSection entries={[]} />)

    expect(screen.getByText('No drift detected')).toBeInTheDocument()
    expect(
      screen.getByText(/No setting differs between the instances of this cluster/),
    ).toBeInTheDocument()
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

  it('UI-CLUS-028 breaks a tie between two replica values the same way in any order', () => {
    const replica = (instance_id: string, value: string) => ({
      instance_id,
      role: 'standby',
      value,
    })
    const values = [
      { instance_id: 'primary-01', role: 'primary', value: '100' },
      replica('replica-01', '150'),
      replica('replica-02', '150'),
      replica('replica-03', '200'),
      replica('replica-04', '200'),
    ]
    const flaggedInstances = () =>
      screen
        .queryAllByLabelText(/differs from the reference/)
        .map((cell) => cell.getAttribute('aria-label'))
    const expected = [
      'primary-01 differs from the reference',
      'replica-03 differs from the reference',
      'replica-04 differs from the reference',
    ]

    const inOrder = renderWithProviders(
      <DriftSection entries={[{ name: 'max_connections', values }]} />,
    )
    expect(flaggedInstances()).toEqual(expected)
    inOrder.unmount()

    renderWithProviders(
      <DriftSection entries={[{ name: 'max_connections', values: [...values].reverse() }]} />,
    )
    expect(flaggedInstances()).toEqual(expected)
  })

  it('TBL-UI-010 configuration-drift pages, searches and sorts the settings', () => {
    const entries = seq(25).map((n) => ({
      name: `setting_${n}`,
      values: [
        { instance_id: 'primary', role: 'primary', value: '1' },
        { instance_id: 'standby-1', role: 'standby', value: '1' },
      ],
    }))
    const { router } = renderWithProviders(<DriftSection entries={entries} />)
    const name = 'Configuration drift'

    expect(dataRows(name)).toHaveLength(20)
    searchTable(name, 'setting_2')
    expect(columnTexts(name)).toEqual(
      seq(25)
        .slice(19)
        .map((n) => `setting_${n}`),
    )
    sortTable(name, 'Setting', 'descending')
    expect(columnTexts(name)[0]).toBe('setting_25')
    expect(router.state.location.search).toContain('configuration-drift.q=setting_2')
  })
})
