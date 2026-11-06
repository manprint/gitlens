import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { StandbyStatsNote } from './StandbyStatsNote'

import { isStandby, STANDBY_STATS_NOTE } from '@/lib/standby'

describe('StandbyStatsNote', () => {
  it('HA-V-10-1 a standby gets the note, any other role nothing', () => {
    expect(isStandby('standby')).toBe(true)
    for (const role of ['primary', 'unknown', null, undefined]) expect(isStandby(role)).toBe(false)

    const { rerender } = render(<StandbyStatsNote role="standby" />)
    const note = screen.getByRole('note')
    expect(note).toHaveTextContent(STANDBY_STATS_NOTE)
    expect(note).toHaveTextContent(/statistics are this standby's own/i)
    expect(note).toHaveAttribute('title', STANDBY_STATS_NOTE)

    for (const role of ['primary', null, undefined]) {
      rerender(<StandbyStatsNote role={role} />)
      expect(screen.queryByRole('note')).not.toBeInTheDocument()
    }
  })
})
