import { act, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderWithProviders } from '@/test/render'

import {
  readTableState,
  useLocalTableState,
  useTableState,
  type TableDefaults,
  type TableStateControls,
} from './useTableState'

let controls: TableStateControls | undefined

const keep = (value: TableStateControls) => {
  controls = value
}

function Probe({ prefix = 't', defaults }: { prefix?: string; defaults?: TableDefaults }) {
  const value = useTableState(prefix, defaults)
  keep(value)
  return <output data-testid="state">{JSON.stringify(value.state)}</output>
}

function LocalProbe({ defaults }: { defaults?: TableDefaults }) {
  const value = useLocalTableState(defaults)
  keep(value)
  return <output data-testid="state">{JSON.stringify(value.state)}</output>
}

function current(): TableStateControls {
  if (!controls) throw new Error('probe not rendered')
  return controls
}

describe('useTableState', () => {
  it('TSTATE-001 starts from the defaults and writes nothing to the URL', () => {
    const { router } = renderWithProviders(<Probe defaults={{ sort: 'size', dir: 'desc' }} />, {
      route: '/tables?range=1h',
    })

    expect(current().state).toEqual({
      q: '',
      sort: 'size',
      dir: 'desc',
      page: 1,
      size: 20,
      filters: {},
    })
    expect(current().activeFilterCount).toBe(0)
    expect(router.state.location.search).toBe('?range=1h')
  })

  it('TSTATE-002 writes the search with replace navigation and returns to page 1', () => {
    const { router } = renderWithProviders(<Probe />, { route: '/tables?t.page=3&range=1h' })
    expect(current().state.page).toBe(3)

    act(() => current().set({ q: 'orders' }))

    expect(router.state.historyAction).toBe('REPLACE')
    expect(router.state.location.search).toBe('?range=1h&t.q=orders')
    expect(current().state).toMatchObject({ q: 'orders', page: 1 })
    expect(current().activeFilterCount).toBe(1)

    act(() => current().set({ page: 2 }))
    expect(router.state.location.search).toBe('?range=1h&t.q=orders&t.page=2')
    act(() => current().set({ filters: { kind: 'table' }, sort: 'name' }))
    expect(router.state.location.search).toBe(
      '?range=1h&t.q=orders&t.sort=name&t.dir=asc&t.f.kind=table',
    )
    expect(current().activeFilterCount).toBe(2)
    // Choosing the default again removes the key.
    act(() => current().set({ sort: null, size: 20 }))
    expect(router.state.location.search).toBe('?range=1h&t.q=orders&t.f.kind=table')
  })

  it('TSTATE-003 falls back to the defaults for invalid values', () => {
    const state = readTableState(
      new URLSearchParams('t.dir=sideways&t.size=7&t.page=-2&t.sort=&t.f.kind='),
      't',
      { sort: 'name', dir: 'desc', size: 50 },
    )
    expect(state).toEqual({ q: '', sort: 'name', dir: 'desc', page: 1, size: 50, filters: {} })
    expect(readTableState(new URLSearchParams('t.page=2.5&t.size=100'), 't')).toMatchObject({
      page: 1,
      size: 100,
    })
  })

  it('TSTATE-004 clears the search and filters of its own prefix only', () => {
    const { router } = renderWithProviders(<Probe />, {
      route: '/x?t.q=a&t.f.kind=table&t.sort=name&t.dir=desc&t.page=2&other.q=b&other.f.kind=view',
    })

    act(() => current().clear())

    expect(router.state.location.search).toBe('?other.q=b&other.f.kind=view&t.sort=name&t.dir=desc')
    expect(current().activeFilterCount).toBe(0)
  })

  it('keeps a cleared default sort as `none`', () => {
    const { router } = renderWithProviders(<Probe defaults={{ sort: 'size', dir: 'desc' }} />, {
      route: '/x',
    })

    act(() => current().set({ sort: null }))
    expect(router.state.location.search).toBe('?t.sort=none')
    expect(current().state.sort).toBeNull()
    act(() => current().set({ sort: 'size', dir: 'desc' }))
    expect(router.state.location.search).toBe('')
  })
})

describe('useLocalTableState', () => {
  it('holds the same state without a Router', () => {
    render(<LocalProbe defaults={{ size: 50 }} />)

    act(() => current().set({ page: 2 }))
    act(() => current().set({ filters: { kind: 'index' } }))

    expect(JSON.parse(screen.getByTestId('state').textContent ?? '')).toEqual({
      q: '',
      sort: null,
      dir: 'asc',
      page: 1,
      size: 50,
      filters: { kind: 'index' },
    })
    act(() => current().clear())
    expect(current().activeFilterCount).toBe(0)
  })
})
