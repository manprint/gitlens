import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { useKeepFocus } from './useKeepFocus'

// The select moves between two places, as page controls do when a table empties.
function Moving() {
  const [empty, setEmpty] = useState(false)
  const keepFocus = useKeepFocus(empty ? 'bar' : 'table')
  const control = (
    <div className="contents" {...keepFocus}>
      <select
        aria-label="Database"
        id="moving-db"
        onChange={(event) => setEmpty(event.target.value === 'none')}
        value={empty ? 'none' : 'app'}
      >
        <option value="app">app</option>
        <option value="none">none</option>
      </select>
    </div>
  )
  return empty ? <section aria-label="bar">{control}</section> : <div>{control}</div>
}

describe('useKeepFocus', () => {
  it('FHD-APPLY-050 keeps focus on a control that moves when the table empties', () => {
    render(<Moving />)
    const select = screen.getByRole('combobox', { name: 'Database' })
    select.focus()
    fireEvent.change(select, { target: { value: 'none' } })
    const moved = screen.getByRole('combobox', { name: 'Database' })
    expect(screen.getByRole('region', { name: 'bar' })).toContainElement(moved)
    expect(moved).toHaveFocus()
  })

  it('FHD-APPLY-051 does not take focus back once it moved elsewhere', () => {
    render(
      <>
        <Moving />
        <button type="button">Elsewhere</button>
      </>,
    )
    const select = screen.getByRole('combobox', { name: 'Database' })
    select.focus()
    const elsewhere = screen.getByRole('button', { name: 'Elsewhere' })
    fireEvent.blur(select, { relatedTarget: elsewhere })
    elsewhere.focus()
    fireEvent.change(select, { target: { value: 'none' } })
    expect(elsewhere).toHaveFocus()
  })
})
