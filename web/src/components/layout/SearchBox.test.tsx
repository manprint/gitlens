import { act, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'

import { SqlText } from './SqlText'

import {
  FULL_SEARCH_HINT,
  HighlightedText,
  SEARCH_DEBOUNCE_MS,
  SEARCH_TOO_SHORT,
  SearchBox,
} from './SearchBox'

const box = () => screen.getByRole('searchbox', { name: 'Search statements' })
const type = (text: string) => fireEvent.change(box(), { target: { value: text } })
const wait = (ms: number) => act(() => vi.advanceTimersByTimeAsync(ms))

/** A box whose committed value follows its calls, as the URL does on a page. */
function Controlled({
  onChange,
  initial = '',
}: {
  onChange: (value: string) => void
  initial?: string
}) {
  const [value, setValue] = useState(initial)
  return (
    <SearchBox
      hint={FULL_SEARCH_HINT}
      label="Search statements"
      onChange={(next) => {
        onChange(next)
        setValue(next)
      }}
      value={value}
    />
  )
}

describe('SearchBox', () => {
  it('SRCH-UI-004 debounces: two quick inputs give one call with the last value after 300 ms', async () => {
    const onChange = vi.fn()
    render(<Controlled onChange={onChange} />)

    type('res')
    await wait(100)
    type('res_partner')
    await wait(SEARCH_DEBOUNCE_MS - 1)
    expect(onChange).not.toHaveBeenCalled()
    await wait(1)
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenCalledWith('res_partner')
    expect(box()).toHaveValue('res_partner')

    // The same term again (only white space added) is not a new search.
    type('res_partner ')
    await wait(SEARCH_DEBOUNCE_MS)
    expect(onChange).toHaveBeenCalledTimes(1)
  })

  it('SRCH-UI-005 shows the hint under 2 characters and does not search', async () => {
    const onChange = vi.fn()
    render(<Controlled onChange={onChange} />)
    const search = screen.getByRole('search', { name: 'Search statements' })

    expect(box()).toHaveAccessibleDescription(FULL_SEARCH_HINT)
    type('r')
    await wait(SEARCH_DEBOUNCE_MS * 2)
    expect(onChange).not.toHaveBeenCalled()
    expect(search).toHaveTextContent(SEARCH_TOO_SHORT)
    expect(box()).toHaveAccessibleDescription(SEARCH_TOO_SHORT)
    // White space does not count.
    type(' r ')
    await wait(SEARCH_DEBOUNCE_MS * 2)
    expect(onChange).not.toHaveBeenCalled()

    type('re')
    expect(search).not.toHaveTextContent(SEARCH_TOO_SHORT)
    await wait(SEARCH_DEBOUNCE_MS)
    expect(onChange).toHaveBeenCalledWith('re')
  })

  it('SRCH-UI-006 the clear button empties the box and the search at once', async () => {
    const onChange = vi.fn()
    render(<Controlled initial="orders" onChange={onChange} />)
    expect(box()).toHaveValue('orders')

    fireEvent.click(screen.getByRole('button', { name: 'Clear search' }))
    expect(onChange).toHaveBeenCalledWith('')
    expect(box()).toHaveValue('')
    expect(box()).toHaveFocus()
    expect(screen.queryByRole('button', { name: 'Clear search' })).not.toBeInTheDocument()
    await wait(SEARCH_DEBOUNCE_MS)
    expect(onChange).toHaveBeenCalledTimes(1)

    // Emptying the box by hand searches for nothing after the wait.
    type('ab')
    await wait(SEARCH_DEBOUNCE_MS)
    type('')
    await wait(SEARCH_DEBOUNCE_MS)
    expect(onChange).toHaveBeenLastCalledWith('')
  })

  it('takes a new value from outside and marks a running search busy', () => {
    const { rerender } = render(
      <SearchBox busy label="Search statements" onChange={vi.fn()} value="orders" />,
    )
    expect(screen.getByRole('search')).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByTestId('search-busy')).toBeInTheDocument()

    rerender(<SearchBox label="Search statements" onChange={vi.fn()} value="accounts" />)
    expect(box()).toHaveValue('accounts')
    expect(screen.getByRole('search')).toHaveAttribute('aria-busy', 'false')
    expect(screen.queryByTestId('search-busy')).not.toBeInTheDocument()
  })

  it('hides the hint line from 1600 px but keeps it as the description and tooltip (D26)', () => {
    render(<Controlled onChange={vi.fn()} />)
    expect(screen.getByText(FULL_SEARCH_HINT)).toHaveClass('min-[1600px]:sr-only')
    expect(box()).toHaveAttribute('title', FULL_SEARCH_HINT)
  })

  it('keeps newer typing when the URL takes the term the box passed on, and follows any other value', async () => {
    const onChange = vi.fn()
    const { rerender } = render(
      <SearchBox label="Search statements" onChange={onChange} value="" />,
    )
    type('ab')
    await wait(SEARCH_DEBOUNCE_MS)
    expect(onChange).toHaveBeenLastCalledWith('ab')
    type('abcd')
    // The URL takes `ab` after the user typed on: the newer text stays.
    rerender(<SearchBox label="Search statements" onChange={onChange} value="ab" />)
    expect(box()).toHaveValue('abcd')
    await wait(SEARCH_DEBOUNCE_MS)
    expect(onChange).toHaveBeenLastCalledWith('abcd')
    // A value the box did not pass on (a clear action, the back button) replaces the text.
    rerender(<SearchBox label="Search statements" onChange={onChange} value="xy" />)
    expect(box()).toHaveValue('xy')
  })

  it('keeps newer typing while the URL takes two terms the box passed on, oldest first', async () => {
    const onChange = vi.fn()
    const { rerender } = render(
      <SearchBox label="Search statements" onChange={onChange} value="" />,
    )
    type('ab')
    await wait(SEARCH_DEBOUNCE_MS)
    type('abc')
    await wait(SEARCH_DEBOUNCE_MS)
    expect(onChange.mock.calls.map(([value]) => value as string)).toEqual(['ab', 'abc'])
    type('abcd')
    rerender(<SearchBox label="Search statements" onChange={onChange} value="ab" />)
    expect(box()).toHaveValue('abcd')
    rerender(<SearchBox label="Search statements" onChange={onChange} value="abc" />)
    expect(box()).toHaveValue('abcd')
    // Both are settled: `ab` again is a new value from outside (the back button).
    rerender(<SearchBox label="Search statements" onChange={onChange} value="ab" />)
    expect(box()).toHaveValue('ab')
  })

  it('takes at most 200 characters and announces the hints politely', () => {
    render(<Controlled onChange={vi.fn()} />)
    expect(box()).toHaveAttribute('maxlength', '200')
    expect(screen.getByText(FULL_SEARCH_HINT)).toHaveAttribute('aria-live', 'polite')
    type('a')
    const hint = screen.getByText(SEARCH_TOO_SHORT)
    expect(hint).toHaveAttribute('aria-live', 'polite')
    // From 1600 px it floats under the box on one line, so the toolbar keeps its height (D26).
    expect(hint).toHaveClass('min-[1600px]:absolute', 'min-[1600px]:whitespace-nowrap')
  })

  it('has no serious accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render(<Controlled initial="orders" onChange={vi.fn()} />)
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})

describe('HighlightedText', () => {
  it('puts each match in a mark with the original case', () => {
    const { container } = render(
      <p>
        <HighlightedText q="RES" text="Select * From res_partner, Res_users" />
      </p>,
    )
    expect(screen.getAllByRole('mark').map((mark) => mark.textContent)).toEqual(['res', 'Res'])
    expect(container).toHaveTextContent('Select * From res_partner, Res_users')
  })
})

describe('SqlText with a search term', () => {
  it('marks a match that spans tokens and keeps the whole statement', () => {
    const { container } = render(<SqlText highlight="from RES" sql="SELECT * FROM res_partner" />)
    expect(
      screen
        .getAllByRole('mark')
        .map((mark) => mark.textContent)
        .join(''),
    ).toBe('FROM res')
    expect(container).toHaveTextContent('SELECT * FROM res_partner')
  })

  it('marks nothing without a term', () => {
    render(<SqlText sql="SELECT 1" />)
    expect(screen.queryByRole('mark')).not.toBeInTheDocument()
  })
})
